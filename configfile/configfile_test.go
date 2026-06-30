package configfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	testEmbeddedHostName        = "CONFIGFILE_TEST_HOST"
	testMissingEnvironmentNameA = "CONFIGFILE_TEST_MISSING_A"
	testMissingEnvironmentNameB = "CONFIGFILE_TEST_MISSING_B"
	testScalarBooleanName       = "CONFIGFILE_TEST_BOOL"
	testScalarEmptyName         = "CONFIGFILE_TEST_EMPTY"
	testScalarFloatName         = "CONFIGFILE_TEST_FLOAT"
	testScalarIntegerName       = "CONFIGFILE_TEST_INTEGER"
	testScalarStringName        = "CONFIGFILE_TEST_STRING"
)

const (
	testContractExtraName    = "CONFIGFILE_TEST_CONTRACT_EXTRA"
	testContractOptionalName = "CONFIGFILE_TEST_CONTRACT_OPTIONAL"
	testContractRequiredName = "CONFIGFILE_TEST_CONTRACT_REQUIRED"
)

type configFileFixture struct {
	Server configFileServerFixture `yaml:"server"`
}

type configFileServerFixture struct {
	Enabled     bool     `yaml:"enabled"`
	HostURL     string   `yaml:"host_url"`
	LiteralCost string   `yaml:"literal_cost"`
	MaxWorkers  int      `yaml:"max_workers"`
	EmptyValue  *string  `yaml:"empty_value"`
	Ratio       float64  `yaml:"ratio"`
	Secret      string   `yaml:"secret"`
	Names       []string `yaml:"names"`
}

type configFileContractFixture struct {
	Service configFileContractServiceFixture `yaml:"service"`
}

type configFileContractServiceFixture struct {
	Enabled bool   `yaml:"enabled"`
	HostURL string `yaml:"host_url"`
	Secret  string `yaml:"secret"`
	APIKey  string `yaml:"api_key"`
	Label   string `yaml:"label"`
}

func TestLoadYAMLReadsFileAndInterpolatesScalars(testingHandle *testing.T) {
	testingHandle.Setenv(testScalarBooleanName, "true")
	testingHandle.Setenv(testEmbeddedHostName, "config.example.com")
	testingHandle.Setenv(testScalarIntegerName, "12")
	testingHandle.Setenv(testScalarFloatName, "1.25")
	testingHandle.Setenv(testScalarStringName, "secret-value")
	testingHandle.Setenv(testScalarEmptyName, "")

	configPath := writeConfigFile(testingHandle, strings.TrimSpace(`
server:
  enabled: $CONFIGFILE_TEST_BOOL
  host_url: "https://${CONFIGFILE_TEST_HOST}/api"
  literal_cost: "cost is $20"
  max_workers: "${CONFIGFILE_TEST_INTEGER}"
  empty_value: $CONFIGFILE_TEST_EMPTY
  ratio: $CONFIGFILE_TEST_FLOAT
  secret: "${CONFIGFILE_TEST_STRING}"
  names:
    - "$CONFIGFILE_TEST_STRING"
`))

	var decoded configFileFixture
	loadError := LoadYAML(configPath, &decoded)
	if loadError != nil {
		testingHandle.Fatalf("LoadYAML returned error: %v", loadError)
	}

	if !decoded.Server.Enabled {
		testingHandle.Fatal("expected enabled to be true")
	}
	if decoded.Server.HostURL != "https://config.example.com/api" {
		testingHandle.Fatalf("unexpected host URL: %s", decoded.Server.HostURL)
	}
	if decoded.Server.LiteralCost != "cost is $20" {
		testingHandle.Fatalf("unexpected literal cost: %s", decoded.Server.LiteralCost)
	}
	if decoded.Server.MaxWorkers != 12 {
		testingHandle.Fatalf("unexpected max workers: %d", decoded.Server.MaxWorkers)
	}
	if decoded.Server.EmptyValue != nil {
		testingHandle.Fatalf("expected empty whole environment reference to decode as nil, got %#v", decoded.Server.EmptyValue)
	}
	if decoded.Server.Ratio != 1.25 {
		testingHandle.Fatalf("unexpected ratio: %f", decoded.Server.Ratio)
	}
	if decoded.Server.Secret != "secret-value" {
		testingHandle.Fatalf("unexpected secret: %s", decoded.Server.Secret)
	}
	if !reflect.DeepEqual(decoded.Server.Names, []string{"secret-value"}) {
		testingHandle.Fatalf("unexpected names: %#v", decoded.Server.Names)
	}
}

func TestEnvContractRegistryForYAMLExposesMandatoryReferences(testingHandle *testing.T) {
	booleanSchema := mustTestSchema(testingHandle, "bool", func(value string) error {
		normalizedValue := strings.ToLower(strings.TrimSpace(value))
		if normalizedValue != "true" && normalizedValue != "false" {
			return errors.New("expected boolean")
		}
		return nil
	})
	requiredFlag := mustTestRequirement(testingHandle, true, testScalarBooleanName, booleanSchema)
	optionalSecret := mustTestRequirement(testingHandle, false, testContractOptionalName, nil)
	extraRequired := mustTestRequirement(testingHandle, true, testContractExtraName, nil)
	contract, contractError := NewEnvContract([]EnvRequirement{requiredFlag, optionalSecret, extraRequired})
	if contractError != nil {
		testingHandle.Fatalf("NewEnvContract returned error: %v", contractError)
	}

	registry, registryError := contract.RegistryForYAML([]byte(strings.TrimSpace(`
service:
  enabled: ${CONFIGFILE_TEST_BOOL}
  host_url: "https://${CONFIGFILE_TEST_HOST}/api"
  secret: ${CONFIGFILE_TEST_CONTRACT_REQUIRED}
  api_key: ${CONFIGFILE_TEST_CONTRACT_OPTIONAL}
`)))
	if registryError != nil {
		testingHandle.Fatalf("RegistryForYAML returned error: %v", registryError)
	}

	expectedMandatory := []string{
		testScalarBooleanName,
		testContractExtraName,
		testContractRequiredName,
		testEmbeddedHostName,
	}
	if mandatoryNames := requirementNames(registry.Mandatory()); !reflect.DeepEqual(mandatoryNames, expectedMandatory) {
		testingHandle.Fatalf("expected mandatory names %#v, got %#v", expectedMandatory, mandatoryNames)
	}
	if requirements := registry.Requirements(); len(requirements) != 5 {
		testingHandle.Fatalf("expected 5 registry requirements, got %#v", requirements)
	}
	if paths := registry.ReferencePaths(testEmbeddedHostName); !reflect.DeepEqual(paths, []string{"service.host_url"}) {
		testingHandle.Fatalf("unexpected host reference paths: %#v", paths)
	}
	if paths := registry.ReferencePaths(testContractExtraName); len(paths) != 0 {
		testingHandle.Fatalf("expected extra requirement without reference paths, got %#v", paths)
	}
	if optionalRequirement, found := registry.Requirement(testContractOptionalName); !found || optionalRequirement.Required() {
		testingHandle.Fatalf("expected optional requirement, found=%v requirement=%#v", found, optionalRequirement)
	}
	if flagRequirement, found := contract.Requirement(testScalarBooleanName); !found || flagRequirement.SchemaName() != "bool" {
		testingHandle.Fatalf("expected bool schema requirement, found=%v requirement=%#v", found, flagRequirement)
	}
}

func TestLoadYAMLWithOptionsValidatesRequiredEnvironmentBeforeDecode(testingHandle *testing.T) {
	booleanSchema := mustTestSchema(testingHandle, "bool", func(value string) error {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "false":
			return nil
		default:
			return errors.New("expected boolean")
		}
	})
	requiredSecret := mustTestRequirement(testingHandle, true, testContractRequiredName, nil)
	requiredFlag := mustTestRequirement(testingHandle, true, testScalarBooleanName, booleanSchema)
	requiredExtra := mustTestRequirement(testingHandle, true, testContractExtraName, nil)
	registry, registryError := NewEnvRegistry([]EnvRequirement{requiredSecret, requiredFlag, requiredExtra})
	if registryError != nil {
		testingHandle.Fatalf("NewEnvRegistry returned error: %v", registryError)
	}

	var decoded configFileContractFixture
	loadError := LoadYAMLBytesWithOptions(
		[]byte("service:\n  unknown: true\n"),
		&decoded,
		EnvironmentOptions{
			Lookup: mapEnvironmentLookup(map[string]string{
				testContractRequiredName: "",
				testScalarBooleanName:    "not-bool",
			}),
			Registry: registry,
		},
	)
	if !errors.Is(loadError, ErrEnvironmentValidation) {
		testingHandle.Fatalf("expected ErrEnvironmentValidation, got %v", loadError)
	}
	if strings.Contains(loadError.Error(), "not-bool") {
		testingHandle.Fatalf("expected validation error to omit raw value, got %v", loadError)
	}

	var validationError EnvValidationError
	if !errors.As(loadError, &validationError) {
		testingHandle.Fatalf("expected EnvValidationError, got %T", loadError)
	}
	issues := validationError.Issues()
	if len(issues) != 3 {
		testingHandle.Fatalf("expected 3 validation issues, got %#v", issues)
	}
	expectedKinds := map[string]EnvValidationIssueKind{
		testContractExtraName:    EnvValidationIssueMissing,
		testContractRequiredName: EnvValidationIssueEmpty,
		testScalarBooleanName:    EnvValidationIssueInvalid,
	}
	for _, issue := range issues {
		if issue.Kind() != expectedKinds[issue.Name()] {
			testingHandle.Fatalf("unexpected issue %#v", issue)
		}
		if issue.Name() == testScalarBooleanName && (issue.SchemaName() != "bool" || issue.Detail() != "expected boolean") {
			testingHandle.Fatalf("unexpected schema issue %#v", issue)
		}
		if !issue.Required() {
			testingHandle.Fatalf("expected issue to be required: %#v", issue)
		}
	}
}

func TestLoadYAMLWithOptionsAllowsOptionalMissingValues(testingHandle *testing.T) {
	optionalAPIKey := mustTestRequirement(testingHandle, false, testContractOptionalName, nil)
	contract, contractError := NewEnvContract([]EnvRequirement{optionalAPIKey})
	if contractError != nil {
		testingHandle.Fatalf("NewEnvContract returned error: %v", contractError)
	}
	payload := []byte(strings.TrimSpace(`
service:
  enabled: true
  host_url: "https://example.invalid"
  secret: "available"
  api_key: ${CONFIGFILE_TEST_CONTRACT_OPTIONAL}
  label: "prefix-${CONFIGFILE_TEST_CONTRACT_OPTIONAL}"
`))
	registry, registryError := contract.RegistryForYAML(payload)
	if registryError != nil {
		testingHandle.Fatalf("RegistryForYAML returned error: %v", registryError)
	}

	var decoded configFileContractFixture
	loadError := LoadYAMLBytesWithOptions(payload, &decoded, EnvironmentOptions{
		Lookup:   mapEnvironmentLookup(nil),
		Registry: registry,
	})
	if loadError != nil {
		testingHandle.Fatalf("LoadYAMLBytesWithOptions returned error: %v", loadError)
	}
	if decoded.Service.APIKey != "" {
		testingHandle.Fatalf("expected optional API key to decode empty, got %q", decoded.Service.APIKey)
	}
	if decoded.Service.Label != "prefix-" {
		testingHandle.Fatalf("expected inline optional reference to decode prefix-, got %q", decoded.Service.Label)
	}
}

func TestEnvRegistryValidatesOptionalSchemaOnlyWhenValueIsPresent(testingHandle *testing.T) {
	prefixSchema := mustTestSchema(testingHandle, "prefix", func(value string) error {
		if !strings.HasPrefix(value, "ok-") {
			return errors.New("expected ok prefix")
		}
		return nil
	})
	optionalValue := mustTestRequirement(testingHandle, false, testContractOptionalName, prefixSchema)
	registry, registryError := NewEnvRegistry([]EnvRequirement{optionalValue})
	if registryError != nil {
		testingHandle.Fatalf("NewEnvRegistry returned error: %v", registryError)
	}
	testingHandle.Setenv(testContractOptionalName, "ok-os")

	for _, lookup := range []EnvironmentLookup{
		mapEnvironmentLookup(nil),
		mapEnvironmentLookup(map[string]string{testContractOptionalName: ""}),
		mapEnvironmentLookup(map[string]string{testContractOptionalName: "ok-value"}),
		nil,
	} {
		if validateError := registry.Validate(lookup); validateError != nil {
			testingHandle.Fatalf("expected optional validation success, got %v", validateError)
		}
	}

	validateError := registry.Validate(mapEnvironmentLookup(map[string]string{testContractOptionalName: "bad"}))
	if !errors.Is(validateError, ErrEnvironmentValidation) {
		testingHandle.Fatalf("expected optional schema validation error, got %v", validateError)
	}
	var validationError EnvValidationError
	if !errors.As(validateError, &validationError) {
		testingHandle.Fatalf("expected EnvValidationError, got %T", validateError)
	}
	issues := validationError.Issues()
	if len(issues) != 1 || issues[0].Required() || issues[0].Kind() != EnvValidationIssueInvalid {
		testingHandle.Fatalf("unexpected optional schema issue: %#v", issues)
	}
}

func TestEnvironmentContractRejectsInvalidDeclarations(testingHandle *testing.T) {
	if _, schemaError := NewEnvValueSchema(" ", func(string) error { return nil }); !errors.Is(schemaError, ErrInvalidEnvironmentRequirement) {
		testingHandle.Fatalf("expected schema name error, got %v", schemaError)
	}
	if _, schemaError := NewEnvValueSchema("schema", nil); !errors.Is(schemaError, ErrInvalidEnvironmentRequirement) {
		testingHandle.Fatalf("expected schema validator error, got %v", schemaError)
	}
	if _, requirementError := NewRequiredEnv("bad-name", nil); !errors.Is(requirementError, ErrInvalidEnvironmentRequirement) {
		testingHandle.Fatalf("expected invalid env name error, got %v", requirementError)
	}

	duplicateRequirement := mustTestRequirement(testingHandle, true, testContractRequiredName, nil)
	if _, contractError := NewEnvContract([]EnvRequirement{duplicateRequirement, duplicateRequirement}); !errors.Is(contractError, ErrInvalidEnvironmentRequirement) {
		testingHandle.Fatalf("expected duplicate contract error, got %v", contractError)
	}
	if _, contractError := NewEnvContract([]EnvRequirement{{}}); !errors.Is(contractError, ErrInvalidEnvironmentRequirement) {
		testingHandle.Fatalf("expected empty contract requirement error, got %v", contractError)
	}
	if _, registryError := NewEnvRegistry([]EnvRequirement{duplicateRequirement, duplicateRequirement}); !errors.Is(registryError, ErrInvalidEnvironmentRequirement) {
		testingHandle.Fatalf("expected duplicate registry error, got %v", registryError)
	}
	if _, registryError := NewEnvRegistry([]EnvRequirement{{}}); !errors.Is(registryError, ErrInvalidEnvironmentRequirement) {
		testingHandle.Fatalf("expected empty registry requirement error, got %v", registryError)
	}

	contract, contractError := NewEnvContract(nil)
	if contractError != nil {
		testingHandle.Fatalf("NewEnvContract(nil) returned error: %v", contractError)
	}
	_, registryError := contract.RegistryForYAML([]byte("service:\n  secret: ${CONFIGFILE_TEST_MISSING:-default}\n"))
	if !errors.Is(registryError, ErrInvalidEnvironmentReference) {
		testingHandle.Fatalf("expected invalid reference error, got %v", registryError)
	}

	if referenceError := collectEnvironmentReferences(nil, "", map[string]map[string]struct{}{}); referenceError != nil {
		testingHandle.Fatalf("expected nil node to be ignored, got %v", referenceError)
	}
	unknownKindError := EnvValidationError{issues: []EnvValidationIssue{{name: "CONFIGFILE_TEST_UNKNOWN", kind: EnvValidationIssueKind("unknown")}}}
	if !strings.Contains(unknownKindError.Error(), "CONFIGFILE_TEST_UNKNOWN invalid") {
		testingHandle.Fatalf("expected unknown issue fallback, got %v", unknownKindError)
	}
}

func TestEnvContractCoversSequencePathsAndNoopBranches(testingHandle *testing.T) {
	contract, contractError := NewEnvContract(nil)
	if contractError != nil {
		testingHandle.Fatalf("NewEnvContract returned error: %v", contractError)
	}
	if _, found := contract.Requirement(testContractRequiredName); found {
		testingHandle.Fatal("expected empty contract lookup to miss")
	}
	if _, registryError := contract.RegistryForYAML([]byte("service: [")); !errors.Is(registryError, ErrParse) {
		testingHandle.Fatalf("expected parse error, got %v", registryError)
	}

	registry, registryError := contract.RegistryForYAML([]byte(strings.TrimSpace(`
- ${CONFIGFILE_TEST_CONTRACT_REQUIRED}
- name: ${CONFIGFILE_TEST_BOOL}
- "cost is $20"
`)))
	if registryError != nil {
		testingHandle.Fatalf("RegistryForYAML returned error: %v", registryError)
	}
	if paths := registry.ReferencePaths(testContractRequiredName); !reflect.DeepEqual(paths, []string{"[0]"}) {
		testingHandle.Fatalf("unexpected root sequence paths: %#v", paths)
	}
	if paths := registry.ReferencePaths(testScalarBooleanName); !reflect.DeepEqual(paths, []string{"[1].name"}) {
		testingHandle.Fatalf("unexpected nested sequence paths: %#v", paths)
	}
	nestedRegistry, nestedRegistryError := contract.RegistryForYAML([]byte(strings.TrimSpace(`
services:
  - secret: ${CONFIGFILE_TEST_CONTRACT_EXTRA}
`)))
	if nestedRegistryError != nil {
		testingHandle.Fatalf("nested RegistryForYAML returned error: %v", nestedRegistryError)
	}
	if paths := nestedRegistry.ReferencePaths(testContractExtraName); !reflect.DeepEqual(paths, []string{"services[0].secret"}) {
		testingHandle.Fatalf("unexpected nested service paths: %#v", paths)
	}
	rootScalarRegistry, rootScalarRegistryError := contract.RegistryForYAML([]byte("${CONFIGFILE_TEST_STRING}\n"))
	if rootScalarRegistryError != nil {
		testingHandle.Fatalf("root scalar RegistryForYAML returned error: %v", rootScalarRegistryError)
	}
	if paths := rootScalarRegistry.ReferencePaths(testScalarStringName); !reflect.DeepEqual(paths, []string{"$"}) {
		testingHandle.Fatalf("unexpected root scalar paths: %#v", paths)
	}
	keyRegistry, keyRegistryError := contract.RegistryForYAML([]byte("\"${CONFIGFILE_TEST_CONTRACT_REQUIRED}\": plain\n"))
	if keyRegistryError != nil {
		testingHandle.Fatalf("key RegistryForYAML returned error: %v", keyRegistryError)
	}
	if paths := keyRegistry.ReferencePaths(testContractRequiredName); !reflect.DeepEqual(paths, []string{"${CONFIGFILE_TEST_CONTRACT_REQUIRED}"}) {
		testingHandle.Fatalf("unexpected key reference paths: %#v", paths)
	}
	_, keyRegistryError = contract.RegistryForYAML([]byte("\"${CONFIGFILE_TEST_MISSING:-default}\": plain\n"))
	if !errors.Is(keyRegistryError, ErrInvalidEnvironmentReference) {
		testingHandle.Fatalf("expected invalid key reference error, got %v", keyRegistryError)
	}
	_, sequenceRegistryError := contract.RegistryForYAML([]byte("- ${CONFIGFILE_TEST_MISSING:-default}\n"))
	if !errors.Is(sequenceRegistryError, ErrInvalidEnvironmentReference) {
		testingHandle.Fatalf("expected invalid sequence reference error, got %v", sequenceRegistryError)
	}
	if references, referenceError := scalarEnvironmentReferences("cost is $20"); referenceError != nil || len(references) != 0 {
		testingHandle.Fatalf("expected literal dollar amount without references, got references=%#v error=%v", references, referenceError)
	}
	if path := joinYAMLPath("parent", " "); path != "parent.?" {
		testingHandle.Fatalf("unexpected blank-key path: %s", path)
	}
	if path := indexedYAMLPath("", 2); path != "[2]" {
		testingHandle.Fatalf("unexpected root index path: %s", path)
	}

	if validateError := (EnvRegistry{}).Validate(mapEnvironmentLookup(nil)); validateError != nil {
		testingHandle.Fatalf("expected empty registry validation success, got %v", validateError)
	}
	if paths := mustEmptyPathRegistry(testingHandle).ReferencePaths(testScalarBooleanName); paths != nil {
		testingHandle.Fatalf("expected explicit registry without paths to return nil, got %#v", paths)
	}

	requiredFlag := mustTestRequirement(testingHandle, true, testScalarBooleanName, mustTestSchema(testingHandle, "bool", func(value string) error {
		if strings.TrimSpace(value) == "true" {
			return nil
		}
		return errors.New("expected true")
	}))
	requiredSecret := mustTestRequirement(testingHandle, true, testContractRequiredName, nil)
	successRegistry, successRegistryError := NewEnvRegistry([]EnvRequirement{requiredFlag, requiredSecret})
	if successRegistryError != nil {
		testingHandle.Fatalf("NewEnvRegistry returned error: %v", successRegistryError)
	}
	successLookup := mapEnvironmentLookup(map[string]string{
		testScalarBooleanName:    "true",
		testContractRequiredName: "secret",
	})
	if validateError := successRegistry.Validate(successLookup); validateError != nil {
		testingHandle.Fatalf("expected registry validation success, got %v", validateError)
	}
}

func TestLoadYAMLRejectsEmptyPath(testingHandle *testing.T) {
	var decoded configFileFixture
	loadError := LoadYAML("   ", &decoded)
	if !errors.Is(loadError, ErrMissingPath) {
		testingHandle.Fatalf("expected ErrMissingPath, got %v", loadError)
	}
}

func TestLoadYAMLWrapsReadFailure(testingHandle *testing.T) {
	var decoded configFileFixture
	loadError := LoadYAML(filepath.Join(testingHandle.TempDir(), "missing.yml"), &decoded)
	if !errors.Is(loadError, ErrRead) {
		testingHandle.Fatalf("expected ErrRead, got %v", loadError)
	}
}

func TestLoadYAMLWrapsDecodeFailure(testingHandle *testing.T) {
	configPath := writeConfigFile(testingHandle, "server:\n  unknown: true")
	var decoded configFileFixture
	loadError := LoadYAML(configPath, &decoded)
	if !errors.Is(loadError, ErrParse) {
		testingHandle.Fatalf("expected ErrParse, got %v", loadError)
	}
}

func TestLoadYAMLBytesRejectsInvalidTarget(testingHandle *testing.T) {
	testCases := []struct {
		name   string
		target any
	}{
		{name: "nil", target: nil},
		{name: "non pointer", target: configFileFixture{}},
		{name: "nil pointer", target: (*configFileFixture)(nil)},
	}

	for _, testCase := range testCases {
		testingHandle.Run(testCase.name, func(testingHandle *testing.T) {
			loadError := LoadYAMLBytes([]byte("server: {}\n"), testCase.target)
			if !errors.Is(loadError, ErrNilTarget) {
				testingHandle.Fatalf("expected ErrNilTarget, got %v", loadError)
			}
		})
	}
}

func TestLoadYAMLBytesReturnsInterpolationFailure(testingHandle *testing.T) {
	unsetEnvironment(testingHandle, testMissingEnvironmentNameA)
	var decoded configFileFixture
	loadError := LoadYAMLBytes([]byte("server:\n  secret: $CONFIGFILE_TEST_MISSING_A\n"), &decoded)
	if !errors.Is(loadError, ErrMissingEnvironmentVariables) {
		testingHandle.Fatalf("expected ErrMissingEnvironmentVariables, got %v", loadError)
	}
}

func TestLoadYAMLBytesRejectsUnknownFields(testingHandle *testing.T) {
	var decoded configFileFixture
	loadError := LoadYAMLBytes([]byte("server:\n  missing_field: true\n"), &decoded)
	if !errors.Is(loadError, ErrParse) {
		testingHandle.Fatalf("expected ErrParse, got %v", loadError)
	}
}

func TestLoadYAMLBytesRejectsTrailingDocuments(testingHandle *testing.T) {
	var decoded configFileFixture
	loadError := LoadYAMLBytes([]byte(strings.TrimSpace(`
server:
  enabled: true
---
server:
  max_workers: 12
`)), &decoded)

	if !errors.Is(loadError, ErrParse) {
		testingHandle.Fatalf("expected ErrParse, got %v", loadError)
	}
	if !strings.Contains(loadError.Error(), "trailing YAML document") {
		testingHandle.Fatalf("expected trailing document error, got %v", loadError)
	}
}

func TestLoadYAMLBytesRejectsTrailingDocumentAfterStrictDecode(testingHandle *testing.T) {
	originalMarshalYAML := marshalYAML
	marshalYAML = func(input any) ([]byte, error) {
		return []byte("server: {}\n---\nserver: {}\n"), nil
	}
	testingHandle.Cleanup(func() {
		marshalYAML = originalMarshalYAML
	})

	var decoded configFileFixture
	loadError := LoadYAMLBytes([]byte("server: {}\n"), &decoded)
	if !errors.Is(loadError, ErrParse) {
		testingHandle.Fatalf("expected ErrParse, got %v", loadError)
	}
	if !strings.Contains(loadError.Error(), "trailing YAML document") {
		testingHandle.Fatalf("expected trailing document error, got %v", loadError)
	}
}

func TestInterpolateYAMLAcceptsEmptyPayload(testingHandle *testing.T) {
	if _, interpolateError := InterpolateYAML(nil); interpolateError != nil {
		testingHandle.Fatalf("expected empty YAML payload to interpolate, got %v", interpolateError)
	}
}

func TestInterpolateYAMLRejectsMalformedTrailingDocument(testingHandle *testing.T) {
	_, interpolateError := InterpolateYAML([]byte("server: {}\n---\nserver: [\n"))
	if !errors.Is(interpolateError, ErrParse) {
		testingHandle.Fatalf("expected ErrParse, got %v", interpolateError)
	}
}

func TestInterpolateYAMLReportsMissingEnvironmentVariables(testingHandle *testing.T) {
	testingHandle.Setenv(testScalarStringName, "available")
	unsetEnvironment(testingHandle, testMissingEnvironmentNameA)
	unsetEnvironment(testingHandle, testMissingEnvironmentNameB)

	_, interpolateError := InterpolateYAML([]byte(strings.TrimSpace(`
server:
  first: ${CONFIGFILE_TEST_MISSING_B}
  second: "$CONFIGFILE_TEST_MISSING_A"
  third: "$CONFIGFILE_TEST_MISSING_B"
  fourth: "prefix-$CONFIGFILE_TEST_MISSING_A"
  available: "$CONFIGFILE_TEST_STRING"
`)))

	if !errors.Is(interpolateError, ErrMissingEnvironmentVariables) {
		testingHandle.Fatalf("expected ErrMissingEnvironmentVariables, got %v", interpolateError)
	}

	var missingVariablesError MissingEnvironmentVariablesError
	if !errors.As(interpolateError, &missingVariablesError) {
		testingHandle.Fatalf("expected MissingEnvironmentVariablesError, got %T", interpolateError)
	}
	expectedNames := []string{testMissingEnvironmentNameA, testMissingEnvironmentNameB}
	if !reflect.DeepEqual(missingVariablesError.Names, expectedNames) {
		testingHandle.Fatalf("expected missing names %#v, got %#v", expectedNames, missingVariablesError.Names)
	}
}

func TestInterpolateYAMLRejectsDefaultSyntax(testingHandle *testing.T) {
	_, interpolateError := InterpolateYAML([]byte("server:\n  port: ${CONFIGFILE_TEST_PORT:-8080} ${CONFIGFILE_TEST_OTHER:-9090}\n"))
	if !errors.Is(interpolateError, ErrInvalidEnvironmentReference) {
		testingHandle.Fatalf("expected ErrInvalidEnvironmentReference, got %v", interpolateError)
	}

	var invalidReferenceError InvalidEnvironmentReferenceError
	if !errors.As(interpolateError, &invalidReferenceError) {
		testingHandle.Fatalf("expected InvalidEnvironmentReferenceError, got %T", interpolateError)
	}
	if invalidReferenceError.Reference != "${CONFIGFILE_TEST_PORT:-8080}" {
		testingHandle.Fatalf("unexpected invalid reference: %s", invalidReferenceError.Reference)
	}
}

func TestInterpolateYAMLWrapsMarshalFailure(testingHandle *testing.T) {
	originalMarshalYAML := marshalYAML
	marshalYAML = func(input any) ([]byte, error) {
		return nil, fmt.Errorf("forced marshal failure")
	}
	testingHandle.Cleanup(func() {
		marshalYAML = originalMarshalYAML
	})

	_, interpolateError := InterpolateYAML([]byte("server: {}\n"))
	if !errors.Is(interpolateError, ErrParse) {
		testingHandle.Fatalf("expected ErrParse, got %v", interpolateError)
	}
}

func TestInterpolateYAMLRejectsInvalidYAML(testingHandle *testing.T) {
	_, interpolateError := InterpolateYAML([]byte("server: ["))
	if !errors.Is(interpolateError, ErrParse) {
		testingHandle.Fatalf("expected ErrParse, got %v", interpolateError)
	}
}

func TestInterpolateNodeAcceptsNilAndNonScalarNodes(testingHandle *testing.T) {
	missingVariables := map[string]struct{}{}
	if interpolationError := interpolateNode(nil, missingVariables, OSEnvironmentLookup, EnvRegistry{}); interpolationError != nil {
		testingHandle.Fatalf("expected nil node to be ignored, got %v", interpolationError)
	}
	if interpolationError := interpolateScalarNode(nil, missingVariables, OSEnvironmentLookup, EnvRegistry{}); interpolationError != nil {
		testingHandle.Fatalf("expected nil scalar to be ignored, got %v", interpolationError)
	}
	if environmentName, referenceError := environmentNameFromReference("$CONFIGFILE_TEST_DIRECT"); referenceError != nil || environmentName != "CONFIGFILE_TEST_DIRECT" {
		testingHandle.Fatalf("expected direct environment name, got name=%s error=%v", environmentName, referenceError)
	}
	if _, referenceError := environmentNameFromReference("no-reference"); !errors.Is(referenceError, ErrInvalidEnvironmentReference) {
		testingHandle.Fatalf("expected invalid environment reference, got %v", referenceError)
	}
}

func TestErrorCompatibility(testingHandle *testing.T) {
	missingVariablesError := MissingEnvironmentVariablesError{Names: []string{testMissingEnvironmentNameA}}
	if !errors.Is(missingVariablesError, ErrMissingEnvironmentVariables) {
		testingHandle.Fatal("expected missing-variable error compatibility")
	}
	if !strings.Contains(missingVariablesError.Error(), testMissingEnvironmentNameA) {
		testingHandle.Fatalf("expected missing-variable error to include name, got %s", missingVariablesError.Error())
	}

	invalidReferenceError := InvalidEnvironmentReferenceError{Reference: "${BROKEN:-default}"}
	if !errors.Is(invalidReferenceError, ErrInvalidEnvironmentReference) {
		testingHandle.Fatal("expected invalid-reference error compatibility")
	}
	if !strings.Contains(invalidReferenceError.Error(), "${BROKEN:-default}") {
		testingHandle.Fatalf("expected invalid-reference error to include reference, got %s", invalidReferenceError.Error())
	}
}

func writeConfigFile(testingHandle *testing.T, payload string) string {
	testingHandle.Helper()
	configPath := filepath.Join(testingHandle.TempDir(), "config.yml")
	writeError := os.WriteFile(configPath, []byte(payload+"\n"), 0o600)
	if writeError != nil {
		testingHandle.Fatalf("failed to write config fixture: %v", writeError)
	}
	return configPath
}

func unsetEnvironment(testingHandle *testing.T, environmentName string) {
	testingHandle.Helper()
	originalValue, hadOriginalValue := os.LookupEnv(environmentName)
	if unsetError := os.Unsetenv(environmentName); unsetError != nil {
		testingHandle.Fatalf("failed to unset %s: %v", environmentName, unsetError)
	}
	testingHandle.Cleanup(func() {
		if hadOriginalValue {
			if restoreError := os.Setenv(environmentName, originalValue); restoreError != nil {
				testingHandle.Fatalf("failed to restore %s: %v", environmentName, restoreError)
			}
			return
		}
		if restoreError := os.Unsetenv(environmentName); restoreError != nil {
			testingHandle.Fatalf("failed to keep %s unset: %v", environmentName, restoreError)
		}
	})
}

func mustTestSchema(testingHandle *testing.T, name string, validate func(value string) error) EnvValueSchema {
	testingHandle.Helper()
	schema, schemaError := NewEnvValueSchema(name, validate)
	if schemaError != nil {
		testingHandle.Fatalf("NewEnvValueSchema returned error: %v", schemaError)
	}
	return schema
}

func mustTestRequirement(testingHandle *testing.T, required bool, name string, schema EnvValueSchema) EnvRequirement {
	testingHandle.Helper()
	var requirement EnvRequirement
	var requirementError error
	if required {
		requirement, requirementError = NewRequiredEnv(name, schema)
	} else {
		requirement, requirementError = NewOptionalEnv(name, schema)
	}
	if requirementError != nil {
		testingHandle.Fatalf("new env requirement returned error: %v", requirementError)
	}
	return requirement
}

func requirementNames(requirements []EnvRequirement) []string {
	names := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		names = append(names, requirement.Name())
	}
	return names
}

func mapEnvironmentLookup(values map[string]string) EnvironmentLookup {
	return func(name string) (string, bool) {
		if values == nil {
			return "", false
		}
		value, found := values[name]
		return value, found
	}
}

func mustEmptyPathRegistry(testingHandle *testing.T) EnvRegistry {
	testingHandle.Helper()
	requirement := mustTestRequirement(testingHandle, true, testScalarBooleanName, nil)
	registry, registryError := NewEnvRegistry([]EnvRequirement{requirement})
	if registryError != nil {
		testingHandle.Fatalf("NewEnvRegistry returned error: %v", registryError)
	}
	return registry
}
