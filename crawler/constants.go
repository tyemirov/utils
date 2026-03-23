package crawler

const (
	ctxProductIDKey       = "crawler_product_id"
	ctxProductPlatformKey = "crawler_product_platform"
	ctxProductURLKey      = "crawler_product_url"
	ctxRunContextKey      = "crawler_run_context"
	ctxHTTPStatusCodeKey  = "crawler_http_status"
	ctxProductErrorKey    = "crawler_product_error"
	ctxProductTitleKey    = "crawler_product_title"
	ctxInitialURLKey      = "crawler_initial_url"
	ctxFinalURLKey        = "crawler_final_url"
	ctxCanonicalURLKey    = "crawler_canonical_url"

	unknownProductID     = "UnknownProductID"
	htmlTitleTag         = "title"
	titleNotFoundMessage = "Title Not Found"
	unknownURLValue      = "UnknownURL"
	unknownPlatformValue = "UnknownPlatform"
	htmlExtension        = "html"

	retryCountKey  = "crawler_retry_count"
	retriedFlagKey = "crawler_retried"
)
