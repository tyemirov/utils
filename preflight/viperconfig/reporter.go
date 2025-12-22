package viperconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"github.com/tyemirov/utils/preflight"
)

// ErrReporter indicates the Viper reporter is invalid.
var ErrReporter = errors.New("preflight.viper.invalid")

const (
	errorCodeMissingConfigPath = "preflight.viper.missing_config_path"
	errorCodeMissingRedactor   = "preflight.viper.missing_redactor"
	errorCodeConfigRead        = "preflight.viper.read_config"
	errorCodeRedact            = "preflight.viper.redact"
)

// EnvBinding binds a config key to an environment variable name.
type EnvBinding struct {
	Key string
	Env string
}

// Redactor transforms the effective config into a redacted payload.
type Redactor interface {
	Redact(settings map[string]interface{}, mode preflight.RedactionMode) (json.RawMessage, error)
}

// Reporter builds effective-config reports from Viper settings.
type Reporter struct {
	configPath  string
	envBindings []EnvBinding
	redactor    Redactor
}

// NewReporter constructs a Viper-backed config reporter.
func NewReporter(configPath string, envBindings []EnvBinding, redactor Redactor) (*Reporter, error) {
	cleanPath := strings.TrimSpace(configPath)
	if cleanPath == "" {
		return nil, fmt.Errorf("%w: %s", ErrReporter, errorCodeMissingConfigPath)
	}
	if redactor == nil {
		return nil, fmt.Errorf("%w: %s", ErrReporter, errorCodeMissingRedactor)
	}
	cleanBindings := make([]EnvBinding, 0, len(envBindings))
	for _, binding := range envBindings {
		cleanKey := strings.TrimSpace(binding.Key)
		cleanEnv := strings.TrimSpace(binding.Env)
		if cleanKey == "" || cleanEnv == "" {
			continue
		}
		cleanBindings = append(cleanBindings, EnvBinding{Key: cleanKey, Env: cleanEnv})
	}
	return &Reporter{
		configPath:  cleanPath,
		envBindings: cleanBindings,
		redactor:    redactor,
	}, nil
}

// Build loads config via Viper, applies env bindings, and redacts the payload.
func (reporter *Reporter) Build(mode preflight.RedactionMode) (json.RawMessage, error) {
	settings, loadErr := reporter.loadSettings()
	if loadErr != nil {
		return nil, loadErr
	}
	payload, redactErr := reporter.redactor.Redact(settings, mode)
	if redactErr != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrReporter, errorCodeRedact, redactErr)
	}
	return payload, nil
}

func (reporter *Reporter) loadSettings() (map[string]interface{}, error) {
	viperInstance := viper.New()
	viperInstance.SetConfigFile(reporter.configPath)
	if filepath.Ext(reporter.configPath) == "" {
		viperInstance.SetConfigType("yaml")
	}
	for _, binding := range reporter.envBindings {
		if err := viperInstance.BindEnv(binding.Key, binding.Env); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrReporter, errorCodeConfigRead, err)
		}
	}
	viperInstance.AutomaticEnv()
	if err := viperInstance.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrReporter, errorCodeConfigRead, err)
	}
	return viperInstance.AllSettings(), nil
}
