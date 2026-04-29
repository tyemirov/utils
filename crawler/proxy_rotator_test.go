package crawler

import (
	"net/http"
	"testing"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/require"
)

func TestNewProxyRotatorReturnsRoundRobinFunctionWithoutHealthTracker(t *testing.T) {
	raw := []string{
		"http://user:pass@proxy-one.test:8080",
		"http://proxy-two.test:9000",
	}

	proxyFn, err := newProxyRotator(raw, nil, nil)
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

func TestProxyRotatorKeepsSuccessfulProxyStickyUntilFailure(t *testing.T) {
	const (
		firstProxyURL  = "http://proxy-one.test:8080"
		secondProxyURL = "http://proxy-two.test:8080"
		thirdProxyURL  = "http://proxy-three.test:8080"
	)
	raw := []string{firstProxyURL, secondProxyURL, thirdProxyURL}
	tracker := newProxyHealthTracker(raw, nil)
	tracker.now = func() time.Time { return time.Unix(0, 0) }
	proxyFn, err := newProxyRotator(raw, tracker, nil)
	require.NoError(t, err)
	require.NotNil(t, proxyFn)

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	first, err := proxyFn(req)
	require.NoError(t, err)
	require.Equal(t, firstProxyURL, first.String())
	require.Equal(t, firstProxyURL, req.Context().Value(colly.ProxyURLKey))

	tracker.RecordCriticalFailure(first.String())

	second, err := proxyFn(req)
	require.NoError(t, err)
	require.Equal(t, secondProxyURL, second.String())
	require.Equal(t, secondProxyURL, req.Context().Value(colly.ProxyURLKey))

	tracker.RecordSuccess(second.String())

	third, err := proxyFn(req)
	require.NoError(t, err)
	require.Equal(t, secondProxyURL, third.String())
	require.Equal(t, secondProxyURL, req.Context().Value(colly.ProxyURLKey))

	fourth, err := proxyFn(req)
	require.NoError(t, err)
	require.Equal(t, secondProxyURL, fourth.String())
	require.Equal(t, secondProxyURL, req.Context().Value(colly.ProxyURLKey))

	tracker.RecordCriticalFailure(fourth.String())

	fifth, err := proxyFn(req)
	require.NoError(t, err)
	require.Equal(t, thirdProxyURL, fifth.String())
	require.Equal(t, thirdProxyURL, req.Context().Value(colly.ProxyURLKey))

	tracker.RecordSuccess(fifth.String())

	sixth, err := proxyFn(req)
	require.NoError(t, err)
	require.Equal(t, thirdProxyURL, sixth.String())
	require.Equal(t, thirdProxyURL, req.Context().Value(colly.ProxyURLKey))
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
