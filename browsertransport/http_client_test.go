package browsertransport

import (
	"net/http"
	"net/url"
	"testing"
	"time"
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

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()

	parsedURL, parseError := url.Parse(rawURL)
	if parseError != nil {
		t.Fatalf("url.Parse() error = %v", parseError)
	}
	return parsedURL
}
