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
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/proxy"
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

	// IgnoreCertErrors makes the browser accept invalid/self-signed certificates.
	// Required for some residential proxies (e.g. Bright Data) that do TLS interception.
	IgnoreCertErrors bool
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

	var localForwarder *socksForwarder
	if config.ProxyURL != "" {
		if isSOCKSProxy(config.ProxyURL) {
			// Chrome doesn't support SOCKS5 with username/password auth.
			// Start a local unauthenticated SOCKS5 listener that forwards
			// connections to the real authenticated upstream proxy.
			// Chrome connects to localhost; the forwarder handles the auth
			// handshake with the upstream proxy transparently.
			forwarder, forwarderError := newSOCKSForwarder(config.ProxyURL)
			if forwarderError != nil {
				return nil, fmt.Errorf("jseval.RenderPage: starting SOCKS forwarder: %w", forwarderError)
			}
			localForwarder = forwarder
			allocatorOptions = append(allocatorOptions,
				chromedp.ProxyServer(fmt.Sprintf("socks5://%s", forwarder.addr)),
			)
		} else {
			// HTTP proxies: strip auth from URL for --proxy-server flag;
			// the Fetch domain handles 407 auth challenges below.
			proxyServerURL := stripProxyAuth(config.ProxyURL)
			allocatorOptions = append(allocatorOptions,
				chromedp.ProxyServer(proxyServerURL),
			)
		}
	}

	if config.IgnoreCertErrors {
		allocatorOptions = append(allocatorOptions,
			chromedp.Flag("ignore-certificate-errors", true),
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

	// Set up Fetch-based proxy auth for HTTP proxies only.
	// SOCKS5 auth is handled by the local forwarder above.
	if config.ProxyURL != "" && !isSOCKSProxy(config.ProxyURL) {
		proxyUsername, proxyPassword := extractProxyCredentials(config.ProxyURL)
		if proxyUsername != "" {
			setupProxyAuth(browserCtx, proxyUsername, proxyPassword)

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

	runError := chromedp.Run(renderCtx, actions...)

	// Stop the local forwarder after Chrome is done, regardless of error.
	if localForwarder != nil {
		localForwarder.close()
	}

	if runError != nil {
		return nil, fmt.Errorf("jseval.RenderPage(%s): %w", targetURL, runError)
	}

	return &Result{
		HTML:     renderedHTML,
		Title:    documentTitle,
		FinalURL: finalURL,
	}, nil
}

// socksForwarder is a local unauthenticated SOCKS5 listener that forwards
// connections to an authenticated upstream SOCKS5 proxy. This works around
// Chrome's inability to handle SOCKS5 username/password auth.
type socksForwarder struct {
	listener net.Listener
	addr     string
	dialer   proxy.Dialer
}

// newSOCKSForwarder starts a local TCP listener that accepts unauthenticated
// SOCKS5 connections and forwards them through the authenticated upstream proxy.
func newSOCKSForwarder(upstreamProxyURL string) (*socksForwarder, error) {
	parsed, parseError := url.Parse(upstreamProxyURL)
	if parseError != nil {
		return nil, fmt.Errorf("parsing proxy URL: %w", parseError)
	}

	var auth *proxy.Auth
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		auth = &proxy.Auth{
			User:     parsed.User.Username(),
			Password: password,
		}
	}

	upstreamDialer, dialerError := proxy.SOCKS5("tcp", parsed.Host, auth, proxy.Direct)
	if dialerError != nil {
		return nil, fmt.Errorf("creating upstream SOCKS5 dialer: %w", dialerError)
	}

	listener, listenError := net.Listen("tcp", "127.0.0.1:0")
	if listenError != nil {
		return nil, fmt.Errorf("listening on localhost: %w", listenError)
	}

	forwarder := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   upstreamDialer,
	}

	go forwarder.acceptLoop()

	return forwarder, nil
}

func (forwarder *socksForwarder) close() {
	forwarder.listener.Close()
}

func (forwarder *socksForwarder) acceptLoop() {
	for {
		clientConn, acceptError := forwarder.listener.Accept()
		if acceptError != nil {
			return // listener closed
		}
		go forwarder.handleConnection(clientConn)
	}
}

func (forwarder *socksForwarder) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	// SOCKS5 handshake: read version + auth methods
	header := make([]byte, 2)
	if _, readError := io.ReadFull(clientConn, header); readError != nil {
		return
	}
	if header[0] != 0x05 {
		return
	}

	methods := make([]byte, header[1])
	if _, readError := io.ReadFull(clientConn, methods); readError != nil {
		return
	}

	// Reply: no auth required (we're local, Chrome doesn't send auth)
	clientConn.Write([]byte{0x05, 0x00})

	// Read CONNECT request: version + cmd + rsv + atyp
	request := make([]byte, 4)
	if _, readError := io.ReadFull(clientConn, request); readError != nil {
		return
	}
	if request[1] != 0x01 { // only CONNECT
		return
	}

	var targetHost string
	switch request[3] {
	case 0x01: // IPv4
		ipBytes := make([]byte, 4)
		io.ReadFull(clientConn, ipBytes)
		targetHost = net.IP(ipBytes).String()
	case 0x03: // Domain name
		lengthByte := make([]byte, 1)
		io.ReadFull(clientConn, lengthByte)
		domainBytes := make([]byte, lengthByte[0])
		io.ReadFull(clientConn, domainBytes)
		targetHost = string(domainBytes)
	case 0x04: // IPv6
		ipBytes := make([]byte, 16)
		io.ReadFull(clientConn, ipBytes)
		targetHost = net.IP(ipBytes).String()
	default:
		return
	}

	portBytes := make([]byte, 2)
	io.ReadFull(clientConn, portBytes)
	targetPort := int(portBytes[0])<<8 | int(portBytes[1])

	targetAddr := fmt.Sprintf("%s:%d", targetHost, targetPort)

	// Dial the real target through the authenticated upstream SOCKS5 proxy
	upstreamConn, dialError := forwarder.dialer.Dial("tcp", targetAddr)
	if dialError != nil {
		// SOCKS5 failure reply
		clientConn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstreamConn.Close()

	// SOCKS5 success reply
	clientConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// Bidirectional copy
	done := make(chan struct{}, 2)
	go func() { io.Copy(upstreamConn, clientConn); done <- struct{}{} }()
	go func() { io.Copy(clientConn, upstreamConn); done <- struct{}{} }()
	<-done
}

// isSOCKSProxy returns true if the proxy URL uses a SOCKS protocol.
func isSOCKSProxy(rawProxyURL string) bool {
	parsed, parseError := url.Parse(rawProxyURL)
	if parseError != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return scheme == "socks4" || scheme == "socks5" || scheme == "socks5h"
}

// stripProxyAuth removes user:password from a proxy URL so it can be passed
// to Chrome's --proxy-server flag (which doesn't support inline auth for HTTP proxies).
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

// setupProxyAuth configures CDP to respond to HTTP proxy authentication challenges.
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
