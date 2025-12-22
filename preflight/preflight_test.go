package preflight

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

const (
	testSchemaVersion      = "schema"
	testServiceName        = "tauth"
	testEffectiveConfigKey = "key"
	testEffectiveConfigVal = "value"
)

type stubReporter struct {
	payload json.RawMessage
	err     error
}

func (reporter stubReporter) Build(mode RedactionMode) (json.RawMessage, error) {
	if reporter.err != nil {
		return nil, reporter.err
	}
	return reporter.payload, nil
}

type stubChecker struct {
	status DependencyStatus
	err    error
}

func (checker stubChecker) Check(ctx context.Context) (DependencyStatus, error) {
	if checker.err != nil {
		return DependencyStatus{}, checker.err
	}
	return checker.status, nil
}

type testReportPayload struct {
	SchemaVersion   string             `json:"schema_version"`
	Service         testServicePayload `json:"service"`
	EffectiveConfig map[string]string  `json:"effective_config"`
	Dependencies    []DependencyStatus `json:"dependencies"`
}

type testServicePayload struct {
	Name string `json:"service_name"`
}

func TestNewServiceInfoRequiresName(testingHandle *testing.T) {
	_, err := NewServiceInfo(" ", "", "", "", "", "")
	if err == nil || !errors.Is(err, ErrServiceInfo) {
		testingHandle.Fatalf("expected service info error, got %v", err)
	}
}

func TestBuildReportRequiresReporter(testingHandle *testing.T) {
	serviceInfo, err := NewServiceInfo(testServiceName, "", "", "", "", "")
	if err != nil {
		testingHandle.Fatalf("service info: %v", err)
	}
	_, reportErr := BuildReport(context.Background(), testSchemaVersion, serviceInfo, nil, nil, RedactionModeRedacted)
	if reportErr == nil {
		testingHandle.Fatalf("expected report error")
	}
}

func TestBuildReportRequiresServiceInfo(testingHandle *testing.T) {
	reporter := stubReporter{payload: json.RawMessage(`{"` + testEffectiveConfigKey + `":"` + testEffectiveConfigVal + `"}`)}
	_, reportErr := BuildReport(context.Background(), testSchemaVersion, nil, reporter, nil, RedactionModeRedacted)
	if reportErr == nil {
		testingHandle.Fatalf("expected report error")
	}
}

func TestBuildReportAssemblesPayload(testingHandle *testing.T) {
	serviceInfo, err := NewServiceInfo(testServiceName, "1.0.0", "commit", "time", testSchemaVersion, "endpoint")
	if err != nil {
		testingHandle.Fatalf("service info: %v", err)
	}
	reporter := stubReporter{payload: json.RawMessage(`{"` + testEffectiveConfigKey + `":"` + testEffectiveConfigVal + `"}`)}
	checker := stubChecker{
		status: DependencyStatus{
			Name:  "refresh_store",
			Type:  "memory",
			Ready: true,
		},
	}

	reportBytes, reportErr := BuildReport(context.Background(), testSchemaVersion, serviceInfo, reporter, []DependencyChecker{checker}, RedactionModeRedacted)
	if reportErr != nil {
		testingHandle.Fatalf("build report: %v", reportErr)
	}
	var payload testReportPayload
	if err := json.Unmarshal(reportBytes, &payload); err != nil {
		testingHandle.Fatalf("decode report: %v", err)
	}
	if payload.SchemaVersion != testSchemaVersion || payload.Service.Name != testServiceName {
		testingHandle.Fatalf("unexpected report metadata")
	}
	if payload.EffectiveConfig[testEffectiveConfigKey] != testEffectiveConfigVal {
		testingHandle.Fatalf("unexpected effective config")
	}
	if len(payload.Dependencies) != 1 || payload.Dependencies[0].Name != "refresh_store" {
		testingHandle.Fatalf("unexpected dependency payload")
	}
}
