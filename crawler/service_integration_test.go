package crawler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/require"
)

type trackingProxyHealth struct {
	failures         []string
	criticalFailures []string
	successes        []string
}

func (tracker *trackingProxyHealth) IsAvailable(_ string) bool {
	return true
}

func (tracker *trackingProxyHealth) RecordSuccess(proxy string) {
	tracker.successes = append(tracker.successes, proxy)
}

func (tracker *trackingProxyHealth) RecordFailure(proxy string) {
	tracker.failures = append(tracker.failures, proxy)
}

func (tracker *trackingProxyHealth) RecordCriticalFailure(proxy string) {
	tracker.criticalFailures = append(tracker.criticalFailures, proxy)
}

type stubRetryHandler struct {
	calls   []*colly.Response
	options []RetryOptions
	result  bool
}

func (handler *stubRetryHandler) Retry(response *colly.Response, options RetryOptions) bool {
	handler.calls = append(handler.calls, response)
	handler.options = append(handler.options, options)
	return handler.result
}

type recordedResult struct {
	success    bool
	errorText  string
	productID  string
	statusCode int
}

type stubResponseProcessor struct {
	results []recordedResult
}

func (processor *stubResponseProcessor) Setup(_ *colly.Collector) {}

func (processor *stubResponseProcessor) SendFinalResult(resp *colly.Response, success bool, errorText string) {
	productID := resp.Ctx.Get(ctxProductIDKey)
	processor.results = append(processor.results, recordedResult{
		success:    success,
		errorText:  errorText,
		productID:  productID,
		statusCode: resp.StatusCode,
	})
}

func (processor *stubResponseProcessor) SetResultCallback(func(*colly.Response)) {}

func (processor *stubResponseProcessor) SetResponseHandlers([]ResponseHandler) {}

type bindingResponseHandler struct {
	NoopResponseHandler
	collector     *colly.Collector
	filePersister FilePersister
	retryHandler  RetryHandler
}

func (handler *bindingResponseHandler) BindRuntime(
	collector *colly.Collector,
	filePersister FilePersister,
	retryHandler RetryHandler,
) {
	handler.collector = collector
	handler.filePersister = filePersister
	handler.retryHandler = retryHandler
}

func TestNewCollectorEnablesTLSVerificationByDefault(t *testing.T) {
	t.Parallel()

	cfg := Config{
		PlatformID: "AMZN",
		Scraper: ScraperConfig{
			MaxDepth:                   1,
			Parallelism:                1,
			RetryCount:                 1,
			InsecureSkipVerify:         false,
			ProxyCircuitBreakerEnabled: false,
		},
		Platform: PlatformConfig{
			AllowedDomains: []string{"example.com"},
		},
	}

	_, _, transport, err := newCollector(cfg, noopLogger{})
	require.NoError(t, err)

	httpTransport, ok := transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, httpTransport.TLSClientConfig)
	require.False(t, httpTransport.TLSClientConfig.InsecureSkipVerify)
}

func TestNewCollectorAllowsInsecureSkipVerifyWhenEnabled(t *testing.T) {
	t.Parallel()

	cfg := Config{
		PlatformID: "AMZN",
		Scraper: ScraperConfig{
			MaxDepth:                   1,
			Parallelism:                1,
			RetryCount:                 1,
			InsecureSkipVerify:         true,
			ProxyCircuitBreakerEnabled: false,
		},
		Platform: PlatformConfig{
			AllowedDomains: []string{"example.com"},
		},
	}

	_, _, transport, err := newCollector(cfg, noopLogger{})
	require.NoError(t, err)

	httpTransport, ok := transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, httpTransport.TLSClientConfig)
	require.True(t, httpTransport.TLSClientConfig.InsecureSkipVerify)
}

func TestShouldOverrideCollectorRequestTimeout(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		timeout  time.Duration
		expected bool
	}{
		{
			name:     "disabled timeout removes hidden cap",
			timeout:  0,
			expected: true,
		},
		{
			name:     "short timeout keeps idle timeout semantics",
			timeout:  5 * time.Second,
			expected: false,
		},
		{
			name:     "default cap leaves collector unchanged",
			timeout:  defaultCollyRequestTimeout,
			expected: false,
		},
		{
			name:     "long timeout lifts hidden cap",
			timeout:  60 * time.Second,
			expected: true,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.expected, shouldOverrideCollectorRequestTimeout(testCase.timeout))
		})
	}
}

func TestNewCollectorSingleProxyAnnotatesRequestContext(t *testing.T) {
	t.Parallel()

	cfg := Config{
		PlatformID: "AMZN",
		Scraper: ScraperConfig{
			MaxDepth:                   1,
			Parallelism:                1,
			RetryCount:                 1,
			ProxyList:                  []string{"http://user:pass@proxy-one.test:8080"},
			ProxyCircuitBreakerEnabled: false,
		},
		Platform: PlatformConfig{
			AllowedDomains: []string{"example.com"},
		},
	}

	_, _, transport, err := newCollector(cfg, noopLogger{})
	require.NoError(t, err)

	httpTransport, ok := transport.(*http.Transport)
	require.True(t, ok)

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	proxyURL, err := httpTransport.Proxy(req)
	require.NoError(t, err)
	require.NotNil(t, proxyURL)
	require.Equal(t, "proxy-one.test:8080", proxyURL.Host)
	require.Equal(t, "http://user:pass@proxy-one.test:8080", req.Context().Value(colly.ProxyURLKey))
}

func TestNewServiceBindsRuntimeToResponseHandlers(t *testing.T) {
	t.Parallel()

	responseHandler := &bindingResponseHandler{}
	cfg := Config{
		PlatformID: "AMZN",
		Scraper: ScraperConfig{
			MaxDepth:    1,
			Parallelism: 2,
			RetryCount:  1,
		},
		Platform: PlatformConfig{
			AllowedDomains: []string{"example.com"},
		},
		RuleEvaluator: fixedRuleEvaluator{},
		FilePersister: &stubFilePersister{},
		Logger:        noopLogger{},
	}

	results := make(chan *Result, 1)
	service, err := NewService(cfg, results, WithResponseHandlers(responseHandler))
	require.NoError(t, err)

	require.NotNil(t, responseHandler.collector)
	require.NotNil(t, responseHandler.filePersister)
	require.NotNil(t, responseHandler.retryHandler)
	require.Same(t, service.collector, responseHandler.collector)
	require.Same(t, service.filePersister, responseHandler.filePersister)
	require.Same(t, service.retryHandler, responseHandler.retryHandler)
}

func TestErrorHandlingDoesNotCountHTTPStatusAsProxyFailure(t *testing.T) {
	tracker := &trackingProxyHealth{}
	processor := &stubResponseProcessor{}
	retryHandler := &stubRetryHandler{}
	collector := colly.NewCollector(colly.AllowURLRevisit())
	collector.SetRequestTimeout(time.Second)
	collector.WithTransport(staticStatusTransport{status: http.StatusNotFound})
	collector.OnRequest(func(request *colly.Request) {
		request.ProxyURL = "http://working-proxy"
	})

	setupErrorHandling(collector, processor, retryHandler, tracker, noopLogger{})

	context := colly.NewContext()
	context.Put(ctxProductIDKey, "ASIN404")

	err := collector.Request(http.MethodGet, "http://example.com/missing", nil, context, nil)
	require.ErrorContains(t, err, http.StatusText(http.StatusNotFound))
	collector.Wait()

	require.Empty(t, tracker.failures)
	require.Len(t, processor.results, 1)
	require.Equal(t, http.StatusNotFound, processor.results[0].statusCode)
}

func TestErrorHandlingCountsProxyConnectionFailures(t *testing.T) {
	tracker := &trackingProxyHealth{}
	processor := &stubResponseProcessor{}
	retryHandler := &stubRetryHandler{}
	collector := colly.NewCollector(colly.AllowURLRevisit())
	collector.SetRequestTimeout(time.Second)
	collector.WithTransport(failingTransport{err: errors.New("proxy connection failed")})
	collector.OnRequest(func(request *colly.Request) {
		request.ProxyURL = "http://failing-proxy"
	})

	setupErrorHandling(collector, processor, retryHandler, tracker, noopLogger{})

	context := colly.NewContext()
	context.Put(ctxProductIDKey, "ASIN000")

	err := collector.Request(http.MethodGet, "http://example.com/unreachable", nil, context, nil)
	require.ErrorContains(t, err, "proxy connection failed")
	collector.Wait()

	require.Equal(t, []string{"http://failing-proxy"}, tracker.failures)
	require.Len(t, processor.results, 1)
	require.Equal(t, 0, processor.results[0].statusCode)
}

func TestHandleCollectorErrorHandlesNilResponse(t *testing.T) {
	tracker := &trackingProxyHealth{}
	processor := &stubResponseProcessor{}
	retryHandler := &stubRetryHandler{}
	logger := &capturingLogger{}
	testErr := errors.New("transport unavailable")

	require.NotPanics(t, func() {
		handleCollectorError(nil, testErr, processor, retryHandler, tracker, logger)
	})

	require.Empty(t, tracker.failures)
	require.Empty(t, retryHandler.calls)
	require.Empty(t, processor.results)
	require.Len(t, logger.errors, 1)
	require.Contains(t, logger.errors[0], "URL: "+unknownURLValue)
	require.Contains(t, logger.errors[0], testErr.Error())
}

func TestHandleCollectorErrorHandlesMissingRequest(t *testing.T) {
	tracker := &trackingProxyHealth{}
	processor := &stubResponseProcessor{}
	retryHandler := &stubRetryHandler{}
	logger := &capturingLogger{}
	testErr := errors.New("proxy connection failed")
	response := &colly.Response{
		StatusCode: 0,
		Ctx:        colly.NewContext(),
	}
	response.Ctx.Put(ctxProductIDKey, "ASIN000")

	require.NotPanics(t, func() {
		handleCollectorError(response, testErr, processor, retryHandler, tracker, logger)
	})

	require.Empty(t, tracker.failures)
	require.Empty(t, retryHandler.calls)
	require.Len(t, processor.results, 1)
	require.Equal(t, "ASIN000", processor.results[0].productID)
	require.Equal(t, 0, processor.results[0].statusCode)
	require.Equal(t, testErr.Error(), processor.results[0].errorText)
	require.Len(t, logger.errors, 1)
	require.Contains(t, logger.errors[0], "URL: "+unknownURLValue)
	require.Contains(t, logger.errors[0], testErr.Error())
}

func TestHandleCollectorErrorInitializesMissingContext(t *testing.T) {
	tracker := &trackingProxyHealth{}
	processor := &stubResponseProcessor{}
	retryHandler := &stubRetryHandler{}
	logger := &capturingLogger{}
	testErr := errors.New("request failed")
	response := &colly.Response{
		StatusCode: http.StatusBadGateway,
	}

	require.NotPanics(t, func() {
		handleCollectorError(response, testErr, processor, retryHandler, tracker, logger)
	})

	require.Empty(t, tracker.failures)
	require.Empty(t, retryHandler.calls)
	require.Len(t, processor.results, 1)
	require.Equal(t, "", processor.results[0].productID)
	require.Equal(t, http.StatusBadGateway, processor.results[0].statusCode)
	require.Equal(t, testErr.Error(), processor.results[0].errorText)
	require.Len(t, logger.errors, 1)
}

type staticStatusTransport struct {
	status int
}

func (transport staticStatusTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: transport.status,
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    request,
	}, nil
}

type failingTransport struct {
	err error
}

func (transport failingTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, transport.err
}

func TestContextAwareTransportHonorsRequestTimeouts(t *testing.T) {
	t.Parallel()

	client := http.Client{
		Transport: newContextAwareTransport(blockingRequestTransport{wait: 50 * time.Millisecond}, func() context.Context {
			return context.Background()
		}),
		Timeout: 20 * time.Millisecond,
	}

	_, err := client.Get("http://example.com/hanging-request")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestServiceAllowsSlowStreamingResponsesPastHTTPTimeout(t *testing.T) {
	t.Parallel()

	const requestTimeout = 150 * time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		flusher, ok := writer.(http.Flusher)
		require.True(t, ok)

		_, writeErr := writer.Write([]byte("<html><head><title>"))
		require.NoError(t, writeErr)
		flusher.Flush()

		time.Sleep(100 * time.Millisecond)

		_, writeErr = writer.Write([]byte("Title</title></head><body><span id=\"productTitle\">"))
		require.NoError(t, writeErr)
		flusher.Flush()

		time.Sleep(100 * time.Millisecond)

		_, writeErr = writer.Write([]byte("Title</span></body></html>"))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	results := make(chan *Result, 1)
	cfg := Config{
		PlatformID: "AMZN",
		Scraper: ScraperConfig{
			MaxDepth:                   1,
			Parallelism:                1,
			RetryCount:                 0,
			HTTPTimeout:                requestTimeout,
			RateLimit:                  0,
			ProxyList:                  nil,
			SaveFiles:                  false,
			ProxyCircuitBreakerEnabled: false,
		},
		Platform: PlatformConfig{
			AllowedDomains: []string{serverURL.Hostname()},
		},
		RuleEvaluator: fixedRuleEvaluator{},
		Logger:        noopLogger{},
	}

	service, err := NewService(cfg, results)
	require.NoError(t, err)

	runErr := service.Run(context.Background(), []Product{
		{
			ID:       "B00SLOWBODY1",
			Platform: "AMZN",
			URL:      server.URL + "/dp/B00SLOWBODY1",
		},
	})
	require.NoError(t, runErr)

	select {
	case result := <-results:
		require.True(t, result.Success)
		require.Equal(t, http.StatusOK, result.HTTPStatusCode)
	case <-time.After(time.Second):
		t.Fatal("expected crawler result")
	}
}

func TestServiceAllowsSlowResponseHeadersPastHTTPTimeout(t *testing.T) {
	t.Parallel()

	const requestTimeout = 100 * time.Millisecond

	responseBody := "<html><head><title>Title</title></head><body><span id=\"productTitle\">Title</span></body></html>"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hijacker, ok := writer.(http.Hijacker)
		require.True(t, ok)

		conn, bufferedConn, err := hijacker.Hijack()
		require.NoError(t, err)
		defer conn.Close()

		responseParts := []string{
			"HTTP/1.1 200 OK\r\n",
			"Content-Type: text/html\r\n",
			fmt.Sprintf("Content-Length: %d\r\n", len(responseBody)),
			"\r\n",
			responseBody,
		}
		for index, part := range responseParts {
			_, err = bufferedConn.WriteString(part)
			require.NoError(t, err)
			require.NoError(t, bufferedConn.Flush())
			if index < len(responseParts)-1 {
				time.Sleep(70 * time.Millisecond)
			}
		}
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	results := make(chan *Result, 1)
	cfg := Config{
		PlatformID: "AMZN",
		Scraper: ScraperConfig{
			MaxDepth:                   1,
			Parallelism:                1,
			RetryCount:                 0,
			HTTPTimeout:                requestTimeout,
			RateLimit:                  0,
			ProxyList:                  nil,
			SaveFiles:                  false,
			ProxyCircuitBreakerEnabled: false,
		},
		Platform: PlatformConfig{
			AllowedDomains: []string{serverURL.Hostname()},
		},
		RuleEvaluator: fixedRuleEvaluator{},
		Logger:        noopLogger{},
	}

	service, err := NewService(cfg, results)
	require.NoError(t, err)

	runErr := service.Run(context.Background(), []Product{
		{
			ID:       "B00SLOWHEAD1",
			Platform: "AMZN",
			URL:      server.URL + "/dp/B00SLOWHEAD1",
		},
	})
	require.NoError(t, runErr)

	select {
	case result := <-results:
		require.True(t, result.Success)
		require.Equal(t, http.StatusOK, result.HTTPStatusCode)
	case <-time.After(time.Second):
		t.Fatal("expected crawler result")
	}
}

func TestServiceTimesOutWhenResponseHeadersNeverArrive(t *testing.T) {
	t.Parallel()

	const requestTimeout = 100 * time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(250 * time.Millisecond)
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte("<html><head><title>Late</title></head><body><span id=\"productTitle\">Late</span></body></html>"))
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	results := make(chan *Result, 1)
	cfg := Config{
		PlatformID: "AMZN",
		Scraper: ScraperConfig{
			MaxDepth:                   1,
			Parallelism:                1,
			RetryCount:                 0,
			HTTPTimeout:                requestTimeout,
			RateLimit:                  0,
			ProxyList:                  nil,
			SaveFiles:                  false,
			ProxyCircuitBreakerEnabled: false,
		},
		Platform: PlatformConfig{
			AllowedDomains: []string{serverURL.Hostname()},
		},
		RuleEvaluator: fixedRuleEvaluator{},
		Logger:        noopLogger{},
	}

	service, err := NewService(cfg, results)
	require.NoError(t, err)

	runErr := service.Run(context.Background(), []Product{
		{
			ID:       "B00NOHEADR1",
			Platform: "AMZN",
			URL:      server.URL + "/dp/B00NOHEADR1",
		},
	})
	require.NoError(t, runErr)

	select {
	case result := <-results:
		require.False(t, result.Success)
		require.Equal(t, 0, result.HTTPStatusCode)
		normalizedError := strings.ToLower(result.ErrorMessage)
		require.True(
			t,
			strings.Contains(normalizedError, "timeout") || strings.Contains(normalizedError, "deadline exceeded"),
		)
	case <-time.After(time.Second):
		t.Fatal("expected crawler result")
	}
}

type blockingRequestTransport struct {
	wait time.Duration
}

func (transport blockingRequestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-time.After(transport.wait):
		return nil, errors.New("request context was not cancelled")
	}
}

type fixedRuleEvaluator struct{}

func (fixedRuleEvaluator) Evaluate(_ string, _ *goquery.Document) (RuleEvaluation, error) {
	return RuleEvaluation{}, nil
}

func (fixedRuleEvaluator) ConfiguredVerifierCount() int {
	return 0
}

func TestServiceHookAfterInitCalledDuringNewService(t *testing.T) {
	t.Parallel()

	results := make(chan *Result, 1)
	hook := &recordingServiceHook{}
	cfg := Config{
		PlatformID: "TEST",
		Scraper: ScraperConfig{
			MaxDepth:    1,
			Parallelism: 1,
			RetryCount:  0,
		},
		Platform: PlatformConfig{
			AllowedDomains: []string{"example.com"},
		},
		RuleEvaluator: fixedRuleEvaluator{},
		Logger:        noopLogger{},
	}

	service, serviceErr := NewService(cfg, results, WithServiceHook(hook))
	require.NoError(t, serviceErr)
	require.NotNil(t, service)

	require.True(t, hook.afterInitCalled)
	require.NotNil(t, hook.initCollector)
	require.NotNil(t, hook.initTransport)
}

func TestServiceHookBeforeRunAndAfterRunCalledDuringRun(t *testing.T) {
	t.Parallel()

	responseBody := `<html><head><title>Hook Test</title></head><body></body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte(responseBody))
	}))
	defer server.Close()

	serverURL, parseErr := url.Parse(server.URL)
	require.NoError(t, parseErr)

	results := make(chan *Result, 1)
	hook := &recordingServiceHook{}
	cfg := Config{
		PlatformID: "TEST",
		Scraper: ScraperConfig{
			MaxDepth:    1,
			Parallelism: 1,
			RetryCount:  0,
		},
		Platform: PlatformConfig{
			AllowedDomains: []string{serverURL.Hostname()},
		},
		RuleEvaluator: fixedRuleEvaluator{},
		Logger:        noopLogger{},
	}

	service, serviceErr := NewService(cfg, results, WithServiceHook(hook))
	require.NoError(t, serviceErr)

	runContext := context.Background()
	runErr := service.Run(runContext, []Product{
		{
			ID:       "HOOK-TEST-001",
			Platform: "TEST",
			URL:      server.URL + "/product/HOOK-TEST-001",
		},
	})
	require.NoError(t, runErr)

	require.True(t, hook.beforeRunCalled)
	require.NotNil(t, hook.beforeRunContext)
	require.True(t, hook.afterRunCalled)
}

func TestServiceHookDefaultsToNoopWhenNotProvided(t *testing.T) {
	t.Parallel()

	results := make(chan *Result, 1)
	cfg := Config{
		PlatformID: "TEST",
		Scraper: ScraperConfig{
			MaxDepth:    1,
			Parallelism: 1,
			RetryCount:  0,
		},
		Platform: PlatformConfig{
			AllowedDomains: []string{"example.com"},
		},
		RuleEvaluator: fixedRuleEvaluator{},
		Logger:        noopLogger{},
	}

	service, serviceErr := NewService(cfg, results)
	require.NoError(t, serviceErr)
	require.NotNil(t, service)

	_, isNoop := service.serviceHook.(noopServiceHook)
	require.True(t, isNoop)
}
