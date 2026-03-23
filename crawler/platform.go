package crawler

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type RetryPolicy uint8

const (
	RetryPolicyDefault RetryPolicy = iota
	RetryPolicyRotateProxy
)

type RetryExhaustionBehavior uint8

const (
	RetryExhaustionBehaviorFail RetryExhaustionBehavior = iota
	RetryExhaustionBehaviorContinue
)

type RetryDecision struct {
	ShouldRetry        bool
	Message            string
	LogMessage         string
	Policy             RetryPolicy
	ExhaustionBehavior RetryExhaustionBehavior
}

func (decision RetryDecision) ResolvedLogMessage() string {
	if message := strings.TrimSpace(decision.LogMessage); message != "" {
		return message
	}
	return strings.TrimSpace(decision.Message)
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
