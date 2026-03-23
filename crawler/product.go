package crawler

import "fmt"

// Product describes a single page to crawl.
type Product struct {
	ID          string
	Platform    string
	URL         string
	OriginalID  string
	OriginalURL string
}

// NewProduct constructs a Product after validating mandatory fields.
func NewProduct(id, platform, url string, opts ...ProductOption) (Product, error) {
	if id == "" {
		return Product{}, fmt.Errorf("crawler: product id is required")
	}
	if platform == "" {
		return Product{}, fmt.Errorf("crawler: product platform is required")
	}
	if url == "" {
		return Product{}, fmt.Errorf("crawler: product url is required")
	}

	product := Product{
		ID:       id,
		Platform: platform,
		URL:      url,
	}
	for _, opt := range opts {
		opt(&product)
	}
	return product, nil
}

// ProductOption mutates optional fields on Product construction.
type ProductOption func(*Product)

// WithOriginalID sets the original identifier when different from ID.
func WithOriginalID(originalID string) ProductOption {
	return func(product *Product) {
		product.OriginalID = originalID
	}
}

// WithOriginalURL records the source URL before redirects.
func WithOriginalURL(originalURL string) ProductOption {
	return func(product *Product) {
		product.OriginalURL = originalURL
	}
}
