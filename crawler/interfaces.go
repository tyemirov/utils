package crawler

import (
	"context"
	"net/http"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
)

// ResponseHandler processes HTTP responses and emits results.
// Implementations are injected into the Service at construction time.
// The default handler parses HTML, runs an Evaluator, and sends Results.
// Custom handlers can implement platform-specific logic (image download,
// discoverability probing, etc.).
type ResponseHandler interface {
	Setup(collector *colly.Collector)
	SendResult(resp *colly.Response, success bool, errorMessage string)
	SetSlotReleaser(releaser func(*colly.Response))
}

// Evaluator processes a fetched HTML document and produces findings.
// Used by the default ResponseHandler. Not needed when a custom
// ResponseHandler is provided.
type Evaluator interface {
	Evaluate(targetID string, document *goquery.Document) (Evaluation, error)
}

// RetryHandler encapsulates retry behaviour for failed responses.
type RetryHandler interface {
	Retry(response *colly.Response, options RetryOptions) bool
}

// RetryOptions controls retry behaviour per-request.
type RetryOptions struct {
	SkipDelay    bool
	LimitRetries bool
	MaxRetries   int
}

// Logger emits structured diagnostic messages. Safe for concurrent use.
type Logger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warning(format string, args ...interface{})
	Error(format string, args ...interface{})
}

// HeaderProvider decorates outbound HTTP requests. Optional.
type HeaderProvider interface {
	Apply(category string, request *colly.Request)
}

// CookieProvider returns cookies for a given domain. Optional.
type CookieProvider func(domain string) []*http.Cookie

// RequestHook runs before each outbound request. Optional.
type RequestHook interface {
	BeforeRequest(ctx context.Context, target Target) error
}

// FilePersister persists binary artifacts associated with a target.
type FilePersister interface {
	Save(targetID, fileName string, content []byte) error
	Close() error
}

// ProxyHealth tracks proxy availability for circuit-breaker rotation.
type ProxyHealth interface {
	IsAvailable(proxy string) bool
	RecordSuccess(proxy string)
	RecordFailure(proxy string)
	RecordCriticalFailure(proxy string)
}

type noopLogger struct{}

func (noopLogger) Debug(string, ...interface{})   {}
func (noopLogger) Info(string, ...interface{})    {}
func (noopLogger) Warning(string, ...interface{}) {}
func (noopLogger) Error(string, ...interface{})   {}

func ensureLogger(logger Logger) Logger {
	if logger == nil {
		return noopLogger{}
	}
	return logger
}

type headerProviderFunc func(category string, request *colly.Request)

func (fn headerProviderFunc) Apply(category string, request *colly.Request) {
	if fn != nil {
		fn(category, request)
	}
}

func ensureHeaders(provider HeaderProvider) HeaderProvider {
	if provider == nil {
		return headerProviderFunc(func(_ string, request *colly.Request) {
			if request.Headers.Get("User-Agent") == "" {
				request.Headers.Set("User-Agent", "Mozilla/5.0 (compatible; Crawler/1.0)")
			}
		})
	}
	return provider
}

type requestHookFunc func(ctx context.Context, target Target) error

func (fn requestHookFunc) BeforeRequest(ctx context.Context, target Target) error {
	if fn == nil {
		return nil
	}
	return fn(ctx, target)
}

func ensureRequestHook(hook RequestHook) RequestHook {
	if hook == nil {
		return requestHookFunc(func(context.Context, Target) error { return nil })
	}
	return hook
}
