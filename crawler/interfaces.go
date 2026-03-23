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

// DiscoverabilityProber evaluates search discoverability for a product identifier.
type DiscoverabilityProber interface {
	Probe(ctx context.Context, targetASIN string) (Discoverability, error)
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

// packageLogger is used for diagnostic messages emitted outside of a Service
// context (e.g., Result.CalculateScore). It defaults to a no-op logger.
var packageLogger Logger = noopLogger{}

// SetPackageLogger replaces the package-level logger used by standalone functions.
func SetPackageLogger(logger Logger) {
	if logger != nil {
		packageLogger = logger
	}
}

func ensureLogger(logger Logger) Logger {
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

// ImageStatusHook receives asynchronous image status transitions for a product identifier.
type ImageStatusHook func(productID string, status ImageStatus)

// EnsureImageStatusHook returns a no-op hook when the provided hook is nil.
func EnsureImageStatusHook(hook ImageStatusHook) ImageStatusHook {
	if hook == nil {
		return func(string, ImageStatus) {}
	}
	return hook
}
