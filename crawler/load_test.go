package crawler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
)

// TestCrawlerHandlesSyntheticLoad verifies the crawler can deterministically process
// a large batch of products without reaching the network.
func TestCrawlerHandlesSyntheticLoad(t *testing.T) {
	const (
		productCount = 50_000
	)

	results := make(chan *Result, productCount)
	captchaFixture := readFixture(t, filepath.Join("testdata", "truncated_detail.html"))
	defaultFixture := readFixture(t, filepath.Join("testdata", "title_emojis.html"))

	evaluator := &syntheticRuleEvaluator{configuredVerifierCount: 1}

	cfg := Config{
		PlatformID: "AMZN",
		Scraper: ScraperConfig{
			MaxDepth:                   1,
			Parallelism:                128,
			RetryCount:                 1,
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
		RuleEvaluator: evaluator,
		PlatformHooks: truncatedPageDetectingHooks{},
		Logger:        noopLogger{},
	}

	service, err := NewService(cfg, results)
	require.NoError(t, err, "crawler service must build with synthetic configuration")

	specialBodies := make(map[string][]byte)
	for index := 0; index < 200; index++ {
		specialBodies[fmt.Sprintf("/product/%d", index)] = captchaFixture
	}

	syntheticTransport := &scriptedTransport{
		statusCode:  http.StatusOK,
		defaultBody: defaultFixture,
		responses:   specialBodies,
		headers:     http.Header{"Content-Type": []string{"text/html"}},
	}
	service.collector.WithTransport(
		newPanicSafeTransport(
			newContextAwareTransport(syntheticTransport, service.currentRunContext),
			service.logger,
		),
	)

	products := make([]Product, 0, productCount)
	for index := 0; index < productCount; index++ {
		productID := fmt.Sprintf("SYNTH-%05d", index)
		productURL := fmt.Sprintf("https://example.com/product/%d", index)
		product, productErr := NewProduct(productID, "AMZN", productURL)
		require.NoError(t, productErr)
		products = append(products, product)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err = service.Run(ctx, products)
	require.NoError(t, err, "crawler run should complete without network access")

	var failureCount int
	var successCount int
	for i := 0; i < productCount; i++ {
		select {
		case result := <-results:
			if !result.Success {
				failureCount++
			} else {
				successCount++
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for crawler result %d", i)
		}
	}
	require.Greater(t, failureCount, 0, "expected captcha responses to fail after retries")

	require.Equal(t, successCount, int(evaluator.callCount()), "rule evaluator should run once per successful product")
}

type scriptedTransport struct {
	statusCode  int
	defaultBody []byte
	responses   map[string][]byte
	headers     http.Header
}

func (transport *scriptedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	responseBody := transport.defaultBody
	if transport.responses != nil {
		if special, ok := transport.responses[request.URL.Path]; ok {
			responseBody = special
		}
	}

	bodyCopy := bytes.NewReader(responseBody)
	resp := &http.Response{
		StatusCode: transport.statusCode,
		Header:     transport.headers.Clone(),
		Body:       io.NopCloser(bodyCopy),
		Request:    request,
	}
	return resp, nil
}

type syntheticRuleEvaluator struct {
	configuredVerifierCount int
	calls                   int64
}

func (evaluator *syntheticRuleEvaluator) Evaluate(productID string, document *goquery.Document) (RuleEvaluation, error) {
	_ = productID
	_ = document
	atomic.AddInt64(&evaluator.calls, 1)
	return RuleEvaluation{
		Passed:             true,
		ConfiguredVerifier: evaluator.configuredVerifierCount,
		RuleResults: []RuleResult{
			{
				Description:         "synthetic rule",
				Passed:              true,
				VerificationResults: nil,
			},
		},
	}, nil
}

func (evaluator *syntheticRuleEvaluator) ConfiguredVerifierCount() int {
	return evaluator.configuredVerifierCount
}

func (evaluator *syntheticRuleEvaluator) callCount() int64 {
	return atomic.LoadInt64(&evaluator.calls)
}

func readFixture(tb testing.TB, path string) []byte {
	tb.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("failed to read fixture %s: %v", path, err)
	}
	return content
}

type truncatedPageDetectingHooks struct {
	noopPlatformHooks
}

func (truncatedPageDetectingHooks) IsContentComplete(document *goquery.Document) bool {
	if document == nil {
		return true
	}
	dpContainer := document.Find("#dp-container")
	if dpContainer.Length() == 0 {
		return true
	}
	productTitle := document.Find("#productTitle").First()
	if productTitle.Length() > 0 && strings.TrimSpace(productTitle.Text()) != "" {
		return true
	}
	return false
}
