package crawler

import (
	"net/http"
	"time"

	"github.com/tyemirov/utils/httptransport"
)

// DefaultHTTPTimeout is used when an HTTP client timeout is not explicitly
// provided.
const DefaultHTTPTimeout = httptransport.DefaultTimeout

// HTTPProfile describes how to build an HTTP client transport.
type HTTPProfile = httptransport.Profile

// InferHTTPProfile derives an HTTP transport profile from a raw proxy URL.
func InferHTTPProfile(rawProxyURL string, ignoreCertErrors bool) (HTTPProfile, error) {
	return httptransport.InferProfile(rawProxyURL, ignoreCertErrors)
}

// NormalizeHTTPProfile trims and validates a profile before client creation.
func NormalizeHTTPProfile(httpProfile HTTPProfile) (HTTPProfile, error) {
	return httptransport.NormalizeProfile(httpProfile)
}

// NewHTTPClient builds an HTTP client bound to one transport profile.
func NewHTTPClient(httpProfile HTTPProfile, timeout time.Duration) (*http.Client, error) {
	return httptransport.NewClient(httpProfile, timeout)
}

// HTTPTimeoutOrDefault returns DefaultHTTPTimeout when timeout is not positive.
func HTTPTimeoutOrDefault(timeout time.Duration) time.Duration {
	return httptransport.TimeoutOrDefault(timeout)
}

// IsSOCKSProxy reports whether a proxy URL uses a supported SOCKS scheme.
func IsSOCKSProxy(rawProxyURL string) bool {
	return httptransport.IsSOCKSProxy(rawProxyURL)
}
