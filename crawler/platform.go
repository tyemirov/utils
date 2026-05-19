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

// RetryProxyFailureSeverity controls how rotate-proxy retries affect proxy health.
type RetryProxyFailureSeverity uint8

const (
	// RetryProxyFailureSeverityNormal rotates away from the current proxy without
	// forcing a global cooldown. This is the default for content-level retry
	// decisions such as captchas or wrong delivery context.
	RetryProxyFailureSeverityNormal RetryProxyFailureSeverity = iota
	// RetryProxyFailureSeverityCritical rotates and immediately cools down the
	// proxy. Use this only when the response proves the proxy candidate itself is
	// unhealthy for future requests.
	RetryProxyFailureSeverityCritical
)

// RetryDecision captures the outcome of a platform retry check.
type RetryDecision struct {
	ShouldRetry          bool
	Message              string
	LogMessage           string
	Policy               RetryPolicy
	ExhaustionBehavior   RetryExhaustionBehavior
	ProxyFailureSeverity RetryProxyFailureSeverity
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
