package crawler

const (
	// Context keys for Colly request context.
	CtxTargetIDKey       = "crawler_target_id"
	CtxTargetCategoryKey = "crawler_target_category"
	CtxTargetURLKey      = "crawler_target_url"
	CtxRunContextKey     = "crawler_run_context"
	CtxHTTPStatusCodeKey = "crawler_http_status"
	CtxErrorKey          = "crawler_error"
	CtxEvaluationKey     = "crawler_evaluation"
	CtxTitleKey          = "crawler_title"
	CtxInitialURLKey     = "crawler_initial_url"
	CtxFinalURLKey       = "crawler_final_url"
	CtxCanonicalURLKey   = "crawler_canonical_url"
	CtxRedirectedKey     = "crawler_redirected"
	CtxNotFoundKey       = "crawler_not_found"

	RetryCountKey  = "crawler_retry_count"
	RetriedFlagKey = "crawler_retried"

	TitleNotFound = "Title Not Found"
	UnknownURL    = "UnknownURL"
	HTMLExtension = "html"
)
