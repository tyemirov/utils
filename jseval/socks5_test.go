package jseval

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// readProxyURLsFromDotEnv walks up the directory tree to find a .env file
// in the trademark project and reads PROXY_URLS from it.
func readProxyURLsFromDotEnv(t *testing.T) []string {
	t.Helper()

	// Check a few known locations
	candidates := []string{
		filepath.Join(os.Getenv("HOME"), "Development", "trademark", ".env"),
	}

	// Also walk up from cwd
	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		candidates = append(candidates, filepath.Join(dir, ".env"))
		dir = filepath.Dir(dir)
	}

	for _, envPath := range candidates {
		file, openError := os.Open(envPath)
		if openError != nil {
			continue
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "PROXY_URLS=") {
				raw := strings.TrimPrefix(line, "PROXY_URLS=")
				var result []string
				for _, part := range strings.Split(raw, ",") {
					trimmed := strings.TrimSpace(part)
					if trimmed != "" {
						result = append(result, trimmed)
					}
				}
				return result
			}
		}
	}
	return nil
}

func findProxy(proxyURLs []string, keyword string) string {
	for _, rawProxy := range proxyURLs {
		if strings.Contains(rawProxy, keyword) {
			return rawProxy
		}
	}
	return ""
}

// TestRenderPage_SOCKS5_LocalTarget verifies Chrome renders a local page
// through a real SOCKS5 proxy. This confirms the --proxy-server flag works
// with socks5:// URLs including inline auth.
func TestRenderPage_SOCKS5_LocalTarget(t *testing.T) {
	proxyURLs := readProxyURLsFromDotEnv(t)
	webshareProxy := findProxy(proxyURLs, "webshare")
	if webshareProxy == "" {
		t.Skip("no webshare proxy found")
	}

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><div id="marker">socks5-local-ok</div></body></html>`))
	}))
	defer targetServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, renderError := RenderPage(ctx, targetServer.URL, Config{
		Timeout:      15 * time.Second,
		WaitSelector: "#marker",
		ProxyURL:     webshareProxy,
	})

	if renderError != nil {
		t.Fatalf("RenderPage through SOCKS5 (local target) failed: %v", renderError)
	}

	if !strings.Contains(result.HTML, "socks5-local-ok") {
		t.Errorf("expected 'socks5-local-ok' in HTML, got: %.200s", result.HTML)
	}
}

// TestRenderPage_SOCKS5_ExternalHTTPS verifies Chrome renders an external
// HTTPS page through SOCKS5. This is the exact failure that was happening
// in production (ERR_SOCKS_CONNECTION_FAILED) before the fix.
func TestRenderPage_SOCKS5_ExternalHTTPS(t *testing.T) {
	proxyURLs := readProxyURLsFromDotEnv(t)
	webshareProxy := findProxy(proxyURLs, "webshare")
	if webshareProxy == "" {
		t.Skip("no webshare proxy found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, renderError := RenderPage(ctx, "https://api.ipify.org", Config{
		Timeout:  15 * time.Second,
		ProxyURL: webshareProxy,
	})

	if renderError != nil {
		t.Fatalf("RenderPage through SOCKS5 (external HTTPS) failed: %v", renderError)
	}

	ip := strings.TrimSpace(result.HTML)
	t.Logf("Chrome exit IP through SOCKS5: %.100s", ip)

	if len(ip) < 7 {
		t.Errorf("expected an IP address, got: %.200s", result.HTML)
	}
}

// TestRenderPage_SOCKS5_BrightData_ExternalHTTPS tests Bright Data's
// residential SOCKS5 proxy with an external HTTPS target.
// Bright Data residential proxies do TLS interception, so this test
// requires IgnoreCertErrors to be set.
func TestRenderPage_SOCKS5_BrightData_ExternalHTTPS(t *testing.T) {
	proxyURLs := readProxyURLsFromDotEnv(t)
	brightdataProxy := findProxy(proxyURLs, "brd.superproxy")
	if brightdataProxy == "" {
		t.Skip("no brightdata proxy found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, renderError := RenderPage(ctx, "https://api.ipify.org", Config{
		Timeout:          15 * time.Second,
		ProxyURL:         brightdataProxy,
		IgnoreCertErrors: true,
	})

	if renderError != nil {
		t.Fatalf("RenderPage through BrightData SOCKS5 failed: %v", renderError)
	}

	ip := strings.TrimSpace(result.HTML)
	t.Logf("Chrome exit IP through BrightData: %.100s", ip)
}

// TestIsSOCKSProxy verifies the scheme detection helper.
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

	for _, tc := range testCases {
		t.Run(tc.proxyURL, func(t *testing.T) {
			got := isSOCKSProxy(tc.proxyURL)
			if got != tc.expected {
				t.Errorf("isSOCKSProxy(%q) = %v, want %v", tc.proxyURL, got, tc.expected)
			}
		})
	}
}
