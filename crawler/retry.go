package crawler

import (
	"math"
	"math/rand"
	"time"

	"github.com/gocolly/colly/v2"
)

// RetryHandler encapsulates retry behaviour for failed responses.
type RetryHandler interface {
	Retry(response *colly.Response, options RetryOptions) bool
}

type RetryOptions struct {
	SkipDelay    bool
	LimitRetries bool
	MaxRetries   int
}

type retryHandler struct {
	maxRetries    int
	logger        Logger
	proxyPoolSize int
	sleepFn       func(time.Duration)
}

func newRetryHandler(scraper ScraperConfig, logger Logger) RetryHandler {
	return &retryHandler{
		maxRetries:    scraper.RetryCount,
		logger:        logger,
		proxyPoolSize: len(scraper.ProxyList),
		sleepFn:       time.Sleep,
	}
}

func (handler *retryHandler) Retry(response *colly.Response, options RetryOptions) bool {
	maxRetries := handler.effectiveMaxRetries(options)
	if maxRetries == 0 {
		return false
	}

	if hasRetried, ok := response.Ctx.GetAny(retriedFlagKey).(bool); ok && hasRetried {
		handler.logger.Debug("Skipping retry for %s; already retried.", response.Request.URL.String())
		return false
	}

	attempt := getRetryAttempt(response)
	if attempt >= maxRetries {
		handler.logger.Error("No retries left for URL: %s", response.Request.URL.String())
		response.Ctx.Put(retriedFlagKey, true)
		return false
	}

	nextAttempt := attempt + 1
	if handler.shouldDelay(nextAttempt, options) {
		handler.sleep(handler.backoffDuration(attempt))
	}

	response.Ctx.Put(retryCountKey, nextAttempt)
	if err := response.Request.Retry(); err != nil {
		handler.logger.Error("Failed to retry URL: %s, Error: %v", response.Request.URL.String(), err)
		return false
	}
	handler.logger.Debug("Retrying URL %s; %d retries left.", response.Request.URL.String(), maxRetries-attempt-1)
	return true
}

func getRetryAttempt(response *colly.Response) int {
	if retries, ok := response.Ctx.GetAny(retryCountKey).(int); ok {
		return retries
	}
	return 0
}

func (handler *retryHandler) effectiveMaxRetries(options RetryOptions) int {
	if !options.LimitRetries || options.MaxRetries >= handler.maxRetries {
		return handler.maxRetries
	}
	if options.MaxRetries < 0 {
		return 0
	}
	return options.MaxRetries
}

func (handler *retryHandler) shouldDelay(nextAttempt int, options RetryOptions) bool {
	if options.SkipDelay {
		return false
	}
	if handler.proxyPoolSize <= 1 {
		return true
	}
	return nextAttempt%handler.proxyPoolSize == 0
}

func (handler *retryHandler) backoffDuration(attempt int) time.Duration {
	backoff := time.Second * time.Duration(math.Pow(2, float64(attempt+1)))
	jitter := time.Duration(rand.Int63n(int64(backoff / 4)))
	return backoff + jitter
}

func (handler *retryHandler) sleep(duration time.Duration) {
	if handler.sleepFn == nil {
		time.Sleep(duration)
		return
	}
	handler.sleepFn(duration)
}
