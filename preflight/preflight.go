package preflight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrReport indicates the report could not be built.
var ErrReport = errors.New("preflight.report.invalid")

const (
	errorCodeMissingSchemaVersion = "preflight.report.missing_schema_version"
	errorCodeMissingReporter      = "preflight.report.missing_reporter"
	errorCodeMissingServiceInfo   = "preflight.report.missing_service_info"
	errorCodeBuildConfig          = "preflight.report.build_config"
	errorCodeBuildDependencies    = "preflight.report.build_dependencies"
	errorCodeEncodeReport         = "preflight.report.encode"
)

// ConfigReporter builds the effective config payload.
type ConfigReporter interface {
	Build(mode RedactionMode) (json.RawMessage, error)
}

// DependencyChecker validates external dependencies.
type DependencyChecker interface {
	Check(ctx context.Context) (DependencyStatus, error)
}

// DependencyStatus describes one dependency check.
type DependencyStatus struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Ready   bool              `json:"ready"`
	Details map[string]string `json:"details,omitempty"`
}

type reportPayload struct {
	SchemaVersion   string             `json:"schema_version"`
	Service         servicePayload     `json:"service"`
	EffectiveConfig json.RawMessage    `json:"effective_config"`
	Dependencies    []DependencyStatus `json:"dependencies"`
}

type servicePayload struct {
	Name                    string `json:"service_name"`
	Version                 string `json:"version"`
	Commit                  string `json:"build_commit"`
	BuildTime               string `json:"build_time"`
	ConfigSchemaVersion     string `json:"config_schema_version"`
	EndpointContractVersion string `json:"endpoint_contract_version"`
}

// BuildReport assembles a preflight report.
func BuildReport(ctx context.Context, schemaVersion string, serviceInfo ServiceInfo, reporter ConfigReporter, checkers []DependencyChecker, mode RedactionMode) ([]byte, error) {
	if schemaVersion == "" {
		return nil, fmt.Errorf("%w: %s", ErrReport, errorCodeMissingSchemaVersion)
	}
	if reporter == nil {
		return nil, fmt.Errorf("%w: %s", ErrReport, errorCodeMissingReporter)
	}
	if serviceInfo == nil {
		return nil, fmt.Errorf("%w: %s", ErrReport, errorCodeMissingServiceInfo)
	}

	configPayload, configErr := reporter.Build(mode)
	if configErr != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrReport, errorCodeBuildConfig, configErr)
	}

	dependencies := make([]DependencyStatus, 0, len(checkers))
	for _, checker := range checkers {
		status, statusErr := checker.Check(ctx)
		if statusErr != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrReport, errorCodeBuildDependencies, statusErr)
		}
		dependencies = append(dependencies, status)
	}

	report := reportPayload{
		SchemaVersion: schemaVersion,
		Service: servicePayload{
			Name:                    serviceInfo.Name(),
			Version:                 serviceInfo.Version(),
			Commit:                  serviceInfo.Commit(),
			BuildTime:               serviceInfo.BuildTime(),
			ConfigSchemaVersion:     serviceInfo.ConfigSchemaVersion(),
			EndpointContractVersion: serviceInfo.EndpointContractVersion(),
		},
		EffectiveConfig: configPayload,
		Dependencies:    dependencies,
	}

	reportBytes, marshalErr := json.MarshalIndent(report, "", "  ")
	if marshalErr != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrReport, errorCodeEncodeReport, marshalErr)
	}
	return append(reportBytes, '\n'), nil
}
