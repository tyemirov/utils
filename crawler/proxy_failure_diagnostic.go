package crawler

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// ProxyFailureKind classifies why a proxy lease was rotated or marked unhealthy.
type ProxyFailureKind string

const (
	ProxyFailureKindUnknown         ProxyFailureKind = "unknown"
	ProxyFailureKindChallenge       ProxyFailureKind = "challenge"
	ProxyFailureKindTransport       ProxyFailureKind = "transport"
	ProxyFailureKindStatus          ProxyFailureKind = "status"
	ProxyFailureKindProviderAuth    ProxyFailureKind = "provider_auth"
	ProxyFailureKindProviderAccount ProxyFailureKind = "provider_account"
)

// ProxyFailureDiagnostic captures structured proxy rotation diagnostics.
type ProxyFailureDiagnostic struct {
	Kind       ProxyFailureKind
	Reason     string
	StatusCode int
}

func newProxyFailureDiagnostic(kind ProxyFailureKind, reason string, statusCode int) ProxyFailureDiagnostic {
	if kind == "" {
		kind = ProxyFailureKindUnknown
	}
	return ProxyFailureDiagnostic{
		Kind:       kind,
		Reason:     strings.TrimSpace(reason),
		StatusCode: statusCode,
	}
}

func classifyProxyFailureDiagnostic(statusCode int, reason string) ProxyFailureDiagnostic {
	normalizedReason := strings.TrimSpace(reason)
	lowerReason := strings.ToLower(normalizedReason)
	switch {
	case statusCode == http.StatusPaymentRequired || strings.Contains(lowerReason, "payment required"):
		return newProxyFailureDiagnostic(ProxyFailureKindProviderAccount, normalizedReason, statusCode)
	case statusCode == http.StatusProxyAuthRequired || strings.Contains(lowerReason, "proxy authentication required"):
		return newProxyFailureDiagnostic(ProxyFailureKindProviderAuth, normalizedReason, statusCode)
	case strings.Contains(lowerReason, "captcha") || strings.Contains(lowerReason, "challenge"):
		return newProxyFailureDiagnostic(ProxyFailureKindChallenge, normalizedReason, statusCode)
	case statusCode == http.StatusBadGateway || statusCode == http.StatusServiceUnavailable || statusCode == http.StatusGatewayTimeout:
		return newProxyFailureDiagnostic(ProxyFailureKindStatus, normalizedReason, statusCode)
	case statusCode == 0:
		return newProxyFailureDiagnostic(ProxyFailureKindTransport, normalizedReason, statusCode)
	default:
		return newProxyFailureDiagnostic(ProxyFailureKindUnknown, normalizedReason, statusCode)
	}
}

func (diagnostic ProxyFailureDiagnostic) normalized() ProxyFailureDiagnostic {
	return newProxyFailureDiagnostic(diagnostic.Kind, diagnostic.Reason, diagnostic.StatusCode)
}

func (diagnostic ProxyFailureDiagnostic) hasDiagnostic() bool {
	return diagnostic.Kind != "" || strings.TrimSpace(diagnostic.Reason) != "" || diagnostic.StatusCode != 0
}

func (diagnostic ProxyFailureDiagnostic) nonHealthPenalizing() bool {
	return diagnostic.normalized().Kind == ProxyFailureKindChallenge
}

func (diagnostic ProxyFailureDiagnostic) providerCredentialFailure() bool {
	kind := diagnostic.normalized().Kind
	return kind == ProxyFailureKindProviderAuth || kind == ProxyFailureKindProviderAccount
}

func (diagnostic ProxyFailureDiagnostic) bucket() string {
	normalizedDiagnostic := diagnostic.normalized()
	switch normalizedDiagnostic.Kind {
	case ProxyFailureKindStatus:
		if normalizedDiagnostic.StatusCode != 0 {
			return fmt.Sprintf("status_%d", normalizedDiagnostic.StatusCode)
		}
	case ProxyFailureKindTransport:
		if normalizedDiagnostic.StatusCode == 0 {
			return "status_0"
		}
	}
	return string(normalizedDiagnostic.Kind)
}

func proxyFailureDiagnosticSummary(diagnostics []ProxyFailureDiagnostic) string {
	if len(diagnostics) == 0 {
		return ""
	}
	countByBucket := map[string]int{}
	for _, diagnostic := range diagnostics {
		countByBucket[diagnostic.bucket()]++
	}
	buckets := make([]string, 0, len(countByBucket))
	for bucket := range countByBucket {
		buckets = append(buckets, bucket)
	}
	sort.Strings(buckets)

	parts := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		parts = append(parts, fmt.Sprintf("%s=%d", bucket, countByBucket[bucket]))
	}
	return strings.Join(parts, ", ")
}
