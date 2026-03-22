package crawler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
)

const defaultRequestTimeout = 30 * time.Second

// context keys for Colly request context
const (
	ctxPageIDKey       = "crawler_page_id"
	ctxPageCategoryKey = "crawler_page_category"
	ctxPageURLKey      = "crawler_page_url"
	ctxFinalURLKey     = "crawler_final_url"
	ctxTitleKey        = "crawler_title"
	ctxHTTPStatusKey   = "crawler_http_status"
	ctxEvalKey         = "crawler_evaluation"
	ctxRetryCountKey   = "crawler_retry_count"
	ctxRetriedKey      = "crawler_retried"
)

// Service orchestrates crawling pages and emits results.
type Service struct {
	cfg       Config
	collector *colly.Collector
	results   chan<- *Result
	logger    Logger
	slots     chan struct{}
	mu        sync.RWMutex
	runCtx    context.Context
}

// NewService constructs a crawler service.
func NewService(cfg Config, results chan<- *Result) (*Service, error) {
	if results == nil {
		return nil, fmt.Errorf("crawler: results channel is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	logger := cfg.Logger
	if logger == nil {
		logger = noopLog{}
	}

	collector, err := newCollector(cfg, logger)
	if err != nil {
		return nil, err
	}

	svc := &Service{
		cfg:       cfg,
		collector: collector,
		results:   results,
		logger:    logger,
		slots:     make(chan struct{}, cfg.Parallelism),
	}

	// Wire up request configuration
	svc.setupRequests(collector)
	// Wire up response processing
	svc.setupResponses(collector)
	// Wire up error handling with retries
	svc.setupErrors(collector)

	return svc, nil
}

// Run visits each page and blocks until all are processed or ctx is cancelled.
func (svc *Service) Run(ctx context.Context, pages []Page) error {
	if len(pages) == 0 {
		return fmt.Errorf("crawler: no pages provided")
	}

	svc.mu.Lock()
	svc.runCtx = ctx
	svc.mu.Unlock()
	defer func() {
		svc.mu.Lock()
		svc.runCtx = nil
		svc.mu.Unlock()
	}()

	for _, page := range pages {
		select {
		case <-ctx.Done():
			svc.logger.Info("Crawler received shutdown signal")
			goto Wait
		default:
		}
		if err := svc.visit(ctx, page); err != nil {
			svc.logger.Warning("Failed to visit %s: %v", page.ID, err)
			if ctx.Err() != nil {
				goto Wait
			}
		}
	}

Wait:
	svc.collector.Wait()
	return ctx.Err()
}

func (svc *Service) visit(ctx context.Context, page Page) error {
	// Acquire slot (bounded parallelism)
	select {
	case svc.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Run hook if configured
	if svc.cfg.Hook != nil {
		if err := svc.cfg.Hook.BeforeRequest(ctx, page); err != nil {
			<-svc.slots
			svc.results <- &Result{
				PageID:       page.ID,
				PageURL:      page.URL,
				Category:     page.Category,
				Success:      false,
				ErrorMessage: err.Error(),
			}
			return nil
		}
	}

	reqCtx := colly.NewContext()
	reqCtx.Put(ctxPageIDKey, page.ID)
	reqCtx.Put(ctxPageCategoryKey, page.Category)
	reqCtx.Put(ctxPageURLKey, page.URL)

	svc.logger.Debug("Visiting %s (%s)", page.URL, page.ID)
	if err := svc.collector.Request(http.MethodGet, page.URL, nil, reqCtx, nil); err != nil {
		<-svc.slots
		svc.logger.Error("Request failed for %s: %v", page.URL, err)
	}
	return nil
}

func (svc *Service) releaseSlot() {
	select {
	case <-svc.slots:
	default:
	}
}

// --- Collector setup ---

func newCollector(cfg Config, logger Logger) (*colly.Collector, error) {
	c := colly.NewCollector(
		colly.AllowURLRevisit(),
		colly.AllowedDomains(cfg.AllowedDomains...),
		colly.Async(),
		colly.MaxDepth(cfg.MaxDepth),
		colly.IgnoreRobotsTxt(),
	)

	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	c.SetRequestTimeout(timeout)

	rule := &colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: cfg.Parallelism,
	}
	if cfg.RateLimit > 0 {
		rule.Delay = cfg.RateLimit
		rule.RandomDelay = cfg.RateLimit / 2
	}
	if err := c.Limit(rule); err != nil {
		return nil, err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("crawler: cookie jar: %w", err)
	}
	c.SetCookieJar(jar)

	return c, nil
}

func (svc *Service) setupRequests(c *colly.Collector) {
	// Set cookies if configured
	if svc.cfg.CookieProvider != nil {
		for _, domain := range svc.cfg.CookieDomains {
			cookies := svc.cfg.CookieProvider(domain)
			for _, cookie := range cookies {
				c.SetCookies("https://"+domain, []*http.Cookie{cookie})
			}
		}
	}

	c.OnRequest(func(r *colly.Request) {
		// Apply custom headers
		if svc.cfg.Headers != nil {
			svc.cfg.Headers.Apply(r)
		}
		// Default user agent
		if r.Headers.Get("User-Agent") == "" {
			r.Headers.Set("User-Agent", "Mozilla/5.0 (compatible; Crawler/1.0)")
		}
	})
}

func (svc *Service) setupResponses(c *colly.Collector) {
	c.OnResponse(func(resp *colly.Response) {
		pageID := resp.Ctx.Get(ctxPageIDKey)
		category := resp.Ctx.Get(ctxPageCategoryKey)
		pageURL := resp.Ctx.Get(ctxPageURLKey)
		finalURL := ""
		if resp.Request != nil && resp.Request.URL != nil {
			finalURL = resp.Request.URL.String()
		}

		// Parse HTML
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
		if err != nil {
			svc.sendResult(resp, &Result{
				PageID:         pageID,
				PageURL:        pageURL,
				FinalURL:       finalURL,
				Category:       category,
				Success:        false,
				ErrorMessage:   fmt.Sprintf("parse error: %v", err),
				HTTPStatusCode: resp.StatusCode,
			})
			return
		}

		// Extract title
		title := doc.Find("title").First().Text()

		// Run evaluator
		eval, evalErr := svc.cfg.Evaluator.Evaluate(pageID, doc)
		if evalErr != nil {
			svc.sendResult(resp, &Result{
				PageID:         pageID,
				PageURL:        pageURL,
				FinalURL:       finalURL,
				Category:       category,
				Title:          title,
				Success:        false,
				ErrorMessage:   fmt.Sprintf("evaluator error: %v", evalErr),
				HTTPStatusCode: resp.StatusCode,
				Document:       doc,
			})
			return
		}

		svc.sendResult(resp, &Result{
			PageID:         pageID,
			PageURL:        pageURL,
			FinalURL:       finalURL,
			Category:       category,
			Title:          title,
			Success:        true,
			HTTPStatusCode: resp.StatusCode,
			Findings:       eval.Findings,
			Document:       doc,
		})
	})
}

func (svc *Service) setupErrors(c *colly.Collector) {
	c.OnError(func(resp *colly.Response, err error) {
		if resp == nil || resp.Ctx == nil {
			svc.logger.Error("Nil response in error handler: %v", err)
			return
		}

		pageID := resp.Ctx.Get(ctxPageIDKey)
		category := resp.Ctx.Get(ctxPageCategoryKey)
		pageURL := resp.Ctx.Get(ctxPageURLKey)
		errText := ""
		if err != nil {
			errText = err.Error()
		}

		url := "<unknown>"
		if resp.Request != nil && resp.Request.URL != nil {
			url = resp.Request.URL.String()
		}
		svc.logger.Error("Error fetching %s (status %d): %v", url, resp.StatusCode, err)

		// Don't retry 404s
		if resp.StatusCode == http.StatusNotFound {
			svc.sendResult(resp, &Result{
				PageID:         pageID,
				PageURL:        pageURL,
				Category:       category,
				Success:        false,
				ErrorMessage:   errText,
				HTTPStatusCode: resp.StatusCode,
			})
			return
		}

		// Retry with exponential backoff
		if svc.retry(resp) {
			return
		}

		svc.sendResult(resp, &Result{
			PageID:         pageID,
			PageURL:        pageURL,
			Category:       category,
			Success:        false,
			ErrorMessage:   errText,
			HTTPStatusCode: resp.StatusCode,
		})
	})
}

func (svc *Service) sendResult(resp *colly.Response, result *Result) {
	svc.releaseSlot()
	svc.results <- result
}

// --- Retry logic (exponential backoff) ---

func (svc *Service) retry(resp *colly.Response) bool {
	if svc.cfg.RetryCount == 0 || resp.Request == nil {
		return false
	}
	if retried, ok := resp.Ctx.GetAny(ctxRetriedKey).(bool); ok && retried {
		return false
	}

	attempt := 0
	if v, ok := resp.Ctx.GetAny(ctxRetryCountKey).(int); ok {
		attempt = v
	}
	if attempt >= svc.cfg.RetryCount {
		resp.Ctx.Put(ctxRetriedKey, true)
		svc.logger.Error("No retries left for %s", resp.Request.URL)
		return false
	}

	next := attempt + 1
	backoff := time.Second * (1 << uint(next)) // 2s, 4s, 8s, ...
	svc.logger.Debug("Retrying %s (attempt %d/%d, backoff %s)", resp.Request.URL, next, svc.cfg.RetryCount, backoff)
	time.Sleep(backoff)

	resp.Ctx.Put(ctxRetryCountKey, next)
	if err := resp.Request.Retry(); err != nil {
		svc.logger.Error("Retry failed for %s: %v", resp.Request.URL, err)
		return false
	}
	return true
}

// --- Logger ---

type noopLog struct{}

func (noopLog) Debug(string, ...interface{})   {}
func (noopLog) Info(string, ...interface{})    {}
func (noopLog) Warning(string, ...interface{}) {}
func (noopLog) Error(string, ...interface{})   {}
