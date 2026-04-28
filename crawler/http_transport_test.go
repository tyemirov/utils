package crawler

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCrawlerHTTPTransportFacade(t *testing.T) {
	t.Parallel()

	profile, err := InferHTTPProfile(" http://proxy.example.com:8080 ", true)
	require.NoError(t, err)
	require.Equal(t, "http://proxy.example.com:8080", profile.URL)
	require.True(t, profile.IgnoreCertErrors)

	normalized, err := NormalizeHTTPProfile(HTTPProfile{URL: " http://proxy.example.com:8080 "})
	require.NoError(t, err)
	require.Equal(t, "proxy.example.com:8080", normalized.ID)
	require.Equal(t, "proxy.example.com", normalized.Provider)

	client, err := NewHTTPClient(HTTPProfile{URL: "http://proxy.example.com:8080"}, 2*time.Second)
	require.NoError(t, err)
	require.Equal(t, 2*time.Second, client.Timeout)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.Proxy)

	require.Equal(t, DefaultHTTPTimeout, HTTPTimeoutOrDefault(0))
	require.Equal(t, 3*time.Second, HTTPTimeoutOrDefault(3*time.Second))
	require.True(t, IsSOCKSProxy("socks5://proxy.example.com:1080"))
	require.False(t, IsSOCKSProxy("http://proxy.example.com:8080"))
}

func TestCrawlerHTTPTransportFacadeErrors(t *testing.T) {
	t.Parallel()

	_, err := InferHTTPProfile("://bad\x00proxy", false)
	require.Error(t, err)

	_, err = NormalizeHTTPProfile(HTTPProfile{URL: "http://"})
	require.Error(t, err)

	_, err = NewHTTPClient(HTTPProfile{URL: "://bad\x00proxy"}, time.Second)
	require.Error(t, err)
}
