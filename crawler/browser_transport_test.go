package crawler

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCrawlerBrowserTransportFacade(t *testing.T) {
	t.Parallel()

	directProfile, err := InferBrowserProfile("", true)
	require.NoError(t, err)
	require.Equal(t, BrowserModeDirect, directProfile.Mode)
	require.True(t, directProfile.IgnoreCertErrors)

	proxyProfile, err := InferBrowserProfile(" http://user:pass@proxy.example.com:8080 ", false)
	require.NoError(t, err)
	require.Equal(t, BrowserModeHTTPFetchAuth, proxyProfile.Mode)
	require.Equal(t, "http://user:pass@proxy.example.com:8080", proxyProfile.URL)

	userAgent := DefaultBrowserUserAgent("/path/to/missing/chrome")
	require.True(t, strings.Contains(userAgent, "Chrome/"))
	require.Empty(t, DetectBrowserChromeVersion("/path/to/missing/chrome"))
	require.NotEmpty(t, strings.TrimSpace(DefaultBrowserStealthScript))
}

func TestCrawlerBrowserTransportFacadeErrors(t *testing.T) {
	t.Parallel()

	_, err := InferBrowserProfile("://bad\x00proxy", false)
	require.Error(t, err)

	_, err = NewBrowserSession(context.Background(), BrowserProfile{Mode: BrowserMode("unsupported")}, BrowserLaunchOptions{})
	require.Error(t, err)

	_, err = RenderBrowserPage(context.Background(), "https://example.com", BrowserConfig{ProxyURL: "://bad\x00proxy"})
	require.Error(t, err)

	results, errorsByIndex := RenderBrowserPages(context.Background(), []string{"https://example.com"}, BrowserConfig{ProxyURL: "://bad\x00proxy"})
	require.Len(t, results, 1)
	require.Nil(t, results[0])
	require.Len(t, errorsByIndex, 1)
	require.Error(t, errorsByIndex[0])
}
