// Package runtimeconfig loads strict YAML runtime configuration through one
// interpolation boundary.
package runtimeconfig

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/tyemirov/utils/configfile"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigPath is the conventional runtime config file name.
	DefaultConfigPath = "config.yml"
)

var (
	// ErrInvalidOptions reports an invalid loader declaration.
	ErrInvalidOptions = errors.New("runtimeconfig.invalid_options")
	// ErrInvalidValueMapping reports a non-scalar effective value mapping.
	ErrInvalidValueMapping = errors.New("runtimeconfig.invalid_value_mapping")
	// ErrMissingConfig reports a missing runtime config file.
	ErrMissingConfig = errors.New("runtimeconfig.missing_config")
	// ErrMissingSection reports a missing required config section.
	ErrMissingSection = errors.New("runtimeconfig.missing_section")
	// ErrParse reports malformed YAML or target decode failures.
	ErrParse = errors.New("runtimeconfig.parse")
	// ErrRead reports config file read failures.
	ErrRead = errors.New("runtimeconfig.read")
	// ErrValidation reports application validation failures after strict decode.
	ErrValidation = errors.New("runtimeconfig.validation")
)

// ExpansionLookup resolves one YAML interpolation reference.
type ExpansionLookup func(name string) (string, bool)

// ValueMapping maps a scalar YAML path into the effective config value map.
type ValueMapping struct {
	Key  string
	Path []string
}

// Contract declares a typed runtime config shape and loading behavior.
type Contract[Config any] struct {
	DefaultConfigPath string
	ExpansionLookup   ExpansionLookup
	ValueMappings     []ValueMapping
	Validate          func(Config) error
}

// Loader loads typed runtime config values.
type Loader[Config any] struct {
	defaultConfigPath string
	expansionLookup   ExpansionLookup
	valueMappings     []ValueMapping
	validate          func(Config) error
}

// Loaded contains the typed config and reusable effective-config surfaces.
type Loaded[Config any] struct {
	Path          string
	Config        Config
	Settings      map[string]any
	Values        ConfigValues
	EffectiveYAML []byte
}

// ConfigValues exposes selected scalar values from the effective config.
type ConfigValues struct {
	values map[string]string
}

// NewLoader creates a typed runtime config loader.
func NewLoader[Config any](contract Contract[Config]) (Loader[Config], error) {
	defaultConfigPath := strings.TrimSpace(contract.DefaultConfigPath)
	if defaultConfigPath == "" {
		defaultConfigPath = DefaultConfigPath
	}
	valueMappings := make([]ValueMapping, 0, len(contract.ValueMappings))
	seenKeys := make(map[string]struct{}, len(contract.ValueMappings))
	for _, mapping := range contract.ValueMappings {
		normalizedKey := strings.TrimSpace(mapping.Key)
		if normalizedKey == "" {
			return Loader[Config]{}, fmt.Errorf("%w: value mapping key is required", ErrInvalidOptions)
		}
		if len(mapping.Path) == 0 {
			return Loader[Config]{}, fmt.Errorf("%w: value mapping %s path is required", ErrInvalidOptions, normalizedKey)
		}
		if _, exists := seenKeys[normalizedKey]; exists {
			return Loader[Config]{}, fmt.Errorf("%w: duplicate value mapping %s", ErrInvalidOptions, normalizedKey)
		}
		seenKeys[normalizedKey] = struct{}{}
		valueMappings = append(valueMappings, ValueMapping{
			Key:  normalizedKey,
			Path: normalizedPath(mapping.Path),
		})
	}
	return Loader[Config]{
		defaultConfigPath: defaultConfigPath,
		expansionLookup:   contract.ExpansionLookup,
		valueMappings:     valueMappings,
		validate:          contract.Validate,
	}, nil
}

// ResolvePath resolves an explicit config path or the loader default.
func (loader Loader[Config]) ResolvePath(configPath string) (string, error) {
	trimmedConfigPath := strings.TrimSpace(configPath)
	if trimmedConfigPath != "" {
		return trimmedConfigPath, nil
	}
	defaultConfigPath := strings.TrimSpace(loader.defaultConfigPath)
	if defaultConfigPath == "" {
		defaultConfigPath = DefaultConfigPath
	}
	if _, statError := os.Stat(defaultConfigPath); statError == nil {
		return defaultConfigPath, nil
	} else if !errors.Is(statError, os.ErrNotExist) {
		return "", fmt.Errorf("%w: stat %s: %w", ErrRead, defaultConfigPath, statError)
	}
	return "", fmt.Errorf("%w: --config is required when %s is absent", ErrMissingConfig, defaultConfigPath)
}

// Load reads, validates, expands, and decodes a complete runtime config file.
func (loader Loader[Config]) Load(configPath string) (Loaded[Config], error) {
	resolvedPath, resolveError := loader.ResolvePath(configPath)
	if resolveError != nil {
		return Loaded[Config]{}, resolveError
	}
	configPayload, readError := os.ReadFile(resolvedPath)
	if readError != nil {
		return Loaded[Config]{}, fmt.Errorf("%w: path=%s: %w", ErrRead, resolvedPath, readError)
	}
	loaded, loadError := loader.loadPayload(resolvedPath, configPayload)
	if loadError != nil {
		return Loaded[Config]{}, loadError
	}
	return loaded, nil
}

// LoadSection reads, validates, expands, and decodes one required YAML section.
func (loader Loader[Config]) LoadSection(configPath string, sectionPath []string) (Loaded[Config], error) {
	resolvedPath, resolveError := loader.ResolvePath(configPath)
	if resolveError != nil {
		return Loaded[Config]{}, resolveError
	}
	configPayload, readError := os.ReadFile(resolvedPath)
	if readError != nil {
		return Loaded[Config]{}, fmt.Errorf("%w: path=%s: %w", ErrRead, resolvedPath, readError)
	}
	sectionPayload, sectionError := sectionPayload(configPayload, sectionPath)
	if sectionError != nil {
		return Loaded[Config]{}, sectionError
	}
	loaded, loadError := loader.loadPayload(resolvedPath, sectionPayload)
	if loadError != nil {
		return Loaded[Config]{}, loadError
	}
	return loaded, nil
}

func (loader Loader[Config]) loadPayload(resolvedPath string, configPayload []byte) (Loaded[Config], error) {
	effectiveYAML, interpolationError := configfile.InterpolateYAMLWithOptions(configPayload, interpolationOptions(loader.expansionLookup))
	if interpolationError != nil {
		if errors.Is(interpolationError, configfile.ErrParse) {
			return Loaded[Config]{}, fmt.Errorf("%w: %w", ErrParse, interpolationError)
		}
		return Loaded[Config]{}, interpolationError
	}

	var typedConfig Config
	if decodeError := decodeKnownFields(effectiveYAML, &typedConfig); decodeError != nil {
		return Loaded[Config]{}, decodeError
	}
	if loader.validate != nil {
		if validationError := loader.validate(typedConfig); validationError != nil {
			return Loaded[Config]{}, fmt.Errorf("%w: %w", ErrValidation, validationError)
		}
	}
	settings, settingsError := decodeSettings(effectiveYAML)
	if settingsError != nil {
		return Loaded[Config]{}, settingsError
	}
	values, valueError := loadConfigValues(settings, loader.valueMappings)
	if valueError != nil {
		return Loaded[Config]{}, valueError
	}
	return Loaded[Config]{
		Path:          resolvedPath,
		Config:        typedConfig,
		Settings:      settings,
		Values:        values,
		EffectiveYAML: append([]byte(nil), effectiveYAML...),
	}, nil
}

// Lookup resolves a config value by key.
func (values ConfigValues) Lookup(key string) (string, bool) {
	if len(values.values) == 0 {
		return "", false
	}
	value, found := values.values[strings.TrimSpace(key)]
	return value, found
}

// Resolve returns a config value by key, or empty string when absent.
func (values ConfigValues) Resolve(key string) string {
	value, _ := values.Lookup(key)
	return value
}

// Map returns a defensive copy of all config values.
func (values ConfigValues) Map() map[string]string {
	copiedValues := make(map[string]string, len(values.values))
	for key, value := range values.values {
		copiedValues[key] = value
	}
	return copiedValues
}

// Resolver returns a legacy string resolver backed by effective config values.
func (values ConfigValues) Resolver() func(string) string {
	return values.Resolve
}

func decodeKnownFields(configPayload []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(configPayload))
	decoder.KnownFields(true)
	if decodeError := decoder.Decode(target); decodeError != nil {
		return fmt.Errorf("%w: %w", ErrParse, decodeError)
	}
	return nil
}

func interpolationOptions(expansionLookup ExpansionLookup) configfile.EnvironmentOptions {
	if expansionLookup == nil {
		return configfile.EnvironmentOptions{}
	}
	return configfile.EnvironmentOptions{
		Lookup: func(name string) (string, bool) {
			return expansionLookup(name)
		},
	}
}

func decodeSettings(configPayload []byte) (map[string]any, error) {
	settings := make(map[string]any)
	decoder := yaml.NewDecoder(bytes.NewReader(configPayload))
	if decodeError := decoder.Decode(&settings); decodeError != nil {
		return nil, fmt.Errorf("%w: %w", ErrParse, decodeError)
	}
	if settings == nil {
		return map[string]any{}, nil
	}
	return settings, nil
}

func sectionPayload(configPayload []byte, sectionPath []string) ([]byte, error) {
	normalizedSectionPath := normalizedPath(sectionPath)
	if len(normalizedSectionPath) == 0 {
		return nil, fmt.Errorf("%w: section path is required", ErrInvalidOptions)
	}
	rootNode, parseError := configfile.ParseYAMLDocument(configPayload)
	if parseError != nil {
		return nil, fmt.Errorf("%w: %w", ErrParse, parseError)
	}
	sectionNode := lookupMappingPath(documentContentNode(&rootNode), normalizedSectionPath)
	if sectionNode == nil {
		return nil, fmt.Errorf("%w: %s", ErrMissingSection, strings.Join(normalizedSectionPath, "."))
	}
	payload, _ := yaml.Marshal(sectionNode)
	return payload, nil
}

func loadConfigValues(settings map[string]any, mappings []ValueMapping) (ConfigValues, error) {
	values := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		value, found := lookupSettingsPath(settings, mapping.Path)
		if !found {
			continue
		}
		switch typedValue := value.(type) {
		case string:
			values[mapping.Key] = strings.TrimSpace(typedValue)
		case int:
			values[mapping.Key] = fmt.Sprintf("%d", typedValue)
		case float64:
			values[mapping.Key] = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", typedValue), "0"), ".")
		case bool:
			values[mapping.Key] = fmt.Sprintf("%t", typedValue)
		case nil:
			values[mapping.Key] = ""
		default:
			return ConfigValues{}, fmt.Errorf("%w: %s must be scalar", ErrInvalidValueMapping, strings.Join(mapping.Path, "."))
		}
	}
	return ConfigValues{values: values}, nil
}

func lookupSettingsPath(settings map[string]any, path []string) (any, bool) {
	var currentValue any = settings
	for _, pathElement := range path {
		currentMap, ok := currentValue.(map[string]any)
		if !ok {
			return nil, false
		}
		currentValue, ok = currentMap[pathElement]
		if !ok {
			return nil, false
		}
	}
	return currentValue, true
}

func lookupMappingPath(node *yaml.Node, path []string) *yaml.Node {
	currentNode := node
	for _, pathElement := range path {
		currentNode = mappingChild(currentNode, pathElement)
		if currentNode == nil {
			return nil
		}
	}
	return currentNode
}

func documentContentNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return node
}

func mappingChild(node *yaml.Node, key string) *yaml.Node {
	node = documentContentNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for contentIndex := 0; contentIndex+1 < len(node.Content); contentIndex += 2 {
		if node.Content[contentIndex].Value == key {
			return node.Content[contentIndex+1]
		}
	}
	return nil
}

func normalizedPath(path []string) []string {
	normalizedElements := make([]string, 0, len(path))
	for _, element := range path {
		trimmedElement := strings.TrimSpace(element)
		if trimmedElement != "" {
			normalizedElements = append(normalizedElements, trimmedElement)
		}
	}
	return normalizedElements
}
