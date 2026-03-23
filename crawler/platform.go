package crawler

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// RetryPolicy controls how retries are performed.
type RetryPolicy uint8

const (
	RetryPolicyDefault RetryPolicy = iota
	RetryPolicyRotateProxy
)

// RetryExhaustionBehavior controls what happens when retries are exhausted.
type RetryExhaustionBehavior uint8

const (
	RetryExhaustionBehaviorFail RetryExhaustionBehavior = iota
	RetryExhaustionBehaviorContinue
)

// RetryDecision captures the outcome of a platform retry check.
type RetryDecision struct {
	ShouldRetry        bool
	Message            string
	LogMessage         string
	Policy             RetryPolicy
	ExhaustionBehavior RetryExhaustionBehavior
}

// ResolvedLogMessage returns the log message or falls back to the message.
func (d RetryDecision) ResolvedLogMessage() string {
	if msg := strings.TrimSpace(d.LogMessage); msg != "" {
		return msg
	}
	return strings.TrimSpace(d.Message)
}

// PlatformHooks provide platform-specific normalisation and retry logic.
type PlatformHooks interface {
	NormalizeTitle(title string) string
	ShouldRetry(title string, document *goquery.Document) RetryDecision
}

type noopPlatformHooks struct{}

func (noopPlatformHooks) NormalizeTitle(title string) string { return title }
func (noopPlatformHooks) ShouldRetry(string, *goquery.Document) RetryDecision {
	return RetryDecision{}
}

func ensurePlatformHooks(hooks PlatformHooks) PlatformHooks {
	if hooks == nil {
		return noopPlatformHooks{}
	}
	return hooks
}
