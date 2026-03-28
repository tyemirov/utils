package browsertransport

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestRenderPageBasicHTML(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/html")
		_, _ = responseWriter.Write([]byte(`<!DOCTYPE html><html><head><title>Test Page</title></head><body><div id="content">Static</div><script>document.getElementById('content').textContent = 'Dynamic';</script></body></html>`))
	}))
	defer testServer.Close()

	result, renderError := RenderPage(context.Background(), testServer.URL, Config{
		Timeout: 10 * time.Second,
	})
	if renderError != nil {
		t.Fatalf("RenderPage() error = %v", renderError)
	}
	if result.Title != "Test Page" {
		t.Fatalf("Title = %q", result.Title)
	}
	if !strings.Contains(result.HTML, "Dynamic") {
		t.Fatalf("HTML = %q", result.HTML)
	}
	if !strings.Contains(result.FinalURL, testServer.URL) {
		t.Fatalf("FinalURL = %q", result.FinalURL)
	}
}

func TestRenderPageWaitSelectorAndDefaultTimeout(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/html")
		_, _ = responseWriter.Write([]byte(`<!DOCTYPE html><html><head><title>Delayed</title></head><body><script>setTimeout(function(){var el=document.createElement('div');el.id='loaded';el.textContent='Ready';document.body.appendChild(el);},100);</script></body></html>`))
	}))
	defer testServer.Close()

	result, renderError := RenderPage(context.Background(), testServer.URL, Config{
		WaitSelector: "#loaded",
	})
	if renderError != nil {
		t.Fatalf("RenderPage() error = %v", renderError)
	}
	if !strings.Contains(result.HTML, "Ready") {
		t.Fatalf("HTML = %q", result.HTML)
	}
}

func TestRenderPageTimeoutAndInvalidURL(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/html")
		_, _ = responseWriter.Write([]byte(`<!DOCTYPE html><html><head><title>Slow</title></head><body></body></html>`))
	}))
	defer testServer.Close()

	if _, renderError := RenderPage(context.Background(), testServer.URL, Config{
		Timeout:      time.Second,
		WaitSelector: "#never-exists",
	}); renderError == nil {
		t.Fatal("RenderPage(timeout) error = nil")
	}

	if _, renderError := RenderPage(context.Background(), "http://localhost:1/nonexistent", Config{
		Timeout: 5 * time.Second,
	}); renderError == nil {
		t.Fatal("RenderPage(invalid URL) error = nil")
	}

	if _, renderError := RenderPage(context.Background(), "https://example.com", Config{
		ProxyURL: "://bad\x00proxy",
	}); renderError == nil {
		t.Fatal("RenderPage(invalid proxy URL) error = nil")
	}
}

func TestRenderPagesConcurrent(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/html")
		_, _ = responseWriter.Write([]byte(`<!DOCTYPE html><html><head><title>Page</title></head><body>Content</body></html>`))
	}))
	defer testServer.Close()

	results, resultErrors := RenderPages(context.Background(), []string{
		testServer.URL + "/a",
		testServer.URL + "/b",
		testServer.URL + "/c",
	}, Config{
		Timeout: 10 * time.Second,
	})

	if len(results) != 3 || len(resultErrors) != 3 {
		t.Fatalf("RenderPages() lengths = %d/%d", len(results), len(resultErrors))
	}
	for resultIndex, renderError := range resultErrors {
		if renderError != nil {
			t.Fatalf("RenderPages() error[%d] = %v", resultIndex, renderError)
		}
		if results[resultIndex] == nil || results[resultIndex].Title != "Page" {
			t.Fatalf("RenderPages() result[%d] = %#v", resultIndex, results[resultIndex])
		}
	}
}

func TestRenderPageProxyAndTLSOptions(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/html")
		_, _ = responseWriter.Write([]byte(`<html><head><title>Proxied</title></head><body><div id="ok">proxy-auth-ok</div></body></html>`))
	}))
	defer targetServer.Close()

	proxyServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("testuser:testpass"))
		if request.Header.Get("Proxy-Authorization") != expectedAuth {
			responseWriter.Header().Set("Proxy-Authenticate", "Basic realm=\"proxy\"")
			responseWriter.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		response, requestError := http.Get(request.URL.String())
		if requestError != nil {
			http.Error(responseWriter, requestError.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			for _, value := range values {
				responseWriter.Header().Add(key, value)
			}
		}
		responseWriter.WriteHeader(response.StatusCode)
		_, _ = io.Copy(responseWriter, response.Body)
	}))
	defer proxyServer.Close()

	result, renderError := RenderPage(context.Background(), targetServer.URL, Config{
		Timeout:  10 * time.Second,
		ProxyURL: strings.Replace(proxyServer.URL, "http://", "http://testuser:testpass@", 1),
	})
	if renderError != nil {
		t.Fatalf("RenderPage(proxy auth) error = %v", renderError)
	}
	if !strings.Contains(result.HTML, "proxy-auth-ok") {
		t.Fatalf("HTML = %q", result.HTML)
	}

	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/html")
		_, _ = responseWriter.Write([]byte(`<html><head><title>TLS</title></head><body><div id="secure">tls-ok</div></body></html>`))
	}))
	defer tlsServer.Close()

	result, renderError = RenderPage(context.Background(), tlsServer.URL, Config{
		Timeout:          10 * time.Second,
		IgnoreCertErrors: true,
		WaitSelector:     "#secure",
		UserAgent:        "CustomBot/1.0",
	})
	if renderError != nil {
		t.Fatalf("RenderPage(ignore certs) error = %v", renderError)
	}
	if !strings.Contains(result.HTML, "tls-ok") {
		t.Fatalf("HTML = %q", result.HTML)
	}
}

func TestRenderPageInjectedHTTPProxyBranches(t *testing.T) {
	restoreHooks := resetBrowserTransportHooks()
	defer restoreHooks()

	originalRenderTimeout := defaultRenderTimeout
	_ = originalRenderTimeout

	type contextKey string

	const nameKey contextKey = "name"

	chromedpNewExecAllocator = func(parent context.Context, options ...chromedp.ExecAllocatorOption) (context.Context, context.CancelFunc) {
		return context.WithValue(parent, nameKey, "allocator"), func() {}
	}

	newContextCallCount := 0
	chromedpNewContext = func(parent context.Context, options ...chromedp.ContextOption) (context.Context, context.CancelFunc) {
		newContextCallCount++
		contextName := "browser"
		if newContextCallCount == 2 {
			contextName = "render-target"
		}
		return context.WithValue(parent, nameKey, contextName), func() {}
	}

	var setupContextName string
	setupProxyAuthFn = func(ctx context.Context, username string, password string) {
		setupContextName, _ = ctx.Value(nameKey).(string)
	}

	var runContextNames []string
	chromedpRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		contextName, _ := ctx.Value(nameKey).(string)
		runContextNames = append(runContextNames, contextName)
		return nil
	}

	result, renderError := RenderPage(context.Background(), "https://example.com", Config{
		Timeout:  time.Second,
		ProxyURL: "http://user:pass@proxy.example.com:8080",
	})
	if renderError != nil {
		t.Fatalf("RenderPage(render target auth) error = %v", renderError)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if newContextCallCount != 2 {
		t.Fatalf("new context call count = %d", newContextCallCount)
	}
	if setupContextName != "render-target" {
		t.Fatalf("setup context = %q", setupContextName)
	}
	if len(runContextNames) < 3 {
		t.Fatalf("run context names = %v", runContextNames)
	}
	if runContextNames[0] != "browser" {
		t.Fatalf("run context[0] = %q", runContextNames[0])
	}
	for runIndex, contextName := range runContextNames[1:] {
		if contextName != "render-target" {
			t.Fatalf("run context[%d] = %q", runIndex+1, contextName)
		}
	}

	runCallCount := 0
	chromedpRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		runCallCount++
		if runCallCount == 3 {
			return fmt.Errorf("mock fetch enable error")
		}
		return nil
	}
	if _, renderError = RenderPage(context.Background(), "https://example.com", Config{
		Timeout:  time.Second,
		ProxyURL: "http://user:pass@proxy.example.com:8080",
	}); renderError == nil || !strings.Contains(renderError.Error(), "enabling fetch for proxy auth") {
		t.Fatalf("RenderPage(fetch enable) error = %v", renderError)
	}

	runCallCount = 0
	chromedpRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		runCallCount++
		if runCallCount == 1 {
			return fmt.Errorf("mock browser start error")
		}
		return nil
	}
	if _, renderError = RenderPage(context.Background(), "https://example.com", Config{}); renderError == nil || !strings.Contains(renderError.Error(), "mock browser start error") {
		t.Fatalf("RenderPage(session start) error = %v", renderError)
	}
}
