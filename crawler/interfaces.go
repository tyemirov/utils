package crawler

import (
	"context"
	"net/http"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
)

// RuleEvaluator produces a RuleEvaluation for a fetched document.
type RuleEvaluator interface {
	Evaluate(productID string, document *goquery.Document) (RuleEvaluation, error)
	ConfiguredVerifierCount() int
}

// CookieGenerator returns cookies for a specific domain.
type CookieGenerator func(domain string) []*http.Cookie

// FilePersister persists binary artifacts associated with a product.
type FilePersister interface {
	Save(productID, fileName string, content []byte) error
	Close() error
}

// Logger emits structured diagnostic messages. Implementations should be safe
// for concurrent use. Methods follow fmt.Sprintf semantics.
type Logger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warning(format string, args ...interface{})
	Error(format string, args ...interface{})
}

type noopLogger struct{}

func (noopLogger) Debug(string, ...interface{})   {}
func (noopLogger) Info(string, ...interface{})    {}
func (noopLogger) Warning(string, ...interface{}) {}
func (noopLogger) Error(string, ...interface{})   {}

var packageLogger Logger = noopLogger{}

// SetPackageLogger replaces the package-level logger used by standalone functions.
func SetPackageLogger(logger Logger) {
	if logger != nil {
		packageLogger = logger
	}
}

// EnsureLogger returns the provided logger if non-nil, otherwise a no-op logger.
func EnsureLogger(logger Logger) Logger {
	if logger == nil {
		return noopLogger{}
	}
	return logger
}

// RequestHeaderProvider decorates outbound collector requests.
type RequestHeaderProvider interface {
	Apply(platformID string, request *colly.Request)
}

type requestHeaderProviderFunc func(platformID string, request *colly.Request)

func (provider requestHeaderProviderFunc) Apply(platformID string, request *colly.Request) {
	if provider == nil {
		return
	}
	provider(platformID, request)
}

func ensureRequestHeaders(provider RequestHeaderProvider) RequestHeaderProvider {
	if provider == nil {
		return requestHeaderProviderFunc(func(_ string, request *colly.Request) {
			if request.Headers.Get("User-Agent") == "" {
				request.Headers.Set("User-Agent", "Mozilla/5.0 (compatible; Crawler/1.0)")
			}
		})
	}
	return provider
}

// ResponseHandler extends the crawling pipeline with domain-specific behaviour.
// Implementations are called at specific points during response processing.
type ResponseHandler interface {
	// HandleBinaryResponse processes non-HTML responses (e.g. images).
	// Return true to indicate the response was handled and stop further processing.
	HandleBinaryResponse(resp *colly.Response, productID string, fileExtension string) bool

	// BeforeEvaluation is called after HTML parsing and content validation but
	// before rule evaluation. Use for tasks like image retrieval.
	BeforeEvaluation(resp *colly.Response, document *goquery.Document)

	// AfterEvaluation is called after rule evaluation and result emission.
	// Use for tasks like discoverability probing or file persistence.
	AfterEvaluation(resp *colly.Response, document *goquery.Document, result *Result)
}

// NoopResponseHandler provides default no-op implementations of ResponseHandler.
type NoopResponseHandler struct{}

// HandleBinaryResponse returns false, indicating the response was not handled.
func (NoopResponseHandler) HandleBinaryResponse(*colly.Response, string, string) bool { return false }

// BeforeEvaluation does nothing.
func (NoopResponseHandler) BeforeEvaluation(*colly.Response, *goquery.Document) {}

// AfterEvaluation does nothing.
func (NoopResponseHandler) AfterEvaluation(*colly.Response, *goquery.Document, *Result) {}

// ServiceHook provides lifecycle callbacks for the crawler service.
type ServiceHook interface {
	// AfterInit is called after the collector, transport, and response processor
	// are fully wired. Use for binding domain-specific network configuration.
	AfterInit(collector *colly.Collector, transport http.RoundTripper)

	// BeforeRun is called before the product visit loop starts.
	BeforeRun(ctx context.Context)

	// AfterRun is called after all products have been visited and the collector
	// has finished. Use for cleanup (e.g. stopping image converter workers).
	AfterRun()
}

type noopServiceHook struct{}

func (noopServiceHook) AfterInit(*colly.Collector, http.RoundTripper) {}
func (noopServiceHook) BeforeRun(context.Context)                     {}
func (noopServiceHook) AfterRun()                                     {}

// ServiceOption configures a Service during construction.
type ServiceOption func(*Service)

// WithResponseHandlers registers ResponseHandlers that extend the crawling pipeline.
func WithResponseHandlers(handlers ...ResponseHandler) ServiceOption {
	return func(service *Service) {
		service.responseHandlers = append(service.responseHandlers, handlers...)
	}
}

// WithServiceHook registers a lifecycle hook for the crawler service.
func WithServiceHook(hook ServiceHook) ServiceOption {
	return func(service *Service) {
		if hook != nil {
			service.serviceHook = hook
		}
	}
}

type RequestHook interface {
	BeforeRequest(ctx context.Context, product Product) error
}

type requestHookFunc func(ctx context.Context, product Product) error

func (hook requestHookFunc) BeforeRequest(ctx context.Context, product Product) error {
	if hook == nil {
		return nil
	}
	return hook(ctx, product)
}

func ensureRequestHook(hook RequestHook) RequestHook {
	if hook == nil {
		return requestHookFunc(func(context.Context, Product) error {
			return nil
		})
	}
	return hook
}
