// Package system contains utilities for interacting with the host environment
// such as reading environment variables.
package system

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// logFatalf is the function used by GetEnvOrFail to terminate on missing variables.
// Tests may override it to avoid process exit.
var logFatalf = log.Fatalf

// GetEnvOrFail retrieves the value of the environment variable with the given
// name. If the variable is not set, the process exits via log.Fatalf.
func GetEnvOrFail(name string) string {
	value := os.Getenv(name)
	if value == "" {
		logFatalf("%s environment variable not set", name)
	}
	return value
}

// ExpandEnvVar expands an environment variable reference and returns its value
// trimmed of surrounding spaces. An error is returned if the referenced
// variable is not set.
func ExpandEnvVar(envVar string) (string, error) {
	trimmedEnvVar := strings.TrimSpace(envVar)
	if envValue := os.ExpandEnv(trimmedEnvVar); envValue != "" {
		return strings.TrimSpace(envValue), nil
	}
	return "", fmt.Errorf("environment variable %s is not setup", trimmedEnvVar)
}
