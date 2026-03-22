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

func (e *testEvaluator) Evaluate(pageID string, doc *goquery.Document) (Evaluation, error) {
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

func TestCrawlerBasic(t *testing.T) {
	// Serve a test page
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>
			<div class="item">Camp A</div>
			<div class="item">Camp B</div>
			<div class="item">Camp C</div>
		</body></html>`)
	}))
	defer server.Close()

	results := make(chan *Result, 10)

	svc, err := NewService(Config{
		AllowedDomains: []string{"127.0.0.1"},
		Parallelism:    2,
		HTTPTimeout:    5 * time.Second,
		Evaluator:      &testEvaluator{selector: ".item"},
	}, results)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	go func() {
		svc.Run(context.Background(), []Page{
			{ID: "test1", Category: "camps", URL: server.URL + "/page1"},
		})
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
	if r.PageID != "test1" {
		t.Errorf("expected pageID test1, got %s", r.PageID)
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
		AllowedDomains: []string{"127.0.0.1"},
		Parallelism:    1,
		RetryCount:     3,
		HTTPTimeout:    5 * time.Second,
		Evaluator:      &testEvaluator{selector: ".ok"},
	}, results)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	go func() {
		svc.Run(context.Background(), []Page{
			{ID: "retry-test", URL: server.URL + "/flaky"},
		})
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
