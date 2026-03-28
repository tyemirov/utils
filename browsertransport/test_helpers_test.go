package browsertransport

import (
	"context"
	"net"
	"net/http/cookiejar"
	"sync"

	"github.com/chromedp/chromedp"
)

func resetBrowserTransportHooks() func() {
	originalNetListen := netListen
	originalProxySocks := proxySocks
	originalProxyFromURL := proxyFromURL
	originalURLParse := urlParse
	originalChromedpRunner := chromedpRunner
	originalChromedpNewExecAllocator := chromedpNewExecAllocator
	originalChromedpNewContext := chromedpNewContext
	originalChromedpListenTarget := chromedpListenTarget
	originalCookieJarNew := cookieJarNew
	originalSetupProxyAuthFn := setupProxyAuthFn
	originalProxyAuthRunner := proxyAuthRunner

	return func() {
		netListen = originalNetListen
		proxySocks = originalProxySocks
		proxyFromURL = originalProxyFromURL
		urlParse = originalURLParse
		chromedpRunner = originalChromedpRunner
		chromedpNewExecAllocator = originalChromedpNewExecAllocator
		chromedpNewContext = originalChromedpNewContext
		chromedpListenTarget = originalChromedpListenTarget
		cookieJarNew = originalCookieJarNew
		setupProxyAuthFn = originalSetupProxyAuthFn
		proxyAuthRunner = originalProxyAuthRunner
	}
}

type dialerFunc func(network string, address string) (net.Conn, error)

func (dialer dialerFunc) Dial(network string, address string) (net.Conn, error) {
	return dialer(network, address)
}

type contextDialerFunc func(ctx context.Context, network string, address string) (net.Conn, error)

func (dialer contextDialerFunc) DialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	return dialer(ctx, network, address)
}

func (dialer contextDialerFunc) Dial(network string, address string) (net.Conn, error) {
	return dialer(context.Background(), network, address)
}

type stubAddr string

func (address stubAddr) Network() string {
	return "tcp"
}

func (address stubAddr) String() string {
	return string(address)
}

type stubListener struct {
	address    net.Addr
	closeCount int
	mu         sync.Mutex
}

func (listener *stubListener) Accept() (net.Conn, error) {
	return nil, net.ErrClosed
}

func (listener *stubListener) Close() error {
	listener.mu.Lock()
	listener.closeCount++
	listener.mu.Unlock()
	return nil
}

func (listener *stubListener) Addr() net.Addr {
	return listener.address
}

func (listener *stubListener) ClosedCount() int {
	listener.mu.Lock()
	defer listener.mu.Unlock()
	return listener.closeCount
}

func allocatorContext(parent context.Context, options ...chromedp.ExecAllocatorOption) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

func browserContext(parent context.Context, options ...chromedp.ContextOption) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

func newCookieJar(options *cookiejar.Options) (*cookiejar.Jar, error) {
	return cookiejar.New(options)
}
