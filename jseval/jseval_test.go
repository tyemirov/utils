package jseval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRenderPage_BasicHTML(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/html")
		responseWriter.Write([]byte(`<!DOCTYPE html><html><head><title>Test Page</title></head><body><div id="content">Static</div><script>document.getElementById('content').textContent = 'Dynamic';</script></body></html>`))
	}))
	defer testServer.Close()

	result, renderError := RenderPage(context.Background(), testServer.URL, Config{
		Timeout: 10 * time.Second,
	})
	if renderError != nil {
		t.Fatalf("render failed: %v", renderError)
	}

	if result.Title != "Test Page" {
		t.Errorf("expected title 'Test Page', got '%s'", result.Title)
	}

	// The JS should have changed "Static" to "Dynamic"
	if !strings.Contains(result.HTML, "Dynamic") {
		t.Error("expected rendered HTML to contain 'Dynamic' (JS-modified content)")
	}

	if !strings.Contains(result.FinalURL, testServer.URL) {
		t.Errorf("expected final URL to contain server URL, got '%s'", result.FinalURL)
	}
}

func TestRenderPage_WaitSelector(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/html")
		responseWriter.Write([]byte(`<!DOCTYPE html><html><head><title>Delayed</title></head><body><script>setTimeout(function(){var el=document.createElement('div');el.id='loaded';el.textContent='Ready';document.body.appendChild(el);},100);</script></body></html>`))
	}))
	defer testServer.Close()

	result, renderError := RenderPage(context.Background(), testServer.URL, Config{
		Timeout:      10 * time.Second,
		WaitSelector: "#loaded",
	})
	if renderError != nil {
		t.Fatalf("render failed: %v", renderError)
	}

	if !strings.Contains(result.HTML, "Ready") {
		t.Error("expected rendered HTML to contain 'Ready' after waiting for selector")
	}
}

func TestRenderPage_Timeout(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/html")
		responseWriter.Write([]byte(`<!DOCTYPE html><html><head><title>Slow</title></head><body></body></html>`))
	}))
	defer testServer.Close()

	_, renderError := RenderPage(context.Background(), testServer.URL, Config{
		Timeout:      1 * time.Second,
		WaitSelector: "#never-exists",
	})
	if renderError == nil {
		t.Fatal("expected timeout error when waiting for non-existent selector")
	}
}

func TestRenderPage_InvalidURL(t *testing.T) {
	_, renderError := RenderPage(context.Background(), "http://localhost:1/nonexistent", Config{
		Timeout: 5 * time.Second,
	})
	if renderError == nil {
		t.Fatal("expected error for unreachable URL")
	}
}

func TestRenderPages_Concurrent(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/html")
		responseWriter.Write([]byte(`<!DOCTYPE html><html><head><title>Page</title></head><body>Content</body></html>`))
	}))
	defer testServer.Close()

	urls := []string{testServer.URL + "/a", testServer.URL + "/b", testServer.URL + "/c"}

	results, errors := RenderPages(context.Background(), urls, Config{
		Timeout: 10 * time.Second,
	})

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for urlIndex, renderError := range errors {
		if renderError != nil {
			t.Errorf("URL %d error: %v", urlIndex, renderError)
		}
	}

	for urlIndex, result := range results {
		if result == nil {
			t.Errorf("URL %d result is nil", urlIndex)
			continue
		}

		if result.Title != "Page" {
			t.Errorf("URL %d expected title 'Page', got '%s'", urlIndex, result.Title)
		}
	}
}
