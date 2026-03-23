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

const defaultCollyRequestTimeout = 10 * time.Second

// Service orchestrates crawling of targets and emits results.
type Service struct {
	cfg             Config
	collector       *colly.Collector
	responseHandler ResponseHandler
	retryHandler    RetryHandler
	filePersister   FilePersister
	logger          Logger
	hook            RequestHook
	ctxMu           sync.RWMutex
	runCtx          context.Context
	slots           chan struct{}
}

// NewService constructs a crawler service.
// When cfg.ResponseHandler is nil, a DefaultResponseHandler is created using
// cfg.Evaluator and the results channel.
// When cfg.ResponseHandler is set, results can be nil.
func NewService(cfg Config, results chan<- *Result) (*Service, error) {
	if cfg.ResponseHandler == nil && results == nil {
		return nil, fmt.Errorf("crawler: results channel is required when no response handler is provided")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	logger := ensureLogger(cfg.Logger)

	filePersister := cfg.FilePersister
	if filePersister == nil && cfg.OutputDirectory != "" {
		filePersister = NewDirectoryFilePersister(cfg.OutputDirectory, cfg.Category, cfg.RunFolder)
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
		filePersister = NewBackgroundFilePersister(filePersister, workerCount, bufferSize, logger)
	}

	retryHandler := NewRetryHandler(cfg.Scraper, logger)
	requestConfigurator := NewRequestConfigurator(cfg, logger)
	hook := ensureRequestHook(cfg.Hook)

	collector, proxyTracker, transport, err := newCollector(cfg, logger)
	if err != nil {
		return nil, err
	}

	var responseHandler ResponseHandler
	if cfg.ResponseHandler != nil {
		responseHandler = cfg.ResponseHandler
	} else {
		responseHandler = NewDefaultResponseHandler(cfg, retryHandler, proxyTracker, filePersister, results, logger)
	}

	requestConfigurator.Configure(collector)
	responseHandler.Setup(collector)
	SetupErrorHandling(collector, responseHandler, retryHandler, proxyTracker, logger)

	svc := &Service{
		cfg:             cfg,
		collector:       collector,
		responseHandler: responseHandler,
		retryHandler:    retryHandler,
		filePersister:   filePersister,
		logger:          logger,
		hook:            hook,
		slots:           make(chan struct{}, cfg.Scraper.Parallelism),
	}

	responseHandler.SetSlotReleaser(svc.releaseSlot)

	contextTransport := NewContextAwareTransport(transport, svc.currentRunContext)
	panicSafeTransport := NewPanicSafeTransport(contextTransport, logger)
	collector.WithTransport(panicSafeTransport)

	return svc, nil
}

// Run visits each target and blocks until all are processed or ctx is cancelled.
func (svc *Service) Run(ctx context.Context, targets []Target) error {
	if len(targets) == 0 {
		return fmt.Errorf("crawler: no targets provided")
	}

	cleanup := svc.assignRunContext(ctx)
	defer cleanup()

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	for _, target := range targets {
		select {
		case <-runCtx.Done():
			svc.logger.Info("Crawler received shutdown signal. Stopping loop...")
			goto Cleanup
		default:
		}
		if err := svc.processTarget(runCtx, target); err != nil {
			svc.logger.Warning("Failed to process target %s: %v", target.ID, err)
			if runCtx.Err() != nil {
				goto Cleanup
			}
		}
	}

Cleanup:
	svc.collector.Wait()
	if svc.filePersister != nil {
		if err := svc.filePersister.Close(); err != nil {
			svc.logger.Error("Failed to close file persister: %v", err)
		}
	}
	return runCtx.Err()
}

// RetryHandler returns the service's retry handler for use by custom ResponseHandlers.
func (svc *Service) RetryHandler() RetryHandler {
	return svc.retryHandler
}

func (svc *Service) processTarget(ctx context.Context, target Target) error {
	if err := svc.reserveSlot(ctx, target.ID); err != nil {
		return err
	}

	reqCtx := colly.NewContext()
	reqCtx.Put(CtxTargetIDKey, target.ID)
	reqCtx.Put(CtxTargetCategoryKey, target.Category)
	reqCtx.Put(CtxTargetURLKey, target.URL)
	reqCtx.Put(CtxRunContextKey, ctx)

	if hookErr := svc.hook.BeforeRequest(ctx, target); hookErr != nil {
		reqCtx.Put(CtxErrorKey, hookErr)
		svc.responseHandler.SendResult(&colly.Response{Ctx: reqCtx}, false, hookErr.Error())
		return nil
	}

	svc.logger.Debug("Visiting URL for target: %s (%s)", target.ID, target.URL)
	if err := svc.collector.Request(http.MethodGet, target.URL, nil, reqCtx, nil); err != nil {
		svc.logger.Error("Failed to visit URL: %s, Error: %v", target.URL, err)
		svc.releaseSlotByID(target.ID)
	}
	return nil
}

func (svc *Service) reserveSlot(ctx context.Context, targetID string) error {
	select {
	case svc.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (svc *Service) releaseSlot(resp *colly.Response) {
	select {
	case <-svc.slots:
	default:
	}
}

func (svc *Service) releaseSlotByID(targetID string) {
	svc.releaseSlot(nil)
}

func (svc *Service) assignRunContext(ctx context.Context) func() {
	svc.ctxMu.Lock()
	svc.runCtx = ctx
	svc.ctxMu.Unlock()
	return func() {
		svc.ctxMu.Lock()
		svc.runCtx = nil
		svc.ctxMu.Unlock()
	}
}

func (svc *Service) currentRunContext() context.Context {
	svc.ctxMu.RLock()
	defer svc.ctxMu.RUnlock()
	return svc.runCtx
}

// --- Collector setup ---

func newCollector(cfg Config, logger Logger) (*colly.Collector, ProxyHealth, *http.Transport, error) {
	c := colly.NewCollector(
		colly.AllowURLRevisit(),
		colly.AllowedDomains(cfg.Platform.AllowedDomains...),
		colly.Async(),
		colly.MaxDepth(cfg.Scraper.MaxDepth),
		colly.IgnoreRobotsTxt(),
	)

	transport := NewHTTPTransport(cfg.Scraper.InsecureSkipVerify, cfg.Scraper.HTTPTimeout)
	c.WithTransport(transport)
	if shouldOverrideTimeout(cfg.Scraper.HTTPTimeout) {
		c.SetRequestTimeout(cfg.Scraper.HTTPTimeout)
	}

	rule := &colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: cfg.Scraper.Parallelism,
	}
	if cfg.Scraper.RateLimit > 0 {
		rule.Delay = cfg.Scraper.RateLimit
		rule.RandomDelay = cfg.Scraper.RateLimit / 2
	}
	if err := c.Limit(rule); err != nil {
		return nil, nil, nil, err
	}

	var tracker ProxyHealth
	switch len(cfg.Scraper.ProxyList) {
	case 0:
	case 1:
		proxyFn, err := NewProxyRotator(cfg.Scraper.ProxyList, nil, logger)
		if err != nil {
			return nil, nil, nil, err
		}
		c.SetProxyFunc(proxyFn)
	default:
		if cfg.Scraper.ProxyCircuitBreakerEnabled {
			tracker = NewProxyHealthTracker(cfg.Scraper.ProxyList, logger)
		}
		proxyFn, err := NewProxyRotator(cfg.Scraper.ProxyList, tracker, logger)
		if err != nil {
			return nil, nil, nil, err
		}
		c.SetProxyFunc(proxyFn)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("crawler: failed to create cookie jar: %w", err)
	}
	c.SetCookieJar(jar)

	return c, tracker, transport, nil
}

func shouldOverrideTimeout(timeout time.Duration) bool {
	return timeout <= 0 || timeout > defaultCollyRequestTimeout
}
