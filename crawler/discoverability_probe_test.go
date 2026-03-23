package crawler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/require"
)

func TestAmazonDiscoverabilityProberBindNetworkAppliesCrawlerHeaders(t *testing.T) {
	t.Parallel()

	var (
		seenPlatform string
		seenUA       string
		seenMarker   string
	)

	transport := discoverabilityRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		seenUA = request.Header.Get("User-Agent")
		seenMarker = request.Header.Get("X-Crawler-Discoverability")
		responseBody := `<html><body><div data-component-type="s-search-result" data-asin="B00TARGET123"></div></body></html>`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(responseBody)),
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Request:    request,
		}, nil
	})

	prober := NewAmazonDiscoverabilityProber(nil, noopLogger{}).(*amazonDiscoverabilityProber)
	prober.bindNetwork(
		"AMZN",
		transport,
		3*time.Second,
		requestHeaderProviderFunc(func(platformID string, request *colly.Request) {
			seenPlatform = platformID
			request.Headers.Set("User-Agent", "test-agent")
			request.Headers.Set("X-Crawler-Discoverability", "bound")
		}),
	)

	discoverability, err := prober.Probe(context.Background(), "b00target123")
	require.NoError(t, err)
	require.Equal(t, DiscoverabilityStatusFirstOrganic, discoverability.Status)
	require.Equal(t, 1, discoverability.TargetOrganicRank)
	require.Equal(t, "AMZN", seenPlatform)
	require.Equal(t, "test-agent", seenUA)
	require.Equal(t, "bound", seenMarker)
	require.Zero(t, prober.httpClient.Timeout)
}

func TestAmazonDiscoverabilityProberAllowsSlowProgressingResponses(t *testing.T) {
	t.Parallel()

	const requestTimeout = 100 * time.Millisecond

	responseBody := `<html><body><div data-component-type="s-search-result" data-asin="B00TARGET123"></div></body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hijacker, ok := writer.(http.Hijacker)
		require.True(t, ok)

		conn, bufferedConn, err := hijacker.Hijack()
		require.NoError(t, err)
		defer conn.Close()

		responseParts := []string{
			"HTTP/1.1 200 OK\r\n",
			"Content-Type: text/html\r\n",
			fmt.Sprintf("Content-Length: %d\r\n", len(responseBody)),
			"\r\n",
			responseBody,
		}
		for index, part := range responseParts {
			_, err = bufferedConn.WriteString(part)
			require.NoError(t, err)
			require.NoError(t, bufferedConn.Flush())
			if index < len(responseParts)-1 {
				time.Sleep(70 * time.Millisecond)
			}
		}
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	prober := NewAmazonDiscoverabilityProber(nil, noopLogger{}).(*amazonDiscoverabilityProber)
	prober.bindNetwork("AMZN", nil, requestTimeout, nil)
	prober.httpClient.Transport = rewriteDiscoverabilityHostTransport{
		base: prober.httpClient.Transport,
		host: serverURL.Host,
	}

	discoverability, err := prober.Probe(context.Background(), "b00target123")
	require.NoError(t, err)
	require.Equal(t, DiscoverabilityStatusFirstOrganic, discoverability.Status)
	require.Zero(t, prober.httpClient.Timeout)
}

type discoverabilityRoundTripFunc func(request *http.Request) (*http.Response, error)

func (fn discoverabilityRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type rewriteDiscoverabilityHostTransport struct {
	base http.RoundTripper
	host string
}

func (transport rewriteDiscoverabilityHostTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	rewrittenRequest := request.Clone(request.Context())
	rewrittenURL := *request.URL
	rewrittenURL.Scheme = "http"
	rewrittenURL.Host = transport.host
	rewrittenRequest.URL = &rewrittenURL
	rewrittenRequest.Host = transport.host

	if transport.base == nil {
		return nil, fmt.Errorf("rewrite discoverability host transport requires a base transport")
	}
	return transport.base.RoundTrip(rewrittenRequest)
}
