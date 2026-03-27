package jseval

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRenderPage_HTTPProxyWithAuth(t *testing.T) {
	// Create a target server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Proxied</title></head><body><div id="ok">proxy-auth-ok</div></body></html>`))
	}))
	defer targetServer.Close()

	// Create a simple HTTP proxy that requires Basic auth
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyAuth := r.Header.Get("Proxy-Authorization")
		expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("testuser:testpass"))
		if proxyAuth != expectedAuth {
			w.Header().Set("Proxy-Authenticate", "Basic realm=\"proxy\"")
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		// Forward the request
		if r.Method == http.MethodConnect {
			// For CONNECT tunneling, we need to hijack
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijacking not supported", http.StatusInternalServerError)
				return
			}
			clientConn, _, _ := hijacker.Hijack()
			clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
			// In a real proxy we'd tunnel, but for this test Chrome uses non-CONNECT for HTTP
			clientConn.Close()
			return
		}
		// For plain HTTP proxy requests
		resp, err := http.Get(r.URL.String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		buf := make([]byte, 32*1024)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
			}
			if readErr != nil {
				break
			}
		}
	}))
	defer proxyServer.Close()

	proxyURL := strings.Replace(proxyServer.URL, "http://", "http://testuser:testpass@", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, renderError := RenderPage(ctx, targetServer.URL, Config{
		Timeout:  10 * time.Second,
		ProxyURL: proxyURL,
	})
	if renderError != nil {
		t.Fatalf("RenderPage with HTTP proxy auth failed: %v", renderError)
	}

	if !strings.Contains(result.HTML, "proxy-auth-ok") {
		t.Errorf("expected 'proxy-auth-ok' in HTML, got: %.200s", result.HTML)
	}
}

func TestRenderPage_IgnoreCertErrors(t *testing.T) {
	// Create a TLS server with a self-signed cert
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>TLS</title></head><body><div id="secure">tls-ok</div></body></html>`))
	}))
	defer tlsServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, renderError := RenderPage(ctx, tlsServer.URL, Config{
		Timeout:          10 * time.Second,
		IgnoreCertErrors: true,
		WaitSelector:     "#secure",
	})
	if renderError != nil {
		t.Fatalf("RenderPage with IgnoreCertErrors failed: %v", renderError)
	}

	if !strings.Contains(result.HTML, "tls-ok") {
		t.Errorf("expected 'tls-ok' in HTML, got: %.200s", result.HTML)
	}
}

func TestRenderPage_UserAgent(t *testing.T) {
	var receivedUA string
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>UA</title></head><body>ok</body></html>`))
	}))
	defer targetServer.Close()

	_, renderError := RenderPage(context.Background(), targetServer.URL, Config{
		Timeout:   10 * time.Second,
		UserAgent: "CustomBot/1.0",
	})
	if renderError != nil {
		t.Fatalf("render failed: %v", renderError)
	}
	if receivedUA != "CustomBot/1.0" {
		t.Errorf("expected UserAgent 'CustomBot/1.0', got %q", receivedUA)
	}
}

func TestRenderPage_DefaultTimeout(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Default</title></head><body>ok</body></html>`))
	}))
	defer targetServer.Close()

	// Config with zero timeout should use default 30s
	result, renderError := RenderPage(context.Background(), targetServer.URL, Config{})
	if renderError != nil {
		t.Fatalf("render failed: %v", renderError)
	}
	if result.Title != "Default" {
		t.Errorf("expected title 'Default', got %q", result.Title)
	}
}

func TestRenderPage_HTTPProxyFetchEnableError(t *testing.T) {
	// Use an already-cancelled context so that chromedp.Run for fetch.Enable fails
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, renderError := RenderPage(ctx, "http://example.com", Config{
		Timeout:  5 * time.Second,
		ProxyURL: "http://user:pass@proxy.example.com:8080",
	})
	if renderError == nil {
		t.Fatal("expected error when context is cancelled before fetch.Enable")
	}
}

func TestRenderPage_HTTPProxyWithoutAuth(t *testing.T) {
	// Create a target server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Proxied</title></head><body>proxy-no-auth-ok</body></html>`))
	}))
	defer targetServer.Close()

	// Create a simple HTTP proxy without auth
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijacking not supported", http.StatusInternalServerError)
				return
			}
			clientConn, _, _ := hijacker.Hijack()
			clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
			clientConn.Close()
			return
		}
		resp, err := http.Get(r.URL.String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		buf := make([]byte, 32*1024)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
			}
			if readErr != nil {
				break
			}
		}
	}))
	defer proxyServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, renderError := RenderPage(ctx, targetServer.URL, Config{
		Timeout:  10 * time.Second,
		ProxyURL: proxyServer.URL,
	})
	if renderError != nil {
		t.Fatalf("RenderPage with HTTP proxy (no auth) failed: %v", renderError)
	}

	if !strings.Contains(result.HTML, "proxy-no-auth-ok") {
		t.Errorf("expected 'proxy-no-auth-ok' in HTML, got: %.200s", result.HTML)
	}
}
