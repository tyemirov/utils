package crawler

import (
	"math"
	"math/rand"
	"time"

	"github.com/gocolly/colly/v2"
)

type retryHandler struct {
	maxRetries    int
	logger        Logger
	proxyPoolSize int
	sleepFn       func(time.Duration)
}

// NewRetryHandler constructs a retry handler from scraper config.
func NewRetryHandler(scraper ScraperConfig, logger Logger) RetryHandler {
	return &retryHandler{
		maxRetries:    scraper.RetryCount,
		logger:        logger,
		proxyPoolSize: len(scraper.ProxyList),
		sleepFn:       time.Sleep,
	}
}

func (h *retryHandler) Retry(response *colly.Response, options RetryOptions) bool {
	maxRetries := h.effectiveMaxRetries(options)
	if maxRetries == 0 {
		return false
	}
	if hasRetried, ok := response.Ctx.GetAny(RetriedFlagKey).(bool); ok && hasRetried {
		h.logger.Debug("Skipping retry for %s; already retried.", response.Request.URL.String())
		return false
	}
	attempt := GetRetryAttempt(response)
	if attempt >= maxRetries {
		h.logger.Error("No retries left for URL: %s", response.Request.URL.String())
		response.Ctx.Put(RetriedFlagKey, true)
		return false
	}
	nextAttempt := attempt + 1
	if h.shouldDelay(nextAttempt, options) {
		h.sleep(h.backoffDuration(attempt))
	}
	response.Ctx.Put(RetryCountKey, nextAttempt)
	if err := response.Request.Retry(); err != nil {
		h.logger.Error("Failed to retry URL: %s, Error: %v", response.Request.URL.String(), err)
		return false
	}
	h.logger.Debug("Retrying URL %s; %d retries left.", response.Request.URL.String(), maxRetries-attempt-1)
	return true
}

// GetRetryAttempt returns the current retry attempt from the response context.
func GetRetryAttempt(response *colly.Response) int {
	if retries, ok := response.Ctx.GetAny(RetryCountKey).(int); ok {
		return retries
	}
	return 0
}

func (h *retryHandler) effectiveMaxRetries(options RetryOptions) int {
	if !options.LimitRetries || options.MaxRetries >= h.maxRetries {
		return h.maxRetries
	}
	if options.MaxRetries < 0 {
		return 0
	}
	return options.MaxRetries
}

func (h *retryHandler) shouldDelay(nextAttempt int, options RetryOptions) bool {
	if options.SkipDelay {
		return false
	}
	if h.proxyPoolSize <= 1 {
		return true
	}
	return nextAttempt%h.proxyPoolSize == 0
}

func (h *retryHandler) backoffDuration(attempt int) time.Duration {
	backoff := time.Second * time.Duration(math.Pow(2, float64(attempt+1)))
	jitter := time.Duration(rand.Int63n(int64(backoff / 4)))
	return backoff + jitter
}

func (h *retryHandler) sleep(duration time.Duration) {
	if h.sleepFn == nil {
		time.Sleep(duration)
		return
	}
	h.sleepFn(duration)
}
