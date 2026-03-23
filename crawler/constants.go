package crawler

const (
	ctxProductIDKey                  = "crawler_product_id"
	ctxProductPlatformKey            = "crawler_product_platform"
	ctxProductURLKey                 = "crawler_product_url"
	ctxRunContextKey                 = "crawler_run_context"
	ctxHTTPStatusCodeKey             = "crawler_http_status"
	ctxProductErrorKey               = "crawler_product_error"
	ctxProductRulesKey               = "crawler_product_rules"
	ctxProductImageURLKey            = "crawler_product_image"
	ctxProductImageStatusKey         = "crawler_product_image_status"
	ctxProductTitleKey               = "crawler_product_title"
	ctxProductNotFoundFlag           = "crawler_product_not_found"
	ctxInitialURLKey                 = "crawler_initial_url"
	ctxRedirectedKey                 = "crawler_redirected"
	ctxRedirectedProductKey          = "crawler_redirected_product_id"
	ctxFinalURLKey                   = "crawler_final_url"
	ctxCanonicalURLKey               = "crawler_canonical_url"
	ctxDiscoverabilityStatusKey      = "crawler_discoverability_status"
	ctxTargetOrganicRankKey          = "crawler_discoverability_target_organic_rank"
	ctxFirstOrganicASINKey           = "crawler_discoverability_first_organic_asin"
	ctxSponsoredBeforeTargetCountKey = "crawler_discoverability_sponsored_before_target_count"
	ctxDiscoverabilitySearchURLKey   = "crawler_discoverability_search_url"

	pageNotFoundText     = "Page Not Found"
	captchaPageText      = "CAPTCHA"
	unknownProductID     = "UnknownProductID"
	htmlTitleTag         = "title"
	titleNotFoundMessage = "Title Not Found"

	unknownURLValue      = "UnknownURL"
	unknownPlatformValue = "UnknownPlatform"
	productImageNotFound = ""

	htmlExtension = "html"
	webpExtension = "webp"

	retryCountKey  = "crawler_retry_count"
	retriedFlagKey = "crawler_retried"
)
