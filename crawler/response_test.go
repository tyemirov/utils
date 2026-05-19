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
	retryMessage         string
	logMessage           string
	retryPolicy          RetryPolicy
	exhaustionBehavior   RetryExhaustionBehavior
	proxyFailureSeverity RetryProxyFailureSeverity
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
			ShouldRetry:          true,
			Message:              hooks.retryMessage,
			LogMessage:           hooks.logMessage,
			Policy:               hooks.retryPolicy,
			ExhaustionBehavior:   hooks.exhaustionBehavior,
			ProxyFailureSeverity: hooks.proxyFailureSeverity,
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
	require.Equal(t, []string{"http://proxy-one.test:8080"}, tracker.failures)
	require.Empty(t, tracker.criticalFailures)
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
	require.Equal(t, []string{"http://user:pass@proxy-one.test:8080"}, tracker.failures)
	require.Empty(t, tracker.criticalFailures)
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

func TestHandleResponseRotateProxyRetryCanRequestCriticalCooldown(t *testing.T) {
	results := make(chan *Result, 1)
	retryHandler := &stubRetryHandler{result: true}
	tracker := &trackingProxyHealth{}
	processor := &responseProcessor{
		scraperConfig: ScraperConfig{
			ProxyList: []string{
				"http://proxy-one.test:8080",
				"http://proxy-two.test:8080",
			},
		},
		platformHooks: retryingPlatformHooks{
			retryMessage:         "proxy rendered block page",
			retryPolicy:          RetryPolicyRotateProxy,
			proxyFailureSeverity: RetryProxyFailureSeverityCritical,
		},
		retryHandler:  retryHandler,
		ruleEvaluator: &countingRuleEvaluator{},
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

	require.Empty(t, tracker.failures)
	require.Equal(t, []string{"http://proxy-one.test:8080"}, tracker.criticalFailures)
	require.Len(t, retryHandler.calls, 1)
	require.True(t, retryHandler.options[0].SkipDelay)
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

	skipped := processor.skipEvaluationOnRedirect(resp, nil)
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

func TestSkipEvaluationOnRedirectCallsAfterEvaluationHandlers(t *testing.T) {
	results := make(chan *Result, 1)
	handler := &recordingResponseHandler{}
	processor := &responseProcessor{
		platformConfig: PlatformConfig{
			SkipRulesOnRedirect: true,
		},
		ruleEvaluator:    &countingRuleEvaluator{configured: 7},
		results:          results,
		logger:           noopLogger{},
		responseHandlers: []ResponseHandler{handler},
	}
	resp := newTestResponse("ASIN123")
	resp.Ctx.Put(ctxRedirectedProductKey, "B00REDIRECT")
	resp.Ctx.Put(ctxRedirectedKey, true)

	document, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body><div id="redirected"></div></body></html>`))
	require.NoError(t, err)

	skipped := processor.skipEvaluationOnRedirect(resp, document)
	require.True(t, skipped)

	result := <-results
	require.False(t, result.Success)
	require.Equal(t, "B00REDIRECT", result.ProductID)
	require.Equal(t, 1, handler.afterEvalCalls)
	require.Len(t, handler.afterEvalResults, 1)
	require.Same(t, result, handler.afterEvalResults[0])
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

	skipped := processor.skipEvaluationOnRedirect(resp, nil)
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

	skipped := processor.skipEvaluationOnRedirect(resp, nil)
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

type recordingResponseHandler struct {
	binaryResponseCalls []recordedBinaryCall
	binaryReturnValue   bool
	beforeEvalCalls     int
	afterEvalCalls      int
	afterEvalResults    []*Result
}

type recordedBinaryCall struct {
	productID     string
	fileExtension string
}

func (handler *recordingResponseHandler) HandleBinaryResponse(_ *colly.Response, productID string, fileExtension string) bool {
	handler.binaryResponseCalls = append(handler.binaryResponseCalls, recordedBinaryCall{
		productID:     productID,
		fileExtension: fileExtension,
	})
	return handler.binaryReturnValue
}

func (handler *recordingResponseHandler) BeforeEvaluation(_ *colly.Response, _ *goquery.Document) {
	handler.beforeEvalCalls++
}

func (handler *recordingResponseHandler) AfterEvaluation(_ *colly.Response, _ *goquery.Document, result *Result) {
	handler.afterEvalCalls++
	handler.afterEvalResults = append(handler.afterEvalResults, result)
}

func TestHandleResponseCallsBeforeAndAfterEvaluationHandlers(t *testing.T) {
	t.Parallel()

	results := make(chan *Result, 1)
	firstHandler := &recordingResponseHandler{}
	secondHandler := &recordingResponseHandler{}
	ruleEvaluator := &countingRuleEvaluator{configured: 1}
	processor := &responseProcessor{
		platformID:       "TEST",
		platformHooks:    noopPlatformHooks{},
		retryHandler:     newRetryHandler(ScraperConfig{RetryCount: 0}, noopLogger{}),
		ruleEvaluator:    ruleEvaluator,
		results:          results,
		logger:           noopLogger{},
		responseHandlers: []ResponseHandler{firstHandler, secondHandler},
	}

	response := newTestResponse("EVAL-PRODUCT-001")
	response.Body = []byte(`<html><head><title>Valid Title</title></head><body></body></html>`)
	response.StatusCode = http.StatusOK
	headers := http.Header{}
	response.Headers = &headers
	pageURL, parseErr := url.Parse("https://example.com/product/EVAL-PRODUCT-001")
	require.NoError(t, parseErr)
	response.Request.URL = pageURL

	processor.handleResponse(response)

	result := <-results
	require.True(t, result.Success)

	require.Equal(t, 1, firstHandler.beforeEvalCalls)
	require.Equal(t, 1, secondHandler.beforeEvalCalls)
	require.Equal(t, 1, firstHandler.afterEvalCalls)
	require.Equal(t, 1, secondHandler.afterEvalCalls)
	require.NotNil(t, firstHandler.afterEvalResults[0])
	require.Equal(t, "EVAL-PRODUCT-001", firstHandler.afterEvalResults[0].ProductID)
}

func TestHandleResponseBinaryHandlerShortCircuitsWhenReturningTrue(t *testing.T) {
	t.Parallel()

	results := make(chan *Result, 1)
	interceptingHandler := &recordingResponseHandler{binaryReturnValue: true}
	unreachedHandler := &recordingResponseHandler{}
	ruleEvaluator := &countingRuleEvaluator{configured: 1}
	processor := &responseProcessor{
		platformID:       "TEST",
		platformHooks:    noopPlatformHooks{},
		retryHandler:     newRetryHandler(ScraperConfig{RetryCount: 0}, noopLogger{}),
		ruleEvaluator:    ruleEvaluator,
		results:          results,
		logger:           noopLogger{},
		responseHandlers: []ResponseHandler{interceptingHandler, unreachedHandler},
	}

	response := newTestResponse("BINARY-PRODUCT-001")
	response.Body = []byte{0xFF, 0xD8, 0xFF, 0xE0}
	response.StatusCode = http.StatusOK
	headers := http.Header{}
	response.Headers = &headers
	pageURL, parseErr := url.Parse("https://example.com/images/photo.jpg")
	require.NoError(t, parseErr)
	response.Request.URL = pageURL

	processor.handleResponse(response)

	require.Len(t, interceptingHandler.binaryResponseCalls, 1)
	require.Equal(t, "BINARY-PRODUCT-001", interceptingHandler.binaryResponseCalls[0].productID)
	require.Equal(t, ".jpg", interceptingHandler.binaryResponseCalls[0].fileExtension)

	require.Empty(t, unreachedHandler.binaryResponseCalls)
	require.Zero(t, interceptingHandler.beforeEvalCalls)
	require.Zero(t, interceptingHandler.afterEvalCalls)
	require.Zero(t, ruleEvaluator.calls)

	select {
	case <-results:
		t.Fatal("expected no result when binary handler short-circuits")
	default:
	}
}

func TestHandleResponseBinaryHandlerContinuesWhenReturningFalse(t *testing.T) {
	t.Parallel()

	results := make(chan *Result, 1)
	passThroughHandler := &recordingResponseHandler{binaryReturnValue: false}
	ruleEvaluator := &countingRuleEvaluator{configured: 1}
	processor := &responseProcessor{
		platformID:       "TEST",
		platformHooks:    noopPlatformHooks{},
		retryHandler:     newRetryHandler(ScraperConfig{RetryCount: 0}, noopLogger{}),
		ruleEvaluator:    ruleEvaluator,
		results:          results,
		logger:           noopLogger{},
		responseHandlers: []ResponseHandler{passThroughHandler},
	}

	response := newTestResponse("HTML-PRODUCT-001")
	response.Body = []byte(`<html><head><title>Product Page</title></head><body></body></html>`)
	response.StatusCode = http.StatusOK
	headers := http.Header{}
	response.Headers = &headers
	pageURL, parseErr := url.Parse("https://example.com/product/HTML-PRODUCT-001")
	require.NoError(t, parseErr)
	response.Request.URL = pageURL

	processor.handleResponse(response)

	result := <-results
	require.True(t, result.Success)
	require.Equal(t, 1, ruleEvaluator.calls)
	require.Equal(t, 1, passThroughHandler.beforeEvalCalls)
	require.Equal(t, 1, passThroughHandler.afterEvalCalls)
}

func TestHandleResponseReleasesNeutralProxyLeaseBeforeTerminalReturn(t *testing.T) {
	testCases := []struct {
		name             string
		body             []byte
		requestURL       string
		responseHandlers []ResponseHandler
		platformHooks    PlatformHooks
		expectResult     bool
		expectedError    string
	}{
		{
			name:             "binary handler short circuit",
			body:             []byte{0xFF, 0xD8, 0xFF, 0xE0},
			requestURL:       "https://example.com/images/photo.jpg",
			responseHandlers: []ResponseHandler{&recordingResponseHandler{binaryReturnValue: true}},
			platformHooks:    noopPlatformHooks{},
		},
		{
			name:          "page not found title",
			body:          []byte(`<html><head><title>Page Not Found</title></head><body></body></html>`),
			requestURL:    "https://example.com/dp/PROD404",
			platformHooks: noopPlatformHooks{},
			expectResult:  true,
			expectedError: pageNotFoundText,
		},
		{
			name:          "missing title retry exhausted",
			body:          []byte(`<html><head></head><body></body></html>`),
			requestURL:    "https://example.com/dp/NOTITLE",
			platformHooks: noopPlatformHooks{},
			expectResult:  true,
			expectedError: titleNotFoundMessage,
		},
		{
			name:          "incomplete content retry exhausted",
			body:          []byte(`<html><head><title>Title</title></head><body></body></html>`),
			requestURL:    "https://example.com/dp/INCOMPLETE",
			platformHooks: incompleteContentHooks{},
			expectResult:  true,
			expectedError: detailIncompleteMessage,
		},
		{
			name:       "default retry policy exhausted",
			body:       []byte(`<html><head><title>Title</title></head><body><div id="wrong-context"></div></body></html>`),
			requestURL: "https://example.com/dp/DEFAULT-RETRY",
			platformHooks: retryingPlatformHooks{
				retryMessage:       "retry default",
				retryPolicy:        RetryPolicyDefault,
				exhaustionBehavior: RetryExhaustionBehaviorFail,
			},
			expectResult:  true,
			expectedError: "retry default",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			results := make(chan *Result, 1)
			selector, selectorErr := NewProxyLeaseSelector([]ProxyRotationProviderConfig{{
				Name: "provider-one",
				Users: []ProxyRotationUserConfig{
					{Name: "user-one", URL: "http://proxy-one.example:8080"},
					{Name: "user-two", URL: "http://proxy-two.example:8080"},
				},
			}})
			require.NoError(t, selectorErr)
			tracker := &recordingLeaseTracker{selector: selector}
			lease, leaseErr := selector.AcquireRequired()
			require.NoError(t, leaseErr)

			processor := &responseProcessor{
				platformID:       "TEST",
				platformHooks:    testCase.platformHooks,
				retryHandler:     newRetryHandler(ScraperConfig{RetryCount: 0}, noopLogger{}),
				ruleEvaluator:    &countingRuleEvaluator{configured: 1},
				proxyTracker:     tracker,
				results:          results,
				logger:           noopLogger{},
				responseHandlers: testCase.responseHandlers,
			}

			response := newTestResponse("LEASE-PRODUCT")
			response.Body = testCase.body
			response.StatusCode = http.StatusOK
			headers := http.Header{}
			response.Headers = &headers
			pageURL, parseErr := url.Parse(testCase.requestURL)
			require.NoError(t, parseErr)
			response.Request.URL = pageURL
			response.Request.ProxyURL = lease.ProxyURL
			attachTrackedProxySelection(response.Request, lease)

			processor.handleResponse(response)

			if testCase.expectResult {
				result := <-results
				require.False(t, result.Success)
				require.Equal(t, testCase.expectedError, result.ErrorMessage)
			} else {
				select {
				case result := <-results:
					t.Fatalf("expected no result for neutral terminal path, got %+v", result)
				default:
				}
			}

			require.Equal(t, []ProxyLease{lease}, tracker.releases)
			require.Empty(t, tracker.successes)
			require.Empty(t, tracker.failures)
			require.Empty(t, tracker.criticalFailures)

			nextLease, nextLeaseErr := selector.AcquireRequired()
			require.NoError(t, nextLeaseErr)
			require.Equal(t, lease.ProxyURL, nextLease.ProxyURL)
		})
	}
}

type recordingLeaseTracker struct {
	selector         *ProxyLeaseSelector
	releases         []ProxyLease
	successes        []ProxyLease
	failures         []ProxyLease
	criticalFailures []ProxyLease
}

func (tracker *recordingLeaseTracker) IsAvailable(proxyURL string) bool {
	return tracker.selector.IsAvailable(proxyURL)
}

func (tracker *recordingLeaseTracker) RecordSuccess(proxyURL string) {
	lease, found := tracker.selector.SelectionForProxyURL(proxyURL)
	if !found {
		return
	}
	tracker.ReportSuccess(lease)
}

func (tracker *recordingLeaseTracker) RecordFailure(proxyURL string) {
	lease, found := tracker.selector.SelectionForProxyURL(proxyURL)
	if !found {
		return
	}
	tracker.ReportFailure(lease)
}

func (tracker *recordingLeaseTracker) RecordCriticalFailure(proxyURL string) {
	lease, found := tracker.selector.SelectionForProxyURL(proxyURL)
	if !found {
		return
	}
	tracker.ReportCriticalFailure(lease)
}

func (tracker *recordingLeaseTracker) Release(lease ProxyLease) {
	tracker.releases = append(tracker.releases, lease)
	tracker.selector.Release(lease)
}

func (tracker *recordingLeaseTracker) ReportSuccess(lease ProxyLease) {
	tracker.successes = append(tracker.successes, lease)
	tracker.selector.ReportSuccess(lease)
}

func (tracker *recordingLeaseTracker) ReportFailure(lease ProxyLease) {
	tracker.failures = append(tracker.failures, lease)
	tracker.selector.ReportFailure(lease)
}

func (tracker *recordingLeaseTracker) ReportCriticalFailure(lease ProxyLease) {
	tracker.criticalFailures = append(tracker.criticalFailures, lease)
	tracker.selector.ReportCriticalFailure(lease)
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
