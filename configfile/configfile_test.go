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
	if interpolationError := interpolateNode(nil, missingVariables); interpolationError != nil {
		testingHandle.Fatalf("expected nil node to be ignored, got %v", interpolationError)
	}
	if interpolationError := interpolateScalarNode(nil, missingVariables); interpolationError != nil {
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
