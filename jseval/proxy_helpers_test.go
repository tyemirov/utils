package jseval

import (
	"fmt"
	"net/url"
	"testing"
)

func TestStripProxyAuth(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"with auth", "http://user:pass@proxy.example.com:8080", "http://proxy.example.com:8080"},
		{"without auth", "http://proxy.example.com:8080", "http://proxy.example.com:8080"},
		{"socks with auth", "socks5://user:pass@proxy.example.com:1080", "socks5://proxy.example.com:1080"},
		{"username only", "http://user@proxy.example.com:8080", "http://proxy.example.com:8080"},
		{"with path", "http://user:pass@proxy.example.com:8080/path", "http://proxy.example.com:8080/path"},
		{"invalid url", "://invalid", "://invalid"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripProxyAuth(tc.input)
			if got != tc.expected {
				t.Errorf("stripProxyAuth(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestExtractProxyCredentials(t *testing.T) {
	testCases := []struct {
		name             string
		input            string
		expectedUsername string
		expectedPassword string
	}{
		{"with auth", "http://user:pass@proxy.example.com:8080", "user", "pass"},
		{"username only", "http://user@proxy.example.com:8080", "user", ""},
		{"no auth", "http://proxy.example.com:8080", "", ""},
		{"socks with auth", "socks5://myuser:mypass@host:1080", "myuser", "mypass"},
		{"special chars in password", "http://user:p%40ss@host:8080", "user", "p@ss"},
		{"empty string", "", "", ""},
		{"invalid url", "://invalid", "", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			username, password := extractProxyCredentials(tc.input)
			if username != tc.expectedUsername {
				t.Errorf("extractProxyCredentials(%q) username = %q, want %q", tc.input, username, tc.expectedUsername)
			}
			if password != tc.expectedPassword {
				t.Errorf("extractProxyCredentials(%q) password = %q, want %q", tc.input, password, tc.expectedPassword)
			}
		})
	}
}

func TestIsSOCKSProxyParseError(t *testing.T) {
	original := urlParse
	defer func() { urlParse = original }()

	urlParse = func(rawURL string) (*url.URL, error) {
		return nil, fmt.Errorf("mock parse error")
	}

	got := isSOCKSProxy("socks5://host:1080")
	if got {
		t.Error("expected false when url.Parse fails")
	}
}
