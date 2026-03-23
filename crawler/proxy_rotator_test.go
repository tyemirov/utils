package crawler

import (
	"net/http"
	"testing"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/require"
)

func TestNewProxyRotatorReturnsRoundRobinFunction(t *testing.T) {
	raw := []string{
		"http://user:pass@proxy-one.test:8080",
		"http://proxy-two.test:9000",
	}

	proxyFn, err := newProxyRotator(raw, newProxyHealthTracker(raw, nil), nil)
	require.NoError(t, err)
	require.NotNil(t, proxyFn)

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	first, err := proxyFn(req)
	require.NoError(t, err)
	require.Equal(t, "proxy-one.test:8080", first.Host)
	require.Equal(t, "http://user:pass@proxy-one.test:8080", req.Context().Value(colly.ProxyURLKey))

	second, err := proxyFn(req)
	require.NoError(t, err)
	require.Equal(t, "proxy-two.test:9000", second.Host)
	require.Equal(t, "http://proxy-two.test:9000", req.Context().Value(colly.ProxyURLKey))

	third, err := proxyFn(req)
	require.NoError(t, err)
	require.Equal(t, "proxy-one.test:8080", third.Host)
	require.Equal(t, "http://user:pass@proxy-one.test:8080", req.Context().Value(colly.ProxyURLKey))
}

func TestNewProxyRotatorValidatesInput(t *testing.T) {
	tracker := newProxyHealthTracker(nil, nil)
	_, err := newProxyRotator(nil, tracker, nil)
	require.Error(t, err)

	_, err = newProxyRotator([]string{""}, tracker, nil)
	require.Error(t, err)

	_, err = newProxyRotator([]string{"http://good.one:8080", "://bad"}, tracker, nil)
	require.Error(t, err)
}

func TestProxyRotatorSkipsCoolingProxies(t *testing.T) {
	raw := []string{
		"http://proxy-one.test:8080",
		"http://proxy-two.test:8080",
	}
	tracker := newProxyHealthTracker(raw, nil)
	tracker.now = func() time.Time { return time.Unix(0, 0) }
	proxyFn, err := newProxyRotator(raw, tracker, nil)
	require.NoError(t, err)
	require.NotNil(t, proxyFn)

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	first, err := proxyFn(req)
	require.NoError(t, err)
	require.Equal(t, "proxy-one.test:8080", first.Host)

	tracker.RecordFailure(first.String())
	tracker.RecordFailure(first.String())
	tracker.RecordFailure(first.String())
	tracker.RecordFailure(first.String())
	tracker.RecordFailure(first.String())

	second, err := proxyFn(req)
	require.NoError(t, err)
	require.Equal(t, "proxy-two.test:8080", second.Host)
}
