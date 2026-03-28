// Package browsertransport provides reusable browser and HTTP transport helpers
// for proxy-aware scraping runtimes.
package browsertransport

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/proxy"
)

// DefaultStealthScript removes common browser automation markers before page
// JavaScript runs.
const DefaultStealthScript = `
		Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
		Object.defineProperty(navigator, 'plugins', { get: () => [1, 2, 3, 4, 5] });
		Object.defineProperty(navigator, 'languages', { get: () => ['en-US', 'en'] });
		window.chrome = { runtime: {} };
		const originalQuery = window.navigator.permissions.query;
		window.navigator.permissions.query = (parameters) => (
			parameters.name === 'notifications' ?
				Promise.resolve({ state: Notification.permission }) :
				originalQuery(parameters)
		);
`

const defaultRenderTimeout = 30 * time.Second
const defaultHTTPTimeout = 15 * time.Second

// BrowserMode describes how a browser should reach the upstream network.
type BrowserMode string

const (
	// BrowserModeDirect uses Chrome's native proxy handling or a direct network path.
	BrowserModeDirect BrowserMode = "direct"
	// BrowserModeHTTPFetchAuth strips inline auth from an HTTP proxy URL and
	// supplies credentials through the Fetch domain.
	BrowserModeHTTPFetchAuth BrowserMode = "http_fetch_auth"
	// BrowserModeSOCKSForwarder bridges an authenticated SOCKS proxy through a
	// local unauthenticated forwarder Chrome can consume.
	BrowserModeSOCKSForwarder BrowserMode = "socks_forwarder"
)

// BrowserProfile describes how to launch a browser transport.
type BrowserProfile struct {
	ID               string
	Provider         string
	URL              string
	Mode             BrowserMode
	IgnoreCertErrors bool
}

// HTTPProfile describes how to build an HTTP client transport.
type HTTPProfile struct {
	ID               string
	Provider         string
	URL              string
	IgnoreCertErrors bool
}

// LaunchOptions controls browser process launch behavior.
type LaunchOptions struct {
	ExecPath                   string
	UserAgent                  string
	AdditionalAllocatorOptions []chromedp.ExecAllocatorOption
}

// TabOptions controls one render tab opened on an existing browser session.
type TabOptions struct {
	Timeout        time.Duration
	PreludeActions []chromedp.Action
}

// PageRequest describes a generic "navigate, wait, capture" render.
type PageRequest struct {
	TargetURL     string
	Timeout       time.Duration
	WaitSelector  string
	StealthScript string
}

// Config keeps the historical one-shot render surface used by jseval callers.
type Config struct {
	Timeout                    time.Duration
	WaitSelector               string
	ProxyURL                   string
	UserAgent                  string
	IgnoreCertErrors           bool
	ExecPath                   string
	StealthScript              string
	AdditionalAllocatorOptions []chromedp.ExecAllocatorOption
}

// Result holds the rendered page content.
type Result struct {
	HTML     string
	Title    string
	FinalURL string
}

// Session owns a browser instance bound to one browser transport profile.
type Session struct {
	mu            sync.Mutex
	allocCancel   context.CancelFunc
	browserCtx    context.Context
	browserCancel context.CancelFunc
	forwarder     *socksForwarder
	profile       BrowserProfile
	closed        bool
}

var (
	netListen                = net.Listen
	proxySocks               = proxy.SOCKS5
	proxyFromURL             = proxy.FromURL
	urlParse                 = url.Parse
	chromedpRunner           = chromedp.Run
	chromedpNewExecAllocator = chromedp.NewExecAllocator
	chromedpNewContext       = chromedp.NewContext
	chromedpListenTarget     = chromedp.ListenTarget
	cookieJarNew             = cookiejar.New
	setupProxyAuthFn         = setupProxyAuth
)

// InferBrowserProfile derives a browser profile from a raw proxy URL.
func InferBrowserProfile(rawProxyURL string, ignoreCertErrors bool) (BrowserProfile, error) {
	trimmedProxyURL := strings.TrimSpace(rawProxyURL)
	if trimmedProxyURL == "" {
		return BrowserProfile{
			ID:               "direct",
			Provider:         "direct",
			Mode:             BrowserModeDirect,
			IgnoreCertErrors: ignoreCertErrors,
		}, nil
	}

	parsedProxyURL, parseError := url.Parse(trimmedProxyURL)
	if parseError != nil {
		return BrowserProfile{}, fmt.Errorf("parsing browser proxy URL: %w", parseError)
	}
	if parsedProxyURL.Scheme == "" || parsedProxyURL.Host == "" {
		return BrowserProfile{}, fmt.Errorf("invalid browser proxy URL %q", trimmedProxyURL)
	}

	profileMode := BrowserModeDirect
	switch {
	case isSOCKSProxy(trimmedProxyURL):
		profileMode = BrowserModeSOCKSForwarder
	case parsedProxyURL.User != nil:
		profileMode = BrowserModeHTTPFetchAuth
	}

	return BrowserProfile{
		ID:               inferredTransportID(trimmedProxyURL),
		Provider:         inferredProvider(trimmedProxyURL),
		URL:              trimmedProxyURL,
		Mode:             profileMode,
		IgnoreCertErrors: ignoreCertErrors,
	}, nil
}

// InferHTTPProfile derives an HTTP transport profile from a raw proxy URL.
func InferHTTPProfile(rawProxyURL string, ignoreCertErrors bool) (HTTPProfile, error) {
	trimmedProxyURL := strings.TrimSpace(rawProxyURL)
	if trimmedProxyURL == "" {
		return HTTPProfile{
			ID:               "direct",
			Provider:         "direct",
			IgnoreCertErrors: ignoreCertErrors,
		}, nil
	}

	parsedProxyURL, parseError := url.Parse(trimmedProxyURL)
	if parseError != nil {
		return HTTPProfile{}, fmt.Errorf("parsing HTTP proxy URL: %w", parseError)
	}
	if parsedProxyURL.Scheme == "" || parsedProxyURL.Host == "" {
		return HTTPProfile{}, fmt.Errorf("invalid HTTP proxy URL %q", trimmedProxyURL)
	}

	return HTTPProfile{
		ID:               inferredTransportID(trimmedProxyURL),
		Provider:         inferredProvider(trimmedProxyURL),
		URL:              trimmedProxyURL,
		IgnoreCertErrors: ignoreCertErrors,
	}, nil
}

// NewSession launches a reusable browser session for the given profile.
func NewSession(ctx context.Context, browserProfile BrowserProfile, launchOptions LaunchOptions) (*Session, error) {
	normalizedProfile, normalizeError := normalizeBrowserProfile(browserProfile)
	if normalizeError != nil {
		return nil, normalizeError
	}

	allocatorOptions := chromedp.DefaultExecAllocatorOptions[:]
	allocatorOptions = append(allocatorOptions,
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("exclude-switches", "enable-automation"),
		chromedp.Flag("disable-infobars", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-popup-blocking", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.WindowSize(1920, 1080),
		chromedp.Flag("lang", "en-US,en"),
	)

	if launchOptions.ExecPath != "" {
		allocatorOptions = append(allocatorOptions, chromedp.ExecPath(launchOptions.ExecPath))
	}
	if normalizedProfile.IgnoreCertErrors {
		allocatorOptions = append(allocatorOptions, chromedp.Flag("ignore-certificate-errors", true))
	}
	if launchOptions.UserAgent != "" {
		allocatorOptions = append(allocatorOptions, chromedp.UserAgent(launchOptions.UserAgent))
	}
	allocatorOptions = append(allocatorOptions, launchOptions.AdditionalAllocatorOptions...)

	var localForwarder *socksForwarder
	switch normalizedProfile.Mode {
	case BrowserModeDirect:
		if normalizedProfile.URL != "" {
			allocatorOptions = append(allocatorOptions, chromedp.ProxyServer(normalizedProfile.URL))
		}
	case BrowserModeHTTPFetchAuth:
		allocatorOptions = append(allocatorOptions, chromedp.ProxyServer(stripProxyAuth(normalizedProfile.URL)))
	case BrowserModeSOCKSForwarder:
		forwarder, forwarderError := newSOCKSForwarder(normalizedProfile.URL)
		if forwarderError != nil {
			return nil, fmt.Errorf("starting SOCKS forwarder: %w", forwarderError)
		}
		localForwarder = forwarder
		allocatorOptions = append(allocatorOptions, chromedp.ProxyServer(fmt.Sprintf("socks5://%s", forwarder.addr)))
	}

	allocatorCtx, cancelAllocator := chromedpNewExecAllocator(ctx, allocatorOptions...)
	browserCtx, cancelBrowser := chromedpNewContext(allocatorCtx)
	if browserStartError := chromedpRunner(browserCtx); browserStartError != nil {
		cancelBrowser()
		cancelAllocator()
		if localForwarder != nil {
			localForwarder.close()
		}
		return nil, browserStartError
	}

	return &Session{
		allocCancel:   cancelAllocator,
		browserCtx:    browserCtx,
		browserCancel: cancelBrowser,
		forwarder:     localForwarder,
		profile:       normalizedProfile,
	}, nil
}

// WithTab opens a fresh render tab on the session, applies proxy auth and
// optional preparation actions, then runs the caller callback.
func (session *Session) WithTab(ctx context.Context, tabOptions TabOptions, run func(context.Context) error) error {
	if run == nil {
		return fmt.Errorf("browser tab run callback is required")
	}

	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return fmt.Errorf("browser session is closed")
	}
	browserCtx := session.browserCtx
	browserProfile := session.profile
	session.mu.Unlock()

	tabCtx, cancelTab := chromedpNewContext(browserCtx)
	defer cancelTab()

	timeout := tabOptions.Timeout
	if timeout <= 0 {
		timeout = defaultRenderTimeout
	}
	tabRunCtx, cancelTabRun := context.WithTimeout(tabCtx, timeout)
	defer cancelTabRun()

	cancelDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cancelTabRun()
		case <-cancelDone:
		}
	}()
	defer close(cancelDone)

	if tabInitError := chromedpRunner(tabRunCtx); tabInitError != nil {
		return fmt.Errorf("initializing browser tab: %w", tabInitError)
	}

	renderCtx, cancelRender := context.WithTimeout(tabRunCtx, timeout)
	defer cancelRender()

	if proxyAuthError := enableHTTPProxyAuth(renderCtx, browserProfile); proxyAuthError != nil {
		return proxyAuthError
	}

	if len(tabOptions.PreludeActions) > 0 {
		if prepareError := chromedpRunner(renderCtx, tabOptions.PreludeActions...); prepareError != nil {
			return fmt.Errorf("preparing browser tab: %w", prepareError)
		}
	}

	return run(renderCtx)
}

// RenderPage renders a page through an existing session.
func (session *Session) RenderPage(ctx context.Context, pageRequest PageRequest) (*Result, error) {
	stealthScript := strings.TrimSpace(pageRequest.StealthScript)
	if stealthScript == "" {
		stealthScript = DefaultStealthScript
	}

	result := &Result{}
	preludeActions := []chromedp.Action{
		chromedp.ActionFunc(func(runContext context.Context) error {
			_, addScriptError := page.AddScriptToEvaluateOnNewDocument(stealthScript).Do(runContext)
			return addScriptError
		}),
	}

	renderError := session.WithTab(ctx, TabOptions{
		Timeout:        pageRequest.Timeout,
		PreludeActions: preludeActions,
	}, func(runContext context.Context) error {
		actions := []chromedp.Action{
			chromedp.Navigate(pageRequest.TargetURL),
		}
		if pageRequest.WaitSelector != "" {
			actions = append(actions, chromedp.WaitVisible(pageRequest.WaitSelector, chromedp.ByQuery))
		} else {
			actions = append(actions, chromedp.WaitReady("body", chromedp.ByQuery))
		}
		actions = append(actions,
			chromedp.OuterHTML("html", &result.HTML),
			chromedp.Title(&result.Title),
			chromedp.Location(&result.FinalURL),
		)
		return chromedpRunner(runContext, actions...)
	})
	if renderError != nil {
		return nil, renderError
	}

	return result, nil
}

// Close releases the browser process and any local proxy forwarder.
func (session *Session) Close() {
	if session == nil {
		return
	}

	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return
	}
	session.closed = true
	forwarder := session.forwarder
	browserCancel := session.browserCancel
	allocCancel := session.allocCancel
	session.forwarder = nil
	session.browserCancel = nil
	session.allocCancel = nil
	session.browserCtx = nil
	session.mu.Unlock()

	if browserCancel != nil {
		browserCancel()
	}
	if allocCancel != nil {
		allocCancel()
	}
	if forwarder != nil {
		forwarder.close()
	}
}

// RenderPage launches a one-shot browser session and captures the rendered page.
func RenderPage(ctx context.Context, targetURL string, config Config) (*Result, error) {
	browserProfile, profileError := InferBrowserProfile(config.ProxyURL, config.IgnoreCertErrors)
	if profileError != nil {
		return nil, profileError
	}

	session, sessionError := NewSession(ctx, browserProfile, LaunchOptions{
		ExecPath:                   config.ExecPath,
		UserAgent:                  config.UserAgent,
		AdditionalAllocatorOptions: config.AdditionalAllocatorOptions,
	})
	if sessionError != nil {
		return nil, sessionError
	}
	defer session.Close()

	return session.RenderPage(ctx, PageRequest{
		TargetURL:     targetURL,
		Timeout:       config.Timeout,
		WaitSelector:  config.WaitSelector,
		StealthScript: config.StealthScript,
	})
}

// RenderPages renders multiple URLs concurrently and returns results in input order.
func RenderPages(ctx context.Context, targetURLs []string, config Config) ([]*Result, []error) {
	results := make([]*Result, len(targetURLs))
	errorsByIndex := make([]error, len(targetURLs))

	type indexedResult struct {
		index  int
		result *Result
		err    error
	}

	resultChannel := make(chan indexedResult, len(targetURLs))
	for targetIndex, targetURL := range targetURLs {
		go func(index int, url string) {
			result, renderError := RenderPage(ctx, url, config)
			resultChannel <- indexedResult{index: index, result: result, err: renderError}
		}(targetIndex, targetURL)
	}

	for range targetURLs {
		indexed := <-resultChannel
		results[indexed.index] = indexed.result
		errorsByIndex[indexed.index] = indexed.err
	}

	return results, errorsByIndex
}

// NewHTTPClient builds an HTTP client bound to one transport profile.
func NewHTTPClient(httpProfile HTTPProfile, timeout time.Duration) (*http.Client, error) {
	normalizedProfile, normalizeError := normalizeHTTPProfile(httpProfile)
	if normalizeError != nil {
		return nil, normalizeError
	}

	jar, jarError := cookieJarNew(nil)
	if jarError != nil {
		return nil, fmt.Errorf("creating cookie jar: %w", jarError)
	}

	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		ForceAttemptHTTP2: true,
	}

	if normalizedProfile.URL != "" {
		parsedProxyURL, _ := url.Parse(normalizedProfile.URL)
		if isSOCKSProxy(normalizedProfile.URL) {
			dialer, dialerError := proxyFromURL(parsedProxyURL, proxy.Direct)
			if dialerError != nil {
				return nil, fmt.Errorf("creating SOCKS dialer: %w", dialerError)
			}
			if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
				transport.DialContext = contextDialer.DialContext
			} else {
				transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
					return dialer.Dial(network, address)
				}
			}
			transport.Proxy = nil
		} else {
			transport.Proxy = http.ProxyURL(parsedProxyURL)
		}
	}
	if normalizedProfile.IgnoreCertErrors {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   httpTimeoutOrDefault(timeout),
	}, nil
}

func normalizeBrowserProfile(browserProfile BrowserProfile) (BrowserProfile, error) {
	trimmedProxyURL := strings.TrimSpace(browserProfile.URL)
	if browserProfile.Mode == "" {
		inferredProfile, inferError := InferBrowserProfile(trimmedProxyURL, browserProfile.IgnoreCertErrors)
		if inferError != nil {
			return BrowserProfile{}, inferError
		}
		if browserProfile.ID != "" {
			inferredProfile.ID = browserProfile.ID
		}
		if browserProfile.Provider != "" {
			inferredProfile.Provider = browserProfile.Provider
		}
		return inferredProfile, nil
	}

	if browserProfile.ID == "" {
		browserProfile.ID = inferredTransportID(trimmedProxyURL)
	}
	if browserProfile.Provider == "" {
		browserProfile.Provider = inferredProvider(trimmedProxyURL)
	}
	browserProfile.URL = trimmedProxyURL

	if trimmedProxyURL != "" {
		parsedProxyURL, parseError := url.Parse(trimmedProxyURL)
		if parseError != nil {
			return BrowserProfile{}, fmt.Errorf("parsing browser proxy URL: %w", parseError)
		}
		if parsedProxyURL.Scheme == "" || parsedProxyURL.Host == "" {
			return BrowserProfile{}, fmt.Errorf("invalid browser proxy URL %q", trimmedProxyURL)
		}
	}

	switch browserProfile.Mode {
	case BrowserModeDirect:
		if trimmedProxyURL != "" && isSOCKSProxy(trimmedProxyURL) {
			return BrowserProfile{}, fmt.Errorf("browser mode %q cannot use a SOCKS proxy URL", browserProfile.Mode)
		}
		if parsedProxyURL, parseError := url.Parse(trimmedProxyURL); trimmedProxyURL != "" && parseError == nil && parsedProxyURL.User != nil {
			return BrowserProfile{}, fmt.Errorf("browser mode %q cannot use inline proxy credentials", browserProfile.Mode)
		}
	case BrowserModeHTTPFetchAuth:
		if trimmedProxyURL == "" {
			return BrowserProfile{}, fmt.Errorf("browser mode %q requires a proxy URL", browserProfile.Mode)
		}
		if isSOCKSProxy(trimmedProxyURL) {
			return BrowserProfile{}, fmt.Errorf("browser mode %q cannot use a SOCKS proxy URL", browserProfile.Mode)
		}
	case BrowserModeSOCKSForwarder:
		if trimmedProxyURL == "" {
			return BrowserProfile{}, fmt.Errorf("browser mode %q requires a proxy URL", browserProfile.Mode)
		}
		if !isSOCKSProxy(trimmedProxyURL) {
			return BrowserProfile{}, fmt.Errorf("browser mode %q requires a SOCKS proxy URL", browserProfile.Mode)
		}
	default:
		return BrowserProfile{}, fmt.Errorf("unsupported browser mode %q", browserProfile.Mode)
	}

	return browserProfile, nil
}

func normalizeHTTPProfile(httpProfile HTTPProfile) (HTTPProfile, error) {
	trimmedProxyURL := strings.TrimSpace(httpProfile.URL)
	if httpProfile.ID == "" {
		httpProfile.ID = inferredTransportID(trimmedProxyURL)
	}
	if httpProfile.Provider == "" {
		httpProfile.Provider = inferredProvider(trimmedProxyURL)
	}
	httpProfile.URL = trimmedProxyURL

	if trimmedProxyURL == "" {
		return httpProfile, nil
	}

	parsedProxyURL, parseError := url.Parse(trimmedProxyURL)
	if parseError != nil {
		return HTTPProfile{}, fmt.Errorf("parsing HTTP proxy URL: %w", parseError)
	}
	if parsedProxyURL.Scheme == "" || parsedProxyURL.Host == "" {
		return HTTPProfile{}, fmt.Errorf("invalid HTTP proxy URL %q", trimmedProxyURL)
	}

	return httpProfile, nil
}

func inferredProvider(proxyURL string) string {
	parsedProxyURL, parseError := url.Parse(proxyURL)
	if parseError != nil {
		return "unknown"
	}
	if parsedProxyURL.Hostname() == "" {
		return "direct"
	}
	return parsedProxyURL.Hostname()
}

func inferredTransportID(proxyURL string) string {
	parsedProxyURL, parseError := url.Parse(proxyURL)
	if parseError != nil || parsedProxyURL.Host == "" {
		return "direct"
	}
	if parsedProxyURL.User != nil {
		return fmt.Sprintf("%s@%s", parsedProxyURL.User.Username(), parsedProxyURL.Host)
	}
	return parsedProxyURL.Host
}

func httpTimeoutOrDefault(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultHTTPTimeout
	}
	return timeout
}

func isSOCKSProxy(rawProxyURL string) bool {
	parsedProxyURL, parseError := urlParse(rawProxyURL)
	if parseError != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(parsedProxyURL.Scheme)) {
	case "socks4", "socks5", "socks5h":
		return true
	default:
		return false
	}
}

func stripProxyAuth(rawProxyURL string) string {
	parsedProxyURL, parseError := url.Parse(rawProxyURL)
	if parseError != nil {
		return rawProxyURL
	}
	parsedProxyURL.User = nil
	return parsedProxyURL.String()
}

func extractProxyCredentials(rawProxyURL string) (string, string) {
	parsedProxyURL, parseError := url.Parse(rawProxyURL)
	if parseError != nil || parsedProxyURL.User == nil {
		return "", ""
	}
	password, _ := parsedProxyURL.User.Password()
	return parsedProxyURL.User.Username(), password
}

func enableHTTPProxyAuth(ctx context.Context, browserProfile BrowserProfile) error {
	if browserProfile.Mode != BrowserModeHTTPFetchAuth || browserProfile.URL == "" {
		return nil
	}

	proxyUsername, proxyPassword := extractProxyCredentials(browserProfile.URL)
	if proxyUsername == "" {
		return nil
	}

	setupProxyAuthFn(ctx, proxyUsername, proxyPassword)
	if fetchEnableError := chromedpRunner(ctx, fetch.Enable().WithHandleAuthRequests(true)); fetchEnableError != nil {
		return fmt.Errorf("enabling fetch for proxy auth: %w", fetchEnableError)
	}

	return nil
}

func setupProxyAuth(ctx context.Context, username string, password string) {
	chromedpListenTarget(ctx, newProxyAuthEventHandler(ctx, username, password))
}

var proxyAuthRunner = func(ctx context.Context, actions ...chromedp.Action) error {
	return chromedp.Run(ctx, actions...)
}

func newProxyAuthEventHandler(ctx context.Context, username string, password string) func(event interface{}) {
	return func(event interface{}) {
		if authRequired, isAuthRequired := event.(*fetch.EventAuthRequired); isAuthRequired {
			go func() {
				_ = proxyAuthRunner(ctx,
					fetch.ContinueWithAuth(authRequired.RequestID, &fetch.AuthChallengeResponse{
						Response: fetch.AuthChallengeResponseResponseProvideCredentials,
						Username: username,
						Password: password,
					}),
				)
			}()
		}

		if requestPaused, isRequestPaused := event.(*fetch.EventRequestPaused); isRequestPaused {
			go func() {
				_ = proxyAuthRunner(ctx, fetch.ContinueRequest(requestPaused.RequestID))
			}()
		}
	}
}

type socksForwarder struct {
	listener net.Listener
	addr     string
	dialer   proxy.Dialer
}

func newSOCKSForwarder(upstreamProxyURL string) (*socksForwarder, error) {
	parsedProxyURL, parseError := url.Parse(upstreamProxyURL)
	if parseError != nil {
		return nil, fmt.Errorf("parsing proxy URL: %w", parseError)
	}

	var auth *proxy.Auth
	if parsedProxyURL.User != nil {
		password, _ := parsedProxyURL.User.Password()
		auth = &proxy.Auth{
			User:     parsedProxyURL.User.Username(),
			Password: password,
		}
	}

	upstreamDialer, dialerError := proxySocks("tcp", parsedProxyURL.Host, auth, proxy.Direct)
	if dialerError != nil {
		return nil, fmt.Errorf("creating upstream SOCKS5 dialer: %w", dialerError)
	}

	listener, listenError := netListen("tcp", "127.0.0.1:0")
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
	_ = forwarder.listener.Close()
}

func (forwarder *socksForwarder) acceptLoop() {
	for {
		clientConnection, acceptError := forwarder.listener.Accept()
		if acceptError != nil {
			return
		}
		go forwarder.handleConnection(clientConnection)
	}
}

func (forwarder *socksForwarder) handleConnection(clientConnection net.Conn) {
	defer clientConnection.Close()

	header := make([]byte, 2)
	if _, readError := io.ReadFull(clientConnection, header); readError != nil {
		return
	}
	if header[0] != 0x05 {
		return
	}

	methods := make([]byte, header[1])
	if _, readError := io.ReadFull(clientConnection, methods); readError != nil {
		return
	}
	_, _ = clientConnection.Write([]byte{0x05, 0x00})

	request := make([]byte, 4)
	if _, readError := io.ReadFull(clientConnection, request); readError != nil {
		return
	}
	if request[1] != 0x01 {
		return
	}

	var targetHost string
	switch request[3] {
	case 0x01:
		ipBytes := make([]byte, 4)
		_, _ = io.ReadFull(clientConnection, ipBytes)
		targetHost = net.IP(ipBytes).String()
	case 0x03:
		lengthByte := make([]byte, 1)
		_, _ = io.ReadFull(clientConnection, lengthByte)
		domainBytes := make([]byte, lengthByte[0])
		_, _ = io.ReadFull(clientConnection, domainBytes)
		targetHost = string(domainBytes)
	case 0x04:
		ipBytes := make([]byte, 16)
		_, _ = io.ReadFull(clientConnection, ipBytes)
		targetHost = net.IP(ipBytes).String()
	default:
		return
	}

	portBytes := make([]byte, 2)
	_, _ = io.ReadFull(clientConnection, portBytes)
	targetPort := int(portBytes[0])<<8 | int(portBytes[1])
	targetAddress := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))

	upstreamConnection, dialError := forwarder.dialer.Dial("tcp", targetAddress)
	if dialError != nil {
		_, _ = clientConnection.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstreamConnection.Close()

	_, _ = clientConnection.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstreamConnection, clientConnection); done <- struct{}{} }()
	go func() { _, _ = io.Copy(clientConnection, upstreamConnection); done <- struct{}{} }()
	<-done
}
