package configfile

import (
	"errors"
	"testing"
)

func TestEnvValueSchemaForKindValidatesCommonRuntimeValues(testingHandle *testing.T) {
	testCases := []struct {
		name         string
		kind         string
		validValue   string
		invalidValue string
	}{
		{name: "bool", kind: EnvSchemaBool, validValue: "true", invalidValue: "yes"},
		{name: "url", kind: EnvSchemaURL, validValue: "https://example.invalid/path", invalidValue: "localhost:8080"},
		{name: "json", kind: EnvSchemaJSON, validValue: `{"ok":true}`, invalidValue: `{"ok"`},
		{name: "base64 32 bytes", kind: EnvSchemaBase64Bytes32, validValue: "MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI=", invalidValue: "c2hvcnQ="},
		{name: "hex 32 bytes", kind: EnvSchemaHexBytes32, validValue: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", invalidValue: "abc"},
		{name: "host port", kind: EnvSchemaHostPort, validValue: "localhost:50051", invalidValue: "localhost"},
		{name: "duration", kind: EnvSchemaDuration, validValue: "30m", invalidValue: "forever"},
		{name: "positive integer", kind: EnvSchemaPositiveInteger, validValue: "15", invalidValue: "0"},
		{name: "email", kind: EnvSchemaEmail, validValue: "admin@example.invalid", invalidValue: "not-email"},
	}

	for _, testCase := range testCases {
		testingHandle.Run(testCase.name, func(testingHandle *testing.T) {
			schema, schemaError := EnvValueSchemaForKind(testCase.kind)
			if schemaError != nil {
				testingHandle.Fatalf("EnvValueSchemaForKind returned error: %v", schemaError)
			}
			if schema.Name() != testCase.kind {
				testingHandle.Fatalf("expected schema name %q, got %q", testCase.kind, schema.Name())
			}
			if validationError := schema.Validate(testCase.validValue); validationError != nil {
				testingHandle.Fatalf("expected %q to validate, got %v", testCase.validValue, validationError)
			}
			if validationError := schema.Validate(testCase.invalidValue); validationError == nil {
				testingHandle.Fatalf("expected %q to fail validation", testCase.invalidValue)
			}
		})
	}
}

func TestEnvValueSchemaForKindRejectsUnknownKind(testingHandle *testing.T) {
	_, schemaError := EnvValueSchemaForKind("unknown")
	if !errors.Is(schemaError, ErrInvalidEnvironmentRequirement) {
		testingHandle.Fatalf("expected invalid requirement error, got %v", schemaError)
	}
}

func TestHostPortSchemaRejectsOutOfRangePort(testingHandle *testing.T) {
	schema, schemaError := EnvValueSchemaForKind(EnvSchemaHostPort)
	if schemaError != nil {
		testingHandle.Fatalf("EnvValueSchemaForKind returned error: %v", schemaError)
	}
	if validationError := schema.Validate("localhost:70000"); validationError == nil {
		testingHandle.Fatal("expected out-of-range port to fail validation")
	}
}
