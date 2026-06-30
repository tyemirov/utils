package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tyemirov/utils/configfile"
)

const (
	testBoolEnv     = "CONFIGENVCHECK_TEST_BOOL"
	testURLenv      = "CONFIGENVCHECK_TEST_URL"
	testJSONEnv     = "CONFIGENVCHECK_TEST_JSON"
	testKeyEnv      = "CONFIGENVCHECK_TEST_KEY"
	testMissingEnv  = "CONFIGENVCHECK_TEST_MISSING"
	testEmptyEnv    = "CONFIGENVCHECK_TEST_EMPTY"
	testOptionalEnv = "CONFIGENVCHECK_TEST_OPTIONAL"
	testShellEnv    = "CONFIGENVCHECK_TEST_SHELL"
)

type exitProcessPanic struct {
	code int
}

func TestRunSucceedsAndPrintsMandatoryRegistry(testingHandle *testing.T) {
	configPath := writeConfigenvcheckFile(testingHandle, "config.yml", `
service:
  enabled: ${CONFIGENVCHECK_TEST_BOOL}
  endpoint: "${CONFIGENVCHECK_TEST_URL}"
  payload: ${CONFIGENVCHECK_TEST_JSON}
  key: "${CONFIGENVCHECK_TEST_KEY}"
  optional: "${CONFIGENVCHECK_TEST_OPTIONAL}"
`)
	envPath := writeConfigenvcheckFile(testingHandle, ".env", strings.Join([]string{
		"CONFIGENVCHECK_TEST_BOOL=true",
		"CONFIGENVCHECK_TEST_URL=https://example.invalid/api",
		`CONFIGENVCHECK_TEST_JSON={"ok":true}`,
		"CONFIGENVCHECK_TEST_KEY=" + testBase64Key(),
	}, "\n"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{
		"--config", configPath,
		"--env-file", envPath,
		"--optional-env", testOptionalEnv,
		"--schema", testBoolEnv + "=" + schemaBool,
		"--schema", testURLenv + "=" + schemaURL,
		"--schema", testJSONEnv + "=" + schemaJSON,
		"--schema", testKeyEnv + "=" + schemaBase64Bytes32,
		"--show-registry",
	}, &stdout, &stderr)
	if exitCode != 0 {
		testingHandle.Fatalf("expected success, exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	expectedRegistryLines := []string{
		"MANDATORY CONFIGENVCHECK_TEST_BOOL schema=bool references=service.enabled",
		"MANDATORY CONFIGENVCHECK_TEST_JSON schema=json references=service.payload",
		"MANDATORY CONFIGENVCHECK_TEST_KEY schema=base64-32-byte references=service.key",
		"MANDATORY CONFIGENVCHECK_TEST_URL schema=url references=service.endpoint",
	}
	for _, expectedLine := range expectedRegistryLines {
		if !strings.Contains(stdout.String(), expectedLine) {
			testingHandle.Fatalf("expected registry line %q in stdout %q", expectedLine, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), testOptionalEnv) {
		testingHandle.Fatalf("expected optional env to stay out of mandatory registry, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		testingHandle.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestRunReportsMissingEmptyAndInvalidEnvironment(testingHandle *testing.T) {
	configPath := writeConfigenvcheckFile(testingHandle, "config.yml", `
missing: ${CONFIGENVCHECK_TEST_MISSING}
empty: ${CONFIGENVCHECK_TEST_EMPTY}
enabled: ${CONFIGENVCHECK_TEST_BOOL}
`)
	envPath := writeConfigenvcheckFile(testingHandle, ".env", `
CONFIGENVCHECK_TEST_EMPTY=
CONFIGENVCHECK_TEST_BOOL=definitely-not-a-bool
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{
		"--config", configPath,
		"--env-file", envPath,
		"--schema", testBoolEnv + "=" + schemaBool,
	}, &stdout, &stderr)
	if exitCode != 1 {
		testingHandle.Fatalf("expected check failure, exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	expectedErrors := []string{
		"ERROR: CONFIGENVCHECK_TEST_BOOL invalid schema=bool references=enabled: expected boolean",
		"ERROR: CONFIGENVCHECK_TEST_EMPTY empty references=empty",
		"ERROR: CONFIGENVCHECK_TEST_MISSING missing references=missing",
		"ERROR: configenvcheck.failed",
	}
	for _, expectedError := range expectedErrors {
		if !strings.Contains(stderr.String(), expectedError) {
			testingHandle.Fatalf("expected stderr to contain %q, got %q", expectedError, stderr.String())
		}
	}
	if strings.Contains(stderr.String(), "definitely-not-a-bool") {
		testingHandle.Fatalf("expected stderr to omit raw invalid value, got %q", stderr.String())
	}
}

func TestRunReportsMissingEnvironmentInMappingKey(testingHandle *testing.T) {
	configPath := writeConfigenvcheckFile(testingHandle, "config.yml", `
"${CONFIGENVCHECK_TEST_MISSING}": value
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"--config", configPath}, &stdout, &stderr)
	if exitCode != 1 {
		testingHandle.Fatalf("expected check failure, exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	expectedError := "ERROR: CONFIGENVCHECK_TEST_MISSING missing references=${CONFIGENVCHECK_TEST_MISSING}"
	if !strings.Contains(stderr.String(), expectedError) {
		testingHandle.Fatalf("expected stderr to contain %q, got %q", expectedError, stderr.String())
	}
}

func TestRunRejectsInvalidArguments(testingHandle *testing.T) {
	testCases := []struct {
		name      string
		arguments []string
		expected  string
	}{
		{name: "missing config", arguments: nil, expected: "--config is required"},
		{name: "unknown flag", arguments: []string{"--unknown"}, expected: "flag provided but not defined"},
		{name: "schema missing separator", arguments: []string{"--config", "config.yml", "--schema", testBoolEnv}, expected: "schema must be NAME=kind"},
		{name: "unknown schema", arguments: []string{"--config", "config.yml", "--schema", testBoolEnv + "=uuid"}, expected: "unsupported schema"},
		{name: "duplicate schema", arguments: []string{"--config", "config.yml", "--schema", testBoolEnv + "=bool", "--schema", testBoolEnv + "=url"}, expected: "duplicate schema"},
		{name: "required optional conflict", arguments: []string{"--config", "config.yml", "--required-env", testBoolEnv, "--optional-env", testBoolEnv}, expected: "both required and optional"},
		{name: "invalid env name", arguments: []string{"--config", "config.yml", "--required-env", "bad-name"}, expected: "invalid environment name"},
		{name: "invalid optional env name", arguments: []string{"--config", "config.yml", "--optional-env", "bad-name"}, expected: "invalid environment name"},
	}

	for _, testCase := range testCases {
		testingHandle.Run(testCase.name, func(testingHandle *testing.T) {
			configPath := writeConfigenvcheckFile(testingHandle, "config.yml", "value: plain\n")
			arguments := replaceConfigPathArgument(testCase.arguments, configPath)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := run(arguments, &stdout, &stderr)
			if testCase.name == "missing config" || testCase.name == "unknown flag" {
				if exitCode != 2 {
					testingHandle.Fatalf("expected argument failure, exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
				}
			} else if exitCode != 1 {
				testingHandle.Fatalf("expected check failure, exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), testCase.expected) {
				testingHandle.Fatalf("expected stderr to contain %q, got %q", testCase.expected, stderr.String())
			}
		})
	}
}

func TestRunReportsInvalidConfigReference(testingHandle *testing.T) {
	configPath := writeConfigenvcheckFile(testingHandle, "config.yml", "value: ${CONFIGENVCHECK_TEST_MISSING:-default}\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"--config", configPath}, &stdout, &stderr)
	if exitCode != 1 {
		testingHandle.Fatalf("expected config reference failure, exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "build registry") || !strings.Contains(stderr.String(), "invalid_environment_reference") {
		testingHandle.Fatalf("expected invalid reference output, got %q", stderr.String())
	}
}

func TestRunReportsConfigAndEnvFileReadErrors(testingHandle *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"--config", filepath.Join(testingHandle.TempDir(), "missing.yml")}, &stdout, &stderr)
	if exitCode != 1 || !strings.Contains(stderr.String(), "read config") {
		testingHandle.Fatalf("expected config read failure, exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}

	configPath := writeConfigenvcheckFile(testingHandle, "config.yml", "value: ${CONFIGENVCHECK_TEST_BOOL}\n")
	stdout.Reset()
	stderr.Reset()
	exitCode = run([]string{"--config", configPath, "--env-file", filepath.Join(testingHandle.TempDir(), "missing.env")}, &stdout, &stderr)
	if exitCode != 1 || !strings.Contains(stderr.String(), "read env file") {
		testingHandle.Fatalf("expected env file read failure, exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestLoadEnvironmentMergesShellAndEnvFiles(testingHandle *testing.T) {
	testingHandle.Setenv(testShellEnv, "from-shell")
	testingHandle.Setenv(testBoolEnv, "from-shell")
	envPath := writeConfigenvcheckFile(testingHandle, ".env", `
# comment
export CONFIGENVCHECK_TEST_BOOL="from-file"
NO_EQUALS
=missing-key
CONFIGENVCHECK_TEST_EMPTY=''
`)

	values, loadErr := loadEnvironment(checkInputs{envFiles: []string{envPath}, inheritShell: true})
	if loadErr != nil {
		testingHandle.Fatalf("loadEnvironment returned error: %v", loadErr)
	}
	if values[testShellEnv] != "from-shell" {
		testingHandle.Fatalf("expected shell value, got %q", values[testShellEnv])
	}
	if values[testBoolEnv] != "from-file" {
		testingHandle.Fatalf("expected env file to override shell, got %q", values[testBoolEnv])
	}
	if values[testEmptyEnv] != "" {
		testingHandle.Fatalf("expected quoted empty value to decode empty, got %q", values[testEmptyEnv])
	}
	if _, exists := values["NO_EQUALS"]; exists {
		testingHandle.Fatal("expected line without equals to be ignored")
	}
}

func TestParseDotEnvFileRejectsInvalidPathAndReadError(testingHandle *testing.T) {
	if _, parseErr := parseDotEnvFile(" "); !errors.Is(parseErr, errInvalidArguments) {
		testingHandle.Fatalf("expected invalid path error, got %v", parseErr)
	}
	if _, parseErr := parseDotEnvFile(testingHandle.TempDir()); parseErr == nil || !strings.Contains(parseErr.Error(), "read env file") {
		testingHandle.Fatalf("expected directory read error, got %v", parseErr)
	}
}

func TestSchemaValidators(testingHandle *testing.T) {
	validators := []struct {
		name         string
		schemaKind   string
		validValue   string
		invalidValue string
	}{
		{name: "bool", schemaKind: schemaBool, validValue: "false", invalidValue: "maybe"},
		{name: "url", schemaKind: schemaURL, validValue: "https://example.invalid/path", invalidValue: "/relative"},
		{name: "json", schemaKind: schemaJSON, validValue: `["admin@example.invalid"]`, invalidValue: "["},
		{name: "base64", schemaKind: schemaBase64Bytes32, validValue: testBase64Key(), invalidValue: base64.StdEncoding.EncodeToString([]byte("short"))},
		{name: "hex", schemaKind: schemaHexBytes32, validValue: strings.Repeat("a", 64), invalidValue: "abc"},
		{name: "hostport", schemaKind: schemaHostPort, validValue: "localhost:50051", invalidValue: "localhost"},
		{name: "duration", schemaKind: schemaDuration, validValue: "30m", invalidValue: "0s"},
		{name: "positive integer", schemaKind: schemaPositiveInteger, validValue: "1", invalidValue: "-1"},
		{name: "email", schemaKind: schemaEmail, validValue: "admin@example.invalid", invalidValue: "admin"},
	}
	for _, validator := range validators {
		testingHandle.Run(validator.name, func(testingHandle *testing.T) {
			schema, schemaErr := schemaForKind(validator.schemaKind)
			if schemaErr != nil {
				testingHandle.Fatalf("schemaForKind returned error: %v", schemaErr)
			}
			if validateErr := schema.Validate(validator.validValue); validateErr != nil {
				testingHandle.Fatalf("expected valid %s value, got %v", validator.schemaKind, validateErr)
			}
			if validateErr := schema.Validate(validator.invalidValue); validateErr == nil {
				testingHandle.Fatalf("expected invalid %s value", validator.schemaKind)
			}
		})
	}
}

func TestReportValidationErrorFallback(testingHandle *testing.T) {
	var stderr bytes.Buffer
	reportValidationError(errors.New("plain failure"), configfile.EnvRegistry{}, &stderr)
	if !strings.Contains(stderr.String(), "plain failure") {
		testingHandle.Fatalf("expected fallback error output, got %q", stderr.String())
	}
}

func TestStringListFlag(testingHandle *testing.T) {
	values := stringListFlag{}
	if values.String() != "" {
		testingHandle.Fatalf("expected empty string, got %q", values.String())
	}
	if setErr := values.Set(" "); setErr != nil {
		testingHandle.Fatalf("Set empty returned error: %v", setErr)
	}
	if setErr := values.Set("value"); setErr != nil {
		testingHandle.Fatalf("Set value returned error: %v", setErr)
	}
	if values.String() != "value" {
		testingHandle.Fatalf("expected joined value, got %q", values.String())
	}
}

func TestMainEntrypoint(testingHandle *testing.T) {
	configPath := writeConfigenvcheckFile(testingHandle, "config.yml", "value: plain\n")
	originalArguments := os.Args
	originalExitProcess := exitProcess
	defer func() {
		os.Args = originalArguments
		exitProcess = originalExitProcess
	}()
	os.Args = []string{commandName, "--config", configPath}
	exitProcess = func(code int) {
		panic(exitProcessPanic{code: code})
	}

	defer func() {
		recoveredValue := recover()
		exitPanic, ok := recoveredValue.(exitProcessPanic)
		if !ok {
			testingHandle.Fatalf("expected exit panic, got %#v", recoveredValue)
		}
		if exitPanic.code != 0 {
			testingHandle.Fatalf("expected main exit 0, got %d", exitPanic.code)
		}
	}()
	main()
}

func writeConfigenvcheckFile(testingHandle *testing.T, name string, contents string) string {
	testingHandle.Helper()
	path := filepath.Join(testingHandle.TempDir(), name)
	if writeErr := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o600); writeErr != nil {
		testingHandle.Fatalf("write %s: %v", name, writeErr)
	}
	return path
}

func replaceConfigPathArgument(arguments []string, configPath string) []string {
	replacedArguments := append([]string(nil), arguments...)
	for argumentIndex, argument := range replacedArguments {
		if argument == "config.yml" {
			replacedArguments[argumentIndex] = configPath
		}
	}
	return replacedArguments
}

func testBase64Key() string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
}
