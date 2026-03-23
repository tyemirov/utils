package viperconfig

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tyemirov/utils/preflight"
)

type passthroughRedactor struct{}

func (passthroughRedactor) Redact(settings map[string]interface{}, mode preflight.RedactionMode) (json.RawMessage, error) {
	payloadBytes, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	return payloadBytes, nil
}

func writeConfigFile(testingHandle *testing.T, contents string) string {
	testingHandle.Helper()
	configPath := filepath.Join(testingHandle.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		testingHandle.Fatalf("write config: %v", err)
	}
	return configPath
}

type errorRedactor struct {
	err error
}

func (r errorRedactor) Redact(_ map[string]interface{}, _ preflight.RedactionMode) (json.RawMessage, error) {
	return nil, r.err
}

func TestReporterRequiresRedactor(testingHandle *testing.T) {
	_, err := NewReporter("config.yaml", nil, nil)
	if err == nil {
		testingHandle.Fatalf("expected redactor error")
	}
}

func TestReporterRequiresConfigPath(testingHandle *testing.T) {
	_, err := NewReporter(" ", nil, passthroughRedactor{})
	if err == nil {
		testingHandle.Fatalf("expected config path error")
	}
}

func TestReporterSkipsEmptyEnvBindings(testingHandle *testing.T) {
	configPath := writeConfigFile(testingHandle, `key: value`)
	reporter, err := NewReporter(configPath, []EnvBinding{{Key: "", Env: "X"}, {Key: "Y", Env: ""}}, passthroughRedactor{})
	if err != nil {
		testingHandle.Fatalf("new reporter: %v", err)
	}
	if len(reporter.envBindings) != 0 {
		testingHandle.Fatalf("expected empty bindings, got %d", len(reporter.envBindings))
	}
}

func TestReporterBuildBindEnvError(testingHandle *testing.T) {
	configPath := writeConfigFile(testingHandle, `key: value`)
	reporter, err := NewReporter(configPath, []EnvBinding{{Key: "k", Env: "V"}}, passthroughRedactor{})
	if err != nil {
		testingHandle.Fatalf("new reporter: %v", err)
	}
	reporter.bindEnvFn = func(_ ...string) error {
		return errors.New("bind env broken")
	}
	_, buildErr := reporter.Build(preflight.RedactionModeRedacted)
	if buildErr == nil {
		testingHandle.Fatalf("expected bind env error")
	}
	if !errors.Is(buildErr, ErrReporter) {
		testingHandle.Fatalf("expected ErrReporter, got %v", buildErr)
	}
}

func TestReporterBuildRedactError(testingHandle *testing.T) {
	configPath := writeConfigFile(testingHandle, `key: value`)
	reporter, err := NewReporter(configPath, nil, errorRedactor{err: errors.New("redact fail")})
	if err != nil {
		testingHandle.Fatalf("new reporter: %v", err)
	}
	_, buildErr := reporter.Build(preflight.RedactionModeRedacted)
	if buildErr == nil {
		testingHandle.Fatalf("expected redact error")
	}
}

func TestReporterBuildMissingConfigFile(testingHandle *testing.T) {
	reporter, err := NewReporter("/nonexistent/config.yaml", nil, passthroughRedactor{})
	if err != nil {
		testingHandle.Fatalf("new reporter: %v", err)
	}
	_, buildErr := reporter.Build(preflight.RedactionModeRedacted)
	if buildErr == nil {
		testingHandle.Fatalf("expected missing config error")
	}
}

func TestReporterBuildNoExtension(testingHandle *testing.T) {
	tmpDir := testingHandle.TempDir()
	configPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(configPath, []byte("key: value\n"), 0o600); err != nil {
		testingHandle.Fatalf("write config: %v", err)
	}
	reporter, err := NewReporter(configPath, nil, passthroughRedactor{})
	if err != nil {
		testingHandle.Fatalf("new reporter: %v", err)
	}
	reportBytes, buildErr := reporter.Build(preflight.RedactionModeRedacted)
	if buildErr != nil {
		testingHandle.Fatalf("build error: %v", buildErr)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(reportBytes, &settings); err != nil {
		testingHandle.Fatalf("decode: %v", err)
	}
	if settings["key"] != "value" {
		testingHandle.Fatalf("expected key=value, got %v", settings["key"])
	}
}

func TestReporterLoadsEnvBindings(testingHandle *testing.T) {
	testingHandle.Setenv("SERVICE_NAME", "override")
	configPath := writeConfigFile(testingHandle, `
service:
  name: "default"
`)
	reporter, err := NewReporter(configPath, []EnvBinding{{Key: "service.name", Env: "SERVICE_NAME"}}, passthroughRedactor{})
	if err != nil {
		testingHandle.Fatalf("new reporter: %v", err)
	}
	reportBytes, reportErr := reporter.Build(preflight.RedactionModeRedacted)
	if reportErr != nil {
		testingHandle.Fatalf("build report: %v", reportErr)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(reportBytes, &settings); err != nil {
		testingHandle.Fatalf("decode report: %v", err)
	}
	serviceValue, ok := settings["service"].(map[string]interface{})
	if !ok {
		testingHandle.Fatalf("expected service settings")
	}
	if serviceValue["name"] != "override" {
		testingHandle.Fatalf("expected env override, got %v", serviceValue["name"])
	}
}
