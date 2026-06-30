// Command configenvcheck validates shell-sourced YAML config environment
// requirements.
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/tyemirov/utils/configfile"
)

const (
	commandName                    = "configenvcheck"
	errorPrefix                    = "ERROR: "
	flagConfig                     = "config"
	flagEnvFile                    = "env-file"
	flagInheritShell               = "inherit-shell"
	flagOptionalEnv                = "optional-env"
	flagRequiredEnv                = "required-env"
	flagSchema                     = "schema"
	flagShowRegistry               = "show-registry"
	schemaBase64Bytes32            = "base64-32-byte"
	schemaBool                     = "bool"
	schemaJSON                     = "json"
	schemaURL                      = "url"
	messageConfigDescription       = "YAML config file whose environment references must be validated"
	messageEnvFileDescription      = "dotenv file that supplies environment values; repeatable"
	messageInheritShellDescription = "Supplement dotenv values with process environment values"
	messageOptionalDescription     = "Environment variable that may be omitted or empty; repeatable"
	messageRequiredDescription     = "Additional required environment variable; repeatable"
	messageSchemaDescription       = "Environment value schema as NAME=bool|url|json|base64-32-byte; repeatable"
	messageRegistryDescription     = "Print the mandatory environment registry on success"
)

var errInvalidArguments = errors.New("configenvcheck.invalid_arguments")
var errCheckFailed = errors.New("configenvcheck.failed")
var exitProcess = os.Exit

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return nil
	}
	*values = append(*values, trimmedValue)
	return nil
}

type checkInputs struct {
	configPath   string
	envFiles     []string
	inheritShell bool
	optionalEnv  []string
	requiredEnv  []string
	schemas      []string
	showRegistry bool
}

func run(arguments []string, stdout io.Writer, stderr io.Writer) int {
	inputs, inputErr := parseInputs(arguments, stderr)
	if inputErr != nil {
		_, _ = fmt.Fprintf(stderr, "%s%v\n", errorPrefix, inputErr)
		return 2
	}
	if checkErr := runCheck(inputs, stdout, stderr); checkErr != nil {
		_, _ = fmt.Fprintf(stderr, "%s%v\n", errorPrefix, checkErr)
		return 1
	}
	return 0
}

func parseInputs(arguments []string, stderr io.Writer) (checkInputs, error) {
	var envFiles stringListFlag
	var optionalEnv stringListFlag
	var requiredEnv stringListFlag
	var schemas stringListFlag
	inputs := checkInputs{}
	flagSet := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	flagSet.StringVar(&inputs.configPath, flagConfig, "", messageConfigDescription)
	flagSet.Var(&envFiles, flagEnvFile, messageEnvFileDescription)
	flagSet.BoolVar(&inputs.inheritShell, flagInheritShell, false, messageInheritShellDescription)
	flagSet.Var(&optionalEnv, flagOptionalEnv, messageOptionalDescription)
	flagSet.Var(&requiredEnv, flagRequiredEnv, messageRequiredDescription)
	flagSet.Var(&schemas, flagSchema, messageSchemaDescription)
	flagSet.BoolVar(&inputs.showRegistry, flagShowRegistry, false, messageRegistryDescription)
	if parseErr := flagSet.Parse(arguments); parseErr != nil {
		return checkInputs{}, fmt.Errorf("%w: %w", errInvalidArguments, parseErr)
	}
	inputs.configPath = strings.TrimSpace(inputs.configPath)
	if inputs.configPath == "" {
		return checkInputs{}, fmt.Errorf("%w: --%s is required", errInvalidArguments, flagConfig)
	}
	inputs.envFiles = append([]string(nil), envFiles...)
	inputs.optionalEnv = append([]string(nil), optionalEnv...)
	inputs.requiredEnv = append([]string(nil), requiredEnv...)
	inputs.schemas = append([]string(nil), schemas...)
	return inputs, nil
}

func runCheck(inputs checkInputs, stdout io.Writer, stderr io.Writer) error {
	configPayload, readErr := os.ReadFile(inputs.configPath)
	if readErr != nil {
		return fmt.Errorf("read config %s: %w", inputs.configPath, readErr)
	}
	contract, contractErr := buildEnvironmentContract(inputs)
	if contractErr != nil {
		return contractErr
	}
	registry, registryErr := contract.RegistryForYAML(configPayload)
	if registryErr != nil {
		return fmt.Errorf("build registry for %s: %w", inputs.configPath, registryErr)
	}
	lookupValues, environmentErr := loadEnvironment(inputs)
	if environmentErr != nil {
		return environmentErr
	}
	lookup := func(name string) (string, bool) {
		value, found := lookupValues[name]
		return value, found
	}
	if validationErr := registry.Validate(lookup); validationErr != nil {
		reportValidationError(validationErr, registry, stderr)
		return errCheckFailed
	}
	if inputs.showRegistry {
		reportMandatoryRegistry(registry, stdout)
	}
	return nil
}

func buildEnvironmentContract(inputs checkInputs) (configfile.EnvContract, error) {
	schemasByName, schemaErr := parseSchemaDeclarations(inputs.schemas)
	if schemaErr != nil {
		return configfile.EnvContract{}, schemaErr
	}
	optionalNames := nameSet(inputs.optionalEnv)
	requiredNames := nameSet(inputs.requiredEnv)
	for optionalName := range optionalNames {
		if _, required := requiredNames[optionalName]; required {
			return configfile.EnvContract{}, fmt.Errorf("%w: %s is both required and optional", errInvalidArguments, optionalName)
		}
	}

	contractNames := make(map[string]struct{})
	for requiredName := range requiredNames {
		contractNames[requiredName] = struct{}{}
	}
	for optionalName := range optionalNames {
		contractNames[optionalName] = struct{}{}
	}
	for schemaName := range schemasByName {
		contractNames[schemaName] = struct{}{}
	}

	requirements := make([]configfile.EnvRequirement, 0, len(contractNames))
	for _, environmentName := range sortedKeys(contractNames) {
		schema := schemasByName[environmentName]
		if _, optional := optionalNames[environmentName]; optional {
			requirement, requirementErr := configfile.NewOptionalEnv(environmentName, schema)
			if requirementErr != nil {
				return configfile.EnvContract{}, requirementErr
			}
			requirements = append(requirements, requirement)
			continue
		}
		requirement, requirementErr := configfile.NewRequiredEnv(environmentName, schema)
		if requirementErr != nil {
			return configfile.EnvContract{}, requirementErr
		}
		requirements = append(requirements, requirement)
	}
	return configfile.NewEnvContract(requirements)
}

func parseSchemaDeclarations(schemaDeclarations []string) (map[string]configfile.EnvValueSchema, error) {
	schemasByName := make(map[string]configfile.EnvValueSchema, len(schemaDeclarations))
	for _, declaration := range schemaDeclarations {
		environmentName, schemaKind, declarationErr := splitSchemaDeclaration(declaration)
		if declarationErr != nil {
			return nil, declarationErr
		}
		if _, exists := schemasByName[environmentName]; exists {
			return nil, fmt.Errorf("%w: duplicate schema for %s", errInvalidArguments, environmentName)
		}
		schema, schemaErr := schemaForKind(schemaKind)
		if schemaErr != nil {
			return nil, schemaErr
		}
		schemasByName[environmentName] = schema
	}
	return schemasByName, nil
}

func splitSchemaDeclaration(declaration string) (string, string, error) {
	environmentName, schemaKind, found := strings.Cut(strings.TrimSpace(declaration), "=")
	environmentName = strings.TrimSpace(environmentName)
	schemaKind = strings.TrimSpace(schemaKind)
	if !found || environmentName == "" || schemaKind == "" {
		return "", "", fmt.Errorf("%w: schema must be NAME=kind", errInvalidArguments)
	}
	return environmentName, schemaKind, nil
}

func schemaForKind(schemaKind string) (configfile.EnvValueSchema, error) {
	switch strings.ToLower(strings.TrimSpace(schemaKind)) {
	case schemaBool:
		return configfile.NewEnvValueSchema(schemaBool, validateBool)
	case schemaURL:
		return configfile.NewEnvValueSchema(schemaURL, validateURL)
	case schemaJSON:
		return configfile.NewEnvValueSchema(schemaJSON, validateJSON)
	case schemaBase64Bytes32:
		return configfile.NewEnvValueSchema(schemaBase64Bytes32, validateBase64Bytes32)
	default:
		return nil, fmt.Errorf("%w: unsupported schema %q", errInvalidArguments, schemaKind)
	}
}

func validateBool(value string) error {
	normalizedValue := strings.ToLower(strings.TrimSpace(value))
	if normalizedValue == "true" || normalizedValue == "false" {
		return nil
	}
	return errors.New("expected boolean")
}

func validateURL(value string) error {
	parsedURL, parseErr := url.Parse(strings.TrimSpace(value))
	if parseErr != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return errors.New("expected absolute URL")
	}
	return nil
}

func validateJSON(value string) error {
	var decoded any
	if unmarshalErr := json.Unmarshal([]byte(value), &decoded); unmarshalErr != nil {
		return errors.New("expected JSON")
	}
	return nil
}

func validateBase64Bytes32(value string) error {
	decodedValue, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if decodeErr != nil || len(decodedValue) != 32 {
		return errors.New("expected base64-encoded 32-byte value")
	}
	return nil
}

func loadEnvironment(inputs checkInputs) (map[string]string, error) {
	values := make(map[string]string)
	if inputs.inheritShell {
		for _, entry := range os.Environ() {
			key, value, found := strings.Cut(entry, "=")
			if found && strings.TrimSpace(key) != "" {
				values[key] = value
			}
		}
	}
	for _, envFile := range inputs.envFiles {
		fileValues, fileErr := parseDotEnvFile(envFile)
		if fileErr != nil {
			return nil, fileErr
		}
		for key, value := range fileValues {
			values[key] = value
		}
	}
	return values, nil
}

func parseDotEnvFile(path string) (map[string]string, error) {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return nil, fmt.Errorf("%w: env file path is required", errInvalidArguments)
	}
	file, openErr := os.Open(normalizedPath)
	if openErr != nil {
		return nil, fmt.Errorf("read env file %s: %w", normalizedPath, openErr)
	}
	defer func() { _ = file.Close() }()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("read env file %s: %w", normalizedPath, scanErr)
	}
	return values, nil
}

func reportValidationError(validationErr error, registry configfile.EnvRegistry, stderr io.Writer) {
	var typedValidationError configfile.EnvValidationError
	if !errors.As(validationErr, &typedValidationError) {
		_, _ = fmt.Fprintf(stderr, "%s%v\n", errorPrefix, validationErr)
		return
	}
	for _, issue := range typedValidationError.Issues() {
		referencePaths := registry.ReferencePaths(issue.Name())
		referenceSummary := ""
		if len(referencePaths) > 0 {
			referenceSummary = fmt.Sprintf(" references=%s", strings.Join(referencePaths, ","))
		}
		schemaSummary := ""
		if issue.SchemaName() != "" {
			schemaSummary = fmt.Sprintf(" schema=%s", issue.SchemaName())
		}
		detailSummary := ""
		if issue.Detail() != "" {
			detailSummary = fmt.Sprintf(": %s", issue.Detail())
		}
		_, _ = fmt.Fprintf(stderr, "%s%s %s%s%s%s\n", errorPrefix, issue.Name(), issue.Kind(), schemaSummary, referenceSummary, detailSummary)
	}
}

func reportMandatoryRegistry(registry configfile.EnvRegistry, stdout io.Writer) {
	for _, requirement := range registry.Mandatory() {
		schemaSummary := ""
		if requirement.SchemaName() != "" {
			schemaSummary = fmt.Sprintf(" schema=%s", requirement.SchemaName())
		}
		referencePaths := registry.ReferencePaths(requirement.Name())
		referenceSummary := ""
		if len(referencePaths) > 0 {
			referenceSummary = fmt.Sprintf(" references=%s", strings.Join(referencePaths, ","))
		}
		_, _ = fmt.Fprintf(stdout, "MANDATORY %s%s%s\n", requirement.Name(), schemaSummary, referenceSummary)
	}
}

func nameSet(names []string) map[string]struct{} {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		trimmedName := strings.TrimSpace(name)
		if trimmedName != "" {
			seen[trimmedName] = struct{}{}
		}
	}
	return seen
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func main() {
	exitProcess(run(os.Args[1:], os.Stdout, os.Stderr))
}
