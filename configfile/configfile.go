// Package configfile loads YAML configuration files with strict environment
// interpolation.
package configfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	environmentNameExpression = `[A-Za-z_][A-Za-z0-9_]*`
)

var (
	// ErrInvalidEnvironmentReference reports unsupported environment syntax in a
	// YAML scalar.
	ErrInvalidEnvironmentReference = errors.New("configfile.invalid_environment_reference")

	// ErrMissingEnvironmentVariables reports YAML references to unset
	// environment variables.
	ErrMissingEnvironmentVariables = errors.New("configfile.missing_environment_variables")

	// ErrMissingPath reports an empty config file path.
	ErrMissingPath = errors.New("configfile.missing_path")

	// ErrNilTarget reports a nil or non-pointer decode target.
	ErrNilTarget = errors.New("configfile.nil_target")

	// ErrParse reports malformed YAML or target decode failures.
	ErrParse = errors.New("configfile.parse")

	// ErrRead reports config file read failures.
	ErrRead = errors.New("configfile.read")

	wholeEnvironmentReferencePattern = regexp.MustCompile(`^\$(?:\{(` + environmentNameExpression + `)\}|(` + environmentNameExpression + `))$`)
	environmentReferencePattern      = regexp.MustCompile(`\$\{([^}]*)\}|\$(` + environmentNameExpression + `)`)
	environmentNamePattern           = regexp.MustCompile(`^` + environmentNameExpression + `$`)
	marshalYAML                      = yaml.Marshal
)

// InvalidEnvironmentReferenceError identifies an unsupported interpolation
// reference.
type InvalidEnvironmentReferenceError struct {
	Reference string
}

// Error returns a stable invalid-reference error string.
func (invalidReferenceError InvalidEnvironmentReferenceError) Error() string {
	return fmt.Sprintf("%s: %s", ErrInvalidEnvironmentReference, invalidReferenceError.Reference)
}

// Is reports compatibility with ErrInvalidEnvironmentReference.
func (invalidReferenceError InvalidEnvironmentReferenceError) Is(target error) bool {
	return target == ErrInvalidEnvironmentReference
}

// MissingEnvironmentVariablesError identifies every unset environment variable
// referenced by a YAML document.
type MissingEnvironmentVariablesError struct {
	Names []string
}

// Error returns a stable missing-environment error string.
func (missingVariablesError MissingEnvironmentVariablesError) Error() string {
	return fmt.Sprintf(
		"%s: missing environment variables for config interpolation: %s",
		ErrMissingEnvironmentVariables,
		strings.Join(missingVariablesError.Names, ", "),
	)
}

// Is reports compatibility with ErrMissingEnvironmentVariables.
func (missingVariablesError MissingEnvironmentVariablesError) Is(target error) bool {
	return target == ErrMissingEnvironmentVariables
}

// LoadYAML reads a YAML config file, expands environment variables only inside
// YAML scalar nodes, and decodes the result with KnownFields enabled.
func LoadYAML(path string, target any) error {
	return LoadYAMLWithOptions(path, target, EnvironmentOptions{})
}

// LoadYAMLWithOptions reads a YAML config file, validates the supplied
// environment registry, expands environment variables only inside YAML scalar
// nodes, and decodes the result with KnownFields enabled.
func LoadYAMLWithOptions(path string, target any, options EnvironmentOptions) error {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return fmt.Errorf("%w: config path is required", ErrMissingPath)
	}

	configPayload, readError := os.ReadFile(normalizedPath)
	if readError != nil {
		return fmt.Errorf("%w: path=%s: %w", ErrRead, normalizedPath, readError)
	}

	if loadError := LoadYAMLBytesWithOptions(configPayload, target, options); loadError != nil {
		return fmt.Errorf("configfile.load path=%s: %w", normalizedPath, loadError)
	}
	return nil
}

// LoadYAMLBytes expands environment variables only inside YAML scalar nodes and
// decodes the result with KnownFields enabled.
func LoadYAMLBytes(configPayload []byte, target any) error {
	return LoadYAMLBytesWithOptions(configPayload, target, EnvironmentOptions{})
}

// LoadYAMLBytesWithOptions validates the supplied environment registry, expands
// environment variables only inside YAML scalar nodes, and decodes the result
// with KnownFields enabled.
func LoadYAMLBytesWithOptions(configPayload []byte, target any, options EnvironmentOptions) error {
	if targetError := validateDecodeTarget(target); targetError != nil {
		return targetError
	}

	interpolatedConfigPayload, interpolationError := InterpolateYAMLWithOptions(configPayload, options)
	if interpolationError != nil {
		return interpolationError
	}

	decoder := yaml.NewDecoder(bytes.NewReader(interpolatedConfigPayload))
	decoder.KnownFields(true)
	if decodeError := decoder.Decode(target); decodeError != nil {
		return fmt.Errorf("%w: %w", ErrParse, decodeError)
	}
	if streamError := requireYAMLDecoderEOF(decoder); streamError != nil {
		return streamError
	}

	return nil
}

// InterpolateYAML expands environment variables only inside YAML scalar nodes.
func InterpolateYAML(configPayload []byte) ([]byte, error) {
	return InterpolateYAMLWithOptions(configPayload, EnvironmentOptions{})
}

// InterpolateYAMLWithOptions validates the supplied environment registry and
// expands environment variables only inside YAML scalar nodes.
func InterpolateYAMLWithOptions(configPayload []byte, options EnvironmentOptions) ([]byte, error) {
	lookup := normalizeEnvironmentLookup(options.Lookup)
	if validationError := options.Registry.Validate(lookup); validationError != nil {
		return nil, validationError
	}

	rootNode, decodeError := decodeSingleYAMLDocument(configPayload)
	if decodeError != nil {
		return nil, decodeError
	}

	missingVariableSet := make(map[string]struct{})
	if interpolationError := interpolateNode(&rootNode, missingVariableSet, lookup, options.Registry); interpolationError != nil {
		return nil, interpolationError
	}
	if len(missingVariableSet) > 0 {
		return nil, MissingEnvironmentVariablesError{Names: sortedNames(missingVariableSet)}
	}

	interpolatedPayload, marshalError := marshalYAML(&rootNode)
	if marshalError != nil {
		return nil, fmt.Errorf("%w: %w", ErrParse, marshalError)
	}
	return interpolatedPayload, nil
}

// ParseYAMLDocument decodes exactly one YAML document and rejects trailing
// documents.
func ParseYAMLDocument(configPayload []byte) (yaml.Node, error) {
	return decodeSingleYAMLDocument(configPayload)
}

func decodeSingleYAMLDocument(configPayload []byte) (yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(configPayload))
	var document yaml.Node
	if decodeError := decoder.Decode(&document); decodeError != nil {
		if errors.Is(decodeError, io.EOF) {
			return yaml.Node{}, nil
		}
		return yaml.Node{}, fmt.Errorf("%w: %w", ErrParse, decodeError)
	}
	if streamError := requireYAMLDecoderEOF(decoder); streamError != nil {
		return yaml.Node{}, streamError
	}
	return document, nil
}

func requireYAMLDecoderEOF(decoder *yaml.Decoder) error {
	var trailingDocument yaml.Node
	trailingDecodeError := decoder.Decode(&trailingDocument)
	if errors.Is(trailingDecodeError, io.EOF) {
		return nil
	}
	if trailingDecodeError != nil {
		return fmt.Errorf("%w: %w", ErrParse, trailingDecodeError)
	}
	return fmt.Errorf("%w: trailing YAML document", ErrParse)
}

func validateDecodeTarget(target any) error {
	if target == nil {
		return ErrNilTarget
	}
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Pointer || targetValue.IsNil() {
		return ErrNilTarget
	}
	return nil
}

func interpolateNode(node *yaml.Node, missingVariableSet map[string]struct{}, lookup EnvironmentLookup, registry EnvRegistry) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		if interpolationError := interpolateScalarNode(node, missingVariableSet, lookup, registry); interpolationError != nil {
			return interpolationError
		}
	}
	for _, childNode := range node.Content {
		if interpolationError := interpolateNode(childNode, missingVariableSet, lookup, registry); interpolationError != nil {
			return interpolationError
		}
	}
	return nil
}

func interpolateScalarNode(node *yaml.Node, missingVariableSet map[string]struct{}, lookup EnvironmentLookup, registry EnvRegistry) error {
	if node == nil || node.Kind != yaml.ScalarNode || !strings.Contains(node.Value, "$") {
		return nil
	}

	expandedWholeValue, isWholeReference := expandWholeEnvironmentReference(node.Value, missingVariableSet, lookup, registry)
	if isWholeReference {
		applyInterpolatedScalarTag(node, expandedWholeValue)
		return nil
	}

	expandedValue, changed, interpolationError := expandInlineEnvironmentReferences(node.Value, missingVariableSet, lookup, registry)
	if interpolationError != nil {
		return interpolationError
	}
	if !changed {
		return nil
	}
	node.Value = expandedValue
	node.Tag = "!!str"
	return nil
}

func expandWholeEnvironmentReference(value string, missingVariableSet map[string]struct{}, lookup EnvironmentLookup, registry EnvRegistry) (string, bool) {
	trimmedValue := strings.TrimSpace(value)
	submatches := wholeEnvironmentReferencePattern.FindStringSubmatch(trimmedValue)
	if len(submatches) == 0 {
		return "", false
	}

	environmentName := submatches[1]
	if environmentName == "" {
		environmentName = submatches[2]
	}
	environmentValue, environmentFound := lookup(environmentName)
	if !environmentFound {
		if !registry.optional(environmentName) {
			missingVariableSet[environmentName] = struct{}{}
		}
	}
	return environmentValue, true
}

func expandInlineEnvironmentReferences(value string, missingVariableSet map[string]struct{}, lookup EnvironmentLookup, registry EnvRegistry) (string, bool, error) {
	changed := false
	var invalidReferenceError error
	expandedValue := environmentReferencePattern.ReplaceAllStringFunc(value, func(reference string) string {
		if invalidReferenceError != nil {
			return reference
		}
		environmentName, referenceError := environmentNameFromReference(reference)
		if referenceError != nil {
			invalidReferenceError = referenceError
			return reference
		}
		changed = true
		environmentValue, environmentFound := lookup(environmentName)
		if !environmentFound {
			if !registry.optional(environmentName) {
				missingVariableSet[environmentName] = struct{}{}
			}
		}
		return environmentValue
	})
	if invalidReferenceError != nil {
		return "", false, invalidReferenceError
	}
	return expandedValue, changed, nil
}

func environmentNameFromReference(reference string) (string, error) {
	if strings.HasPrefix(reference, "${") && strings.HasSuffix(reference, "}") {
		environmentName := strings.TrimSuffix(strings.TrimPrefix(reference, "${"), "}")
		if !environmentNamePattern.MatchString(environmentName) {
			return "", InvalidEnvironmentReferenceError{Reference: reference}
		}
		return environmentName, nil
	}
	if strings.HasPrefix(reference, "$") {
		return strings.TrimPrefix(reference, "$"), nil
	}
	return "", InvalidEnvironmentReferenceError{Reference: reference}
}

func applyInterpolatedScalarTag(node *yaml.Node, expandedValue string) {
	trimmedValue := strings.TrimSpace(expandedValue)
	node.Style = 0

	if trimmedValue == "" {
		node.Tag = "!!null"
		node.Value = ""
		return
	}

	if strings.EqualFold(trimmedValue, "true") || strings.EqualFold(trimmedValue, "false") {
		node.Tag = "!!bool"
		node.Value = strings.ToLower(trimmedValue)
		return
	}

	if _, parseIntError := strconv.ParseInt(trimmedValue, 10, 64); parseIntError == nil {
		node.Tag = "!!int"
		node.Value = trimmedValue
		return
	}

	if _, parseFloatError := strconv.ParseFloat(trimmedValue, 64); parseFloatError == nil {
		node.Tag = "!!float"
		node.Value = trimmedValue
		return
	}

	node.Tag = "!!str"
	node.Value = expandedValue
}

func sortedNames(nameSet map[string]struct{}) []string {
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
