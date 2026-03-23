package crawler

import (
	"errors"
	"fmt"
	"time"
)

// Config wires the crawler service with target metadata, scraping options,
// and effectful collaborators.
type Config struct {
	// Category identifies the target category (e.g., "AMZN", "camps").
	Category string

	// Scraper controls concurrency, retries, and network behaviour.
	Scraper ScraperConfig

	// Platform holds domain-specific settings.
	Platform PlatformConfig

	// ResponseHandler processes HTTP responses. Optional; when nil, a default
	// handler is created using the Evaluator.
	ResponseHandler ResponseHandler

	// Evaluator produces findings for a fetched document. Required when
	// ResponseHandler is nil.
	Evaluator Evaluator

	// PlatformHooks customise title normalisation and retry decisions. Optional.
	PlatformHooks PlatformHooks

	// CookieProvider returns cookies per domain. Optional.
	CookieProvider CookieProvider

	// CookieDomains lists domains for which CookieProvider is called.
	CookieDomains []string

	// FilePersister handles artifact persistence. Optional; a default
	// implementation is created when OutputDirectory is set.
	FilePersister FilePersister

	// OutputDirectory is optional; when set and FilePersister is nil the
	// crawler will persist artifacts under this path.
	OutputDirectory string

	// RunFolder scopes persisted artifacts for a single execution.
	RunFolder string

	// Headers customises outbound requests. Optional.
	Headers HeaderProvider

	// Hook runs before each request. Optional.
	Hook RequestHook

	// Logger receives diagnostic messages. Optional (no-op if nil).
	Logger Logger
}

// Validate ensures required configuration is present and self-consistent.
func (cfg Config) Validate() error {
	if cfg.Category == "" {
		return errors.New("crawler: category is required")
	}
	if err := cfg.Scraper.Validate(); err != nil {
		return fmt.Errorf("crawler: invalid scraper config: %w", err)
	}
	if err := cfg.Platform.Validate(); err != nil {
		return fmt.Errorf("crawler: invalid platform config: %w", err)
	}
	if cfg.ResponseHandler == nil && cfg.Evaluator == nil {
		return errors.New("crawler: evaluator is required when no response handler is provided")
	}
	return nil
}

// ScraperConfig controls concurrency, retries, and network behaviour.
type ScraperConfig struct {
	MaxDepth                   int
	Parallelism                int
	RetryCount                 int
	HTTPTimeout                time.Duration
	InsecureSkipVerify         bool
	RateLimit                  time.Duration
	ProxyList                  []string
	ProxyCircuitBreakerEnabled bool
	SaveFiles                  bool
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

// PlatformConfig restricts the crawler to known domains.
type PlatformConfig struct {
	AllowedDomains []string
	CookieDomains  []string
}

// Validate ensures the platform configuration is usable.
func (cfg PlatformConfig) Validate() error {
	if len(cfg.AllowedDomains) == 0 {
		return errors.New("allowed domains required")
	}
	return nil
}
