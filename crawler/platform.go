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

// ResolvedLogMessage returns the log message or falls back to the general message.
func (decision RetryDecision) ResolvedLogMessage() string {
	if message := strings.TrimSpace(decision.LogMessage); message != "" {
		return message
	}
	return strings.TrimSpace(decision.Message)
}

// PlatformHooks provide platform-specific normalisation, content validation,
// redirect detection, and retry logic. Implementations encapsulate all
// platform-specific behaviour so the core crawler remains generic.
type PlatformHooks interface {
	NormalizeTitle(title string) string
	ShouldRetry(title string, document *goquery.Document) RetryDecision
	ExtractDOMTitle(document *goquery.Document) string
	IsContentComplete(document *goquery.Document) bool
	InferRedirect(productID, originalURL, finalURL, canonicalURL string) (redirected bool, redirectedProductID string)
}

type noopPlatformHooks struct{}

func (noopPlatformHooks) NormalizeTitle(title string) string { return title }
func (noopPlatformHooks) ShouldRetry(string, *goquery.Document) RetryDecision {
	return RetryDecision{}
}
func (noopPlatformHooks) ExtractDOMTitle(*goquery.Document) string { return "" }
func (noopPlatformHooks) IsContentComplete(*goquery.Document) bool { return true }
func (noopPlatformHooks) InferRedirect(string, string, string, string) (bool, string) {
	return false, ""
}

func ensurePlatformHooks(hooks PlatformHooks) PlatformHooks {
	if hooks == nil {
		return noopPlatformHooks{}
	}
	return hooks
}
