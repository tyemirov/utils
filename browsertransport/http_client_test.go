package browsertransport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

func TestNewHTTPClientSupportsDirectAndHTTPProxy(t *testing.T) {
	client, clientError := NewHTTPClient(HTTPProfile{}, 0)
	if clientError != nil {
		t.Fatalf("NewHTTPClient(direct) error = %v", clientError)
	}
	if client.Timeout != defaultHTTPTimeout {
		t.Fatalf("Timeout = %v", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatal("expected proxy func")
	}

	client, clientError = NewHTTPClient(HTTPProfile{
		URL:              "http://proxy.example.com:8080",
		IgnoreCertErrors: true,
	}, 5*time.Second)
	if clientError != nil {
		t.Fatalf("NewHTTPClient(http proxy) error = %v", clientError)
	}
	if client.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %v", client.Timeout)
	}
	transport, ok = client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T", client.Transport)
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("TLSClientConfig = %#v", transport.TLSClientConfig)
	}
	request := &http.Request{URL: mustParseURL(t, "https://example.com")}
	proxyURL, proxyError := transport.Proxy(request)
	if proxyError != nil {
		t.Fatalf("Proxy() error = %v", proxyError)
	}
	if proxyURL == nil || proxyURL.String() != "http://proxy.example.com:8080" {
		t.Fatalf("Proxy() = %v", proxyURL)
	}
}

func TestNewHTTPClientInjectedFailureBranches(t *testing.T) {
	restoreHooks := resetBrowserTransportHooks()
	defer restoreHooks()

	cookieJarNew = func(options *cookiejar.Options) (*cookiejar.Jar, error) {
		return nil, errors.New("jar failed")
	}
	if _, clientError := NewHTTPClient(HTTPProfile{}, time.Second); clientError == nil || !contains(clientError.Error(), "creating cookie jar") {
		t.Fatalf("NewHTTPClient(cookie jar) error = %v", clientError)
	}

	cookieJarNew = newCookieJar
	proxyFromURL = func(parsedURL *url.URL, forward proxy.Dialer) (proxy.Dialer, error) {
		return nil, errors.New("dialer failed")
	}
	if _, clientError := NewHTTPClient(HTTPProfile{URL: "socks5://proxy.example.com:1080"}, time.Second); clientError == nil || !contains(clientError.Error(), "creating SOCKS dialer") {
		t.Fatalf("NewHTTPClient(dialer) error = %v", clientError)
	}

	proxyFromURL = func(parsedURL *url.URL, forward proxy.Dialer) (proxy.Dialer, error) {
		return contextDialerFunc(func(ctx context.Context, network string, address string) (net.Conn, error) {
			return nil, errors.New("unused")
		}), nil
	}
	client, clientError := NewHTTPClient(HTTPProfile{URL: "socks5://proxy.example.com:1080"}, time.Second)
	if clientError != nil {
		t.Fatalf("NewHTTPClient(context dialer) error = %v", clientError)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T", client.Transport)
	}
	if transport.DialContext == nil || transport.Proxy != nil {
		t.Fatalf("Transport = %#v", transport)
	}

	proxyFromURL = func(parsedURL *url.URL, forward proxy.Dialer) (proxy.Dialer, error) {
		return dialerFunc(func(network string, address string) (net.Conn, error) {
			return nil, errors.New("unused")
		}), nil
	}
	client, clientError = NewHTTPClient(HTTPProfile{URL: "socks5://proxy.example.com:1080"}, time.Second)
	if clientError != nil {
		t.Fatalf("NewHTTPClient(non-context dialer) error = %v", clientError)
	}
	transport, ok = client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T", client.Transport)
	}
	if transport.DialContext == nil || transport.Proxy != nil {
		t.Fatalf("Transport = %#v", transport)
	}
	if _, dialError := transport.DialContext(context.Background(), "tcp", "example.com:80"); dialError == nil || dialError.Error() != "unused" {
		t.Fatalf("DialContext() error = %v", dialError)
	}

	if _, clientError = NewHTTPClient(HTTPProfile{URL: "://bad\x00proxy"}, time.Second); clientError == nil {
		t.Fatal("NewHTTPClient(invalid profile) error = nil")
	}
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()

	parsedURL, parseError := url.Parse(rawURL)
	if parseError != nil {
		t.Fatalf("url.Parse() error = %v", parseError)
	}
	return parsedURL
}
