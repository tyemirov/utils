package system

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandEnvVar(t *testing.T) {
	tests := []struct {
		name       string
		envKey     string
		envValue   string
		input      string
		expectFail bool
		expected   string
	}{
		{"VariableExists", "TEST_VAR", "value", "$TEST_VAR", false, "value"},
		{"VariableNotSet", "MISSING_VAR", "", "$MISSING_VAR", true, ""},
		{"MultipleVariables", "MULTI_VAR", "multi_value", "$MULTI_VAR/$TEST_VAR", false, "multi_value/value"},
		{"NoExpansionNeeded", "", "", "NoExpansion", false, "NoExpansion"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				err := os.Setenv(tt.envKey, tt.envValue)
				if err != nil {
					t.Fatalf("failed to set environment variable %s: %v", tt.envKey, err)
				}
			} else {
				_ = os.Unsetenv(tt.envKey)
			}

			result, err := ExpandEnvVar(tt.input)
			if tt.expectFail {
				if err == nil {
					t.Errorf("expected error but got none for input: %s", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("did not expect error but got: %v for input: %s", err, tt.input)
				}
				if result != tt.expected {
					t.Errorf("expected: %s, got: %s", tt.expected, result)
				}
			}
		})
	}
}

func TestGetEnvOrFailExternal(t *testing.T) {
	tests := []struct {
		name        string
		envKey      string
		envValue    string
		expectFail  bool // Expect program to exit with error
		expectedLog string
	}{
		{"EnvVarExists", "TEST_VAR", "value", false, "Environment variable value: value"}, // Expected output for success
		{"EnvVarNotSet", "TEST_VAR", "", true, "TEST_VAR environment variable not set"},
		{"EnvVarEmptyValue", "TEST_VAR", "", true, "TEST_VAR environment variable not set"},
		{"CaseSensitivity", "test_var", "value", false, "Environment variable value: value"}, // Case sensitivity depends on OS, assuming case-sensitive for this example
	}

	// Create a dummy main.go for external testing in a temp directory
	tmpDir := t.TempDir()
	mainFile := filepath.Join(tmpDir, "main.go")
	mainCode := `package main

import (
	"fmt"
	"os"
	"github.com/tyemirov/utils/system"
)

func main() {
	envVarName := os.Args[1]
	value := system.GetEnvOrFail(envVarName)
	fmt.Println("Environment variable value:", value)
}
`
	err := os.WriteFile(mainFile, []byte(mainCode), 0644)
	if err != nil {
		t.Fatalf("failed to create temp main.go: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the command to run the test program - now running main.go and passing env key as arg
			cmd := exec.Command("go", "run", mainFile, tt.envKey)

			// Set environment variables for the command
			env := os.Environ()    // Inherit current env
			if tt.envValue != "" { // Only set if value is not empty to test "not set" and "empty value" cases
				env = append(env, tt.envKey+"="+tt.envValue)
			} else {
				// Unset the variable explicitly to ensure "not set" is tested correctly
				os.Unsetenv(tt.envKey) // Unset for the test environment as well
				var newEnv []string
				for _, e := range env {
					if !strings.HasPrefix(e, tt.envKey+"=") {
						newEnv = append(newEnv, e)
					}
				}
				env = newEnv
			}

			cmd.Env = env

			// Redirect stderr and stdout
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			var stdout bytes.Buffer
			cmd.Stdout = &stdout

			// Run the command
			err := cmd.Run()

			// Check if the command failed as expected
			if tt.expectFail {
				if err == nil {
					t.Fatalf("expected command to fail, but it succeeded. Stdout: %s, Stderr: %s", stdout.String(), stderr.String())
				}
				exitError, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("expected *exec.ExitError, got: %v", err)
				}
				if !exitError.Exited() || exitError.ExitCode() != 1 {
					t.Errorf("expected exit code 1, got: %v, Stderr: %s", exitError.ExitCode(), stderr.String())
				}

				// Check if the log message is as expected in stderr
				if !strings.Contains(stderr.String(), tt.expectedLog) {
					t.Errorf("expected stderr to contain '%s', got: '%s'", tt.expectedLog, stderr.String())
				}

			} else {
				if err != nil {
					t.Fatalf("expected command to succeed, but it failed: %v, Stderr: %s, Stdout: %s", err, stderr.String(), stdout.String())
				}
				// Check if the stdout contains the expected value for success case
				if !strings.Contains(stdout.String(), tt.expectedLog) {
					t.Errorf("expected stdout to contain '%s', got: '%s'", tt.expectedLog, stdout.String())
				}
			}
		})
	}
}
