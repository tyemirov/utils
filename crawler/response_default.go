package crawler

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
)

type defaultResponseProcessor struct {
	config         Config
	retryHandler   RetryHandler
	proxyTracker   proxyHealth
	filePersister  FilePersister
	results        chan<- *Result
	logger         Logger
	resultCallback func(*colly.Response)
}

func newDefaultResponseProcessor(cfg Config, retryHandler RetryHandler, proxyTracker proxyHealth, filePersister FilePersister, results chan<- *Result, logger Logger) ResponseProcessor {
	return &defaultResponseProcessor{
		config:        cfg,
		retryHandler:  retryHandler,
		proxyTracker:  proxyTracker,
		filePersister: filePersister,
		results:       results,
		logger:        logger,
	}
}

func (processor *defaultResponseProcessor) Setup(collector *colly.Collector) {
	collector.OnResponse(processor.handleResponse)
}

func (processor *defaultResponseProcessor) SetResultCallback(callback func(*colly.Response)) {
	processor.resultCallback = callback
}

func (processor *defaultResponseProcessor) SendFinalResult(resp *colly.Response, success bool, errorText string) {
	if processor.resultCallback != nil {
		processor.resultCallback(resp)
	}

	productID := GetProductIDFromContext(resp)
	productURL := GetContextValue(resp.Ctx, ctxProductURLKey, unknownURLValue)
	productPlatform := GetContextValue(resp.Ctx, ctxProductPlatformKey, unknownPlatformValue)

	statusCode := resp.StatusCode
	switch value := resp.Ctx.GetAny(ctxHTTPStatusCodeKey).(type) {
	case int:
		statusCode = value
	case int32:
		statusCode = int(value)
	case int64:
		statusCode = int(value)
	case float64:
		statusCode = int(value)
	}

	finalURL := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	proxyURL := ""
	if resp.Request != nil {
		proxyURL = resp.Request.ProxyURL
	}

	processor.results <- &Result{
		ProductID:       productID,
		ProductURL:      productURL,
		ProductPlatform: productPlatform,
		FinalURL:        finalURL,
		ProxyURL:        strings.TrimSpace(proxyURL),
		Success:         success,
		ErrorMessage:    errorText,
		HTTPStatusCode:  statusCode,
		ProductTitle:    GetContextValue(resp.Ctx, ctxProductTitleKey, titleNotFoundMessage),
	}
}

func (processor *defaultResponseProcessor) handleResponse(resp *colly.Response) {
	productID := GetProductIDFromContext(resp)

	document, parseError := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if parseError != nil {
		processor.SendFinalResult(resp, false, fmt.Sprintf("html parse error: %v", parseError))
		return
	}

	documentTitle := extractDocumentTitle(document)
	platformHooks := ensurePlatformHooks(processor.config.PlatformHooks)
	normalizedTitle := platformHooks.NormalizeTitle(documentTitle)
	resp.Ctx.Put(ctxProductTitleKey, normalizedTitle)

	retryDecision := platformHooks.ShouldRetry(normalizedTitle, document)
	if retryDecision.ShouldRetry {
		retryOptions := RetryOptions{}
		if retryDecision.Policy == RetryPolicyRotateProxy {
			retryOptions.SkipDelay = true
		}
		if processor.retryHandler.Retry(resp, retryOptions) {
			return
		}
		if retryDecision.ExhaustionBehavior == RetryExhaustionBehaviorFail {
			processor.SendFinalResult(resp, false, retryDecision.Message)
			return
		}
	}

	if processor.proxyTracker != nil && resp.Request != nil && resp.Request.ProxyURL != "" {
		processor.proxyTracker.RecordSuccess(resp.Request.ProxyURL)
	}

	if processor.filePersister != nil && processor.config.Scraper.SaveFiles {
		fileName := productID + "." + htmlExtension
		if saveError := processor.filePersister.Save(productID, fileName, resp.Body); saveError != nil {
			processor.logger.Error("Failed to save HTML for %s: %v", productID, saveError)
		}
	}

	var ruleResults []RuleResult
	configuredVerifierCount := 0
	if processor.config.RuleEvaluator != nil {
		evaluation, evaluationError := processor.config.RuleEvaluator.Evaluate(productID, document)
		if evaluationError != nil {
			processor.logger.Error("Rule evaluation failed for %s: %v", productID, evaluationError)
		} else {
			ruleResults = evaluation.RuleResults
			configuredVerifierCount = evaluation.ConfiguredVerifier
		}
	}

	canonicalURL := extractCanonicalURL(document)
	finalURL := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	proxyURL := ""
	if resp.Request != nil {
		proxyURL = resp.Request.ProxyURL
	}

	if processor.resultCallback != nil {
		processor.resultCallback(resp)
	}

	processor.results <- &Result{
		ProductID:               productID,
		ProductURL:              GetContextValue(resp.Ctx, ctxProductURLKey, unknownURLValue),
		ProductPlatform:         GetContextValue(resp.Ctx, ctxProductPlatformKey, unknownPlatformValue),
		ProductTitle:            normalizedTitle,
		FinalURL:                finalURL,
		CanonicalURL:            canonicalURL,
		ProxyURL:                strings.TrimSpace(proxyURL),
		Success:                 true,
		HTTPStatusCode:          resp.StatusCode,
		RuleResults:             ruleResults,
		ConfiguredVerifierCount: configuredVerifierCount,
	}
}

func extractDocumentTitle(document *goquery.Document) string {
	titleElement := document.Find(htmlTitleTag).First()
	rawTitle := titleElement.Text()
	return normalizeTitleWhitespace(rawTitle)
}

func normalizeTitleWhitespace(rawTitle string) string {
	normalized := strings.Join(strings.Fields(rawTitle), " ")
	return strings.TrimSpace(normalized)
}

func extractCanonicalURL(document *goquery.Document) string {
	canonicalHref, _ := document.Find(`link[rel="canonical"]`).Attr("href")
	return strings.TrimSpace(canonicalHref)
}
