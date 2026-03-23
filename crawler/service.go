package crawler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
)

// Service orchestrates crawling of product pages and emits results.
type Service struct {
	config              Config
	collector           *colly.Collector
	results             chan<- *Result
	requestConfigurator RequestConfigurator
	responseProcessor   ResponseProcessor
	retryHandler        RetryHandler
	filePersister       FilePersister
	logger              Logger
	requestHook         RequestHook
	ctxMu               sync.RWMutex
	runCtx              context.Context
	productSlots        chan struct{}
	cleanupFunctions    []func()
}

const defaultCollyRequestTimeout = 10 * time.Second

// ResponseProcessor handles HTTP responses and emits results.
// PoodleScanner provides its own implementation with Amazon-specific logic.
// Simple consumers can use a default implementation via NewDefaultResponseProcessor.
type ResponseProcessor interface {
	Setup(collector *colly.Collector)
	SendFinalResult(resp *colly.Response, success bool, errorText string)
	SetResultCallback(callback func(*colly.Response))
}

// ServiceOption configures optional Service behavior.
type ServiceOption func(*serviceOptions)

type serviceOptions struct {
	cleanupFunctions []func()
}

// WithCleanupFunction registers a function to be called during Run cleanup.
// This allows consumers to inject shutdown hooks (e.g., stopping image converters).
func WithCleanupFunction(cleanupFunction func()) ServiceOption {
	return func(options *serviceOptions) {
		options.cleanupFunctions = append(options.cleanupFunctions, cleanupFunction)
	}
}

// NewService constructs a crawler service configured for a platform.
// The ResponseProcessor must be provided in Config; it controls how HTTP
// responses are processed and results are emitted.
func NewService(cfg Config, results chan<- *Result, opts ...ServiceOption) (*Service, error) {
	if results == nil {
		return nil, fmt.Errorf("crawler: results channel is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	appliedOptions := &serviceOptions{}
	for _, applyOption := range opts {
		applyOption(appliedOptions)
	}

	logger := ensureLogger(cfg.Logger)
	filePersister := cfg.FilePersister
	if filePersister == nil && cfg.OutputDirectory != "" {
		filePersister = newDirectoryFilePersister(cfg.OutputDirectory, cfg.PlatformID, cfg.RunFolder)
	}

	if filePersister != nil {
		workerCount := cfg.Scraper.Parallelism / 4
		if workerCount < 2 {
			workerCount = 2
		}
		if workerCount > 8 {
			workerCount = 8
		}
		bufferSize := cfg.Scraper.Parallelism * 2
		if bufferSize < 16 {
			bufferSize = 16
		}
		filePersister = newBackgroundFilePersister(filePersister, workerCount, bufferSize, logger)
	}

	retryHandler := newRetryHandler(cfg.Scraper, logger)
	requestConfigurator := newRequestConfigurator(cfg, logger)
	requestHook := ensureRequestHook(cfg.RequestHook)

	collector, proxyTracker, transport, err := newCollector(cfg, logger)
	if err != nil {
		return nil, err
	}

	responseProcessor := cfg.ResponseProcessor
	if responseProcessor == nil {
		responseProcessor = newDefaultResponseProcessor(cfg, retryHandler, proxyTracker, filePersister, results, logger)
	}

	requestConfigurator.Configure(collector)
	setupErrorHandling(collector, responseProcessor, retryHandler, proxyTracker, logger)
	responseProcessor.Setup(collector)

	service := &Service{
		config:              cfg,
		collector:           collector,
		results:             results,
		requestConfigurator: requestConfigurator,
		responseProcessor:   responseProcessor,
		retryHandler:        retryHandler,
		filePersister:       filePersister,
		logger:              logger,
		requestHook:         requestHook,
		productSlots:        make(chan struct{}, cfg.Scraper.Parallelism),
		cleanupFunctions:    appliedOptions.cleanupFunctions,
	}

	responseProcessor.SetResultCallback(service.releaseProductSlot)

	contextTransport := newContextAwareTransport(transport, service.currentRunContext)
	panicSafeTransport := newPanicSafeTransport(contextTransport, logger)
	collector.WithTransport(panicSafeTransport)

	if networkBinder, bindable := cfg.DiscoverabilityProber.(discoverabilityNetworkBinder); bindable {
		discoverabilityTimeout := cfg.DiscoverabilityProbeTimeout
		if discoverabilityTimeout <= 0 {
			discoverabilityTimeout = cfg.Scraper.HTTPTimeout
		}
		networkBinder.bindNetwork(cfg.PlatformID, panicSafeTransport, discoverabilityTimeout, cfg.RequestHeaders)
	}

	return service, nil
}

type discoverabilityNetworkBinder interface {
	bindNetwork(
		platformID string,
		transport http.RoundTripper,
		timeout time.Duration,
		requestHeaders RequestHeaderProvider,
	)
}

// RetryHandler returns the service retry handler for use by response processors.
func (service *Service) RetryHandler() RetryHandler {
	return service.retryHandler
}

// FilePersister returns the service file persister for use by response processors.
func (service *Service) FilePersister() FilePersister {
	return service.filePersister
}

// Run visits each product URL once and blocks until completion or context cancellation.
func (service *Service) Run(ctx context.Context, products []Product) error {
	if len(products) == 0 {
		return fmt.Errorf("crawler: no products provided")
	}

	cleanup := service.assignRunContext(ctx)
	defer cleanup()

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	for _, product := range products {
		select {
		case <-runCtx.Done():
			service.logger.Info("Crawler received shutdown signal. Stopping loop...")
			goto Cleanup
		default:
		}
		if err := service.processProduct(runCtx, product); err != nil {
			service.logger.Warning("Failed to process product %s: %v", product.ID, err)
			if runCtx.Err() != nil {
				goto Cleanup
			}
		}
	}

Cleanup:
	service.collector.Wait()

	for _, cleanupFunction := range service.cleanupFunctions {
		cleanupFunction()
	}

	if service.filePersister != nil {
		if err := service.filePersister.Close(); err != nil {
			service.logger.Error("Failed to close file persister: %v", err)
		}
	}
	return runCtx.Err()
}

func (service *Service) processProduct(ctx context.Context, product Product) error {
	if err := service.reserveProductSlot(ctx, product.ID); err != nil {
		return err
	}

	requestContext := colly.NewContext()
	requestContext.Put(ctxProductIDKey, product.ID)
	requestContext.Put(ctxProductPlatformKey, product.Platform)
	requestContext.Put(ctxProductURLKey, product.URL)
	requestContext.Put(ctxRunContextKey, ctx)

	if hookErr := service.requestHook.BeforeRequest(ctx, product); hookErr != nil {
		requestContext.Put(ctxProductErrorKey, hookErr)
		service.responseProcessor.SendFinalResult(&colly.Response{Ctx: requestContext}, false, hookErr.Error())
		return nil
	}

	service.logger.Debug("Visiting URL for product: %+v", product)
	if err := service.collector.Request(http.MethodGet, product.URL, nil, requestContext, nil); err != nil {
		service.logger.Error("Failed to visit URL: %s, Error: %v", product.URL, err)
		service.releaseProductSlotByID(product.ID)
	}
	return nil
}

func newCollector(cfg Config, logger Logger) (*colly.Collector, proxyHealth, *http.Transport, error) {
	webCollector := colly.NewCollector(
		colly.AllowURLRevisit(),
		colly.AllowedDomains(cfg.Platform.AllowedDomains...),
		colly.Async(),
		colly.MaxDepth(cfg.Scraper.MaxDepth),
		colly.IgnoreRobotsTxt(),
	)

	transport := newCrawlerHTTPTransport(cfg.Scraper.InsecureSkipVerify, cfg.Scraper.HTTPTimeout)
	webCollector.WithTransport(transport)
	if shouldOverrideCollectorRequestTimeout(cfg.Scraper.HTTPTimeout) {
		webCollector.SetRequestTimeout(cfg.Scraper.HTTPTimeout)
	}

	limitRule := &colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: cfg.Scraper.Parallelism,
	}
	if cfg.Scraper.RateLimit > 0 {
		limitRule.Delay = cfg.Scraper.RateLimit
		limitRule.RandomDelay = cfg.Scraper.RateLimit / 2
	}

	if err := webCollector.Limit(limitRule); err != nil {
		return nil, nil, nil, err
	}

	var tracker proxyHealth
	proxyHealthEnabled := cfg.Scraper.ProxyCircuitBreakerEnabled
	switch len(cfg.Scraper.ProxyList) {
	case 0:
	case 1:
		proxyFn, err := newProxyRotator(cfg.Scraper.ProxyList, nil, logger)
		if err != nil {
			return nil, nil, nil, err
		}
		webCollector.SetProxyFunc(proxyFn)
	default:
		if proxyHealthEnabled {
			tracker = newProxyHealthTracker(cfg.Scraper.ProxyList, logger)
		}
		proxyFn, err := newProxyRotator(cfg.Scraper.ProxyList, tracker, logger)
		if err != nil {
			return nil, nil, nil, err
		}
		webCollector.SetProxyFunc(proxyFn)
	}

	cookieJar, err := cookiejar.New(nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("crawler: failed to create cookie jar: %w", err)
	}
	webCollector.SetCookieJar(cookieJar)
	return webCollector, tracker, transport, nil
}

func shouldOverrideCollectorRequestTimeout(timeout time.Duration) bool {
	return timeout <= 0 || timeout > defaultCollyRequestTimeout
}

func setupErrorHandling(collector *colly.Collector, processor ResponseProcessor, retryHandler RetryHandler, tracker proxyHealth, logger Logger) {
	collector.OnError(func(resp *colly.Response, err error) {
		handleCollectorError(resp, err, processor, retryHandler, tracker, logger)
	})
}

func handleCollectorError(resp *colly.Response, err error, processor ResponseProcessor, retryHandler RetryHandler, tracker proxyHealth, logger Logger) {
	recordProxyFailure(tracker, resp)
	urlValue, statusCode, proxyURL := extractErrorLogFields(resp)
	logger.Error("URL: %s, StatusCode: %d, Proxy: %s, Error: %v", urlValue, statusCode, describeProxyForLog(proxyURL), err)

	if resp == nil {
		return
	}
	if resp.Ctx == nil {
		resp.Ctx = colly.NewContext()
	}

	errorText := ""
	if err != nil {
		errorText = err.Error()
	}

	resp.Ctx.Put(ctxHTTPStatusCodeKey, resp.StatusCode)
	resp.Ctx.Put(ctxProductErrorKey, err)

	if resp.StatusCode == http.StatusNotFound {
		processor.SendFinalResult(resp, false, errorText)
		return
	}
	if resp.Request == nil || resp.Request.URL == nil {
		processor.SendFinalResult(resp, false, errorText)
		return
	}

	if !retryHandler.Retry(resp, RetryOptions{}) {
		processor.SendFinalResult(resp, false, errorText)
	}
}

func extractErrorLogFields(resp *colly.Response) (urlValue string, statusCode int, proxyURL string) {
	urlValue = unknownURLValue
	if resp == nil {
		return urlValue, 0, ""
	}

	statusCode = resp.StatusCode
	if resp.Request == nil {
		return urlValue, statusCode, ""
	}

	proxyURL = sanitizeProxyURL(resp.Request.ProxyURL)
	if resp.Request.URL != nil {
		urlValue = resp.Request.URL.String()
	}

	return urlValue, statusCode, proxyURL
}

func recordProxyFailure(tracker proxyHealth, resp *colly.Response) {
	if tracker == nil || resp == nil || resp.Request == nil {
		return
	}
	if resp.Request.ProxyURL == "" {
		return
	}
	if resp.StatusCode != 0 {
		return
	}
	tracker.RecordFailure(resp.Request.ProxyURL)
}

func (service *Service) reserveProductSlot(ctx context.Context, productID string) error {
	if service == nil || service.productSlots == nil {
		return nil
	}
	select {
	case service.productSlots <- struct{}{}:
		service.logger.Debug("Reserved crawler slot for product %s", productID)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (service *Service) releaseProductSlot(resp *colly.Response) {
	productID := unknownProductID
	if resp != nil && resp.Ctx != nil {
		productID = getProductIDFromContext(resp)
	}
	service.releaseProductSlotByID(productID)
}

func (service *Service) releaseProductSlotByID(productID string) {
	if service == nil || service.productSlots == nil {
		return
	}
	select {
	case <-service.productSlots:
		service.logger.Debug("Released crawler slot for product %s", productID)
	default:
		service.logger.Warning("Crawler slot release called without reservation for product %s", productID)
	}
}

func (service *Service) assignRunContext(ctx context.Context) func() {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	service.ctxMu.Lock()
	service.runCtx = runCtx
	service.ctxMu.Unlock()

	return func() {
		service.ctxMu.Lock()
		service.runCtx = nil
		service.ctxMu.Unlock()
	}
}

func (service *Service) currentRunContext() context.Context {
	service.ctxMu.RLock()
	defer service.ctxMu.RUnlock()
	return service.runCtx
}
