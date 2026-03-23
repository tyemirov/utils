package crawler

import "fmt"

// Target describes a single URL to crawl.
type Target struct {
	ID       string
	Category string
	URL      string
	Metadata map[string]string
}

// NewTarget constructs a Target after validating mandatory fields.
func NewTarget(id, category, url string, opts ...TargetOption) (Target, error) {
	if id == "" {
		return Target{}, fmt.Errorf("crawler: target id is required")
	}
	if category == "" {
		return Target{}, fmt.Errorf("crawler: target category is required")
	}
	if url == "" {
		return Target{}, fmt.Errorf("crawler: target url is required")
	}
	t := Target{ID: id, Category: category, URL: url}
	for _, opt := range opts {
		opt(&t)
	}
	return t, nil
}

// TargetOption mutates optional fields on Target construction.
type TargetOption func(*Target)

// WithMetadata sets an extensible key-value pair on the target.
func WithMetadata(key, value string) TargetOption {
	return func(t *Target) {
		if t.Metadata == nil {
			t.Metadata = make(map[string]string)
		}
		t.Metadata[key] = value
	}
}

// MetadataValue returns a metadata value by key, or empty string if absent.
func (t Target) MetadataValue(key string) string {
	if t.Metadata == nil {
		return ""
	}
	return t.Metadata[key]
}

// Page is a deprecated alias for Target.
//
// Deprecated: Use Target instead.
type Page = Target
