package jseval

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRenderPageDelegatesToBrowserTransport(t *testing.T) {
	originalRenderPage := renderPage
	defer func() { renderPage = originalRenderPage }()

	expected := &Result{HTML: "<html></html>", Title: "ok", FinalURL: "https://example.com"}
	renderPage = func(ctx context.Context, targetURL string, config Config) (*Result, error) {
		if ctx == nil {
			t.Fatal("expected non-nil context")
		}
		if targetURL != "https://example.com" {
			t.Fatalf("targetURL = %q", targetURL)
		}
		if config.Timeout != time.Second {
			t.Fatalf("Timeout = %v", config.Timeout)
		}
		return expected, nil
	}

	result, renderError := RenderPage(context.Background(), "https://example.com", Config{Timeout: time.Second})
	if renderError != nil {
		t.Fatalf("RenderPage() error = %v", renderError)
	}
	if result != expected {
		t.Fatalf("RenderPage() = %#v", result)
	}

	renderPage = func(context.Context, string, Config) (*Result, error) {
		return nil, errors.New("render failed")
	}
	if _, renderError := RenderPage(context.Background(), "https://example.com", Config{}); renderError == nil {
		t.Fatal("RenderPage(error) error = nil")
	}
}

func TestRenderPagesDelegatesToBrowserTransport(t *testing.T) {
	originalRenderPages := renderPages
	defer func() { renderPages = originalRenderPages }()

	expectedResults := []*Result{{Title: "a"}, {Title: "b"}}
	expectedErrors := []error{nil, errors.New("second failed")}
	renderPages = func(ctx context.Context, targetURLs []string, config Config) ([]*Result, []error) {
		if ctx == nil {
			t.Fatal("expected non-nil context")
		}
		if len(targetURLs) != 2 {
			t.Fatalf("len(targetURLs) = %d", len(targetURLs))
		}
		if config.ProxyURL != "http://proxy.example.com:8080" {
			t.Fatalf("ProxyURL = %q", config.ProxyURL)
		}
		return expectedResults, expectedErrors
	}

	results, resultErrors := RenderPages(context.Background(), []string{"https://one.example.com", "https://two.example.com"}, Config{
		ProxyURL: "http://proxy.example.com:8080",
	})
	if len(results) != 2 || len(resultErrors) != 2 {
		t.Fatalf("RenderPages() lengths = %d/%d", len(results), len(resultErrors))
	}
	if results[0] != expectedResults[0] || resultErrors[1] != expectedErrors[1] {
		t.Fatalf("RenderPages() = %#v %#v", results, resultErrors)
	}
}
