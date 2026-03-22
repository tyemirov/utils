package crawler

import (
	"errors"
	"fmt"
	"time"
)

// Config wires the crawler with domain settings, scraping options, and collaborators.
type Config struct {
	// AllowedDomains restricts crawling to these hosts.
	AllowedDomains []string

	// Parallelism controls concurrent requests. Required, must be > 0.
	Parallelism int

	// RetryCount sets additional retry attempts per page. 0 = no retries.
	RetryCount int

	// HTTPTimeout caps each HTTP request. 0 = no timeout.
	HTTPTimeout time.Duration

	// RateLimit sets minimum delay between requests to the same domain.
	RateLimit time.Duration

	// MaxDepth limits link-following depth. 0 = no link following.
	MaxDepth int

	// Evaluator processes fetched documents. Required.
	Evaluator Evaluator

	// CookieDomains lists domains for which CookieProvider is called.
	CookieDomains []string

	// CookieProvider returns cookies per domain. Optional.
	CookieProvider CookieProvider

	// Headers customizes outbound requests. Optional.
	Headers HeaderProvider

	// Hook runs before each request. Optional.
	Hook RequestHook

	// Logger receives diagnostic messages. Optional (no-op if nil).
	Logger Logger
}

// Validate checks required fields.
func (c Config) Validate() error {
	if len(c.AllowedDomains) == 0 {
		return errors.New("crawler: allowed domains required")
	}
	if c.Parallelism <= 0 {
		return fmt.Errorf("crawler: parallelism must be > 0 (got %d)", c.Parallelism)
	}
	if c.Evaluator == nil {
		return errors.New("crawler: evaluator required")
	}
	return nil
}
