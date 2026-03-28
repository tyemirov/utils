package browsertransport

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"golang.org/x/net/proxy"
)

func TestNewSessionModesAndClose(t *testing.T) {
	restoreHooks := resetBrowserTransportHooks()
	defer restoreHooks()

	chromedpNewExecAllocator = allocatorContext
	chromedpNewContext = browserContext
	chromedpRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		return nil
	}
	proxySocks = func(network string, address string, auth *proxy.Auth, forward proxy.Dialer) (proxy.Dialer, error) {
		return dialerFunc(func(network string, address string) (net.Conn, error) {
			return nil, errors.New("unused")
		}), nil
	}

	directSession, sessionError := NewSession(context.Background(), BrowserProfile{
		URL:              "http://proxy.example.com:8080",
		IgnoreCertErrors: true,
	}, LaunchOptions{
		ExecPath:                   "/bin/echo",
		UserAgent:                  "CustomBot/1.0",
		AdditionalAllocatorOptions: []chromedp.ExecAllocatorOption{chromedp.Flag("mute-audio", true)},
	})
	if sessionError != nil {
		t.Fatalf("NewSession(direct) error = %v", sessionError)
	}
	directSession.Close()

	httpProxySession, sessionError := NewSession(context.Background(), BrowserProfile{
		URL: "http://user:pass@proxy.example.com:8080",
	}, LaunchOptions{})
	if sessionError != nil {
		t.Fatalf("NewSession(http auth) error = %v", sessionError)
	}
	httpProxySession.Close()

	socksSession, sessionError := NewSession(context.Background(), BrowserProfile{
		URL: "socks5://user:pass@proxy.example.com:1080",
	}, LaunchOptions{})
	if sessionError != nil {
		t.Fatalf("NewSession(socks) error = %v", sessionError)
	}
	if socksSession.forwarder == nil {
		t.Fatal("expected SOCKS forwarder")
	}
	socksSession.Close()
}

func TestNewSessionFailurePaths(t *testing.T) {
	restoreHooks := resetBrowserTransportHooks()
	defer restoreHooks()

	chromedpNewExecAllocator = allocatorContext
	chromedpNewContext = browserContext

	if _, sessionError := NewSession(context.Background(), BrowserProfile{
		Mode: BrowserModeHTTPFetchAuth,
		URL:  "://bad\x00proxy",
	}, LaunchOptions{}); sessionError == nil {
		t.Fatal("NewSession(normalize error) error = nil")
	}

	listener := &stubListener{address: stubAddr("127.0.0.1:9999")}
	netListen = func(network string, address string) (net.Listener, error) {
		return listener, nil
	}
	proxySocks = func(network string, address string, auth *proxy.Auth, forward proxy.Dialer) (proxy.Dialer, error) {
		return dialerFunc(func(network string, address string) (net.Conn, error) {
			return nil, errors.New("unused")
		}), nil
	}
	chromedpRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		return errors.New("chrome failed to start")
	}
	if _, sessionError := NewSession(context.Background(), BrowserProfile{
		URL: "socks5://user:pass@proxy.example.com:1080",
	}, LaunchOptions{}); sessionError == nil || !contains(sessionError.Error(), "chrome failed to start") {
		t.Fatalf("NewSession(start error) error = %v", sessionError)
	}
	if listener.ClosedCount() != 1 {
		t.Fatalf("listener close count = %d", listener.ClosedCount())
	}

	proxySocks = func(network string, address string, auth *proxy.Auth, forward proxy.Dialer) (proxy.Dialer, error) {
		return nil, errors.New("dialer failed")
	}
	chromedpRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		return nil
	}
	if _, sessionError := NewSession(context.Background(), BrowserProfile{
		URL: "socks5://user:pass@proxy.example.com:1080",
	}, LaunchOptions{}); sessionError == nil || !contains(sessionError.Error(), "starting SOCKS forwarder") {
		t.Fatalf("NewSession(SOCKS forwarder) error = %v", sessionError)
	}
}

func TestWithTabBranches(t *testing.T) {
	restoreHooks := resetBrowserTransportHooks()
	defer restoreHooks()

	if withTabError := (&Session{}).WithTab(context.Background(), TabOptions{}, nil); withTabError == nil {
		t.Fatal("WithTab(nil callback) error = nil")
	}

	closedSession := &Session{closed: true}
	if withTabError := closedSession.WithTab(context.Background(), TabOptions{}, func(context.Context) error { return nil }); withTabError == nil {
		t.Fatal("WithTab(closed session) error = nil")
	}

	chromedpNewContext = browserContext
	callCount := 0
	chromedpRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		callCount++
		if callCount == 1 {
			return errors.New("tab init failed")
		}
		return nil
	}
	session := &Session{browserCtx: context.Background(), profile: BrowserProfile{Mode: BrowserModeDirect}}
	if withTabError := session.WithTab(context.Background(), TabOptions{}, func(context.Context) error { return nil }); withTabError == nil || !contains(withTabError.Error(), "initializing browser tab") {
		t.Fatalf("WithTab(tab init) error = %v", withTabError)
	}

	callCount = 0
	chromedpRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		callCount++
		if callCount == 2 {
			return errors.New("prelude failed")
		}
		return nil
	}
	if withTabError := session.WithTab(context.Background(), TabOptions{
		PreludeActions: []chromedp.Action{chromedp.WaitReady("body", chromedp.ByQuery)},
	}, func(context.Context) error { return nil }); withTabError == nil || !contains(withTabError.Error(), "preparing browser tab") {
		t.Fatalf("WithTab(prelude) error = %v", withTabError)
	}

	callCount = 0
	setupCalls := 0
	setupProxyAuthFn = func(ctx context.Context, username string, password string) {
		setupCalls++
	}
	chromedpRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		callCount++
		if callCount == 2 {
			return errors.New("fetch enable failed")
		}
		return nil
	}
	session.profile = BrowserProfile{
		Mode: BrowserModeHTTPFetchAuth,
		URL:  "http://user:pass@proxy.example.com:8080",
	}
	if withTabError := session.WithTab(context.Background(), TabOptions{}, func(context.Context) error { return nil }); withTabError == nil || !contains(withTabError.Error(), "enabling fetch for proxy auth") {
		t.Fatalf("WithTab(fetch enable) error = %v", withTabError)
	}
	if setupCalls != 1 {
		t.Fatalf("setup proxy auth calls = %d", setupCalls)
	}

	callCount = 0
	chromedpRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		callCount++
		return nil
	}
	session.profile = BrowserProfile{Mode: BrowserModeDirect}
	outerCtx, cancelOuter := context.WithCancel(context.Background())
	cancelOuter()
	runCalled := false
	if withTabError := session.WithTab(outerCtx, TabOptions{}, func(runContext context.Context) error {
		runCalled = true
		select {
		case <-runContext.Done():
		case <-time.After(time.Second):
			t.Fatal("expected canceled render context")
		}
		return nil
	}); withTabError != nil {
		t.Fatalf("WithTab(canceled context) error = %v", withTabError)
	}
	if !runCalled {
		t.Fatal("expected run callback")
	}
}

func TestCloseNilAndIdempotent(t *testing.T) {
	var nilSession *Session
	nilSession.Close()

	listener, listenError := net.Listen("tcp", "127.0.0.1:0")
	if listenError != nil {
		t.Fatalf("net.Listen() error = %v", listenError)
	}

	browserCancelCalls := 0
	allocatorCancelCalls := 0
	session := &Session{
		browserCancel: func() { browserCancelCalls++ },
		allocCancel:   func() { allocatorCancelCalls++ },
		forwarder:     &socksForwarder{listener: listener, addr: listener.Addr().String()},
	}

	session.Close()
	session.Close()

	if browserCancelCalls != 1 || allocatorCancelCalls != 1 {
		t.Fatalf("cancel calls = %d/%d", browserCancelCalls, allocatorCancelCalls)
	}
	if !session.closed {
		t.Fatal("expected closed session")
	}
}

func TestSetupProxyAuthAndEnableHTTPProxyAuthBranches(t *testing.T) {
	restoreHooks := resetBrowserTransportHooks()
	defer restoreHooks()

	listenCalls := 0
	chromedpListenTarget = func(ctx context.Context, handler func(event interface{})) {
		listenCalls++
	}
	setupProxyAuth(context.Background(), "user", "pass")
	if listenCalls != 1 {
		t.Fatalf("setupProxyAuth() listen calls = %d", listenCalls)
	}

	if authError := enableHTTPProxyAuth(context.Background(), BrowserProfile{Mode: BrowserModeDirect}); authError != nil {
		t.Fatalf("enableHTTPProxyAuth(direct) error = %v", authError)
	}
	if authError := enableHTTPProxyAuth(context.Background(), BrowserProfile{Mode: BrowserModeHTTPFetchAuth}); authError != nil {
		t.Fatalf("enableHTTPProxyAuth(blank URL) error = %v", authError)
	}
	if authError := enableHTTPProxyAuth(context.Background(), BrowserProfile{Mode: BrowserModeHTTPFetchAuth, URL: "http://proxy.example.com:8080"}); authError != nil {
		t.Fatalf("enableHTTPProxyAuth(no credentials) error = %v", authError)
	}

	setupCalls := 0
	setupProxyAuthFn = func(ctx context.Context, username string, password string) {
		setupCalls++
	}
	chromedpRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		return nil
	}
	if authError := enableHTTPProxyAuth(context.Background(), BrowserProfile{
		Mode: BrowserModeHTTPFetchAuth,
		URL:  "http://user:pass@proxy.example.com:8080",
	}); authError != nil {
		t.Fatalf("enableHTTPProxyAuth(authenticated) error = %v", authError)
	}
	if setupCalls != 1 {
		t.Fatalf("setup calls = %d", setupCalls)
	}

	chromedpRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		return errors.New("enable failed")
	}
	if authError := enableHTTPProxyAuth(context.Background(), BrowserProfile{
		Mode: BrowserModeHTTPFetchAuth,
		URL:  "http://user:pass@proxy.example.com:8080",
	}); authError == nil || !contains(authError.Error(), "enabling fetch for proxy auth") {
		t.Fatalf("enableHTTPProxyAuth(enable failure) error = %v", authError)
	}
}
