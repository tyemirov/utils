package crawler

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/require"
)

func TestResponseProcessorSaveFileSkipsWhenPersisterNil(t *testing.T) {
	processor := &responseProcessor{}

	require.NotPanics(t, func() {
		err := processor.saveFile("prod", "file.html", []byte("payload"))
		require.NoError(t, err)
	})
}

func TestResponseProcessorSaveFileDelegatesToPersister(t *testing.T) {
	persister := &stubFilePersister{}
	processor := &responseProcessor{
		filePersister: persister,
	}

	err := processor.saveFile("prod", "file.html", []byte("payload"))
	require.NoError(t, err)
	require.Equal(t, []string{"prod:file.html"}, persister.saved)
}

func TestResponseProcessorSaveFilePropagatesErrors(t *testing.T) {
	expectedErr := errors.New("boom")
	persister := &stubFilePersister{err: expectedErr}
	processor := &responseProcessor{
		filePersister: persister,
	}

	err := processor.saveFile("prod", "file.html", []byte("payload"))
	require.ErrorIs(t, err, expectedErr)
	require.Equal(t, []string{"prod:file.html"}, persister.saved)
}

func TestSendFinalResultWaitsForReceiver(t *testing.T) {
	results := make(chan *Result)
	processor := &responseProcessor{
		results: results,
		logger:  noopLogger{},
	}

	response := &colly.Response{Ctx: colly.NewContext()}
	response.Ctx.Put(ctxProductIDKey, "product-123")

	done := make(chan struct{})
	go func() {
		processor.SendFinalResult(response, true, "")
		close(done)
	}()

	time.Sleep(2500 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("SendFinalResult returned before receiver was ready")
	default:
	}

	var received *Result
	select {
	case received = <-results:
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for result delivery")
	}

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("SendFinalResult did not return after receiver became ready")
	}

	require.NotNil(t, received)
	require.Equal(t, "product-123", received.ProductID)
}

func TestSendFinalResultInvokesCallbackBeforeSendCompletes(t *testing.T) {
	results := make(chan *Result)
	processor := &responseProcessor{
		results: results,
		logger:  noopLogger{},
	}

	response := &colly.Response{Ctx: colly.NewContext()}
	response.Ctx.Put(ctxProductIDKey, "callback-product")

	callbackTriggered := make(chan struct{}, 1)
	processor.SetResultCallback(func(*colly.Response) {
		callbackTriggered <- struct{}{}
	})

	done := make(chan struct{})
	go func() {
		processor.SendFinalResult(response, true, "")
		close(done)
	}()

	select {
	case <-callbackTriggered:
	case <-time.After(time.Second):
		t.Fatal("expected result callback to fire before send completes")
	}

	select {
	case result := <-results:
		require.Equal(t, "callback-product", result.ProductID)
	case <-time.After(time.Second):
		t.Fatal("expected result to be delivered")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SendFinalResult did not return after receiver became ready")
	}
}

func TestPersistHTMLSnapshotWritesWhenBodyPresent(t *testing.T) {
	persister := &stubFilePersister{}
	processor := &responseProcessor{
		filePersister: persister,
		logger:        noopLogger{},
	}

	processor.persistHTMLSnapshot("ASIN123", []byte("<html></html>"))

	require.GreaterOrEqual(t, len(persister.saved), 1)
	require.Equal(t, "ASIN123:ASIN123.html", persister.saved[len(persister.saved)-1])
}

func TestPersistHTMLSnapshotSkipsWhenBodyEmpty(t *testing.T) {
	persister := &stubFilePersister{}
	processor := &responseProcessor{
		filePersister: persister,
		logger:        noopLogger{},
	}

	processor.persistHTMLSnapshot("ASIN123", nil)
	require.Nil(t, persister.saved)
}

type countingRuleEvaluator struct {
	configured int
	calls      int
}

func (e *countingRuleEvaluator) Evaluate(_ string, _ *goquery.Document) (RuleEvaluation, error) {
	e.calls++
	return RuleEvaluation{ConfiguredVerifier: e.configured}, nil
}

func (e *countingRuleEvaluator) ConfiguredVerifierCount() int {
	return e.configured
}

type retryingPlatformHooks struct {
	retryMessage       string
	logMessage         string
	retryPolicy        RetryPolicy
	exhaustionBehavior RetryExhaustionBehavior
}

func (hooks retryingPlatformHooks) NormalizeTitle(title string) string {
	return title
}

func (hooks retryingPlatformHooks) ExtractDOMTitle(_ *goquery.Document) string { return "" }
func (hooks retryingPlatformHooks) IsContentComplete(_ *goquery.Document) bool { return true }
func (hooks retryingPlatformHooks) InferRedirect(_, _, _, _ string) (bool, string) {
	return false, ""
}

type domTitlePlatformHooks struct {
	noopPlatformHooks
	selector string
}

func (hooks domTitlePlatformHooks) ExtractDOMTitle(document *goquery.Document) string {
	if document == nil || hooks.selector == "" {
		return ""
	}
	selection := document.Find(hooks.selector)
	if selection.Length() == 0 {
		return ""
	}
	text := strings.TrimSpace(selection.First().Text())
	return strings.Join(strings.Fields(text), " ")
}

func (hooks retryingPlatformHooks) ShouldRetry(_ string, document *goquery.Document) RetryDecision {
	if document.Find("#wrong-context").Length() > 0 {
		return RetryDecision{
			ShouldRetry:        true,
			Message:            hooks.retryMessage,
			LogMessage:         hooks.logMessage,
			Policy:             hooks.retryPolicy,
			ExhaustionBehavior: hooks.exhaustionBehavior,
		}
	}
	return RetryDecision{}
}

func TestHandleResponseContinuesEvaluationAfterWrongDeliveryContextRetriesExhausted(t *testing.T) {
	results := make(chan *Result, 1)
	ruleEvaluator := &countingRuleEvaluator{configured: 2}
	tracker := &trackingProxyHealth{}
	processor := &responseProcessor{
		scraperConfig: ScraperConfig{
			ProxyList: []string{"http://proxy-one.test:8080"},
		},
		platformHooks: retryingPlatformHooks{
			retryMessage:       "amazon detail page wrong delivery context",
			retryPolicy:        RetryPolicyRotateProxy,
			exhaustionBehavior: RetryExhaustionBehaviorContinue,
		},
		retryHandler:  newRetryHandler(ScraperConfig{RetryCount: 0}, noopLogger{}),
		ruleEvaluator: ruleEvaluator,
		proxyTracker:  tracker,
		results:       results,
		logger:        noopLogger{},
	}

	response := newTestResponse("B00TEST123")
	response.Body = []byte(`<html><head><title>Example Product</title></head><body><div id="wrong-context"></div></body></html>`)
	response.StatusCode = http.StatusOK
	headers := http.Header{}
	response.Headers = &headers
	response.Request.ProxyURL = "http://proxy-one.test:8080"
	pageURL, err := url.Parse("https://www.amazon.com/dp/B00TEST123")
	require.NoError(t, err)
	response.Request.URL = pageURL

	processor.handleResponse(response)

	result := <-results
	require.True(t, result.Success)
	require.Empty(t, result.ErrorMessage)
	require.Equal(t, "Example Product", result.ProductTitle)
	require.Equal(t, 2, result.ConfiguredVerifierCount)
	require.Equal(t, 1, ruleEvaluator.calls)
	require.Empty(t, tracker.successes)
	require.Equal(t, []string{"http://proxy-one.test:8080"}, tracker.criticalFailures)
}

func TestHandleResponseWrongDeliveryContextRotatesProxyWithoutBackoff(t *testing.T) {
	results := make(chan *Result, 1)
	ruleEvaluator := &countingRuleEvaluator{configured: 2}
	retryHandler := &stubRetryHandler{result: true}
	tracker := &trackingProxyHealth{}
	logger := &capturingLogger{}
	processor := &responseProcessor{
		scraperConfig: ScraperConfig{
			ProxyList: []string{
				"http://user:pass@proxy-one.test:8080",
				"http://proxy-two.test:8080",
			},
		},
		platformHooks: retryingPlatformHooks{
			retryMessage:       "amazon detail page wrong delivery context (target=US observed_country_code=CA)",
			logMessage:         "amazon detail page wrong delivery context (target=US observed_country_code=CA)",
			retryPolicy:        RetryPolicyRotateProxy,
			exhaustionBehavior: RetryExhaustionBehaviorContinue,
		},
		retryHandler:  retryHandler,
		ruleEvaluator: ruleEvaluator,
		proxyTracker:  tracker,
		results:       results,
		logger:        logger,
	}

	response := newTestResponse("B00TEST123")
	response.Body = []byte(`<html><head><title>Example Product</title></head><body><div id="wrong-context"></div></body></html>`)
	response.StatusCode = http.StatusOK
	headers := http.Header{}
	response.Headers = &headers
	response.Request.ProxyURL = "http://user:pass@proxy-one.test:8080"
	pageURL, err := url.Parse("https://www.amazon.com/dp/B00TEST123")
	require.NoError(t, err)
	response.Request.URL = pageURL

	processor.handleResponse(response)

	require.Empty(t, tracker.successes)
	require.Equal(t, []string{"http://user:pass@proxy-one.test:8080"}, tracker.criticalFailures)
	require.Len(t, retryHandler.calls, 1)
	require.Len(t, retryHandler.options, 1)
	require.True(t, retryHandler.options[0].SkipDelay)
	require.True(t, retryHandler.options[0].LimitRetries)
	require.Equal(t, 1, retryHandler.options[0].MaxRetries)
	require.Len(t, logger.warnings, 1)
	require.Contains(t, logger.warnings[0], "proxy=http://proxy-one.test:8080")
	require.NotContains(t, logger.warnings[0], "user:pass")

	select {
	case result := <-results:
		t.Fatalf("expected retry without final result, got %+v", result)
	default:
	}
}

func TestHandleResponseWrongDeliveryContextFallsBackToDefaultRetryWithoutAlternateProxy(t *testing.T) {
	results := make(chan *Result, 1)
	ruleEvaluator := &countingRuleEvaluator{configured: 2}
	retryHandler := &stubRetryHandler{result: true}
	processor := &responseProcessor{
		scraperConfig: ScraperConfig{
			ProxyList:  []string{"http://proxy-one.test:8080"},
			RetryCount: 3,
		},
		platformHooks: retryingPlatformHooks{
			retryMessage:       "amazon detail page wrong delivery context",
			logMessage:         "amazon detail page wrong delivery context (target=US observed_country_code=CA)",
			retryPolicy:        RetryPolicyRotateProxy,
			exhaustionBehavior: RetryExhaustionBehaviorContinue,
		},
		retryHandler:  retryHandler,
		ruleEvaluator: ruleEvaluator,
		results:       results,
		logger:        noopLogger{},
	}

	response := newTestResponse("B00TEST123")
	response.Body = []byte(`<html><head><title>Example Product</title></head><body><div id="wrong-context"></div></body></html>`)
	response.StatusCode = http.StatusOK
	headers := http.Header{}
	response.Headers = &headers
	pageURL, err := url.Parse("https://www.amazon.com/dp/B00TEST123")
	require.NoError(t, err)
	response.Request.URL = pageURL

	processor.handleResponse(response)

	require.Len(t, retryHandler.calls, 1)
	require.Len(t, retryHandler.options, 1)
	require.True(t, retryHandler.options[0].SkipDelay)
	require.False(t, retryHandler.options[0].LimitRetries)
	require.Zero(t, retryHandler.options[0].MaxRetries)
	require.Zero(t, ruleEvaluator.calls)

	select {
	case result := <-results:
		t.Fatalf("expected retry without final result, got %+v", result)
	default:
	}
}

func TestNoopPlatformHooksIsContentCompleteDefaultsToTrue(t *testing.T) {
	hooks := noopPlatformHooks{}
	require.True(t, hooks.IsContentComplete(nil))
}

func TestNoopPlatformHooksExtractDOMTitleDefaultsToEmpty(t *testing.T) {
	hooks := noopPlatformHooks{}
	require.Equal(t, "", hooks.ExtractDOMTitle(nil))
}

func TestNoopPlatformHooksInferRedirectDefaultsToFalse(t *testing.T) {
	hooks := noopPlatformHooks{}
	redirected, redirectedID := hooks.InferRedirect("id", "orig", "final", "canon")
	require.False(t, redirected)
	require.Empty(t, redirectedID)
}

func TestSkipEvaluationOnRedirectSendsFailure(t *testing.T) {
	results := make(chan *Result, 1)
	processor := &responseProcessor{
		platformConfig: PlatformConfig{
			SkipRulesOnRedirect: true,
		},
		ruleEvaluator: &countingRuleEvaluator{configured: 7},
		results:       results,
		logger:        noopLogger{},
	}
	resp := newTestResponse("ASIN123")
	resp.Ctx.Put(ctxRedirectedProductKey, "B00REDIRECT")
	resp.Ctx.Put(ctxRedirectedKey, true)

	skipped := processor.skipEvaluationOnRedirect(resp)
	require.True(t, skipped)

	select {
	case result := <-results:
		require.False(t, result.Success)
		require.Equal(t, "B00REDIRECT", result.ProductID)
		require.Equal(t, "Product redirected from ASIN123 to B00REDIRECT.", result.ErrorMessage)
	default:
		t.Fatal("expected redirected result")
	}

	eval, _ := resp.Ctx.GetAny(ctxProductRulesKey).(RuleEvaluation)
	require.Equal(t, 7, eval.ConfiguredVerifier)
}

func TestSkipEvaluationOnRedirectRespectsConfigFlag(t *testing.T) {
	results := make(chan *Result, 1)
	processor := &responseProcessor{
		platformConfig: PlatformConfig{
			SkipRulesOnRedirect: false,
		},
		ruleEvaluator: &countingRuleEvaluator{configured: 3},
		results:       results,
		logger:        noopLogger{},
	}
	resp := newTestResponse("ASIN123")
	resp.Ctx.Put(ctxRedirectedProductKey, "OTHERASIN")

	skipped := processor.skipEvaluationOnRedirect(resp)
	require.False(t, skipped)

	select {
	case <-results:
		t.Fatal("unexpected result when skip flag disabled")
	default:
	}
}

func TestSkipEvaluationOnRedirectIgnoresMissingRedirectID(t *testing.T) {
	results := make(chan *Result, 1)
	processor := &responseProcessor{
		platformConfig: PlatformConfig{SkipRulesOnRedirect: true},
		ruleEvaluator:  &countingRuleEvaluator{configured: 4},
		results:        results,
		logger:         noopLogger{},
	}

	resp := newTestResponse("ASIN123")
	resp.Ctx.Put(ctxRedirectedKey, true)

	skipped := processor.skipEvaluationOnRedirect(resp)
	require.False(t, skipped)

	select {
	case <-results:
		t.Fatal("did not expect result when redirect id missing")
	default:
	}
}

func TestHandleResponseUsesAmazonDOMTitleWhenPresent(t *testing.T) {
	cases := []struct {
		name     string
		fixture  string
		product  string
		expected string
	}{
		{
			name:     "B09ZSV7PBC",
			fixture:  filepath.Join("testdata", "B09ZSV7PBC_raw.html"),
			product:  "B09ZSV7PBC",
			expected: "Headlights Clear PPF Shield for Rivian R1T & Rivian R1S Gen1 2021-2024, Clear 8mil | Headlamp Cover - Enhance and Guard with Durable 8mil Paint Protection Film",
		},
		{
			name:     "B09ZSRQCH8",
			fixture:  filepath.Join("testdata", "B09ZSRQCH8_raw.html"),
			product:  "B09ZSRQCH8",
			expected: "Front Bumper PPF for Rivian R1S & Rivian R1T 2021-2025, Middle Part 8mil Custom Fit Anti Scratch Paint Protection Film Cover, Clear Self Healing Shield Guard, Complete with Install Kit Accessories",
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			htmlBytes, err := os.ReadFile(testCase.fixture)
			require.NoError(t, err)

			results := make(chan *Result, 1)
			processor := &responseProcessor{
				platformID:    "AMZN",
				platformHooks: domTitlePlatformHooks{selector: "#productTitle"},
				retryHandler:  newRetryHandler(ScraperConfig{RetryCount: 0}, noopLogger{}),
				ruleEvaluator: &countingRuleEvaluator{configured: 1},
				results:       results,
				logger:        noopLogger{},
			}

			response := newTestResponse(testCase.product)
			response.Body = htmlBytes
			response.StatusCode = http.StatusOK
			headers := http.Header{}
			response.Headers = &headers
			pageURL, err := url.Parse("https://www.amazon.com/dp/" + testCase.product)
			require.NoError(t, err)
			response.Request.URL = pageURL

			processor.handleResponse(response)

			select {
			case result := <-results:
				require.True(t, result.Success)
				require.Equal(t, testCase.expected, result.ProductTitle)
				require.NotContains(t, result.ProductTitle, ": Automotive")
				require.NotContains(t, result.ProductTitle, ": Cell Phones & Accessories")
			default:
				t.Fatal("expected final result")
			}
		})
	}
}

func newTestResponse(productID string) *colly.Response {
	ctx := colly.NewContext()
	ctx.Put(ctxProductIDKey, productID)
	ctx.Put(ctxProductPlatformKey, "AMZN")
	ctx.Put(ctxProductURLKey, "https://example.com/product")
	return &colly.Response{
		Ctx: ctx,
		Request: &colly.Request{
			Ctx: ctx,
		},
	}
}

func TestNoopPlatformHooksIsContentCompleteForRealDocument(t *testing.T) {
	doc := loadDocumentFromFile(t, filepath.Join("testdata", "B09ZSRQCH8_raw.html"))
	hooks := noopPlatformHooks{}
	require.True(t, hooks.IsContentComplete(doc))
}

func loadDocumentFromFile(t *testing.T, path string) *goquery.Document {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()

	doc, err := goquery.NewDocumentFromReader(file)
	require.NoError(t, err)
	return doc
}
