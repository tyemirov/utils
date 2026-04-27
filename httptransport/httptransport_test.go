package httptransport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

func TestNewClientSupportsDirectAndHTTPProxy(t *testing.T) {
	client, clientError := NewClient(Profile{}, 0)
	if clientError != nil {
		t.Fatalf("NewClient(direct) error = %v", clientError)
	}
	if client.Timeout != DefaultTimeout {
		t.Fatalf("Timeout = %v", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatal("expected proxy func")
	}

	client, clientError = NewClient(Profile{
		URL:              "http://proxy.example.com:8080",
		IgnoreCertErrors: true,
	}, 5*time.Second)
	if clientError != nil {
		t.Fatalf("NewClient(http proxy) error = %v", clientError)
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

func TestNewClientInjectedFailureBranches(t *testing.T) {
	restoreHooks := resetHTTPTransportHooks()
	defer restoreHooks()

	cookieJarNew = func(options *cookiejar.Options) (*cookiejar.Jar, error) {
		return nil, errors.New("jar failed")
	}
	if _, clientError := NewClient(Profile{}, time.Second); clientError == nil || !contains(clientError.Error(), "creating cookie jar") {
		t.Fatalf("NewClient(cookie jar) error = %v", clientError)
	}

	cookieJarNew = newCookieJar
	proxyFromURL = func(parsedURL *url.URL, forward proxy.Dialer) (proxy.Dialer, error) {
		return nil, errors.New("dialer failed")
	}
	if _, clientError := NewClient(Profile{URL: "socks5://proxy.example.com:1080"}, time.Second); clientError == nil || !contains(clientError.Error(), "creating SOCKS dialer") {
		t.Fatalf("NewClient(dialer) error = %v", clientError)
	}

	proxyFromURL = func(parsedURL *url.URL, forward proxy.Dialer) (proxy.Dialer, error) {
		return contextDialerFunc(func(ctx context.Context, network string, address string) (net.Conn, error) {
			return nil, errors.New("unused")
		}), nil
	}
	client, clientError := NewClient(Profile{URL: "socks5://proxy.example.com:1080"}, time.Second)
	if clientError != nil {
		t.Fatalf("NewClient(context dialer) error = %v", clientError)
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
	client, clientError = NewClient(Profile{URL: "socks5://proxy.example.com:1080"}, time.Second)
	if clientError != nil {
		t.Fatalf("NewClient(non-context dialer) error = %v", clientError)
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

	if _, clientError = NewClient(Profile{URL: "://bad\x00proxy"}, time.Second); clientError == nil {
		t.Fatal("NewClient(invalid profile) error = nil")
	}
}

func TestInferNormalizeAndHelpers(t *testing.T) {
	profile, inferError := InferProfile("", true)
	if inferError != nil {
		t.Fatalf("InferProfile(direct) error = %v", inferError)
	}
	if profile.ID != "direct" || profile.Provider != "direct" || profile.URL != "" || !profile.IgnoreCertErrors {
		t.Fatalf("InferProfile(direct) = %#v", profile)
	}

	profile, inferError = InferProfile(" http://user:pass@proxy.example.com:8080 ", false)
	if inferError != nil {
		t.Fatalf("InferProfile(http proxy) error = %v", inferError)
	}
	if profile.ID != "user@proxy.example.com:8080" || profile.Provider != "proxy.example.com" || profile.URL != "http://user:pass@proxy.example.com:8080" {
		t.Fatalf("InferProfile(http proxy) = %#v", profile)
	}

	normalized, normalizeError := NormalizeProfile(Profile{URL: " http://proxy.example.com:8080 "})
	if normalizeError != nil {
		t.Fatalf("NormalizeProfile() error = %v", normalizeError)
	}
	if normalized.ID != "proxy.example.com:8080" || normalized.Provider != "proxy.example.com" || normalized.URL != "http://proxy.example.com:8080" {
		t.Fatalf("NormalizeProfile() = %#v", normalized)
	}

	if _, inferError = InferProfile("://bad\x00proxy", false); inferError == nil || !contains(inferError.Error(), "parsing HTTP proxy URL") {
		t.Fatalf("InferProfile(parse) error = %v", inferError)
	}
	if _, inferError = InferProfile("http://", false); inferError == nil || !contains(inferError.Error(), "invalid HTTP proxy URL") {
		t.Fatalf("InferProfile(shape) error = %v", inferError)
	}
	if _, normalizeError = NormalizeProfile(Profile{URL: "http://"}); normalizeError == nil || !contains(normalizeError.Error(), "invalid HTTP proxy URL") {
		t.Fatalf("NormalizeProfile(shape) error = %v", normalizeError)
	}

	if TimeoutOrDefault(0) != DefaultTimeout {
		t.Fatalf("TimeoutOrDefault(0) = %v", TimeoutOrDefault(0))
	}
	if TimeoutOrDefault(3*time.Second) != 3*time.Second {
		t.Fatalf("TimeoutOrDefault(nonzero) = %v", TimeoutOrDefault(3*time.Second))
	}
	if !IsSOCKSProxy("socks5://proxy.example.com:1080") {
		t.Fatal("IsSOCKSProxy(socks5) = false")
	}
	if IsSOCKSProxy("http://proxy.example.com:8080") {
		t.Fatal("IsSOCKSProxy(http) = true")
	}

	restoreHooks := resetHTTPTransportHooks()
	defer restoreHooks()
	urlParse = func(rawURL string) (*url.URL, error) {
		return nil, errors.New("parse failed")
	}
	if IsSOCKSProxy("socks5://proxy.example.com:1080") {
		t.Fatal("IsSOCKSProxy(parse error) = true")
	}
}

func resetHTTPTransportHooks() func() {
	originalProxyFromURL := proxyFromURL
	originalURLParse := urlParse
	originalCookieJarNew := cookieJarNew

	return func() {
		proxyFromURL = originalProxyFromURL
		urlParse = originalURLParse
		cookieJarNew = originalCookieJarNew
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

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()

	parsedURL, parseError := url.Parse(rawURL)
	if parseError != nil {
		t.Fatalf("url.Parse() error = %v", parseError)
	}
	return parsedURL
}

func newCookieJar(options *cookiejar.Options) (*cookiejar.Jar, error) {
	return cookiejar.New(options)
}

func contains(value string, substring string) bool {
	return strings.Contains(value, substring)
}
