// Package jseval provides headless browser page rendering via chromedp.
// It navigates to a URL using a real Chrome/Chromium instance, waits for
// JavaScript to execute, and returns the fully rendered HTML DOM.
//
// This complements the colly-based crawler package, which handles HTTP-only
// crawling. Use jseval for sites that require JavaScript execution (SPAs,
// Cloudflare challenges, etc.).
package jseval

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
)

// Config controls the headless browser behaviour.
type Config struct {
	// Timeout is the maximum time to wait for the page to render.
	// Default: 30 seconds.
	Timeout time.Duration

	// WaitSelector is an optional CSS selector to wait for before extracting HTML.
	// If empty, the renderer waits for Timeout or document idle, whichever comes first.
	WaitSelector string

	// ProxyURL is an optional HTTP/SOCKS proxy for the browser to use.
	ProxyURL string

	// UserAgent overrides the browser's default User-Agent string.
	UserAgent string
}

// Result holds the rendered page content.
type Result struct {
	// HTML is the fully rendered outer HTML of the page after JS execution.
	HTML string

	// Title is the document title after rendering.
	Title string

	// FinalURL is the URL after any redirects.
	FinalURL string
}

// RenderPage navigates to the given URL in a headless Chrome instance,
// waits for JavaScript to execute, and returns the rendered HTML.
func RenderPage(ctx context.Context, targetURL string, config Config) (*Result, error) {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	allocatorOptions := chromedp.DefaultExecAllocatorOptions[:]
	allocatorOptions = append(allocatorOptions,
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	if config.ProxyURL != "" {
		// Chrome's --proxy-server flag doesn't support inline auth (user:pass@host).
		// Strip auth from the proxy URL for the flag; chromedp will handle
		// proxy auth via the Fetch.AuthRequired CDP event.
		proxyServerURL := stripProxyAuth(config.ProxyURL)
		allocatorOptions = append(allocatorOptions,
			chromedp.ProxyServer(proxyServerURL),
		)
	}

	if config.UserAgent != "" {
		allocatorOptions = append(allocatorOptions,
			chromedp.UserAgent(config.UserAgent),
		)
	}

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, allocatorOptions...)
	defer cancelAllocator()

	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	// Set up proxy authentication if the proxy URL has credentials.
	if config.ProxyURL != "" {
		proxyUsername, proxyPassword := extractProxyCredentials(config.ProxyURL)
		if proxyUsername != "" {
			setupProxyAuth(browserCtx, proxyUsername, proxyPassword)

			// Enable Fetch domain to intercept auth challenges.
			if fetchEnableError := chromedp.Run(browserCtx, fetch.Enable().WithHandleAuthRequests(true)); fetchEnableError != nil {
				return nil, fmt.Errorf("jseval.RenderPage: enabling fetch for proxy auth: %w", fetchEnableError)
			}
		}
	}

	renderCtx, cancelRender := context.WithTimeout(browserCtx, config.Timeout)
	defer cancelRender()

	var renderedHTML string
	var documentTitle string
	var finalURL string

	actions := []chromedp.Action{
		chromedp.Navigate(targetURL),
	}

	if config.WaitSelector != "" {
		actions = append(actions, chromedp.WaitVisible(config.WaitSelector, chromedp.ByQuery))
	} else {
		actions = append(actions, chromedp.WaitReady("body", chromedp.ByQuery))
	}

	actions = append(actions,
		chromedp.OuterHTML("html", &renderedHTML),
		chromedp.Title(&documentTitle),
		chromedp.Location(&finalURL),
	)

	if runError := chromedp.Run(renderCtx, actions...); runError != nil {
		return nil, fmt.Errorf("jseval.RenderPage(%s): %w", targetURL, runError)
	}

	return &Result{
		HTML:     renderedHTML,
		Title:    documentTitle,
		FinalURL: finalURL,
	}, nil
}

// stripProxyAuth removes user:password from a proxy URL so it can be passed
// to Chrome's --proxy-server flag (which doesn't support inline auth).
func stripProxyAuth(rawProxyURL string) string {
	parsed, parseError := url.Parse(rawProxyURL)
	if parseError != nil {
		return rawProxyURL
	}

	parsed.User = nil

	return parsed.String()
}

// extractProxyCredentials returns the username and password from a proxy URL.
func extractProxyCredentials(rawProxyURL string) (string, string) {
	parsed, parseError := url.Parse(rawProxyURL)
	if parseError != nil || parsed.User == nil {
		return "", ""
	}

	password, _ := parsed.User.Password()

	return parsed.User.Username(), password
}

// setupProxyAuth configures CDP to respond to proxy authentication challenges.
func setupProxyAuth(ctx context.Context, username string, password string) {
	chromedp.ListenTarget(ctx, func(event interface{}) {
		if authRequired, isAuthRequired := event.(*fetch.EventAuthRequired); isAuthRequired {
			go func() {
				continueError := chromedp.Run(ctx,
					fetch.ContinueWithAuth(authRequired.RequestID, &fetch.AuthChallengeResponse{
						Response: fetch.AuthChallengeResponseResponseProvideCredentials,
						Username: username,
						Password: password,
					}),
				)
				_ = continueError
			}()
		}

		if requestPaused, isRequestPaused := event.(*fetch.EventRequestPaused); isRequestPaused {
			go func() {
				continueError := chromedp.Run(ctx, fetch.ContinueRequest(requestPaused.RequestID))
				_ = continueError
			}()
		}
	})
}

// RenderPages renders multiple URLs concurrently, each in its own browser tab.
// Returns results in the same order as the input URLs. Errors are per-URL.
func RenderPages(ctx context.Context, targetURLs []string, config Config) ([]*Result, []error) {
	results := make([]*Result, len(targetURLs))
	errors := make([]error, len(targetURLs))

	type indexedResult struct {
		index  int
		result *Result
		err    error
	}

	resultChannel := make(chan indexedResult, len(targetURLs))

	for urlIndex, targetURL := range targetURLs {
		go func(index int, url string) {
			result, renderError := RenderPage(ctx, url, config)
			resultChannel <- indexedResult{index: index, result: result, err: renderError}
		}(urlIndex, targetURL)
	}

	for range targetURLs {
		indexed := <-resultChannel
		results[indexed.index] = indexed.result
		errors[indexed.index] = indexed.err
	}

	return results, errors
}
