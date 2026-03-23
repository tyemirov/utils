package crawler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
)

func TestRequestHookFailureSkipsRequest(testContext *testing.T) {
	testContext.Parallel()

	results := make(chan *Result, 2)
	var hookCalls int64
	requestHook := requestHookFunc(func(_ context.Context, product Product) error {
		atomic.AddInt64(&hookCalls, 1)
		if product.ID == "FAIL" {
			return errors.New("credits.capture.failed")
		}
		return nil
	})

	fixture := []byte("<html><head><title>Test</title></head><body></body></html>")
	transport := &countingTransport{
		statusCode:  http.StatusOK,
		defaultBody: fixture,
		headers:     http.Header{"Content-Type": []string{"text/html"}},
	}

	cfg := Config{
		PlatformID: "AMZN",
		Scraper: ScraperConfig{
			MaxDepth:                   1,
			Parallelism:                1,
			RetryCount:                 0,
			HTTPTimeout:                0,
			RateLimit:                  0,
			ProxyList:                  nil,
			SaveFiles:                  false,
			RetrieveProductImages:      false,
			ProxyCircuitBreakerEnabled: false,
		},
		Platform: PlatformConfig{
			AllowedDomains: []string{"example.com"},
			CookieDomains:  []string{"example.com"},
		},
		RuleEvaluator: noopRuleEvaluator{configuredVerifierCount: 1},
		Logger:        noopLogger{},
		RequestHook:   requestHook,
	}

	service, err := NewService(cfg, results)
	require.NoError(testContext, err)

	service.collector.WithTransport(
		newPanicSafeTransport(
			newContextAwareTransport(transport, service.currentRunContext),
			service.logger,
		),
	)

	products := []Product{
		{ID: "FAIL", Platform: "AMZN", URL: "https://example.com/fail"},
		{ID: "OK", Platform: "AMZN", URL: "https://example.com/ok"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	serviceErr := service.Run(ctx, products)
	require.NoError(testContext, serviceErr)

	var failureCount int
	var successCount int
	var failureMessage string
	for resultIndex := 0; resultIndex < len(products); resultIndex++ {
		select {
		case result := <-results:
			if result.Success {
				successCount++
			} else {
				failureCount++
				failureMessage = result.ErrorMessage
			}
		case <-time.After(1 * time.Second):
			testContext.Fatalf("timeout waiting for result %d", resultIndex)
		}
	}

	require.Equal(testContext, int64(len(products)), atomic.LoadInt64(&hookCalls))
	require.Equal(testContext, 1, successCount)
	require.Equal(testContext, 1, failureCount)
	require.Contains(testContext, failureMessage, "credits.capture.failed")
	require.Equal(testContext, int64(1), atomic.LoadInt64(&transport.requests))
}

type countingTransport struct {
	statusCode  int
	defaultBody []byte
	headers     http.Header
	requests    int64
}

func (transport *countingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	atomic.AddInt64(&transport.requests, 1)
	responseBody := transport.defaultBody
	response := &http.Response{
		StatusCode: transport.statusCode,
		Header:     transport.headers.Clone(),
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
		Request:    request,
	}
	return response, nil
}

type noopRuleEvaluator struct {
	configuredVerifierCount int
}

func (evaluator noopRuleEvaluator) Evaluate(_ string, _ *goquery.Document) (RuleEvaluation, error) {
	return RuleEvaluation{
		Passed:             true,
		ConfiguredVerifier: evaluator.configuredVerifierCount,
		RuleResults:        nil,
	}, nil
}

func (evaluator noopRuleEvaluator) ConfiguredVerifierCount() int {
	return evaluator.configuredVerifierCount
}
