package crawler

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
)

// DefaultResponseHandler processes HTTP responses using an Evaluator.
// It parses HTML, extracts the title, runs the evaluator, and emits Results.
type DefaultResponseHandler struct {
	evaluator     Evaluator
	platformHooks PlatformHooks
	retryHandler  RetryHandler
	proxyTracker  ProxyHealth
	filePersister FilePersister
	results       chan<- *Result
	category      string
	logger        Logger
	scraperCfg    ScraperConfig
	slotReleaser  func(*colly.Response)
}

// NewDefaultResponseHandler creates the standard response handler.
func NewDefaultResponseHandler(
	cfg Config,
	retryHandler RetryHandler,
	proxyTracker ProxyHealth,
	filePersister FilePersister,
	results chan<- *Result,
	logger Logger,
) *DefaultResponseHandler {
	return &DefaultResponseHandler{
		evaluator:     cfg.Evaluator,
		platformHooks: ensurePlatformHooks(cfg.PlatformHooks),
		retryHandler:  retryHandler,
		proxyTracker:  proxyTracker,
		filePersister: filePersister,
		results:       results,
		category:      cfg.Category,
		logger:        logger,
		scraperCfg:    cfg.Scraper,
	}
}

func (h *DefaultResponseHandler) Setup(collector *colly.Collector) {
	collector.OnResponse(h.handleResponse)
}

func (h *DefaultResponseHandler) SetSlotReleaser(releaser func(*colly.Response)) {
	h.slotReleaser = releaser
}

func (h *DefaultResponseHandler) SendResult(resp *colly.Response, success bool, errorMessage string) {
	if h.slotReleaser != nil {
		h.slotReleaser(resp)
	}
	result := h.buildResult(resp, success, errorMessage)
	h.results <- result
}

func (h *DefaultResponseHandler) handleResponse(resp *colly.Response) {
	targetID := GetTargetIDFromContext(resp)
	targetURL := GetTargetURLFromContext(resp)

	doc, err := ParseHTMLResponse(resp.Body)
	if err != nil {
		h.SendResult(resp, false, fmt.Sprintf("parse error: %v", err))
		return
	}

	title := ExtractTitle(doc)
	title = h.platformHooks.NormalizeTitle(title)

	// Check if platform wants a retry
	decision := h.platformHooks.ShouldRetry(title, doc)
	if decision.ShouldRetry {
		h.logger.Info("Platform retry requested for %s: %s", targetID, decision.ResolvedLogMessage())
		opts := RetryOptions{}
		if decision.Policy == RetryPolicyRotateProxy {
			opts.SkipDelay = true
		}
		if h.retryHandler.Retry(resp, opts) {
			return
		}
		if decision.ExhaustionBehavior == RetryExhaustionBehaviorFail {
			h.SendResult(resp, false, decision.Message)
			return
		}
	}

	// Persist HTML if configured
	if h.filePersister != nil && h.scraperCfg.SaveFiles {
		if err := h.filePersister.Save(targetID, targetID+"."+HTMLExtension, resp.Body); err != nil {
			h.logger.Error("Failed to persist HTML for %s: %v", targetID, err)
		}
	}

	// Run evaluator
	eval, evalErr := h.evaluator.Evaluate(targetID, doc)
	if evalErr != nil {
		h.logger.Error("Evaluator error for %s: %v", targetID, evalErr)
	}

	finalURL := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	canonicalURL := ExtractCanonicalURL(doc)
	proxyURL := ""
	if resp.Request != nil {
		proxyURL = resp.Request.ProxyURL
	}

	result := &Result{
		TargetID:       targetID,
		TargetURL:      targetURL,
		FinalURL:       finalURL,
		CanonicalURL:   canonicalURL,
		Category:       h.category,
		Title:          title,
		Success:        evalErr == nil,
		ErrorMessage:   errorText(evalErr),
		HTTPStatusCode: resp.StatusCode,
		Findings:       eval.Findings,
		Document:       doc,
		ProxyURL:       strings.TrimSpace(proxyURL),
	}

	if h.slotReleaser != nil {
		h.slotReleaser(resp)
	}
	h.results <- result
}

func (h *DefaultResponseHandler) buildResult(resp *colly.Response, success bool, errorMessage string) *Result {
	targetID := GetTargetIDFromContext(resp)
	targetURL := GetTargetURLFromContext(resp)
	category := GetTargetCategoryFromContext(resp)
	finalURL := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	proxyURL := ""
	if resp.Request != nil {
		proxyURL = resp.Request.ProxyURL
	}
	return &Result{
		TargetID:       targetID,
		TargetURL:      targetURL,
		FinalURL:       finalURL,
		Category:       category,
		Success:        success,
		ErrorMessage:   errorMessage,
		HTTPStatusCode: resp.StatusCode,
		ProxyURL:       strings.TrimSpace(proxyURL),
	}
}

// --- Exported helpers for custom ResponseHandler implementations ---

// ParseHTMLResponse parses HTML from a response body.
func ParseHTMLResponse(body []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(bytes.NewReader(body))
}

// ExtractTitle returns the text content of the first <title> element.
func ExtractTitle(doc *goquery.Document) string {
	return strings.TrimSpace(doc.Find("title").First().Text())
}

// ExtractCanonicalURL extracts the canonical URL from a <link rel="canonical"> tag.
func ExtractCanonicalURL(doc *goquery.Document) string {
	canonical, _ := doc.Find(`link[rel="canonical"]`).Attr("href")
	return strings.TrimSpace(canonical)
}

// GetTargetIDFromContext retrieves the target ID from a Colly response context.
func GetTargetIDFromContext(resp *colly.Response) string {
	if resp == nil || resp.Ctx == nil {
		return ""
	}
	return resp.Ctx.Get(CtxTargetIDKey)
}

// GetTargetURLFromContext retrieves the target URL from a Colly response context.
func GetTargetURLFromContext(resp *colly.Response) string {
	if resp == nil || resp.Ctx == nil {
		return UnknownURL
	}
	v := resp.Ctx.Get(CtxTargetURLKey)
	if v == "" {
		return UnknownURL
	}
	return v
}

// GetTargetCategoryFromContext retrieves the target category from a Colly response context.
func GetTargetCategoryFromContext(resp *colly.Response) string {
	if resp == nil || resp.Ctx == nil {
		return ""
	}
	return resp.Ctx.Get(CtxTargetCategoryKey)
}

// GetContextValue returns a value from a Colly context, or the fallback if absent.
func GetContextValue(ctx *colly.Context, key, fallback string) string {
	if ctx == nil {
		return fallback
	}
	v := ctx.Get(key)
	if v == "" {
		return fallback
	}
	return v
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// RecordProxyFailure records a proxy failure on the tracker if applicable.
func RecordProxyFailure(tracker ProxyHealth, resp *colly.Response) {
	if tracker == nil || resp == nil || resp.Request == nil {
		return
	}
	if resp.Request.ProxyURL == "" || resp.StatusCode != 0 {
		return
	}
	tracker.RecordFailure(resp.Request.ProxyURL)
}

// SetupErrorHandling wires OnError with retry and proxy tracking.
func SetupErrorHandling(collector *colly.Collector, handler ResponseHandler, retryHandler RetryHandler, tracker ProxyHealth, logger Logger) {
	collector.OnError(func(resp *colly.Response, err error) {
		RecordProxyFailure(tracker, resp)
		url := UnknownURL
		if resp != nil && resp.Request != nil && resp.Request.URL != nil {
			url = resp.Request.URL.String()
		}
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		logger.Error("URL: %s, StatusCode: %d, Error: %v", url, statusCode, err)

		if resp == nil {
			return
		}
		if resp.Ctx == nil {
			resp.Ctx = colly.NewContext()
		}

		errText := ""
		if err != nil {
			errText = err.Error()
		}

		if resp.StatusCode == http.StatusNotFound {
			handler.SendResult(resp, false, errText)
			return
		}
		if resp.Request == nil || resp.Request.URL == nil {
			handler.SendResult(resp, false, errText)
			return
		}
		if !retryHandler.Retry(resp, RetryOptions{}) {
			handler.SendResult(resp, false, errText)
		}
	})
}
