package runtimeconfig

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tyemirov/utils/configfile"
)

const (
	testEnvEnabled = "RUNTIMECONFIG_TEST_ENABLED"
	testEnvHost    = "RUNTIMECONFIG_TEST_HOST"
	testEnvPort    = "RUNTIMECONFIG_TEST_PORT"
	testEnvSecret  = "RUNTIMECONFIG_TEST_SECRET"
)

type runtimeConfigFixture struct {
	Server runtimeServerFixture `yaml:"server"`
}

type runtimeServerFixture struct {
	Enabled bool   `yaml:"enabled"`
	BaseURL string `yaml:"base_url"`
	Port    int    `yaml:"port"`
	Secret  string `yaml:"secret"`
}

type runtimeSectionFixture struct {
	Port int `yaml:"port"`
}

func TestLoaderLoadsTypedConfigSettingsAndValues(testingHandle *testing.T) {
	configPath := writeRuntimeConfigFile(testingHandle, "config.yml", `
server:
  enabled: ${RUNTIMECONFIG_TEST_ENABLED}
  base_url: "https://${RUNTIMECONFIG_TEST_HOST}/api"
  port: ${RUNTIMECONFIG_TEST_PORT}
  secret: ${RUNTIMECONFIG_TEST_SECRET}
`)
	loader, loaderError := NewLoader[runtimeConfigFixture](Contract[runtimeConfigFixture]{
		ExpansionLookup: mapRuntimeExpansionLookup(map[string]string{
			testEnvEnabled: "true",
			testEnvHost:    "service.example.invalid",
			testEnvPort:    "8080",
			testEnvSecret:  "secret-value",
		}),
		ValueMappings: []ValueMapping{
			{Key: "BASE_URL", Path: []string{"server", "base_url"}},
			{Key: "PORT", Path: []string{"server", "port"}},
		},
		Validate: func(configuration runtimeConfigFixture) error {
			if configuration.Server.Port < 1024 {
				return errors.New("server.port must be at least 1024")
			}
			return nil
		},
	})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}

	loaded, loadError := loader.Load(configPath)
	if loadError != nil {
		testingHandle.Fatalf("Load returned error: %v", loadError)
	}

	if !loaded.Config.Server.Enabled || loaded.Config.Server.Port != 8080 {
		testingHandle.Fatalf("unexpected typed config: %#v", loaded.Config)
	}
	if loaded.Config.Server.BaseURL != "https://service.example.invalid/api" {
		testingHandle.Fatalf("unexpected base URL: %s", loaded.Config.Server.BaseURL)
	}
	if loaded.Path != configPath {
		testingHandle.Fatalf("expected loaded path %q, got %q", configPath, loaded.Path)
	}
	if strings.Contains(string(loaded.EffectiveYAML), testEnvSecret) {
		testingHandle.Fatalf("expected effective YAML to be expanded, got %s", loaded.EffectiveYAML)
	}
	if setting := nestedRuntimeSetting(testingHandle, loaded.Settings, "server", "base_url"); setting != "https://service.example.invalid/api" {
		testingHandle.Fatalf("unexpected effective setting: %#v", setting)
	}
	if value, found := loaded.Values.Lookup("PORT"); !found || value != "8080" {
		testingHandle.Fatalf("unexpected mapped port found=%v value=%q", found, value)
	}
	if loaded.Values.Resolve(" BASE_URL ") != "https://service.example.invalid/api" {
		testingHandle.Fatalf("unexpected resolved base URL")
	}
	valueMap := loaded.Values.Map()
	valueMap["PORT"] = "mutated"
	if loaded.Values.Resolve("PORT") != "8080" {
		testingHandle.Fatalf("expected ConfigValues.Map to return a defensive copy")
	}
	if loaded.Values.Resolver()("BASE_URL") != "https://service.example.invalid/api" {
		testingHandle.Fatalf("unexpected resolver output")
	}
}

func TestLoaderRejectsMissingExpansionReference(testingHandle *testing.T) {
	configPath := writeRuntimeConfigFile(testingHandle, "config.yml", `
left: ${RUNTIMECONFIG_TEST_SECRET}
right: ${RUNTIMECONFIG_TEST_HOST}
`)
	loader, loaderError := NewLoader[map[string]string](Contract[map[string]string]{
		ExpansionLookup: mapRuntimeExpansionLookup(nil),
	})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	_, loadError := loader.Load(configPath)
	if !errors.Is(loadError, configfile.ErrMissingEnvironmentVariables) {
		testingHandle.Fatalf("expected missing expansion reference error, got %v", loadError)
	}
	if !strings.Contains(loadError.Error(), testEnvSecret) {
		testingHandle.Fatalf("expected error to include reference name, got %v", loadError)
	}
	if !strings.Contains(loadError.Error(), testEnvHost) {
		testingHandle.Fatalf("expected error to include second reference name, got %v", loadError)
	}
}

func TestLoaderRejectsValidationAndNonScalarValueMappings(testingHandle *testing.T) {
	loader, loaderError := NewLoader[runtimeConfigFixture](Contract[runtimeConfigFixture]{
		ExpansionLookup: mapRuntimeExpansionLookup(map[string]string{testEnvPort: "80"}),
		ValueMappings: []ValueMapping{
			{Key: "SERVER", Path: []string{"server"}},
		},
		Validate: func(configuration runtimeConfigFixture) error {
			if configuration.Server.Port < 1024 {
				return errors.New("server.port must be at least 1024")
			}
			return nil
		},
	})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	configPath := writeRuntimeConfigFile(testingHandle, "config.yml", `
server:
  port: ${RUNTIMECONFIG_TEST_PORT}
`)
	_, loadError := loader.Load(configPath)
	if !errors.Is(loadError, ErrValidation) {
		testingHandle.Fatalf("expected validation error, got %v", loadError)
	}

	loaderWithoutValidation, loaderError := NewLoader[runtimeConfigFixture](Contract[runtimeConfigFixture]{
		ExpansionLookup: mapRuntimeExpansionLookup(map[string]string{testEnvPort: "8080"}),
		ValueMappings: []ValueMapping{
			{Key: "SERVER", Path: []string{"server"}},
		},
	})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	_, loadError = loaderWithoutValidation.Load(configPath)
	if !errors.Is(loadError, ErrInvalidValueMapping) {
		testingHandle.Fatalf("expected invalid value mapping error, got %v", loadError)
	}
}

func TestLoaderLoadsSection(testingHandle *testing.T) {
	configPath := writeRuntimeConfigFile(testingHandle, "config.yml", `
web:
  port: 8080
worker:
  port: ${RUNTIMECONFIG_TEST_PORT}
`)
	loader, loaderError := NewLoader[runtimeSectionFixture](Contract[runtimeSectionFixture]{
		ExpansionLookup: mapRuntimeExpansionLookup(map[string]string{testEnvPort: "9090"}),
		ValueMappings: []ValueMapping{
			{Key: "PORT", Path: []string{"port"}},
		},
	})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}

	loaded, loadError := loader.LoadSection(configPath, []string{"worker"})
	if loadError != nil {
		testingHandle.Fatalf("LoadSection returned error: %v", loadError)
	}
	if loaded.Config.Port != 9090 || loaded.Values.Resolve("PORT") != "9090" {
		testingHandle.Fatalf("unexpected section config: %#v values=%#v", loaded.Config, loaded.Values.Map())
	}
	_, loadError = loader.LoadSection(configPath, []string{"missing"})
	if !errors.Is(loadError, ErrMissingSection) {
		testingHandle.Fatalf("expected missing section error, got %v", loadError)
	}
}

func TestLoaderRejectsInvalidOptions(testingHandle *testing.T) {
	testCases := []struct {
		name     string
		contract Contract[map[string]string]
	}{
		{name: "blank value key", contract: Contract[map[string]string]{ValueMappings: []ValueMapping{{Key: " ", Path: []string{"value"}}}}},
		{name: "missing value path", contract: Contract[map[string]string]{ValueMappings: []ValueMapping{{Key: "VALUE"}}}},
		{name: "duplicate value key", contract: Contract[map[string]string]{ValueMappings: []ValueMapping{{Key: "VALUE", Path: []string{"one"}}, {Key: " VALUE ", Path: []string{"two"}}}}},
	}
	for _, testCase := range testCases {
		testingHandle.Run(testCase.name, func(testingHandle *testing.T) {
			_, loaderError := NewLoader[map[string]string](testCase.contract)
			if !errors.Is(loaderError, ErrInvalidOptions) {
				testingHandle.Fatalf("expected invalid options error, got %v", loaderError)
			}
		})
	}
}

func TestLoaderResolvesDefaultConfigPath(testingHandle *testing.T) {
	tempDir := testingHandle.TempDir()
	defaultPath := filepath.Join(tempDir, DefaultConfigPath)
	if writeError := os.WriteFile(defaultPath, []byte("value: ok\n"), 0o600); writeError != nil {
		testingHandle.Fatalf("write default config: %v", writeError)
	}
	loader, loaderError := NewLoader[map[string]string](Contract[map[string]string]{
		DefaultConfigPath: defaultPath,
	})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	resolvedPath, resolveError := loader.ResolvePath(" ")
	if resolveError != nil {
		testingHandle.Fatalf("ResolvePath returned error: %v", resolveError)
	}
	if resolvedPath != defaultPath {
		testingHandle.Fatalf("expected %q, got %q", defaultPath, resolvedPath)
	}

	missingLoader, loaderError := NewLoader[map[string]string](Contract[map[string]string]{
		DefaultConfigPath: filepath.Join(tempDir, "missing.yml"),
	})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	_, resolveError = missingLoader.ResolvePath("")
	if !errors.Is(resolveError, ErrMissingConfig) {
		testingHandle.Fatalf("expected missing config error, got %v", resolveError)
	}

	testingHandle.Run("zero loader default", func(testingHandle *testing.T) {
		tempDir := testingHandle.TempDir()
		if writeError := os.WriteFile(filepath.Join(tempDir, DefaultConfigPath), []byte("value: ok\n"), 0o600); writeError != nil {
			testingHandle.Fatalf("write default config: %v", writeError)
		}
		testingHandle.Chdir(tempDir)
		var zeroLoader Loader[map[string]string]
		resolvedPath, resolveError := zeroLoader.ResolvePath("")
		if resolveError != nil {
			testingHandle.Fatalf("zero ResolvePath returned error: %v", resolveError)
		}
		if resolvedPath != DefaultConfigPath {
			testingHandle.Fatalf("expected default config path, got %q", resolvedPath)
		}
	})

	statErrorLoader, loaderError := NewLoader[map[string]string](Contract[map[string]string]{
		DefaultConfigPath: "bad\x00path",
	})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	_, resolveError = statErrorLoader.ResolvePath("")
	if !errors.Is(resolveError, ErrRead) {
		testingHandle.Fatalf("expected stat read error, got %v", resolveError)
	}
}

func TestLoaderReportsReadAndResolveErrors(testingHandle *testing.T) {
	missingDefaultLoader, loaderError := NewLoader[map[string]string](Contract[map[string]string]{
		DefaultConfigPath: filepath.Join(testingHandle.TempDir(), "missing.yml"),
	})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	if _, loadError := missingDefaultLoader.Load(""); !errors.Is(loadError, ErrMissingConfig) {
		testingHandle.Fatalf("expected load resolve error, got %v", loadError)
	}
	if _, loadError := missingDefaultLoader.Load(filepath.Join(testingHandle.TempDir(), "also-missing.yml")); !errors.Is(loadError, ErrRead) {
		testingHandle.Fatalf("expected load read error, got %v", loadError)
	}
	if _, loadError := missingDefaultLoader.LoadSection("", []string{"web"}); !errors.Is(loadError, ErrMissingConfig) {
		testingHandle.Fatalf("expected section resolve error, got %v", loadError)
	}
	if _, loadError := missingDefaultLoader.LoadSection(filepath.Join(testingHandle.TempDir(), "missing-section.yml"), []string{"web"}); !errors.Is(loadError, ErrRead) {
		testingHandle.Fatalf("expected section read error, got %v", loadError)
	}
}

func TestLoaderReportsParseExpansionDecodeAndSettingsErrors(testingHandle *testing.T) {
	malformedPath := writeRuntimeConfigFile(testingHandle, "malformed.yml", ":\n")
	loader, loaderError := NewLoader[map[string]string](Contract[map[string]string]{})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	if _, loadError := loader.Load(malformedPath); !errors.Is(loadError, ErrParse) {
		testingHandle.Fatalf("expected config parse error, got %v", loadError)
	}

	missingEnvPath := writeRuntimeConfigFile(testingHandle, "missing-env.yml", "value: ${RUNTIMECONFIG_TEST_SECRET}\n")
	loader, loaderError = NewLoader[map[string]string](Contract[map[string]string]{
		ExpansionLookup: mapRuntimeExpansionLookup(nil),
	})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	if _, loadError := loader.Load(missingEnvPath); !errors.Is(loadError, configfile.ErrMissingEnvironmentVariables) {
		testingHandle.Fatalf("expected missing expansion reference error, got %v", loadError)
	}

	unknownFieldPath := writeRuntimeConfigFile(testingHandle, "unknown.yml", "unknown: true\n")
	typedLoader, loaderError := NewLoader[runtimeConfigFixture](Contract[runtimeConfigFixture]{})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	if _, loadError := typedLoader.Load(unknownFieldPath); !errors.Is(loadError, ErrParse) {
		testingHandle.Fatalf("expected decode parse error, got %v", loadError)
	}

	scalarPath := writeRuntimeConfigFile(testingHandle, "scalar.yml", "plain\n")
	scalarLoader, loaderError := NewLoader[string](Contract[string]{})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	if _, loadError := scalarLoader.Load(scalarPath); !errors.Is(loadError, ErrParse) {
		testingHandle.Fatalf("expected settings parse error, got %v", loadError)
	}
}

func TestLoaderLoadsNullSettingsAsEmptyMap(testingHandle *testing.T) {
	configPath := writeRuntimeConfigFile(testingHandle, "null.yml", "null\n")
	loader, loaderError := NewLoader[any](Contract[any]{})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	loaded, loadError := loader.Load(configPath)
	if loadError != nil {
		testingHandle.Fatalf("Load returned error: %v", loadError)
	}
	if len(loaded.Settings) != 0 {
		testingHandle.Fatalf("expected empty settings, got %#v", loaded.Settings)
	}
}

func TestLoaderHandlesExtraRequirementsAndScalarValueMappings(testingHandle *testing.T) {
	configPath := writeRuntimeConfigFile(testingHandle, "values.yml", `
server:
  name: " app "
  count: 12
  ratio: 1.5
  enabled: true
  empty: null
`)
	loader, loaderError := NewLoader[map[string]any](Contract[map[string]any]{
		ValueMappings: []ValueMapping{
			{Key: "NAME", Path: []string{"server", "name"}},
			{Key: "COUNT", Path: []string{"server", "count"}},
			{Key: "RATIO", Path: []string{"server", "ratio"}},
			{Key: "ENABLED", Path: []string{"server", "enabled"}},
			{Key: "EMPTY", Path: []string{"server", "empty"}},
			{Key: "MISSING", Path: []string{"server", "missing"}},
			{Key: "NOT_MAP", Path: []string{"server", "count", "leaf"}},
		},
	})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	loaded, loadError := loader.Load(configPath)
	if loadError != nil {
		testingHandle.Fatalf("Load returned error: %v", loadError)
	}
	expectedValues := map[string]string{
		"NAME":    "app",
		"COUNT":   "12",
		"RATIO":   "1.5",
		"ENABLED": "true",
		"EMPTY":   "",
	}
	if !reflect.DeepEqual(loaded.Values.Map(), expectedValues) {
		testingHandle.Fatalf("expected values %#v, got %#v", expectedValues, loaded.Values.Map())
	}
}

func TestLoaderReportsSectionErrors(testingHandle *testing.T) {
	configPath := writeRuntimeConfigFile(testingHandle, "config.yml", "web:\n  port: 8080\n")
	loader, loaderError := NewLoader[runtimeSectionFixture](Contract[runtimeSectionFixture]{})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	if _, loadError := loader.LoadSection(configPath, nil); !errors.Is(loadError, ErrInvalidOptions) {
		testingHandle.Fatalf("expected invalid section path error, got %v", loadError)
	}
	malformedPath := writeRuntimeConfigFile(testingHandle, "bad-section.yml", ":\n")
	if _, loadError := loader.LoadSection(malformedPath, []string{"web"}); !errors.Is(loadError, ErrParse) {
		testingHandle.Fatalf("expected section parse error, got %v", loadError)
	}
	scalarPath := writeRuntimeConfigFile(testingHandle, "scalar-section.yml", "plain\n")
	if _, loadError := loader.LoadSection(scalarPath, []string{"web"}); !errors.Is(loadError, ErrMissingSection) {
		testingHandle.Fatalf("expected scalar missing section error, got %v", loadError)
	}
	trailingDocumentPath := writeRuntimeConfigFile(testingHandle, "trailing-section.yml", `
web:
  port: 8080
---
web:
  port: 9090
`)
	if _, loadError := loader.LoadSection(trailingDocumentPath, []string{"web"}); !errors.Is(loadError, ErrParse) {
		testingHandle.Fatalf("expected trailing document parse error, got %v", loadError)
	} else if !strings.Contains(loadError.Error(), "trailing YAML document") {
		testingHandle.Fatalf("expected trailing document error, got %v", loadError)
	}

	missingExpansionPath := writeRuntimeConfigFile(testingHandle, "section-missing-expansion.yml", `
web:
  port: ${RUNTIMECONFIG_TEST_PORT}
`)
	sectionLoader, loaderError := NewLoader[runtimeSectionFixture](Contract[runtimeSectionFixture]{
		ExpansionLookup: mapRuntimeExpansionLookup(nil),
	})
	if loaderError != nil {
		testingHandle.Fatalf("NewLoader returned error: %v", loaderError)
	}
	if _, loadError := sectionLoader.LoadSection(missingExpansionPath, []string{"web"}); !errors.Is(loadError, configfile.ErrMissingEnvironmentVariables) {
		testingHandle.Fatalf("expected section load payload error, got %v", loadError)
	}
}

func TestConfigValuesZeroValue(testingHandle *testing.T) {
	var values ConfigValues
	if value, found := values.Lookup("anything"); found || value != "" {
		testingHandle.Fatalf("expected zero-value lookup miss, found=%v value=%q", found, value)
	}
	if values.Resolve("anything") != "" {
		testingHandle.Fatalf("expected zero-value resolver miss")
	}
}

func TestRuntimeConfigInternalNilMappingGuards(testingHandle *testing.T) {
	if mappingChild(nil, "missing") != nil {
		testingHandle.Fatalf("expected nil mapping child")
	}
}

func writeRuntimeConfigFile(testingHandle *testing.T, name string, contents string) string {
	testingHandle.Helper()
	path := filepath.Join(testingHandle.TempDir(), name)
	if writeError := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o600); writeError != nil {
		testingHandle.Fatalf("write runtime config: %v", writeError)
	}
	return path
}

func mapRuntimeExpansionLookup(values map[string]string) ExpansionLookup {
	return func(name string) (string, bool) {
		value, found := values[name]
		return value, found
	}
}

func nestedRuntimeSetting(testingHandle *testing.T, settings map[string]any, path ...string) any {
	testingHandle.Helper()
	var current any = settings
	for _, element := range path {
		currentMap, ok := current.(map[string]any)
		if !ok {
			testingHandle.Fatalf("expected map at %s, got %#v", element, current)
		}
		current = currentMap[element]
	}
	return current
}
