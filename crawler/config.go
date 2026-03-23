package crawler

import (
	"errors"
	"fmt"
	"time"
)

// Config wires the crawler service with platform metadata, scraping options,
// and effectful collaborators. All fields are mandatory unless marked as optional.
type Config struct {
	// PlatformID identifies the target platform (for example "AMZN").
	PlatformID string

	// Scraper controls concurrency, retries, and network behaviour.
	Scraper ScraperConfig

	// Platform holds domain-specific settings such as allowed hosts.
	Platform PlatformConfig

	// OutputDirectory is optional; when supplied and FilePersister is nil the
	// crawler will persist downloaded artifacts under this path.
	OutputDirectory string

	// RunFolder scopes persisted artifacts for a single execution.
	RunFolder string

	// RuleEvaluator produces rule findings for a fetched document. Mandatory.
	RuleEvaluator RuleEvaluator

	// CookieGenerator returns cookies for a given domain. Optional.
	CookieGenerator CookieGenerator

	// FilePersister handles file persistence. Optional; a default implementation
	// is created when OutputDirectory is set.
	FilePersister FilePersister

	// PlatformHooks customise platform-specific behaviour. Optional.
	PlatformHooks PlatformHooks

	// RequestHeaders applies custom headers before each outbound request.
	RequestHeaders RequestHeaderProvider

	// RequestHook runs before each outbound request. Optional.
	RequestHook RequestHook

	// Logger receives debug/info/warning/error logs. Optional; a no-op logger is
	// used when nil.
	Logger Logger
}

// Validate ensures required configuration is present and self-consistent.
func (cfg Config) Validate() error {
	if cfg.PlatformID == "" {
		return errors.New("crawler: platform id is required")
	}
	if err := cfg.Scraper.Validate(); err != nil {
		return fmt.Errorf("crawler: invalid scraper config: %w", err)
	}
	if err := cfg.Platform.Validate(); err != nil {
		return fmt.Errorf("crawler: invalid platform config: %w", err)
	}
	if cfg.RuleEvaluator == nil {
		return errors.New("crawler: rule evaluator is required")
	}
	return nil
}

// ScraperConfig exposes concurrency and retry knobs for the crawler.
type ScraperConfig struct {
	MaxDepth                   int
	Parallelism                int
	RetryCount                 int
	HTTPTimeout                time.Duration
	InsecureSkipVerify         bool
	RateLimit                  time.Duration
	ProxyList                  []string
	SaveFiles                  bool
	ProxyCircuitBreakerEnabled bool
}

// Validate checks that essential numeric fields are positive.
func (cfg ScraperConfig) Validate() error {
	if cfg.Parallelism <= 0 {
		return fmt.Errorf("parallelism must be greater than zero (got %d)", cfg.Parallelism)
	}
	if cfg.RetryCount < 0 {
		return fmt.Errorf("retry count must be non-negative (got %d)", cfg.RetryCount)
	}
	if cfg.MaxDepth < 0 {
		return fmt.Errorf("max depth must be non-negative (got %d)", cfg.MaxDepth)
	}
	if cfg.RateLimit < 0 {
		return fmt.Errorf("rate limit must be non-negative (got %s)", cfg.RateLimit)
	}
	return nil
}

// PlatformConfig restricts the crawler to known domains and provides selectors.
type PlatformConfig struct {
	AllowedDomains      []string
	CookieDomains       []string
	SkipRulesOnRedirect bool
}

// Validate ensures the platform configuration is usable.
func (cfg PlatformConfig) Validate() error {
	if len(cfg.AllowedDomains) == 0 {
		return errors.New("allowed domains required")
	}
	return nil
}
