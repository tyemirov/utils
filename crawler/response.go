package crawler

import (
	"bytes"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
)

const detailIncompleteMessage = "detail page content missing"

// ResponseProcessor handles incoming responses and emits final results.
type ResponseProcessor interface {
	Setup(collector *colly.Collector)
	SendFinalResult(resp *colly.Response, success bool, errorText string)
	SetResultCallback(callback func(*colly.Response))
	SetResponseHandlers(handlers []ResponseHandler)
}

type responseProcessor struct {
	scraperConfig    ScraperConfig
	platformConfig   PlatformConfig
	ruleEvaluator    RuleEvaluator
	platformHooks    PlatformHooks
	retryHandler     RetryHandler
	proxyTracker     proxyHealth
	filePersister    FilePersister
	results          chan<- *Result
	platformID       string
	runFolder        string
	collector        *colly.Collector
	logger           Logger
	resultCallback   func(*colly.Response)
	responseHandlers []ResponseHandler
}

func newResponseProcessor(
	cfg Config,
	retryHandler RetryHandler,
	proxyTracker proxyHealth,
	filePersister FilePersister,
	results chan<- *Result,
	logger Logger,
) ResponseProcessor {
	return &responseProcessor{
		scraperConfig:  cfg.Scraper,
		platformConfig: cfg.Platform,
		ruleEvaluator:  cfg.RuleEvaluator,
		platformHooks:  ensurePlatformHooks(cfg.PlatformHooks),
		retryHandler:   retryHandler,
		proxyTracker:   proxyTracker,
		filePersister:  filePersister,
		results:        results,
		platformID:     cfg.PlatformID,
		runFolder:      strings.TrimSpace(cfg.RunFolder),
		logger:         logger,
	}
}

func (processor *responseProcessor) Setup(collector *colly.Collector) {
	processor.collector = collector
	collector.OnResponse(processor.handleResponse)
}

func (processor *responseProcessor) handleResponse(resp *colly.Response) {
	resp.Ctx.Put(ctxHTTPStatusCodeKey, resp.StatusCode)
	processor.logger.Debug("Got response for URL: %s", resp.Request.URL)

	productID := getProductIDFromContext(resp)
	fileExtension := filepath.Ext(resp.FileName())

	for _, handler := range processor.responseHandlers {
		if handler.HandleBinaryResponse(resp, productID, fileExtension) {
			return
		}
	}

	document, parseErr := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if parseErr != nil {
		processor.logger.Error("Failed to parse HTML response for %s product: %s, error: %v", processor.platformID, productID, parseErr)
		if !processor.retryHandler.Retry(resp, RetryOptions{}) {
			processor.SendFinalResult(resp, false, parseErr.Error())
		}
		return
	}

	pageTitleText := extractDocumentTitle(document)
	domTitleText := processor.platformHooks.ExtractDOMTitle(document)
	if pageTitleText == "" && domTitleText == "" {
		processor.logger.Warning("No title found for %s product: %s", processor.platformID, productID)
		if !processor.retryHandler.Retry(resp, RetryOptions{}) {
			processor.SendFinalResult(resp, false, titleNotFoundMessage)
		}
		return
	}

	titleForRetry := pageTitleText
	if titleForRetry == "" {
		titleForRetry = domTitleText
	}
	processor.logger.Debug("Title found: %s", titleForRetry)
	titleText := pageTitleText
	if domTitleText != "" {
		titleText = domTitleText
	}
	titleText = processor.platformHooks.NormalizeTitle(titleText)
	resp.Ctx.Put(ctxProductTitleKey, titleText)

	if strings.Contains(strings.ToLower(titleForRetry), strings.ToLower(pageNotFoundText)) {
		// Some platforms return an HTTP 200 for a missing page but signal not-found in the page title.
		// Normalize to 404 so downstream logic (retry/refund) can rely on HTTPStatusCode.
		resp.Ctx.Put(ctxHTTPStatusCodeKey, http.StatusNotFound)
		resp.Ctx.Put(ctxProductErrorKey, pageNotFoundText)
		resp.Ctx.Put(ctxProductNotFoundFlag, true)
		processor.logger.Error("Product not found for ProductID: %s", productID)
		processor.SendFinalResult(resp, false, pageNotFoundText)
		return
	}

	retryDecision := processor.platformHooks.ShouldRetry(titleForRetry, document)
	skipProxySuccess := false
	if retryDecision.ShouldRetry {
		processor.logger.Warning(
			"Retrying URL: %s (status=%d, proxy=%s, reason=%s)",
			resp.Request.URL,
			resp.StatusCode,
			describeProxyForLog(responseProxyURL(resp)),
			retryDecision.ResolvedLogMessage(),
		)
		processor.persistHTMLSnapshot(productID, resp.Body)
		if !processor.retryByDecision(resp, retryDecision) {
			if retryDecision.ExhaustionBehavior == RetryExhaustionBehaviorContinue {
				if retryDecision.Policy == RetryPolicyRotateProxy {
					skipProxySuccess = true
				}
				processor.logger.Warning(
					"Retry budget exhausted for URL: %s; continuing evaluation (reason=%s)",
					resp.Request.URL,
					retryDecision.ResolvedLogMessage(),
				)
			} else {
				processor.SendFinalResult(resp, false, retryDecision.Message)
				return
			}
		} else {
			return
		}
	}

	if !processor.platformHooks.IsContentComplete(document) {
		processor.logger.Warning("Detail page incomplete for %s; retrying", productID)
		processor.persistHTMLSnapshot(productID, resp.Body)
		if !processor.retryHandler.Retry(resp, RetryOptions{}) {
			resp.Ctx.Put(ctxProductErrorKey, detailIncompleteMessage)
			processor.SendFinalResult(resp, false, detailIncompleteMessage)
		}
		return
	}

	resp.Ctx.Put(ctxProductErrorKey, nil)
	resp.Ctx.Put(ctxProductTitleKey, titleText)
	if !skipProxySuccess {
		processor.recordProxySuccess(resp)
	}

	for _, handler := range processor.responseHandlers {
		handler.BeforeEvaluation(resp, document)
	}

	finalURL := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	resp.Ctx.Put(ctxFinalURLKey, finalURL)

	canonicalURL := extractCanonicalURL(document)
	if canonicalURL != "" {
		resp.Ctx.Put(ctxCanonicalURLKey, canonicalURL)
	}

	originalURL := resp.Ctx.Get(ctxInitialURLKey)
	processor.inferRedirect(productID, originalURL, finalURL, canonicalURL, resp.Ctx)

	if processor.skipEvaluationOnRedirect(resp) {
		return
	}

	evaluation, evalErr := processor.ruleEvaluator.Evaluate(productID, document)
	if evalErr != nil {
		processor.logger.Error("Failed to evaluate rules for the URL: %s, error: %v", resp.Request.URL, evalErr)
		return
	}

	resp.Ctx.Put(ctxProductRulesKey, evaluation)
	result := processor.buildResult(resp, true, "")

	for _, handler := range processor.responseHandlers {
		handler.AfterEvaluation(resp, document, result)
	}

	if processor.resultCallback != nil {
		processor.resultCallback(resp)
	}
	processor.results <- result

	if processor.scraperConfig.SaveFiles {
		processor.logger.Debug("Saving HTML for %s product %s", processor.platformID, productID)
		pageFile := fmt.Sprintf("%s.%s", productID, htmlExtension)
		if err := processor.saveFile(productID, pageFile, resp.Body); err != nil {
			processor.logger.Error("Failed to save HTML for ProductID %s: %v", productID, err)
		}
	}
}

func (processor *responseProcessor) SetResultCallback(callback func(*colly.Response)) {
	processor.resultCallback = callback
}

func (processor *responseProcessor) SetResponseHandlers(handlers []ResponseHandler) {
	processor.responseHandlers = handlers
}

func (processor *responseProcessor) recordProxySuccess(resp *colly.Response) {
	proxyURL := responseProxyURL(resp)
	if processor.proxyTracker == nil || proxyURL == "" {
		return
	}
	processor.proxyTracker.RecordSuccess(proxyURL)
}

func (processor *responseProcessor) retryByDecision(resp *colly.Response, decision RetryDecision) bool {
	if processor.retryHandler == nil {
		return false
	}

	options := RetryOptions{}
	if decision.Policy == RetryPolicyRotateProxy {
		processor.recordCriticalProxyFailure(resp)
		options.SkipDelay = true
		if alternativeProxyRetryCount := processor.alternativeProxyRetryCount(); alternativeProxyRetryCount > 0 {
			options.LimitRetries = true
			options.MaxRetries = alternativeProxyRetryCount
		}
	}

	return processor.retryHandler.Retry(resp, options)
}

func (processor *responseProcessor) alternativeProxyRetryCount() int {
	if len(processor.scraperConfig.ProxyList) <= 1 {
		return 0
	}
	return len(processor.scraperConfig.ProxyList) - 1
}

func (processor *responseProcessor) recordCriticalProxyFailure(resp *colly.Response) {
	proxyURL := responseProxyURL(resp)
	if processor.proxyTracker == nil || proxyURL == "" {
		return
	}
	processor.proxyTracker.RecordCriticalFailure(proxyURL)
}

func responseProxyURL(resp *colly.Response) string {
	if resp == nil || resp.Request == nil {
		return ""
	}
	return strings.TrimSpace(resp.Request.ProxyURL)
}

func (processor *responseProcessor) inferRedirect(productID, originalURL, finalURL, canonicalURL string, ctx *colly.Context) {
	redirected, redirectedProductID := processor.platformHooks.InferRedirect(productID, originalURL, finalURL, canonicalURL)
	if redirected {
		ctx.Put(ctxRedirectedKey, true)
		if redirectedProductID != "" {
			ctx.Put(ctxRedirectedProductKey, redirectedProductID)
			processor.logger.Info("Redirect detected %s -> %s", productID, redirectedProductID)
		} else {
			processor.logger.Info("Redirect detected %s -> %s", originalURL, finalURL)
		}
	}
}

func (processor *responseProcessor) SendFinalResult(resp *colly.Response, success bool, errorText string) {
	if processor.resultCallback != nil {
		processor.resultCallback(resp)
	}
	result := processor.buildResult(resp, success, errorText)
	processor.results <- result
}

func (processor *responseProcessor) skipEvaluationOnRedirect(resp *colly.Response) bool {
	if processor == nil || resp == nil || resp.Ctx == nil {
		return false
	}
	if !processor.platformConfig.SkipRulesOnRedirect {
		return false
	}
	ctx := resp.Ctx
	redirectedID := ctx.Get(ctxRedirectedProductKey)
	if redirectedID == "" {
		return false
	}
	originalID := getProductIDFromContext(resp)
	message := fmt.Sprintf("Product redirected to %s.", redirectedID)
	if originalID != "" && originalID != unknownProductID && originalID != redirectedID {
		message = fmt.Sprintf("Product redirected from %s to %s.", originalID, redirectedID)
	}
	ctx.Put(ctxProductRulesKey, RuleEvaluation{
		ConfiguredVerifier: processor.ruleEvaluator.ConfiguredVerifierCount(),
		RuleResults:        nil,
	})
	ctx.Put(ctxProductErrorKey, message)
	processor.SendFinalResult(resp, false, message)
	return true
}

func (processor *responseProcessor) buildResult(resp *colly.Response, success bool, errorText string) *Result {
	ctx := resp.Ctx
	originalProductID := getProductIDFromContext(resp)

	statusCode := resp.StatusCode
	switch value := ctx.GetAny(ctxHTTPStatusCodeKey).(type) {
	case int:
		statusCode = value
	case int32:
		statusCode = int(value)
	case int64:
		statusCode = int(value)
	case float64:
		statusCode = int(value)
	}

	productURL := getContextValue(ctx, ctxProductURLKey, unknownURLValue)
	productTitle := getContextValue(ctx, ctxProductTitleKey, titleNotFoundMessage)
	productPlatform := getContextValue(ctx, ctxProductPlatformKey, unknownPlatformValue)

	originalURL := ctx.Get(ctxInitialURLKey)
	if originalURL == "" {
		originalURL = productURL
	}
	finalURL := ctx.Get(ctxFinalURLKey)
	canonicalURL := ctx.Get(ctxCanonicalURLKey)
	proxyURL := ""
	if resp.Request != nil {
		proxyURL = resp.Request.ProxyURL
	}

	finalProductID := resolveFinalProductID(ctx, originalProductID)

	evaluation, _ := ctx.GetAny(ctxProductRulesKey).(RuleEvaluation)
	ruleResults := evaluation.RuleResults
	if !success {
		ruleResults = nil
	}

	if errorText == "" {
		errorText = getErrorMessage(ctx)
	}

	return &Result{
		ProductID:               finalProductID,
		OriginalProductID:       originalProductID,
		OriginalURL:             originalURL,
		FinalURL:                finalURL,
		CanonicalURL:            canonicalURL,
		ProxyURL:                strings.TrimSpace(proxyURL),
		ProductURL:              productURL,
		ProductTitle:            productTitle,
		ProductPlatform:         productPlatform,
		Success:                 success,
		ErrorMessage:            errorText,
		HTTPStatusCode:          statusCode,
		RuleResults:             ruleResults,
		ConfiguredVerifierCount: evaluation.ConfiguredVerifier,
	}
}

func resolveFinalProductID(ctx *colly.Context, originalProductID string) string {
	if ctx == nil {
		return strings.TrimSpace(originalProductID)
	}
	if redirectedID := strings.TrimSpace(ctx.Get(ctxRedirectedProductKey)); redirectedID != "" {
		return redirectedID
	}
	return strings.TrimSpace(originalProductID)
}

func (processor *responseProcessor) saveFile(productID, fileName string, content []byte) error {
	if processor == nil || processor.filePersister == nil {
		return nil
	}
	return processor.filePersister.Save(productID, fileName, content)
}

func (processor *responseProcessor) persistHTMLSnapshot(productID string, body []byte) {
	if len(body) == 0 {
		return
	}
	pageFile := fmt.Sprintf("%s.%s", productID, htmlExtension)
	if err := processor.saveFile(productID, pageFile, body); err != nil {
		processor.logger.Error("Failed to persist HTML for ProductID %s: %v", productID, err)
	}
}

func getProductIDFromContext(resp *colly.Response) string {
	productID := resp.Ctx.Get(ctxProductIDKey)
	if productID == "" {
		return unknownProductID
	}
	return productID
}

func getContextValue(ctx *colly.Context, key, fallback string) string {
	if value := ctx.Get(key); value != "" {
		return value
	}
	return fallback
}

func getErrorMessage(ctx *colly.Context) string {
	switch val := ctx.GetAny(ctxProductErrorKey).(type) {
	case error:
		return val.Error()
	case string:
		return val
	default:
		return ""
	}
}

func extractCanonicalURL(document *goquery.Document) string {
	canonicalLink := document.Find(`link[rel="canonical"]`).First()
	if canonicalLink.Length() == 0 {
		return ""
	}
	href, exists := canonicalLink.Attr("href")
	if !exists {
		return ""
	}
	return href
}

func extractDocumentTitle(document *goquery.Document) string {
	if document == nil {
		return ""
	}
	titleElement := document.Find(htmlTitleTag).First()
	if titleElement.Length() == 0 {
		return ""
	}
	return normalizeTitleWhitespace(titleElement.Text())
}

func normalizeTitleWhitespace(rawText string) string {
	fields := strings.Fields(strings.TrimSpace(rawText))
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

func looksLikeImagePayload(contentType string, body []byte) bool {
	normalized := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(normalized, "image/") {
		return true
	}
	if len(body) == 0 {
		return false
	}
	sample := body
	if len(sample) > 512 {
		sample = body[:512]
	}
	detected := http.DetectContentType(sample)
	return strings.HasPrefix(strings.ToLower(detected), "image/")
}
