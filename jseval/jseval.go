// Package jseval provides a compatibility wrapper around the shared browser
// transport runtime.
package jseval

import (
	"context"

	"github.com/tyemirov/utils/browsertransport"
)

type Config = browsertransport.Config
type Result = browsertransport.Result

var renderPage = browsertransport.RenderPage
var renderPages = browsertransport.RenderPages

// RenderPage renders one JavaScript-heavy page in a headless browser.
func RenderPage(ctx context.Context, targetURL string, config Config) (*Result, error) {
	return renderPage(ctx, targetURL, config)
}

// RenderPages renders multiple pages concurrently and returns results in input order.
func RenderPages(ctx context.Context, targetURLs []string, config Config) ([]*Result, []error) {
	return renderPages(ctx, targetURLs, config)
}
