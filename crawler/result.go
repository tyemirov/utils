package crawler

import "github.com/PuerkitoBio/goquery"

// Result represents the outcome of crawling a single target.
type Result struct {
	TargetID       string            `json:"targetId"`
	TargetURL      string            `json:"targetUrl"`
	FinalURL       string            `json:"finalUrl,omitempty"`
	CanonicalURL   string            `json:"canonicalUrl,omitempty"`
	Category       string            `json:"category"`
	Title          string            `json:"title,omitempty"`
	Success        bool              `json:"success"`
	ErrorMessage   string            `json:"errorMessage,omitempty"`
	HTTPStatusCode int               `json:"httpStatusCode,omitempty"`
	Findings       []Finding         `json:"findings,omitempty"`
	Document       *goquery.Document `json:"-"`
	ProxyURL       string            `json:"proxyUrl,omitempty"`
}

// Finding captures a single evaluation outcome from the Evaluator.
type Finding struct {
	ID          string `json:"id,omitempty"`
	Description string `json:"description"`
	Passed      bool   `json:"passed"`
	Message     string `json:"message"`
	Data        string `json:"data,omitempty"`
}

// Evaluation is the output of an Evaluator.
type Evaluation struct {
	Findings []Finding
}
