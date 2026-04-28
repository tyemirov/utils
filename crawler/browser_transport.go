package crawler

import (
	"context"

	"github.com/tyemirov/utils/browsertransport"
)

// DefaultBrowserStealthScript is injected into rendered pages unless a caller
// supplies a browser-specific script.
const DefaultBrowserStealthScript = browsertransport.DefaultStealthScript

// BrowserMode describes how a browser should reach the upstream network.
type BrowserMode = browsertransport.BrowserMode

const (
	// BrowserModeDirect uses Chrome's native proxy handling or a direct network
	// path.
	BrowserModeDirect = browsertransport.BrowserModeDirect
	// BrowserModeHTTPFetchAuth strips inline auth from an HTTP proxy URL and
	// supplies credentials through the Fetch domain.
	BrowserModeHTTPFetchAuth = browsertransport.BrowserModeHTTPFetchAuth
	// BrowserModeSOCKSForwarder bridges an authenticated SOCKS proxy through a
	// local unauthenticated forwarder Chrome can consume.
	BrowserModeSOCKSForwarder = browsertransport.BrowserModeSOCKSForwarder
)

// BrowserProfile describes how to launch a browser transport.
type BrowserProfile = browsertransport.BrowserProfile

// BrowserLaunchOptions controls browser process launch behavior.
type BrowserLaunchOptions = browsertransport.LaunchOptions

// BrowserTabOptions controls one render tab opened on an existing browser
// session.
type BrowserTabOptions = browsertransport.TabOptions

// BrowserPageRequest describes a generic "navigate, wait, capture" render.
type BrowserPageRequest = browsertransport.PageRequest

// BrowserConfig keeps the historical one-shot render surface used by jseval
// callers.
type BrowserConfig = browsertransport.Config

// BrowserResult holds the rendered page content.
type BrowserResult = browsertransport.Result

// BrowserSession owns a browser instance bound to one browser transport
// profile.
type BrowserSession = browsertransport.Session

// InferBrowserProfile derives a browser profile from a raw proxy URL.
func InferBrowserProfile(rawProxyURL string, ignoreCertErrors bool) (BrowserProfile, error) {
	return browsertransport.InferBrowserProfile(rawProxyURL, ignoreCertErrors)
}

// NewBrowserSession launches a reusable browser session for the given profile.
func NewBrowserSession(ctx context.Context, browserProfile BrowserProfile, launchOptions BrowserLaunchOptions) (*BrowserSession, error) {
	return browsertransport.NewSession(ctx, browserProfile, launchOptions)
}

// RenderBrowserPage launches a one-shot browser session and captures the
// rendered page.
func RenderBrowserPage(ctx context.Context, targetURL string, config BrowserConfig) (*BrowserResult, error) {
	return browsertransport.RenderPage(ctx, targetURL, config)
}

// RenderBrowserPages renders multiple URLs concurrently and returns results in
// input order.
func RenderBrowserPages(ctx context.Context, targetURLs []string, config BrowserConfig) ([]*BrowserResult, []error) {
	return browsertransport.RenderPages(ctx, targetURLs, config)
}

// DetectBrowserChromeVersion returns the major version of the Chrome binary at
// execPath, or tries common platform paths when execPath is empty.
func DetectBrowserChromeVersion(execPath string) string {
	return browsertransport.DetectChromeVersion(execPath)
}

// DefaultBrowserUserAgent returns a realistic Chrome User-Agent string whose
// major version matches the installed Chrome binary when it can be detected.
func DefaultBrowserUserAgent(execPath string) string {
	return browsertransport.DefaultUserAgent(execPath)
}
