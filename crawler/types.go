// Package crawler provides a generic, configurable web crawler built on Colly.
// It supports concurrent requests, retries with exponential backoff, rate limiting,
// proxy rotation, and pluggable document evaluation via the Evaluator interface.
package crawler

import (
	"context"
	"net/http"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
)

// Page describes a single URL to crawl.
type Page struct {
	ID       string // Unique identifier for this page
	Category string // Grouping label (e.g., platform, source)
	URL      string // Full URL to fetch
}

// Result represents the outcome of crawling a single page.
type Result struct {
	PageID         string            `json:"pageId"`
	PageURL        string            `json:"pageUrl"`
	FinalURL       string            `json:"finalUrl,omitempty"`
	Category       string            `json:"category"`
	Title          string            `json:"title,omitempty"`
	Success        bool              `json:"success"`
	ErrorMessage   string            `json:"errorMessage,omitempty"`
	HTTPStatusCode int               `json:"httpStatusCode,omitempty"`
	Findings       []Finding         `json:"findings,omitempty"`
	Document       *goquery.Document `json:"-"` // parsed HTML, not serialized
}

// Finding captures a single evaluation outcome from the Evaluator.
type Finding struct {
	ID          string `json:"id,omitempty"`
	Description string `json:"description"`
	Passed      bool   `json:"passed"`
	Message     string `json:"message"`
	Data        string `json:"data,omitempty"` // arbitrary payload (e.g., JSON-encoded extracted data)
}

// Evaluation is the output of an Evaluator.
type Evaluation struct {
	Findings []Finding
}

// Evaluator processes a fetched HTML document and produces findings.
// Implementations are injected into the crawler at construction time.
type Evaluator interface {
	Evaluate(pageID string, document *goquery.Document) (Evaluation, error)
}

// Logger emits structured diagnostic messages. Safe for concurrent use.
type Logger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warning(format string, args ...interface{})
	Error(format string, args ...interface{})
}

// RequestHook runs before each outbound request. Optional.
type RequestHook interface {
	BeforeRequest(ctx context.Context, page Page) error
}

// HeaderProvider decorates outbound HTTP requests. Optional.
type HeaderProvider interface {
	Apply(request *colly.Request)
}

// CookieProvider returns cookies for a given domain. Optional.
type CookieProvider func(domain string) []*http.Cookie
