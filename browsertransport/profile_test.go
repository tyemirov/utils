package browsertransport

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestInferBrowserProfile(t *testing.T) {
	testCases := []struct {
		name              string
		rawProxyURL       string
		ignoreCertErrors  bool
		expectedID        string
		expectedProvider  string
		expectedMode      BrowserMode
		expectedProxyURL  string
		expectedIgnoreTLS bool
		expectedError     string
	}{
		{
			name:              "direct",
			rawProxyURL:       "",
			ignoreCertErrors:  true,
			expectedID:        "direct",
			expectedProvider:  "direct",
			expectedMode:      BrowserModeDirect,
			expectedIgnoreTLS: true,
		},
		{
			name:             "http proxy",
			rawProxyURL:      "http://proxy.example.com:8080",
			expectedID:       "proxy.example.com:8080",
			expectedProvider: "proxy.example.com",
			expectedMode:     BrowserModeDirect,
			expectedProxyURL: "http://proxy.example.com:8080",
		},
		{
			name:             "http proxy with auth",
			rawProxyURL:      "http://user:pass@proxy.example.com:8080",
			expectedID:       "user@proxy.example.com:8080",
			expectedProvider: "proxy.example.com",
			expectedMode:     BrowserModeHTTPFetchAuth,
			expectedProxyURL: "http://user:pass@proxy.example.com:8080",
		},
		{
			name:             "socks proxy",
			rawProxyURL:      "socks5://user:pass@proxy.example.com:1080",
			expectedID:       "user@proxy.example.com:1080",
			expectedProvider: "proxy.example.com",
			expectedMode:     BrowserModeSOCKSForwarder,
			expectedProxyURL: "socks5://user:pass@proxy.example.com:1080",
		},
		{
			name:          "invalid parse",
			rawProxyURL:   "://bad\x00proxy",
			expectedError: "parsing browser proxy URL",
		},
		{
			name:          "invalid shape",
			rawProxyURL:   "http://",
			expectedError: "invalid browser proxy URL",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			profile, inferError := InferBrowserProfile(testCase.rawProxyURL, testCase.ignoreCertErrors)
			if testCase.expectedError != "" {
				if inferError == nil || inferError.Error() == "" || !contains(inferError.Error(), testCase.expectedError) {
					t.Fatalf("InferBrowserProfile() error = %v, want substring %q", inferError, testCase.expectedError)
				}
				return
			}
			if inferError != nil {
				t.Fatalf("InferBrowserProfile() error = %v", inferError)
			}
			if profile.ID != testCase.expectedID || profile.Provider != testCase.expectedProvider || profile.Mode != testCase.expectedMode || profile.URL != testCase.expectedProxyURL || profile.IgnoreCertErrors != testCase.expectedIgnoreTLS {
				t.Fatalf("InferBrowserProfile() = %#v", profile)
			}
		})
	}
}

func TestInferHTTPProfile(t *testing.T) {
	testCases := []struct {
		name             string
		rawProxyURL      string
		ignoreCertErrors bool
		expectedID       string
		expectedProvider string
		expectedProxyURL string
		expectedError    string
	}{
		{
			name:             "direct",
			rawProxyURL:      "",
			ignoreCertErrors: true,
			expectedID:       "direct",
			expectedProvider: "direct",
		},
		{
			name:             "http proxy",
			rawProxyURL:      "http://user:pass@proxy.example.com:8080",
			expectedID:       "user@proxy.example.com:8080",
			expectedProvider: "proxy.example.com",
			expectedProxyURL: "http://user:pass@proxy.example.com:8080",
		},
		{
			name:          "invalid parse",
			rawProxyURL:   "://bad\x00proxy",
			expectedError: "parsing HTTP proxy URL",
		},
		{
			name:          "invalid shape",
			rawProxyURL:   "http://",
			expectedError: "invalid HTTP proxy URL",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			profile, inferError := InferHTTPProfile(testCase.rawProxyURL, testCase.ignoreCertErrors)
			if testCase.expectedError != "" {
				if inferError == nil || !contains(inferError.Error(), testCase.expectedError) {
					t.Fatalf("InferHTTPProfile() error = %v, want substring %q", inferError, testCase.expectedError)
				}
				return
			}
			if inferError != nil {
				t.Fatalf("InferHTTPProfile() error = %v", inferError)
			}
			if profile.ID != testCase.expectedID || profile.Provider != testCase.expectedProvider || profile.URL != testCase.expectedProxyURL || profile.IgnoreCertErrors != testCase.ignoreCertErrors {
				t.Fatalf("InferHTTPProfile() = %#v", profile)
			}
		})
	}
}

func TestNormalizeBrowserProfile(t *testing.T) {
	testCases := []struct {
		name          string
		profile       BrowserProfile
		expected      BrowserProfile
		expectedError string
	}{
		{
			name: "infer and preserve metadata",
			profile: BrowserProfile{
				ID:               "custom-id",
				Provider:         "custom-provider",
				URL:              "http://user:pass@proxy.example.com:8080",
				IgnoreCertErrors: true,
			},
			expected: BrowserProfile{
				ID:               "custom-id",
				Provider:         "custom-provider",
				URL:              "http://user:pass@proxy.example.com:8080",
				Mode:             BrowserModeHTTPFetchAuth,
				IgnoreCertErrors: true,
			},
		},
		{
			name: "direct explicit",
			profile: BrowserProfile{
				Mode: BrowserModeDirect,
				URL:  "http://proxy.example.com:8080",
			},
			expected: BrowserProfile{
				ID:       "proxy.example.com:8080",
				Provider: "proxy.example.com",
				URL:      "http://proxy.example.com:8080",
				Mode:     BrowserModeDirect,
			},
		},
		{
			name: "direct cannot use socks",
			profile: BrowserProfile{
				Mode: BrowserModeDirect,
				URL:  "socks5://proxy.example.com:1080",
			},
			expectedError: "cannot use a SOCKS proxy URL",
		},
		{
			name: "direct cannot use inline credentials",
			profile: BrowserProfile{
				Mode: BrowserModeDirect,
				URL:  "http://user:pass@proxy.example.com:8080",
			},
			expectedError: "cannot use inline proxy credentials",
		},
		{
			name: "fetch auth requires url",
			profile: BrowserProfile{
				Mode: BrowserModeHTTPFetchAuth,
			},
			expectedError: "requires a proxy URL",
		},
		{
			name: "fetch auth cannot use socks",
			profile: BrowserProfile{
				Mode: BrowserModeHTTPFetchAuth,
				URL:  "socks5://proxy.example.com:1080",
			},
			expectedError: "cannot use a SOCKS proxy URL",
		},
		{
			name: "socks forwarder requires url",
			profile: BrowserProfile{
				Mode: BrowserModeSOCKSForwarder,
			},
			expectedError: "requires a proxy URL",
		},
		{
			name: "socks forwarder requires socks url",
			profile: BrowserProfile{
				Mode: BrowserModeSOCKSForwarder,
				URL:  "http://proxy.example.com:8080",
			},
			expectedError: "requires a SOCKS proxy URL",
		},
		{
			name: "unsupported mode",
			profile: BrowserProfile{
				Mode: BrowserMode("bad-mode"),
				URL:  "http://proxy.example.com:8080",
			},
			expectedError: "unsupported browser mode",
		},
		{
			name: "invalid parse",
			profile: BrowserProfile{
				Mode: BrowserModeHTTPFetchAuth,
				URL:  "://bad\x00proxy",
			},
			expectedError: "parsing browser proxy URL",
		},
		{
			name: "invalid shape",
			profile: BrowserProfile{
				Mode: BrowserModeHTTPFetchAuth,
				URL:  "http://",
			},
			expectedError: "invalid browser proxy URL",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			profile, normalizeError := normalizeBrowserProfile(testCase.profile)
			if testCase.expectedError != "" {
				if normalizeError == nil || !contains(normalizeError.Error(), testCase.expectedError) {
					t.Fatalf("normalizeBrowserProfile() error = %v, want substring %q", normalizeError, testCase.expectedError)
				}
				return
			}
			if normalizeError != nil {
				t.Fatalf("normalizeBrowserProfile() error = %v", normalizeError)
			}
			if profile != testCase.expected {
				t.Fatalf("normalizeBrowserProfile() = %#v, want %#v", profile, testCase.expected)
			}
		})
	}
}

func TestNormalizeHTTPProfileAndHelpers(t *testing.T) {
	profile, normalizeError := normalizeHTTPProfile(HTTPProfile{})
	if normalizeError != nil {
		t.Fatalf("normalizeHTTPProfile() error = %v", normalizeError)
	}
	if profile.ID != "direct" || profile.Provider != "direct" {
		t.Fatalf("normalizeHTTPProfile() = %#v", profile)
	}

	profile, normalizeError = normalizeHTTPProfile(HTTPProfile{URL: "http://user:pass@proxy.example.com:8080"})
	if normalizeError != nil {
		t.Fatalf("normalizeHTTPProfile(http proxy) error = %v", normalizeError)
	}
	if profile.ID != "user@proxy.example.com:8080" || profile.Provider != "proxy.example.com" {
		t.Fatalf("normalizeHTTPProfile(http proxy) = %#v", profile)
	}

	if _, normalizeError = normalizeHTTPProfile(HTTPProfile{URL: "://bad\x00proxy"}); normalizeError == nil || !contains(normalizeError.Error(), "parsing HTTP proxy URL") {
		t.Fatalf("normalizeHTTPProfile(parse) error = %v", normalizeError)
	}
	if _, normalizeError = normalizeHTTPProfile(HTTPProfile{URL: "http://"}); normalizeError == nil || !contains(normalizeError.Error(), "invalid HTTP proxy URL") {
		t.Fatalf("normalizeHTTPProfile(shape) error = %v", normalizeError)
	}

	if got := inferredProvider("http://user:pass@proxy.example.com:8080"); got != "proxy.example.com" {
		t.Fatalf("inferredProvider(http) = %q", got)
	}
	if got := inferredProvider(""); got != "direct" {
		t.Fatalf("inferredProvider(blank) = %q", got)
	}
	if got := inferredProvider("://bad\x00proxy"); got != "unknown" {
		t.Fatalf("inferredProvider(invalid) = %q", got)
	}

	if got := inferredTransportID("http://user:pass@proxy.example.com:8080"); got != "user@proxy.example.com:8080" {
		t.Fatalf("inferredTransportID(http) = %q", got)
	}
	if got := inferredTransportID("http://proxy.example.com:8080"); got != "proxy.example.com:8080" {
		t.Fatalf("inferredTransportID(no auth) = %q", got)
	}
	if got := inferredTransportID("://bad\x00proxy"); got != "direct" {
		t.Fatalf("inferredTransportID(invalid) = %q", got)
	}

	if got := httpTimeoutOrDefault(0); got != defaultHTTPTimeout {
		t.Fatalf("httpTimeoutOrDefault(0) = %v", got)
	}
	if got := httpTimeoutOrDefault(3 * time.Second); got != 3*time.Second {
		t.Fatalf("httpTimeoutOrDefault(nonzero) = %v", got)
	}

	if _, normalizeError = normalizeBrowserProfile(BrowserProfile{URL: "://bad\x00proxy"}); normalizeError == nil || !contains(normalizeError.Error(), "parsing browser proxy URL") {
		t.Fatalf("normalizeBrowserProfile(inferred parse) error = %v", normalizeError)
	}
}

func TestIsSOCKSProxyParseError(t *testing.T) {
	restoreHooks := resetBrowserTransportHooks()
	defer restoreHooks()

	urlParse = func(rawURL string) (*url.URL, error) {
		return nil, fmt.Errorf("parse failed")
	}

	if isSOCKSProxy("socks5://proxy.example.com:1080") {
		t.Fatal("isSOCKSProxy(parse error) = true")
	}
}

func contains(value string, substring string) bool {
	return substring == "" || (value != "" && strings.Contains(value, substring))
}
