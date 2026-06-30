package configfile

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// EnvSchemaBase64Bytes32 validates base64-encoded 32-byte secrets.
	EnvSchemaBase64Bytes32 = "base64-32-byte"
	// EnvSchemaBool validates boolean strings.
	EnvSchemaBool = "bool"
	// EnvSchemaDuration validates Go duration strings.
	EnvSchemaDuration = "duration"
	// EnvSchemaEmail validates plain email addresses.
	EnvSchemaEmail = "email"
	// EnvSchemaHexBytes32 validates hex-encoded 32-byte secrets.
	EnvSchemaHexBytes32 = "hex-32-byte"
	// EnvSchemaHostPort validates host:port addresses.
	EnvSchemaHostPort = "hostport"
	// EnvSchemaJSON validates JSON payloads.
	EnvSchemaJSON = "json"
	// EnvSchemaPositiveInteger validates integers greater than zero.
	EnvSchemaPositiveInteger = "positive-int"
	// EnvSchemaURL validates absolute URLs.
	EnvSchemaURL = "url"
)

// EnvValueSchemaForKind returns a built-in environment value schema.
func EnvValueSchemaForKind(kind string) (EnvValueSchema, error) {
	normalizedKind := strings.ToLower(strings.TrimSpace(kind))
	switch normalizedKind {
	case EnvSchemaBase64Bytes32:
		return NewEnvValueSchema(EnvSchemaBase64Bytes32, validateBase64Bytes32)
	case EnvSchemaBool:
		return NewEnvValueSchema(EnvSchemaBool, validateBool)
	case EnvSchemaDuration:
		return NewEnvValueSchema(EnvSchemaDuration, validateDuration)
	case EnvSchemaEmail:
		return NewEnvValueSchema(EnvSchemaEmail, validateEmail)
	case EnvSchemaHexBytes32:
		return NewEnvValueSchema(EnvSchemaHexBytes32, validateHexBytes32)
	case EnvSchemaHostPort:
		return NewEnvValueSchema(EnvSchemaHostPort, validateHostPort)
	case EnvSchemaJSON:
		return NewEnvValueSchema(EnvSchemaJSON, validateJSON)
	case EnvSchemaPositiveInteger:
		return NewEnvValueSchema(EnvSchemaPositiveInteger, validatePositiveInteger)
	case EnvSchemaURL:
		return NewEnvValueSchema(EnvSchemaURL, validateURL)
	default:
		return nil, fmt.Errorf("%w: unsupported environment schema %q", ErrInvalidEnvironmentRequirement, kind)
	}
}

func validateBase64Bytes32(value string) error {
	decodedValue, decodeError := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if decodeError != nil || len(decodedValue) != 32 {
		return errors.New("expected base64-encoded 32-byte value")
	}
	return nil
}

func validateBool(value string) error {
	normalizedValue := strings.ToLower(strings.TrimSpace(value))
	if normalizedValue == "true" || normalizedValue == "false" {
		return nil
	}
	return errors.New("expected boolean")
}

func validateDuration(value string) error {
	duration, parseError := time.ParseDuration(strings.TrimSpace(value))
	if parseError != nil || duration <= 0 {
		return errors.New("expected positive duration")
	}
	return nil
}

func validateEmail(value string) error {
	trimmedValue := strings.TrimSpace(value)
	address, parseError := mail.ParseAddress(trimmedValue)
	if parseError != nil || address.Address != trimmedValue {
		return errors.New("expected email address")
	}
	return nil
}

func validateHexBytes32(value string) error {
	decodedValue, decodeError := hex.DecodeString(strings.TrimSpace(value))
	if decodeError != nil || len(decodedValue) != 32 {
		return errors.New("expected hex-encoded 32-byte value")
	}
	return nil
}

func validateHostPort(value string) error {
	host, port, splitError := net.SplitHostPort(strings.TrimSpace(value))
	if splitError != nil || strings.TrimSpace(host) == "" {
		return errors.New("expected host:port")
	}
	portNumber, parseError := strconv.Atoi(port)
	if parseError != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("expected host:port")
	}
	return nil
}

func validateJSON(value string) error {
	var decodedValue any
	if unmarshalError := json.Unmarshal([]byte(value), &decodedValue); unmarshalError != nil {
		return errors.New("expected JSON")
	}
	return nil
}

func validatePositiveInteger(value string) error {
	integerValue, parseError := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if parseError != nil || integerValue <= 0 {
		return errors.New("expected positive integer")
	}
	return nil
}

func validateURL(value string) error {
	parsedURL, parseError := url.Parse(strings.TrimSpace(value))
	if parseError != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return errors.New("expected absolute URL")
	}
	return nil
}
