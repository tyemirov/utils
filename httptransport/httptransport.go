// Package httptransport provides reusable HTTP client transport helpers for
// proxy-aware scraping runtimes.
package httptransport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// DefaultTimeout is used when a client timeout is not explicitly provided.
const DefaultTimeout = 15 * time.Second

// Profile describes how to build an HTTP client transport.
type Profile struct {
	ID               string
	Provider         string
	URL              string
	IgnoreCertErrors bool
}

var (
	proxyFromURL = proxy.FromURL
	urlParse     = url.Parse
	cookieJarNew = cookiejar.New
)

// InferProfile derives an HTTP transport profile from a raw proxy URL.
func InferProfile(rawProxyURL string, ignoreCertErrors bool) (Profile, error) {
	trimmedProxyURL := strings.TrimSpace(rawProxyURL)
	if trimmedProxyURL == "" {
		return Profile{
			ID:               "direct",
			Provider:         "direct",
			IgnoreCertErrors: ignoreCertErrors,
		}, nil
	}

	parsedProxyURL, parseError := url.Parse(trimmedProxyURL)
	if parseError != nil {
		return Profile{}, fmt.Errorf("parsing HTTP proxy URL: %w", parseError)
	}
	if parsedProxyURL.Scheme == "" || parsedProxyURL.Host == "" {
		return Profile{}, fmt.Errorf("invalid HTTP proxy URL %q", trimmedProxyURL)
	}

	return Profile{
		ID:               inferredTransportID(trimmedProxyURL),
		Provider:         inferredProvider(trimmedProxyURL),
		URL:              trimmedProxyURL,
		IgnoreCertErrors: ignoreCertErrors,
	}, nil
}

// NormalizeProfile trims and validates a profile before it is used to build a
// client.
func NormalizeProfile(httpProfile Profile) (Profile, error) {
	trimmedProxyURL := strings.TrimSpace(httpProfile.URL)
	if httpProfile.ID == "" {
		httpProfile.ID = inferredTransportID(trimmedProxyURL)
	}
	if httpProfile.Provider == "" {
		httpProfile.Provider = inferredProvider(trimmedProxyURL)
	}
	httpProfile.URL = trimmedProxyURL

	if trimmedProxyURL == "" {
		return httpProfile, nil
	}

	parsedProxyURL, parseError := url.Parse(trimmedProxyURL)
	if parseError != nil {
		return Profile{}, fmt.Errorf("parsing HTTP proxy URL: %w", parseError)
	}
	if parsedProxyURL.Scheme == "" || parsedProxyURL.Host == "" {
		return Profile{}, fmt.Errorf("invalid HTTP proxy URL %q", trimmedProxyURL)
	}

	return httpProfile, nil
}

// NewClient builds an HTTP client bound to one transport profile.
func NewClient(httpProfile Profile, timeout time.Duration) (*http.Client, error) {
	normalizedProfile, normalizeError := NormalizeProfile(httpProfile)
	if normalizeError != nil {
		return nil, normalizeError
	}

	jar, jarError := cookieJarNew(nil)
	if jarError != nil {
		return nil, fmt.Errorf("creating cookie jar: %w", jarError)
	}

	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		ForceAttemptHTTP2: true,
	}

	if normalizedProfile.URL != "" {
		parsedProxyURL, _ := url.Parse(normalizedProfile.URL)
		if IsSOCKSProxy(normalizedProfile.URL) {
			dialer, dialerError := proxyFromURL(parsedProxyURL, proxy.Direct)
			if dialerError != nil {
				return nil, fmt.Errorf("creating SOCKS dialer: %w", dialerError)
			}
			if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
				transport.DialContext = contextDialer.DialContext
			} else {
				transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
					return dialer.Dial(network, address)
				}
			}
			transport.Proxy = nil
		} else {
			transport.Proxy = http.ProxyURL(parsedProxyURL)
		}
	}
	if normalizedProfile.IgnoreCertErrors {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   TimeoutOrDefault(timeout),
	}, nil
}

// TimeoutOrDefault returns DefaultTimeout when timeout is not positive.
func TimeoutOrDefault(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return DefaultTimeout
	}
	return timeout
}

// IsSOCKSProxy reports whether a proxy URL uses a supported SOCKS scheme.
func IsSOCKSProxy(rawProxyURL string) bool {
	parsedProxyURL, parseError := urlParse(rawProxyURL)
	if parseError != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(parsedProxyURL.Scheme)) {
	case "socks4", "socks5", "socks5h":
		return true
	default:
		return false
	}
}

func inferredProvider(proxyURL string) string {
	parsedProxyURL, parseError := url.Parse(proxyURL)
	if parseError != nil {
		return "unknown"
	}
	if parsedProxyURL.Hostname() == "" {
		return "direct"
	}
	return parsedProxyURL.Hostname()
}

func inferredTransportID(proxyURL string) string {
	parsedProxyURL, parseError := url.Parse(proxyURL)
	if parseError != nil || parsedProxyURL.Host == "" {
		return "direct"
	}
	if parsedProxyURL.User != nil {
		return fmt.Sprintf("%s@%s", parsedProxyURL.User.Username(), parsedProxyURL.Host)
	}
	return parsedProxyURL.Host
}
