package browsertransport

import "testing"

func TestStripProxyAuth(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"http://user:pass@proxy.example.com:8080", "http://proxy.example.com:8080"},
		{"https://user:pass@proxy.example.com:8443", "https://proxy.example.com:8443"},
		{"http://proxy.example.com:8080", "http://proxy.example.com:8080"},
		{"not-a-url", "not-a-url"},
		{"http://proxy.example.com:\x00", "http://proxy.example.com:\x00"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.input, func(t *testing.T) {
			got := stripProxyAuth(testCase.input)
			if got != testCase.expected {
				t.Fatalf("stripProxyAuth(%q) = %q, want %q", testCase.input, got, testCase.expected)
			}
		})
	}
}

func TestExtractProxyCredentials(t *testing.T) {
	testCases := []struct {
		input            string
		expectedUsername string
		expectedPassword string
	}{
		{"http://user:pass@proxy.example.com:8080", "user", "pass"},
		{"https://alice:secret@proxy.example.com:8443", "alice", "secret"},
		{"http://proxy.example.com:8080", "", ""},
		{"not-a-url", "", ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.input, func(t *testing.T) {
			username, password := extractProxyCredentials(testCase.input)
			if username != testCase.expectedUsername {
				t.Fatalf("username = %q, want %q", username, testCase.expectedUsername)
			}
			if password != testCase.expectedPassword {
				t.Fatalf("password = %q, want %q", password, testCase.expectedPassword)
			}
		})
	}
}

func TestIsSOCKSProxy(t *testing.T) {
	testCases := []struct {
		proxyURL string
		expected bool
	}{
		{"socks5://user:pass@host:1080", true},
		{"socks5h://user:pass@host:22228", true},
		{"socks4://host:1080", true},
		{"http://user:pass@host:80", false},
		{"https://host:443", false},
		{"", false},
		{"not-a-url", false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.proxyURL, func(t *testing.T) {
			got := isSOCKSProxy(testCase.proxyURL)
			if got != testCase.expected {
				t.Fatalf("isSOCKSProxy(%q) = %v, want %v", testCase.proxyURL, got, testCase.expected)
			}
		})
	}
}
