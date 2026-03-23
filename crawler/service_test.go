package crawler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type testEvaluator struct {
	selector string
}

func (e *testEvaluator) Evaluate(targetID string, doc *goquery.Document) (Evaluation, error) {
	var findings []Finding
	doc.Find(e.selector).Each(func(_ int, s *goquery.Selection) {
		findings = append(findings, Finding{
			Description: "found",
			Passed:      true,
			Data:        s.Text(),
		})
	})
	return Evaluation{Findings: findings}, nil
}

func TestNewTarget(t *testing.T) {
	_, err := NewTarget("", "cat", "http://example.com")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
	_, err = NewTarget("id", "", "http://example.com")
	if err == nil {
		t.Fatal("expected error for empty category")
	}
	_, err = NewTarget("id", "cat", "")
	if err == nil {
		t.Fatal("expected error for empty url")
	}
	target, err := NewTarget("id", "cat", "http://example.com", WithMetadata("key", "val"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.MetadataValue("key") != "val" {
		t.Errorf("expected metadata key=val, got %q", target.MetadataValue("key"))
	}
}

func TestCrawlerBasic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Test</title></head><body>
			<div class="item">Camp A</div>
			<div class="item">Camp B</div>
			<div class="item">Camp C</div>
		</body></html>`)
	}))
	defer server.Close()

	results := make(chan *Result, 10)

	target, _ := NewTarget("test1", "camps", server.URL+"/page1")
	svc, err := NewService(Config{
		Category:  "camps",
		Scraper:   ScraperConfig{Parallelism: 2, HTTPTimeout: 5 * time.Second},
		Platform:  PlatformConfig{AllowedDomains: []string{"127.0.0.1"}},
		Evaluator: &testEvaluator{selector: ".item"},
	}, results)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	go func() {
		svc.Run(context.Background(), []Target{target})
		close(results)
	}()

	var got []*Result
	for r := range results {
		got = append(got, r)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	r := got[0]
	if !r.Success {
		t.Fatalf("expected success, got error: %s", r.ErrorMessage)
	}
	if r.TargetID != "test1" {
		t.Errorf("expected targetID test1, got %s", r.TargetID)
	}
	if r.Title != "Test" {
		t.Errorf("expected title 'Test', got %q", r.Title)
	}
	if len(r.Findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(r.Findings))
	}
	if r.Findings[0].Data != "Camp A" {
		t.Errorf("expected 'Camp A', got %q", r.Findings[0].Data)
	}
}

func TestCrawlerRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><div class="ok">success</div></body></html>`)
	}))
	defer server.Close()

	results := make(chan *Result, 10)

	svc, err := NewService(Config{
		Category:  "test",
		Scraper:   ScraperConfig{Parallelism: 1, RetryCount: 3, HTTPTimeout: 5 * time.Second},
		Platform:  PlatformConfig{AllowedDomains: []string{"127.0.0.1"}},
		Evaluator: &testEvaluator{selector: ".ok"},
	}, results)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	target, _ := NewTarget("retry-test", "test", server.URL+"/flaky")
	go func() {
		svc.Run(context.Background(), []Target{target})
		close(results)
	}()

	var got []*Result
	for r := range results {
		got = append(got, r)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if !got[0].Success {
		t.Errorf("expected success after retries, got error: %s", got[0].ErrorMessage)
	}
}

func TestConfigValidation(t *testing.T) {
	cfg := Config{Category: "", Scraper: ScraperConfig{Parallelism: 1}, Platform: PlatformConfig{AllowedDomains: []string{"x"}}, Evaluator: &testEvaluator{}}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty category")
	}
	cfg.Category = "test"
	cfg.Scraper.Parallelism = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for zero parallelism")
	}
	cfg.Scraper.Parallelism = 1
	cfg.Platform.AllowedDomains = nil
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty domains")
	}
	cfg.Platform.AllowedDomains = []string{"x"}
	cfg.Evaluator = nil
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for nil evaluator without response handler")
	}
}

func TestSanitizeProxyURL(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"", ""},
		{"http://host:8080", "http://host:8080"},
		{"http://user:pass@host:8080", "http://host:8080"},
		{"http://user:pass@host:8080/path", "http://host:8080/path"},
	}
	for _, tc := range tests {
		got := SanitizeProxyURL(tc.input)
		if got != tc.expected {
			t.Errorf("SanitizeProxyURL(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
