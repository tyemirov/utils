package configfile

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	// ErrEnvironmentValidation reports invalid environment values for a config
	// registry.
	ErrEnvironmentValidation = errors.New("configfile.environment_validation")

	// ErrInvalidEnvironmentRequirement reports an invalid environment contract
	// declaration.
	ErrInvalidEnvironmentRequirement = errors.New("configfile.invalid_environment_requirement")
)

// EnvironmentLookup resolves an environment variable by name.
type EnvironmentLookup func(name string) (string, bool)

// EnvironmentOptions configures YAML environment interpolation and validation.
type EnvironmentOptions struct {
	Lookup   EnvironmentLookup
	Registry EnvRegistry
}

// EnvValueSchema validates one environment value without exposing the value in
// error output.
type EnvValueSchema interface {
	Name() string
	Validate(value string) error
}

type envValueSchema struct {
	name     string
	validate func(value string) error
}

// NewEnvValueSchema creates a named environment value validator.
func NewEnvValueSchema(name string, validate func(value string) error) (EnvValueSchema, error) {
	normalizedName := strings.TrimSpace(name)
	if normalizedName == "" {
		return nil, fmt.Errorf("%w: schema name is required", ErrInvalidEnvironmentRequirement)
	}
	if validate == nil {
		return nil, fmt.Errorf("%w: schema %s validator is required", ErrInvalidEnvironmentRequirement, normalizedName)
	}
	return envValueSchema{name: normalizedName, validate: validate}, nil
}

func (schema envValueSchema) Name() string {
	return schema.name
}

func (schema envValueSchema) Validate(value string) error {
	return schema.validate(value)
}

// EnvRequirement declares whether an environment variable is required or
// optional and which value schema it must satisfy when present.
type EnvRequirement struct {
	name     string
	required bool
	schema   EnvValueSchema
}

// NewRequiredEnv declares a mandatory environment variable.
func NewRequiredEnv(name string, schema EnvValueSchema) (EnvRequirement, error) {
	return newEnvRequirement(name, true, schema)
}

// NewOptionalEnv declares an optional environment variable.
func NewOptionalEnv(name string, schema EnvValueSchema) (EnvRequirement, error) {
	return newEnvRequirement(name, false, schema)
}

func newEnvRequirement(name string, required bool, schema EnvValueSchema) (EnvRequirement, error) {
	normalizedName := strings.TrimSpace(name)
	if !environmentNamePattern.MatchString(normalizedName) {
		return EnvRequirement{}, fmt.Errorf("%w: invalid environment name %q", ErrInvalidEnvironmentRequirement, name)
	}
	return EnvRequirement{name: normalizedName, required: required, schema: schema}, nil
}

// Name returns the environment variable name.
func (requirement EnvRequirement) Name() string {
	return requirement.name
}

// Required reports whether the environment variable must be defined with a
// non-empty value.
func (requirement EnvRequirement) Required() bool {
	return requirement.required
}

// SchemaName returns the value schema name or an empty string when no schema is
// attached.
func (requirement EnvRequirement) SchemaName() string {
	if requirement.schema == nil {
		return ""
	}
	return requirement.schema.Name()
}

// EnvContract marks environment variables as required or optional for a config
// file family.
type EnvContract struct {
	requirements map[string]EnvRequirement
}

// NewEnvContract creates a contract from explicit environment requirements.
func NewEnvContract(requirements []EnvRequirement) (EnvContract, error) {
	requirementsByName := make(map[string]EnvRequirement, len(requirements))
	for _, requirement := range requirements {
		if strings.TrimSpace(requirement.name) == "" {
			return EnvContract{}, fmt.Errorf("%w: empty requirement", ErrInvalidEnvironmentRequirement)
		}
		if _, exists := requirementsByName[requirement.name]; exists {
			return EnvContract{}, fmt.Errorf("%w: duplicate environment name %s", ErrInvalidEnvironmentRequirement, requirement.name)
		}
		requirementsByName[requirement.name] = requirement
	}
	return EnvContract{requirements: requirementsByName}, nil
}

// RegistryForYAML returns the environment registry implied by a YAML config and
// this contract. Referenced variables are required by default unless the
// contract explicitly marks them optional.
func (contract EnvContract) RegistryForYAML(configPayload []byte) (EnvRegistry, error) {
	rootNode, decodeError := decodeSingleYAMLDocument(configPayload)
	if decodeError != nil {
		return EnvRegistry{}, decodeError
	}

	referencesByName := make(map[string]map[string]struct{})
	if referenceError := collectEnvironmentReferences(&rootNode, "", referencesByName); referenceError != nil {
		return EnvRegistry{}, referenceError
	}

	requirementsByName := make(map[string]EnvRequirement)
	for environmentName := range referencesByName {
		requirement, found := contract.Requirement(environmentName)
		if !found {
			defaultRequirement, _ := NewRequiredEnv(environmentName, nil)
			requirement = defaultRequirement
		}
		requirementsByName[environmentName] = requirement
	}
	for environmentName, requirement := range contract.requirements {
		if _, exists := requirementsByName[environmentName]; !exists {
			requirementsByName[environmentName] = requirement
		}
	}

	requirements := make([]EnvRequirement, 0, len(requirementsByName))
	for _, requirement := range requirementsByName {
		requirements = append(requirements, requirement)
	}

	referencePaths := make(map[string][]string, len(referencesByName))
	for environmentName, paths := range referencesByName {
		referencePaths[environmentName] = sortedSetEntries(paths)
	}
	return newEnvRegistry(requirements, referencePaths)
}

// Requirement returns the explicit contract requirement for an environment
// variable.
func (contract EnvContract) Requirement(name string) (EnvRequirement, bool) {
	if len(contract.requirements) == 0 {
		return EnvRequirement{}, false
	}
	requirement, found := contract.requirements[strings.TrimSpace(name)]
	return requirement, found
}

// EnvRegistry exposes environment requirements for preflight validation.
type EnvRegistry struct {
	requirements  []EnvRequirement
	referencePath map[string][]string
}

// NewEnvRegistry creates a registry from explicit requirements.
func NewEnvRegistry(requirements []EnvRequirement) (EnvRegistry, error) {
	return newEnvRegistry(requirements, nil)
}

func newEnvRegistry(requirements []EnvRequirement, referencePaths map[string][]string) (EnvRegistry, error) {
	requirementsByName := make(map[string]EnvRequirement, len(requirements))
	for _, requirement := range requirements {
		if strings.TrimSpace(requirement.name) == "" {
			return EnvRegistry{}, fmt.Errorf("%w: empty requirement", ErrInvalidEnvironmentRequirement)
		}
		if _, exists := requirementsByName[requirement.name]; exists {
			return EnvRegistry{}, fmt.Errorf("%w: duplicate environment name %s", ErrInvalidEnvironmentRequirement, requirement.name)
		}
		requirementsByName[requirement.name] = requirement
	}

	normalizedRequirements := make([]EnvRequirement, 0, len(requirementsByName))
	for _, requirement := range requirementsByName {
		normalizedRequirements = append(normalizedRequirements, requirement)
	}
	sort.Slice(normalizedRequirements, func(leftIndex int, rightIndex int) bool {
		return normalizedRequirements[leftIndex].name < normalizedRequirements[rightIndex].name
	})

	normalizedReferencePaths := make(map[string][]string, len(referencePaths))
	for environmentName, paths := range referencePaths {
		normalizedReferencePaths[environmentName] = append([]string(nil), paths...)
		sort.Strings(normalizedReferencePaths[environmentName])
	}

	return EnvRegistry{
		requirements:  normalizedRequirements,
		referencePath: normalizedReferencePaths,
	}, nil
}

// Requirements returns every declared environment requirement.
func (registry EnvRegistry) Requirements() []EnvRequirement {
	return append([]EnvRequirement(nil), registry.requirements...)
}

// Mandatory returns required environment variables only.
func (registry EnvRegistry) Mandatory() []EnvRequirement {
	mandatory := make([]EnvRequirement, 0, len(registry.requirements))
	for _, requirement := range registry.requirements {
		if requirement.required {
			mandatory = append(mandatory, requirement)
		}
	}
	return mandatory
}

// Requirement returns the registry entry for an environment variable.
func (registry EnvRegistry) Requirement(name string) (EnvRequirement, bool) {
	normalizedName := strings.TrimSpace(name)
	for _, requirement := range registry.requirements {
		if requirement.name == normalizedName {
			return requirement, true
		}
	}
	return EnvRequirement{}, false
}

// ReferencePaths returns config paths that reference an environment variable.
func (registry EnvRegistry) ReferencePaths(name string) []string {
	if len(registry.referencePath) == 0 {
		return nil
	}
	return append([]string(nil), registry.referencePath[strings.TrimSpace(name)]...)
}

// Validate checks required environment values and optional values that are
// present.
func (registry EnvRegistry) Validate(lookup EnvironmentLookup) error {
	if len(registry.requirements) == 0 {
		return nil
	}
	resolvedLookup := normalizeEnvironmentLookup(lookup)
	var issues []EnvValidationIssue
	for _, requirement := range registry.requirements {
		value, found := resolvedLookup(requirement.name)
		trimmedValue := strings.TrimSpace(value)
		if !found {
			if requirement.required {
				issues = append(issues, newEnvValidationIssue(requirement, EnvValidationIssueMissing, ""))
			}
			continue
		}
		if trimmedValue == "" {
			if requirement.required {
				issues = append(issues, newEnvValidationIssue(requirement, EnvValidationIssueEmpty, ""))
			}
			continue
		}
		if requirement.schema == nil {
			continue
		}
		if schemaError := requirement.schema.Validate(value); schemaError != nil {
			issues = append(issues, newEnvValidationIssue(requirement, EnvValidationIssueInvalid, schemaError.Error()))
		}
	}
	if len(issues) > 0 {
		return EnvValidationError{issues: issues}
	}
	return nil
}

func (registry EnvRegistry) optional(name string) bool {
	requirement, found := registry.Requirement(name)
	return found && !requirement.required
}

// EnvValidationIssueKind classifies an environment validation failure.
type EnvValidationIssueKind string

const (
	// EnvValidationIssueMissing means the required key was not defined.
	EnvValidationIssueMissing EnvValidationIssueKind = "missing"
	// EnvValidationIssueEmpty means the required key was defined with an empty
	// value.
	EnvValidationIssueEmpty EnvValidationIssueKind = "empty"
	// EnvValidationIssueInvalid means the value failed its declared schema.
	EnvValidationIssueInvalid EnvValidationIssueKind = "invalid"
)

// EnvValidationIssue describes one invalid environment variable.
type EnvValidationIssue struct {
	name       string
	kind       EnvValidationIssueKind
	required   bool
	schemaName string
	detail     string
}

func newEnvValidationIssue(requirement EnvRequirement, kind EnvValidationIssueKind, detail string) EnvValidationIssue {
	return EnvValidationIssue{
		name:       requirement.name,
		kind:       kind,
		required:   requirement.required,
		schemaName: requirement.SchemaName(),
		detail:     detail,
	}
}

// Name returns the environment variable name.
func (issue EnvValidationIssue) Name() string {
	return issue.name
}

// Kind returns the validation issue category.
func (issue EnvValidationIssue) Kind() EnvValidationIssueKind {
	return issue.kind
}

// Required reports whether the invalid environment variable was mandatory.
func (issue EnvValidationIssue) Required() bool {
	return issue.required
}

// SchemaName returns the failed schema name for invalid-value issues.
func (issue EnvValidationIssue) SchemaName() string {
	return issue.schemaName
}

// Detail returns schema failure detail without the raw environment value.
func (issue EnvValidationIssue) Detail() string {
	return issue.detail
}

// EnvValidationError groups environment validation issues.
type EnvValidationError struct {
	issues []EnvValidationIssue
}

// Issues returns the individual validation issues.
func (validationError EnvValidationError) Issues() []EnvValidationIssue {
	return append([]EnvValidationIssue(nil), validationError.issues...)
}

func (validationError EnvValidationError) Error() string {
	parts := make([]string, 0, len(validationError.issues))
	for _, issue := range validationError.issues {
		switch issue.kind {
		case EnvValidationIssueMissing:
			parts = append(parts, fmt.Sprintf("%s missing", issue.name))
		case EnvValidationIssueEmpty:
			parts = append(parts, fmt.Sprintf("%s empty", issue.name))
		case EnvValidationIssueInvalid:
			parts = append(parts, fmt.Sprintf("%s invalid schema=%s: %s", issue.name, issue.schemaName, issue.detail))
		default:
			parts = append(parts, fmt.Sprintf("%s invalid", issue.name))
		}
	}
	return fmt.Sprintf("%s: %s", ErrEnvironmentValidation, strings.Join(parts, "; "))
}

// Is reports compatibility with ErrEnvironmentValidation.
func (validationError EnvValidationError) Is(target error) bool {
	return target == ErrEnvironmentValidation
}

// OSEnvironmentLookup resolves values from the process environment.
func OSEnvironmentLookup(name string) (string, bool) {
	return os.LookupEnv(name)
}

func normalizeEnvironmentLookup(lookup EnvironmentLookup) EnvironmentLookup {
	if lookup == nil {
		return OSEnvironmentLookup
	}
	return lookup
}

func collectEnvironmentReferences(node *yaml.Node, path string, referencesByName map[string]map[string]struct{}) error {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for _, childNode := range node.Content {
			if referenceError := collectEnvironmentReferences(childNode, path, referencesByName); referenceError != nil {
				return referenceError
			}
		}
	case yaml.MappingNode:
		for contentIndex := 0; contentIndex+1 < len(node.Content); contentIndex += 2 {
			keyNode := node.Content[contentIndex]
			valueNode := node.Content[contentIndex+1]
			childPath := joinYAMLPath(path, keyNode.Value)
			if referenceError := collectEnvironmentReferences(valueNode, childPath, referencesByName); referenceError != nil {
				return referenceError
			}
		}
	case yaml.SequenceNode:
		for sequenceIndex, childNode := range node.Content {
			childPath := indexedYAMLPath(path, sequenceIndex)
			if referenceError := collectEnvironmentReferences(childNode, childPath, referencesByName); referenceError != nil {
				return referenceError
			}
		}
	case yaml.ScalarNode:
		environmentNames, referenceError := scalarEnvironmentReferences(node.Value)
		if referenceError != nil {
			return referenceError
		}
		for _, environmentName := range environmentNames {
			if _, exists := referencesByName[environmentName]; !exists {
				referencesByName[environmentName] = make(map[string]struct{})
			}
			referencePath := path
			if referencePath == "" {
				referencePath = "$"
			}
			referencesByName[environmentName][referencePath] = struct{}{}
		}
	}
	return nil
}

func scalarEnvironmentReferences(value string) ([]string, error) {
	if !strings.Contains(value, "$") {
		return nil, nil
	}
	matches := environmentReferencePattern.FindAllString(value, -1)
	if len(matches) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		environmentName, referenceError := environmentNameFromReference(match)
		if referenceError != nil {
			return nil, referenceError
		}
		seen[environmentName] = struct{}{}
	}
	return sortedSetEntries(seen), nil
}

func joinYAMLPath(parentPath string, key string) string {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		trimmedKey = "?"
	}
	if parentPath == "" {
		return trimmedKey
	}
	return parentPath + "." + trimmedKey
}

func indexedYAMLPath(parentPath string, sequenceIndex int) string {
	if parentPath == "" {
		return fmt.Sprintf("[%d]", sequenceIndex)
	}
	return fmt.Sprintf("%s[%d]", parentPath, sequenceIndex)
}

func sortedSetEntries(values map[string]struct{}) []string {
	entries := make([]string, 0, len(values))
	for value := range values {
		entries = append(entries, value)
	}
	sort.Strings(entries)
	return entries
}
