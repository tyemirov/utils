package viperconfig

import (
	"encoding/json"
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

func TestReporterRequiresConfigPath(testingHandle *testing.T) {
	_, err := NewReporter(" ", nil, passthroughRedactor{})
	if err == nil {
		testingHandle.Fatalf("expected config path error")
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
