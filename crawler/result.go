package crawler

import (
	"net/http"
)

// Result represents the normalized outcome of crawling a single product page.
type Result struct {
	ProductID               string       `json:"product_id" csv:"ID"`
	OriginalProductID       string       `json:"original_product_id,omitempty" csv:"OriginalID"`
	OriginalURL             string       `json:"original_url,omitempty" csv:""`
	FinalURL                string       `json:"final_url,omitempty" csv:""`
	CanonicalURL            string       `json:"canonical_url,omitempty" csv:""`
	ProxyURL                string       `json:"proxy_url,omitempty" csv:"ProxyURL"`
	ProductURL              string       `json:"product_url" csv:"URL"`
	ProductTitle            string       `json:"product_title,omitempty" csv:"Title"`
	ProductPlatform         string       `json:"product_platform"`
	Success                 bool         `json:"success"`
	ErrorMessage            string       `json:"error_message,omitempty" csv:"ErrorMessage"`
	HTTPStatusCode          int          `json:"http_status_code,omitempty" csv:"HTTPStatusCode"`
	Progress                int          `json:"progress,omitempty"`
	RuleResults             []RuleResult `json:"results,omitempty"`
	ConfiguredVerifierCount int          `json:"-" csv:"-"`
	ScoreOverride           *int         `json:"-" csv:"-"`
}

// IsNotFound reports whether the HTTP status code represents a missing page.
func (result Result) IsNotFound() bool {
	return result.HTTPStatusCode == http.StatusNotFound
}

// IsNotRetryable reports whether retrying would be pointless.
func (result Result) IsNotRetryable() bool {
	return result.IsNotFound() || result.Success
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
		packageLogger.Warning(verifierCountMismatchWarning, configuredVerifierCount, actualCount)
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
