package crawler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
)

// ImageEncoder converts image data from the source format to a target format.
type ImageEncoder func(imageData []byte, fileExtension string) ([]byte, error)

var convertToWebP ImageEncoder

const detailIncompleteMessage = "detail page content missing"
const imageUnavailableMessage = "image unavailable"

const (
	defaultDiscoverabilityProbeRetryCount   = 1
	defaultDiscoverabilityProbeRetryBackoff = 500 * time.Millisecond
	defaultDiscoverabilityProbeParallelism  = 8
	maxDiscoverabilityProbeRetryDelay       = 5 * time.Second
)

// ResponseProcessor handles incoming responses and emits final results.
type ResponseProcessor interface {
	Setup(collector *colly.Collector)
	SendFinalResult(resp *colly.Response, success bool, errorText string)
	SetResultCallback(callback func(*colly.Response))
}

type responseProcessor struct {
	scraperConfig                    ScraperConfig
	platformConfig                   PlatformConfig
	ruleEvaluator                    RuleEvaluator
	platformHooks                    PlatformHooks
	retryHandler                     RetryHandler
	proxyTracker                     proxyHealth
	filePersister                    FilePersister
	results                          chan<- *Result
	platformID                       string
	runFolder                        string
	collector                        *colly.Collector
	logger                           Logger
	resultCallback                   func(*colly.Response)
	imageQueue                       chan<- imageJob
	imageEncoder                     ImageEncoder
	imageStatusHook                  ImageStatusHook
	discoverabilityProber            DiscoverabilityProber
	discoverabilityProbeRetryCount   int
	discoverabilityProbeRetryBackoff time.Duration
	discoverabilityProbeSemaphore    chan struct{}
}

func newResponseProcessor(
	cfg Config,
	retryHandler RetryHandler,
	proxyTracker proxyHealth,
	filePersister FilePersister,
	results chan<- *Result,
	logger Logger,
	imageQueue chan<- imageJob,
) ResponseProcessor {
	return &responseProcessor{
		scraperConfig:                    cfg.Scraper,
		platformConfig:                   cfg.Platform,
		ruleEvaluator:                    cfg.RuleEvaluator,
		platformHooks:                    ensurePlatformHooks(cfg.PlatformHooks),
		retryHandler:                     retryHandler,
		proxyTracker:                     proxyTracker,
		filePersister:                    filePersister,
		results:                          results,
		platformID:                       cfg.PlatformID,
		runFolder:                        strings.TrimSpace(cfg.RunFolder),
		logger:                           logger,
		imageQueue:                       imageQueue,
		imageEncoder:                     cfg.ImageEncoder,
		imageStatusHook:                  ensureImageStatusHook(cfg.ImageStatusHook),
		discoverabilityProber:            cfg.DiscoverabilityProber,
		discoverabilityProbeRetryCount:   resolveDiscoverabilityProbeRetryCount(cfg.DiscoverabilityProbeRetryCount),
		discoverabilityProbeRetryBackoff: resolveDiscoverabilityProbeRetryBackoff(cfg.DiscoverabilityProbeRetryBackoff),
		discoverabilityProbeSemaphore:    buildDiscoverabilityProbeSemaphore(cfg.DiscoverabilityProbeParallelism),
	}
}

func resolveDiscoverabilityProbeRetryCount(rawRetryCount int) int {
	if rawRetryCount >= 0 {
		return rawRetryCount
	}
	return defaultDiscoverabilityProbeRetryCount
}

func resolveDiscoverabilityProbeRetryBackoff(rawBackoff time.Duration) time.Duration {
	if rawBackoff >= 0 {
		return rawBackoff
	}
	return defaultDiscoverabilityProbeRetryBackoff
}

func buildDiscoverabilityProbeSemaphore(rawParallelism int) chan struct{} {
	parallelism := rawParallelism
	if parallelism <= 0 {
		parallelism = defaultDiscoverabilityProbeParallelism
	}
	return make(chan struct{}, parallelism)
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
	if productID != unknownProductID && isKnownImageExtension(fileExtension) {
		if !looksLikeImagePayload(resp.Headers.Get("Content-Type"), resp.Body) {
			processor.logger.Warning(
				"%s for ProductID %s (content-type=%s)",
				imageUnavailableMessage,
				productID,
				resp.Headers.Get("Content-Type"),
			)
			processor.retryImageOrDrop(resp, productID)
			return
		}
		processor.enqueueImageConversion(resp, productID, fileExtension)
		return
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

	if processor.scraperConfig.RetrieveProductImages {
		imageURL, err := processor.getProductImage(resp, document, productID)
		if err != nil {
			processor.logger.Error("Failed to get product image for %s, error: %v", productID, err)
		}
		resp.Ctx.Put(ctxProductImageURLKey, imageURL)
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
	processor.probeDiscoverability(resp)

	if processor.skipEvaluationOnRedirect(resp) {
		return
	}

	evaluation, evalErr := processor.ruleEvaluator.Evaluate(productID, document)
	if evalErr != nil {
		processor.logger.Error("Failed to evaluate rules for the URL: %s, error: %v", resp.Request.URL, evalErr)
		return
	}

	resp.Ctx.Put(ctxProductRulesKey, evaluation)
	processor.SendFinalResult(resp, true, "")

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

func (processor *responseProcessor) recordProxySuccess(resp *colly.Response) {
	proxyURL := responseProxyURL(resp)
	if processor.proxyTracker == nil || proxyURL == "" {
		return
	}
	processor.proxyTracker.RecordSuccess(proxyURL)
}

func (processor *responseProcessor) retryImageOrDrop(resp *colly.Response, productID string) {
	if processor.retryHandler != nil && processor.retryHandler.Retry(resp, RetryOptions{}) {
		return
	}
	ensureImageStatusHook(processor.imageStatusHook)(productID, ImageStatusFailed)
	processor.logger.Warning("Image unavailable for ProductID %s after retries exhausted", productID)
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

func (processor *responseProcessor) enqueueImageConversion(resp *colly.Response, productID, fileExtension string) {
	if processor.imageQueue == nil {
		return
	}
	job := imageJob{
		ProductID:   productID,
		Data:        append([]byte(nil), resp.Body...),
		Extension:   fileExtension,
		ContentType: resp.Headers.Get("Content-Type"),
		onSuccess: func() {
			ensureImageStatusHook(processor.imageStatusHook)(productID, ImageStatusReady)
		},
		onFailure: func() {
			processor.retryImageOrDrop(resp, productID)
		},
	}
	processor.enqueueImageJob(job, productID)
}

func (processor *responseProcessor) enqueueImageJob(job imageJob, productID string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if processor.logger != nil {
				processor.logger.Warning("Image conversion queue closed while processing %s", productID)
			}
			if job.onFailure != nil {
				job.onFailure()
			}
		}
	}()
	processor.imageQueue <- job
}

func (processor *responseProcessor) getProductImage(resp *colly.Response, document *goquery.Document, productID string) (string, error) {
	imageSelector := processor.platformConfig.ProductImageSelector
	var selection *goquery.Selection
	if imageSelector != "" {
		selection = document.Find(imageSelector).First()
	}

	candidates := gatherImageCandidates(selection)
	if len(candidates) == 0 {
		processor.persistHTMLSnapshot(productID, resp.Body)
		fallback := document.Find("[data-a-dynamic-image]").First()
		candidates = append(candidates, gatherImageCandidates(fallback)...)
	}
	if len(candidates) == 0 {
		processor.persistHTMLSnapshot(productID, resp.Body)
		fallback := document.Find("img[data-old-hires]").First()
		candidates = append(candidates, gatherImageCandidates(fallback)...)
	}

	var imageURL string
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		absolute := resp.Request.AbsoluteURL(candidate)
		if absolute != "" {
			imageURL = absolute
			break
		}
	}
	if imageURL == "" {
		processor.persistHTMLSnapshot(productID, resp.Body)
		resp.Ctx.Put(ctxProductImageStatusKey, string(ImageStatusFailed))
		return "", fmt.Errorf("image not found for %s", productID)
	}

	if processor.filePersister == nil {
		resp.Ctx.Put(ctxProductImageStatusKey, string(ImageStatusReady))
		return imageURL, nil
	}

	if processor.collector != nil {
		imageContext := colly.NewContext()
		imageContext.Put(ctxProductIDKey, productID)
		if err := processor.collector.Request(http.MethodGet, imageURL, nil, imageContext, nil); err != nil {
			processor.logger.Warning("Failed to enqueue image download for %s: %v", productID, err)
			resp.Ctx.Put(ctxProductImageStatusKey, string(ImageStatusFailed))
			return "", fmt.Errorf("enqueue image download for %s: %w", productID, err)
		}
	}
	fileName := fmt.Sprintf("%s.%s", productID, webpExtension)
	resp.Ctx.Put(ctxProductImageStatusKey, string(ImageStatusPending))
	return buildDownloadPath(processor.platformID, productID, processor.runFolder, fileName), nil
}

func (processor *responseProcessor) probeDiscoverability(resp *colly.Response) {
	if processor == nil || resp == nil || resp.Ctx == nil {
		return
	}
	originalProductID := getProductIDFromContext(resp)
	targetASIN := normalizeASIN(resolveFinalProductID(resp.Ctx, originalProductID))
	if targetASIN == "" || targetASIN == normalizeASIN(unknownProductID) {
		return
	}
	runContext := getRunContextFromResponse(resp)
	if !discoverabilityProbeEnabledFromContext(runContext) {
		return
	}
	if processor.discoverabilityProber == nil {
		return
	}
	probeParentContext := context.Background()
	if runContext != nil {
		probeParentContext = runContext
	}
	if !processor.acquireDiscoverabilityProbeSemaphore(probeParentContext) {
		return
	}
	defer processor.releaseDiscoverabilityProbeSemaphore()

	maxAttempts := processor.discoverabilityProbeRetryCount + 1
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	var probeError error
	for attemptNumber := 1; attemptNumber <= maxAttempts; attemptNumber++ {
		discoverability, err := processor.discoverabilityProber.Probe(probeParentContext, targetASIN)
		if err == nil {
			processor.setDiscoverabilityContext(resp.Ctx, discoverability)
			return
		}
		probeError = err
		if !processor.shouldRetryDiscoverabilityProbe(err, attemptNumber, maxAttempts) {
			break
		}
		if waitErr := processor.waitBeforeDiscoverabilityProbeRetry(probeParentContext, attemptNumber); waitErr != nil {
			break
		}
	}
	if probeError != nil {
		processor.logger.Warning("Failed discoverability probe for ProductID %s: %v", targetASIN, probeError)
	}
}

func (processor *responseProcessor) shouldRetryDiscoverabilityProbe(
	probeError error,
	attemptNumber int,
	maxAttempts int,
) bool {
	if probeError == nil {
		return false
	}
	if attemptNumber >= maxAttempts {
		return false
	}
	if errors.Is(probeError, context.Canceled) {
		return false
	}
	if errors.Is(probeError, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	if errors.As(probeError, &networkError) {
		if networkError.Timeout() {
			return true
		}
	}
	normalizedError := strings.ToLower(strings.TrimSpace(probeError.Error()))
	if strings.Contains(normalizedError, "timeout") {
		return true
	}
	if strings.Contains(normalizedError, "temporar") {
		return true
	}
	if strings.Contains(normalizedError, "connection reset") {
		return true
	}
	if strings.Contains(normalizedError, "connection refused") {
		return true
	}
	return false
}

func (processor *responseProcessor) waitBeforeDiscoverabilityProbeRetry(
	ctx context.Context,
	attemptNumber int,
) error {
	retryDelay := processor.discoverabilityProbeRetryBackoff
	if retryDelay <= 0 {
		return nil
	}
	if attemptNumber < 1 {
		attemptNumber = 1
	}
	for iteration := 1; iteration < attemptNumber; iteration++ {
		if retryDelay >= maxDiscoverabilityProbeRetryDelay {
			retryDelay = maxDiscoverabilityProbeRetryDelay
			break
		}
		retryDelay = retryDelay * 2
		if retryDelay >= maxDiscoverabilityProbeRetryDelay {
			retryDelay = maxDiscoverabilityProbeRetryDelay
			break
		}
	}
	timer := time.NewTimer(retryDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (processor *responseProcessor) acquireDiscoverabilityProbeSemaphore(ctx context.Context) bool {
	if processor == nil || processor.discoverabilityProbeSemaphore == nil {
		return true
	}
	select {
	case processor.discoverabilityProbeSemaphore <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (processor *responseProcessor) releaseDiscoverabilityProbeSemaphore() {
	if processor == nil || processor.discoverabilityProbeSemaphore == nil {
		return
	}
	select {
	case <-processor.discoverabilityProbeSemaphore:
	default:
	}
}

func getRunContextFromResponse(resp *colly.Response) context.Context {
	if resp == nil || resp.Ctx == nil {
		return nil
	}
	runContext, ok := resp.Ctx.GetAny(ctxRunContextKey).(context.Context)
	if !ok || runContext == nil {
		return nil
	}
	return runContext
}

func (processor *responseProcessor) setDiscoverabilityContext(ctx *colly.Context, discoverability Discoverability) {
	if ctx == nil {
		return
	}
	normalizedStatus := NormalizeDiscoverabilityStatus(string(discoverability.Status))
	if normalizedStatus != "" {
		ctx.Put(ctxDiscoverabilityStatusKey, string(normalizedStatus))
	}
	if discoverability.TargetOrganicRank > 0 {
		ctx.Put(ctxTargetOrganicRankKey, discoverability.TargetOrganicRank)
	}
	firstOrganicASIN := normalizeASIN(discoverability.FirstOrganicASIN)
	if firstOrganicASIN != "" {
		ctx.Put(ctxFirstOrganicASINKey, firstOrganicASIN)
	}
	if discoverability.SponsoredBeforeTargetCount > 0 {
		ctx.Put(ctxSponsoredBeforeTargetCountKey, discoverability.SponsoredBeforeTargetCount)
	}
	if strings.TrimSpace(discoverability.SearchURL) != "" {
		ctx.Put(ctxDiscoverabilitySearchURLKey, strings.TrimSpace(discoverability.SearchURL))
	}
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
	productImageURL := getContextValue(ctx, ctxProductImageURLKey, productImageNotFound)
	productImageStatus := ResolveImageStatus(
		ImageStatus(ctx.Get(ctxProductImageStatusKey)),
		productImageURL,
	)
	discoverabilityStatus := NormalizeDiscoverabilityStatus(ctx.Get(ctxDiscoverabilityStatusKey))
	targetOrganicRank := getContextInt(ctx, ctxTargetOrganicRankKey)
	firstOrganicASIN := normalizeASIN(ctx.Get(ctxFirstOrganicASINKey))
	sponsoredBeforeTargetCount := getContextInt(ctx, ctxSponsoredBeforeTargetCountKey)
	discoverabilitySearchURL := strings.TrimSpace(ctx.Get(ctxDiscoverabilitySearchURLKey))

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
		ProductID:                  finalProductID,
		OriginalProductID:          originalProductID,
		OriginalURL:                originalURL,
		FinalURL:                   finalURL,
		CanonicalURL:               canonicalURL,
		ProxyURL:                   strings.TrimSpace(proxyURL),
		ProductImageURL:            productImageURL,
		ImageStatus:                productImageStatus,
		DiscoverabilityStatus:      discoverabilityStatus,
		TargetOrganicRank:          targetOrganicRank,
		FirstOrganicASIN:           firstOrganicASIN,
		SponsoredBeforeTargetCount: sponsoredBeforeTargetCount,
		DiscoverabilitySearchURL:   discoverabilitySearchURL,
		ProductURL:                 productURL,
		ProductTitle:               productTitle,
		ProductPlatform:            productPlatform,
		Success:                    success,
		ErrorMessage:               errorText,
		HTTPStatusCode:             statusCode,
		RuleResults:                ruleResults,
		ConfiguredVerifierCount:    evaluation.ConfiguredVerifier,
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

func getContextInt(ctx *colly.Context, key string) int {
	if value, ok := ctx.GetAny(key).(int); ok {
		return value
	}
	raw := strings.TrimSpace(ctx.Get(key))
	if raw == "" {
		return 0
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return parsed
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

func gatherImageCandidates(selection *goquery.Selection) []string {
	if selection == nil || selection.Length() == 0 {
		return nil
	}

	var candidates []string
	pushCandidate := func(value string) {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			candidates = append(candidates, trimmed)
		}
	}

	if src := selection.AttrOr("src", ""); src != "" {
		pushCandidate(src)
	}
	if oldHires := selection.AttrOr("data-old-hires", ""); oldHires != "" {
		pushCandidate(oldHires)
	}
	if srcset := selection.AttrOr("srcset", ""); srcset != "" {
		pushCandidate(extractFromSrcset(srcset))
	}
	if dynamic := selection.AttrOr("data-a-dynamic-image", ""); dynamic != "" {
		if parsed := parseDynamicImageJSON(dynamic); parsed != "" {
			pushCandidate(parsed)
		}
	}

	return candidates
}

func parseDynamicImageJSON(raw string) string {
	unescaped := html.UnescapeString(strings.TrimSpace(raw))
	if unescaped == "" {
		return ""
	}
	var parsed map[string][]int
	if err := json.Unmarshal([]byte(unescaped), &parsed); err != nil {
		return ""
	}
	bestURL := ""
	bestScore := 0
	for urlCandidate, dimensions := range parsed {
		score := 0
		if len(dimensions) >= 2 {
			score = dimensions[0] * dimensions[1]
		} else if len(dimensions) == 1 {
			score = dimensions[0]
		}
		if score >= bestScore {
			bestScore = score
			bestURL = urlCandidate
		}
	}
	return bestURL
}

func extractFromSrcset(srcset string) string {
	if strings.TrimSpace(srcset) == "" {
		return ""
	}
	parts := strings.Split(srcset, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		chunk := strings.TrimSpace(parts[i])
		if chunk == "" {
			continue
		}
		fields := strings.Fields(chunk)
		if len(fields) == 0 {
			continue
		}
		return fields[0]
	}
	return ""
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

func isKnownImageExtension(fileExtension string) bool {
	switch strings.ToLower(fileExtension) {
	case ".jpg", ".jpeg", ".png", ".webp", ".avif", ".gif", ".bmp", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

const webHTMLDownloadBasePath = "/download/html/"

func buildDownloadPath(platformID, productID, runFolder, fileName string) string {
	normalizedRunFolder := strings.TrimSpace(runFolder)
	normalizedRunFolder = strings.ReplaceAll(normalizedRunFolder, "\\", "/")
	if normalizedRunFolder == "" {
		return filepath.Join(webHTMLDownloadBasePath, strings.TrimSpace(platformID), strings.TrimSpace(productID), strings.TrimSpace(fileName))
	}
	if strings.ContainsAny(normalizedRunFolder, "/\\") {
		return filepath.Join(webHTMLDownloadBasePath, normalizedRunFolder, strings.TrimSpace(platformID), strings.TrimSpace(productID), strings.TrimSpace(fileName))
	}
	return filepath.Join(webHTMLDownloadBasePath, strings.TrimSpace(platformID), strings.TrimSpace(productID), normalizedRunFolder, strings.TrimSpace(fileName))
}
