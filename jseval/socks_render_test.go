package jseval

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// TestRenderPage_SOCKSProxy_ViaLocalForwarder tests the full SOCKS5 proxy path
// in RenderPage by running a local SOCKS5 proxy that forwards to a target server.
func TestRenderPage_SOCKSProxy_ViaLocalForwarder(t *testing.T) {
	// Create target HTTP server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>SOCKS</title></head><body><div id="marker">socks-render-ok</div></body></html>`))
	}))
	defer targetServer.Close()

	// Start a local SOCKS5 proxy that forwards directly (no upstream auth needed)
	socksListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	socksProxy := &socksForwarder{
		listener: socksListener,
		addr:     socksListener.Addr().String(),
		dialer:   &directDialer{},
	}
	go socksProxy.acceptLoop()
	defer socksProxy.close()

	proxyURL := fmt.Sprintf("socks5://testuser:testpass@%s", socksProxy.addr)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, renderError := RenderPage(ctx, targetServer.URL, Config{
		Timeout:  10 * time.Second,
		ProxyURL: proxyURL,
	})
	if renderError != nil {
		t.Fatalf("RenderPage with SOCKS5 proxy failed: %v", renderError)
	}

	if !strings.Contains(result.HTML, "socks-render-ok") {
		t.Errorf("expected 'socks-render-ok' in HTML, got: %.200s", result.HTML)
	}
}

// directDialer dials targets directly (no upstream proxy).
type directDialer struct{}

func (d *directDialer) Dial(network, addr string) (net.Conn, error) {
	return net.DialTimeout(network, addr, 5*time.Second)
}

func TestRenderPage_SOCKSForwarderCreationError(t *testing.T) {
	original := proxySocks
	defer func() { proxySocks = original }()

	proxySocks = func(network, addr string, auth *proxy.Auth, forward proxy.Dialer) (proxy.Dialer, error) {
		return nil, fmt.Errorf("mock dialer error")
	}

	_, renderError := RenderPage(context.Background(), "http://example.com", Config{
		Timeout:  5 * time.Second,
		ProxyURL: "socks5://user:pass@host:1080",
	})
	if renderError == nil {
		t.Fatal("expected error when SOCKS forwarder creation fails")
	}
	if !strings.Contains(renderError.Error(), "SOCKS forwarder") {
		t.Errorf("expected SOCKS forwarder error, got: %v", renderError)
	}
}

func TestNewSOCKSForwarderDialerError(t *testing.T) {
	original := proxySocks
	defer func() { proxySocks = original }()

	proxySocks = func(network, addr string, auth *proxy.Auth, forward proxy.Dialer) (proxy.Dialer, error) {
		return nil, fmt.Errorf("mock dialer error")
	}

	_, err := newSOCKSForwarder("socks5://user:pass@host:1080")
	if err == nil {
		t.Fatal("expected error from SOCKS5 dialer creation")
	}
	if !strings.Contains(err.Error(), "creating upstream SOCKS5 dialer") {
		t.Errorf("expected dialer error, got: %v", err)
	}
}

func TestNewSOCKSForwarderListenError(t *testing.T) {
	original := netListen
	defer func() { netListen = original }()

	netListen = func(network, address string) (net.Listener, error) {
		return nil, fmt.Errorf("mock listen error")
	}

	_, err := newSOCKSForwarder("socks5://user:pass@host:1080")
	if err == nil {
		t.Fatal("expected error from net.Listen")
	}
	if !strings.Contains(err.Error(), "listening on localhost") {
		t.Errorf("expected listen error, got: %v", err)
	}
}

// TestHandleConnectionReadMethodsError tests the path where reading auth methods fails.
func TestHandleConnectionReadMethodsError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	fwd := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &failDialer{},
	}
	go fwd.acceptLoop()
	defer fwd.close()

	conn, err := net.DialTimeout("tcp", fwd.addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Send SOCKS5 version with 1 auth method but then close before sending the method
	conn.Write([]byte{0x05, 0x01})
	conn.Close()

	time.Sleep(50 * time.Millisecond)
}

// TestHandleConnectionReadRequestError tests the path where reading CONNECT request fails.
func TestHandleConnectionReadRequestError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	fwd := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &failDialer{},
	}
	go fwd.acceptLoop()
	defer fwd.close()

	conn, err := net.DialTimeout("tcp", fwd.addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Valid handshake
	conn.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	io.ReadFull(conn, authReply)

	// Close before sending CONNECT request
	conn.Close()

	time.Sleep(50 * time.Millisecond)
}
