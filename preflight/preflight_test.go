package preflight

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func TestBuildReportRequiresSchemaVersion(testingHandle *testing.T) {
	serviceInfo, err := NewServiceInfo(testServiceName, "", "", "", "", "")
	if err != nil {
		testingHandle.Fatalf("service info: %v", err)
	}
	reporter := stubReporter{payload: json.RawMessage(`{}`)}
	_, reportErr := BuildReport(context.Background(), "", serviceInfo, reporter, nil, RedactionModeRedacted)
	if reportErr == nil || !errors.Is(reportErr, ErrReport) {
		testingHandle.Fatalf("expected ErrReport for missing schema version, got %v", reportErr)
	}
}

func TestBuildReportConfigError(testingHandle *testing.T) {
	serviceInfo, err := NewServiceInfo(testServiceName, "", "", "", "", "")
	if err != nil {
		testingHandle.Fatalf("service info: %v", err)
	}
	reporter := stubReporter{err: errors.New("config broken")}
	_, reportErr := BuildReport(context.Background(), testSchemaVersion, serviceInfo, reporter, nil, RedactionModeRedacted)
	if reportErr == nil || !errors.Is(reportErr, ErrReport) {
		testingHandle.Fatalf("expected ErrReport for config error, got %v", reportErr)
	}
}

func TestBuildReportCheckerError(testingHandle *testing.T) {
	serviceInfo, err := NewServiceInfo(testServiceName, "", "", "", "", "")
	if err != nil {
		testingHandle.Fatalf("service info: %v", err)
	}
	reporter := stubReporter{payload: json.RawMessage(`{}`)}
	badChecker := stubChecker{err: errors.New("check failed")}
	_, reportErr := BuildReport(context.Background(), testSchemaVersion, serviceInfo, reporter, []DependencyChecker{badChecker}, RedactionModeRedacted)
	if reportErr == nil || !errors.Is(reportErr, ErrReport) {
		testingHandle.Fatalf("expected ErrReport for checker error, got %v", reportErr)
	}
}

func TestHashSHA256Hex(testingHandle *testing.T) {
	input := []byte("hello")
	expected := sha256.Sum256(input)
	expectedHex := hex.EncodeToString(expected[:])
	got := HashSHA256Hex(input)
	if got != expectedHex {
		testingHandle.Fatalf("expected %s, got %s", expectedHex, got)
	}
}

func TestNewServiceInfoSuccess(testingHandle *testing.T) {
	info, err := NewServiceInfo("svc", "v1", "abc", "now", "s1", "e1")
	if err != nil {
		testingHandle.Fatalf("unexpected error: %v", err)
	}
	if info.Name() != "svc" || info.Version() != "v1" || info.Commit() != "abc" || info.BuildTime() != "now" || info.ConfigSchemaVersion() != "s1" || info.EndpointContractVersion() != "e1" {
		testingHandle.Fatalf("unexpected service info values")
	}
}

func TestServiceInfoMarkerMethod(testingHandle *testing.T) {
	info, err := NewServiceInfo("svc", "", "", "", "", "")
	if err != nil {
		testingHandle.Fatalf("unexpected error: %v", err)
	}
	info.(serviceInfo).serviceInfo()
}

func TestBuildReportMarshalError(testingHandle *testing.T) {
	serviceInfo, err := NewServiceInfo(testServiceName, "", "", "", "", "")
	if err != nil {
		testingHandle.Fatalf("service info: %v", err)
	}
	// invalid json.RawMessage will cause json.MarshalIndent to fail
	reporter := stubReporter{payload: json.RawMessage(`{invalid`)}
	_, reportErr := BuildReport(context.Background(), testSchemaVersion, serviceInfo, reporter, nil, RedactionModeRedacted)
	if reportErr == nil || !errors.Is(reportErr, ErrReport) {
		testingHandle.Fatalf("expected ErrReport for marshal error, got %v", reportErr)
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
