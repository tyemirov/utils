package preflight

import (
	"errors"
	"fmt"
	"strings"
)

// ErrServiceInfo indicates the service metadata is invalid.
var ErrServiceInfo = errors.New("preflight.service_info.invalid")

const errorCodeMissingServiceName = "preflight.service_info.missing_name"

// ServiceInfo captures build and schema metadata.
type ServiceInfo interface {
	Name() string
	Version() string
	Commit() string
	BuildTime() string
	ConfigSchemaVersion() string
	EndpointContractVersion() string
	serviceInfo()
}

type serviceInfo struct {
	name                    string
	version                 string
	commit                  string
	buildTime               string
	configSchemaVersion     string
	endpointContractVersion string
}

// NewServiceInfo constructs ServiceInfo with required fields.
func NewServiceInfo(name string, version string, commit string, buildTime string, configSchemaVersion string, endpointContractVersion string) (ServiceInfo, error) {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return nil, fmt.Errorf("%w: %s", ErrServiceInfo, errorCodeMissingServiceName)
	}
	return serviceInfo{
		name:                    cleanName,
		version:                 strings.TrimSpace(version),
		commit:                  strings.TrimSpace(commit),
		buildTime:               strings.TrimSpace(buildTime),
		configSchemaVersion:     strings.TrimSpace(configSchemaVersion),
		endpointContractVersion: strings.TrimSpace(endpointContractVersion),
	}, nil
}

// Name returns the service name.
func (info serviceInfo) Name() string {
	return info.name
}

// Version returns the build version.
func (info serviceInfo) Version() string {
	return info.version
}

// Commit returns the git commit.
func (info serviceInfo) Commit() string {
	return info.commit
}

// BuildTime returns the build timestamp.
func (info serviceInfo) BuildTime() string {
	return info.buildTime
}

// ConfigSchemaVersion returns the config schema version.
func (info serviceInfo) ConfigSchemaVersion() string {
	return info.configSchemaVersion
}

// EndpointContractVersion returns the endpoint contract version.
func (info serviceInfo) EndpointContractVersion() string {
	return info.endpointContractVersion
}

func (info serviceInfo) serviceInfo() {}
