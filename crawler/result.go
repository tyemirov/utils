package crawler

import (
	"fmt"
	"net/http"
	"strings"
)

// ImageStatus captures product image lifecycle state for a crawl result.
type ImageStatus string

const (
	ImageStatusPending ImageStatus = "pending"
	ImageStatusReady   ImageStatus = "ready"
	ImageStatusFailed  ImageStatus = "failed"
)

// DiscoverabilityStatus captures product discoverability outcome from search results.
type DiscoverabilityStatus string

const (
	DiscoverabilityStatusFirstOrganic    DiscoverabilityStatus = "first_organic"
	DiscoverabilityStatusOrganicNotFirst DiscoverabilityStatus = "organic_not_first"
	DiscoverabilityStatusNotFound        DiscoverabilityStatus = "not_found"
	DiscoverabilityStatusSponsoredOnly   DiscoverabilityStatus = "sponsored_only"
	DiscoverabilityStatusBlocked         DiscoverabilityStatus = "blocked"
)

// Discoverability captures deterministic search-result discoverability metadata.
type Discoverability struct {
	Status                     DiscoverabilityStatus
	TargetOrganicRank          int
	FirstOrganicASIN           string
	SponsoredBeforeTargetCount int
	SearchURL                  string
}

// ProductImageStatusUpdate describes an image status transition for a product identifier.
type ProductImageStatusUpdate struct {
	ProductID string
	Status    ImageStatus
}

// NormalizeImageStatus converts arbitrary status text into a known value.
func NormalizeImageStatus(raw string) ImageStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ImageStatusPending):
		return ImageStatusPending
	case string(ImageStatusReady):
		return ImageStatusReady
	case string(ImageStatusFailed):
		return ImageStatusFailed
	default:
		return ""
	}
}

// NormalizeDiscoverabilityStatus converts arbitrary status text into a known value.
func NormalizeDiscoverabilityStatus(raw string) DiscoverabilityStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(DiscoverabilityStatusFirstOrganic):
		return DiscoverabilityStatusFirstOrganic
	case string(DiscoverabilityStatusOrganicNotFirst):
		return DiscoverabilityStatusOrganicNotFirst
	case string(DiscoverabilityStatusNotFound):
		return DiscoverabilityStatusNotFound
	case string(DiscoverabilityStatusSponsoredOnly):
		return DiscoverabilityStatusSponsoredOnly
	case string(DiscoverabilityStatusBlocked):
		return DiscoverabilityStatusBlocked
	default:
		return ""
	}
}

// ResolveImageStatus derives a stable image status when persistence records are missing legacy values.
func ResolveImageStatus(status ImageStatus, productImageURL string) ImageStatus {
	normalizedStatus := NormalizeImageStatus(string(status))
	if normalizedStatus != "" {
		return normalizedStatus
	}
	if strings.TrimSpace(productImageURL) == "" {
		return ImageStatusFailed
	}
	return ImageStatusReady
}

// Result represents the normalized outcome of crawling a single product page.
type Result struct {
	ProductID                  string                `json:"product_id" csv:"ID"`
	OriginalProductID          string                `json:"original_product_id,omitempty" csv:"OriginalID"`
	OriginalURL                string                `json:"original_url,omitempty" csv:""`
	FinalURL                   string                `json:"final_url,omitempty" csv:""`
	CanonicalURL               string                `json:"canonical_url,omitempty" csv:""`
	ProxyURL                   string                `json:"proxy_url,omitempty" csv:"ProxyURL"`
	ProductImageURL            string                `json:"product_image_url,omitempty"`
	ImageStatus                ImageStatus           `json:"product_image_status,omitempty"`
	DiscoverabilityStatus      DiscoverabilityStatus `json:"discoverability_status"`
	TargetOrganicRank          int                   `json:"target_organic_rank"`
	FirstOrganicASIN           string                `json:"first_organic_asin"`
	SponsoredBeforeTargetCount int                   `json:"sponsored_before_target_count"`
	DiscoverabilitySearchURL   string                `json:"discoverability_search_url"`
	ProductURL                 string                `json:"product_url" csv:"URL"`
	ProductTitle               string                `json:"product_title,omitempty" csv:"Title"`
	ProductPlatform            string                `json:"product_platform"`
	Success                    bool                  `json:"success"`
	ErrorMessage               string                `json:"error_message,omitempty" csv:"ErrorMessage"`
	HTTPStatusCode             int                   `json:"http_status_code,omitempty" csv:"HTTPStatusCode"`
	DownloadLink               string                `json:"download_link,omitempty"`
	Progress                   int                   `json:"progress,omitempty"`
	RuleResults                []RuleResult          `json:"results,omitempty"`
	ConfiguredVerifierCount    int                   `json:"-" csv:"-"`
	ScoreOverride              *int                  `json:"-" csv:"-"`
}

// IsNotFound reports whether the HTTP status code represents a missing page.
func (result Result) IsNotFound() bool {
	return result.HTTPStatusCode == http.StatusNotFound
}

// IsNotRetryable reports whether retrying would be pointless.
func (result Result) IsNotRetryable() bool {
	return result.IsNotFound() || result.Success
}

// ResolvedImageStatus returns a normalized image status value.
func (result Result) ResolvedImageStatus() ImageStatus {
	return ResolveImageStatus(result.ImageStatus, result.ProductImageURL)
}

const verifierCountMismatchWarning = "verifier results count mismatch: configured %d, got %d"

// CalculateScore returns the percentage of configured verifiers that passed.
func (result Result) CalculateScore(configuredVerifierCount int) int {
	if result.ScoreOverride != nil {
		score := *result.ScoreOverride
		if score >= 0 && score <= 100 {
			return score
		}
	}
	if configuredVerifierCount <= 0 {
		configuredVerifierCount = result.ConfiguredVerifierCount
	}
	if configuredVerifierCount <= 0 {
		return 0
	}
	if !result.Success {
		return 0
	}
	actualCount := 0
	passedCount := 0
	for _, rule := range result.RuleResults {
		actualCount += len(rule.VerificationResults)
		for _, verification := range rule.VerificationResults {
			if verification.Passed {
				passedCount++
			}
		}
	}
	if actualCount != configuredVerifierCount {
		packageLogger.Warning(fmt.Sprintf(verifierCountMismatchWarning, configuredVerifierCount, actualCount))
		return 0
	}
	return passedCount * 100 / configuredVerifierCount
}

// RuleResult represents rule-level evaluation outcome.
type RuleResult struct {
	ID                  string               `json:"id,omitempty" csv:"-"`
	Description         string               `json:"description" csv:"Description,keyValue"`
	Passed              bool                 `json:"passed" csv:"passed"`
	ReportingOrder      int                  `json:"reporting_order" csv:"-"`
	Message             string               `json:"message" csv:"message"`
	VerificationResults []VerificationResult `json:"verification_results"`
}

// VerificationResult captures the outcome of an individual verifier.
type VerificationResult struct {
	ID             string `json:"id,omitempty" csv:"-"`
	Description    string `json:"description" csv:"Description,keyValue"`
	Passed         bool   `json:"passed" csv:"passed"`
	Message        string `json:"message" csv:"message"`
	Value          string `json:"value" csv:"value"`
	ReportingOrder int    `json:"reporting_order"`
	IncludeValue   bool   `json:"-"`
}

// RuleEvaluation aggregates evaluation output from the injected RuleEvaluator.
type RuleEvaluation struct {
	Passed             bool
	ConfiguredVerifier int
	RuleResults        []RuleResult
}
