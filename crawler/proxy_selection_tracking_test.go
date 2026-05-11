package crawler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/require"
)

type proxyTrackingRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn proxyTrackingRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestInstallProxySelectionTrackingAnnotatesCollyResponses(t *testing.T) {
	selector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{{
		Name: "provider-one",
		Users: []ProxyRotationUserConfig{{
			Name: "user-one",
			URL:  "http://provider-one.example:8080",
		}},
	}})
	require.NoError(t, err)

	transport := &http.Transport{Proxy: selector.Select}
	runtimeTransport := proxyTrackingRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		proxyURL, proxyErr := transport.Proxy(request)
		require.NoError(t, proxyErr)
		require.Equal(t, "http://provider-one.example:8080", proxyURL.String())
		require.Empty(t, request.Header.Get(proxySelectionTrackingHeader))
		require.Empty(t, request.Header.Get(proxySelectionTrackingLinkHeader))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader("<html><head><title>ok</title></head></html>")),
			Request:    request,
		}, nil
	})
	collector := colly.NewCollector(colly.AllowURLRevisit())
	installProxySelectionTracking(collector, transport, runtimeTransport)

	var responseSelection ProxySelection
	var foundSelection bool
	collector.OnResponse(func(response *colly.Response) {
		responseSelection, foundSelection = selectedProxySelectionFromResponse(response)
	})

	err = collector.Request(http.MethodGet, "http://example.com/product", nil, colly.NewContext(), nil)
	require.NoError(t, err)
	collector.Wait()

	require.True(t, foundSelection)
	require.Equal(t, "provider-one", responseSelection.ProviderName)
	require.Equal(t, "user-one", responseSelection.UserName)
	require.Equal(t, "http://provider-one.example:8080", responseSelection.ProxyURL)
}

func TestInstallProxySelectionTrackingAnnotatesCollyErrors(t *testing.T) {
	selector, err := NewProxyLeaseSelector([]ProxyRotationProviderConfig{{
		Name: "provider-one",
		Users: []ProxyRotationUserConfig{{
			Name: "user-one",
			URL:  "http://provider-one.example:8080",
		}},
	}})
	require.NoError(t, err)

	transport := &http.Transport{Proxy: selector.Select}
	runtimeTransport := proxyTrackingRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		_, proxyErr := transport.Proxy(request)
		require.NoError(t, proxyErr)
		return nil, errors.New("request failed")
	})
	collector := colly.NewCollector(colly.AllowURLRevisit())
	installProxySelectionTracking(collector, transport, runtimeTransport)

	var errorSelection ProxySelection
	var foundSelection bool
	collector.OnError(func(response *colly.Response, _ error) {
		errorSelection, foundSelection = selectedProxySelectionFromResponse(response)
	})

	err = collector.Request(http.MethodGet, "http://example.com/product", nil, colly.NewContext(), nil)
	require.ErrorContains(t, err, "request failed")
	collector.Wait()

	require.True(t, foundSelection)
	require.Equal(t, "provider-one", errorSelection.ProviderName)
	require.Equal(t, "http://provider-one.example:8080", errorSelection.ProxyURL)
}

func TestProxySelectionTrackingBoundaryBranches(t *testing.T) {
	installProxySelectionTracking(nil, nil, nil)
	installProxySelectionTracking(colly.NewCollector(), nil, proxyTrackingRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	}))
	installProxySelectionTracking(colly.NewCollector(), &http.Transport{}, nil)
	installProxySelectionTracking(colly.NewCollector(), &http.Transport{}, proxyTrackingRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	}))

	transport := &proxySelectionTrackingTransport{base: proxyTrackingRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request != nil {
			require.Empty(t, request.Header.Get(proxySelectionTrackingHeader))
			require.Empty(t, request.Header.Get(proxySelectionTrackingLinkHeader))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    request,
		}, nil
	})}

	response, err := transport.RoundTrip(nil)
	require.NoError(t, err)
	require.Nil(t, response.Request)

	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	response, err = transport.RoundTrip(request)
	require.NoError(t, err)
	require.Same(t, request, response.Request)

	request.Header.Set(proxySelectionTrackingHeader, "request-one")
	request.Header.Set(proxySelectionTrackingLinkHeader, "link-one")
	response, err = transport.RoundTrip(request)
	require.NoError(t, err)
	require.NotSame(t, request, response.Request)
	require.Equal(t, "request-one", response.Request.Context().Value(proxySelectionTrackerContextKey{}).(*proxySelectionTracker).requestID)
	trackedRequestID, found := trackedProxySelectionLinks.Load("link-one")
	require.True(t, found)
	require.Equal(t, "request-one", trackedRequestID)
}

func TestTrackSelectedProxyURLBoundaryBranches(t *testing.T) {
	wrapHTTPTransportProxySelector(nil)
	wrapHTTPTransportProxySelector(&http.Transport{})

	proxyURL, err := trackSelectedProxyURL(nil, nil)
	require.NoError(t, err)
	require.Nil(t, proxyURL)

	proxyURL, err = trackSelectedProxyURL(nil, func(*http.Request) (*url.URL, error) {
		return nil, nil
	})
	require.ErrorContains(t, err, "nil request")
	require.Nil(t, proxyURL)

	expectedErr := errors.New("select failed")
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	proxyURL, err = trackSelectedProxyURL(request, func(*http.Request) (*url.URL, error) {
		parsedProxyURL, parseErr := url.Parse("http://proxy.example:8080")
		require.NoError(t, parseErr)
		return parsedProxyURL, expectedErr
	})
	require.ErrorIs(t, err, expectedErr)
	require.Equal(t, "http://proxy.example:8080", proxyURL.String())

	proxyURL, err = trackSelectedProxyURL(request, func(*http.Request) (*url.URL, error) {
		return nil, nil
	})
	require.NoError(t, err)
	require.Nil(t, proxyURL)

	proxyURL, err = trackSelectedProxyURL(request, func(*http.Request) (*url.URL, error) {
		return url.Parse("http://proxy.example:8080")
	})
	require.NoError(t, err)
	require.Equal(t, "http://proxy.example:8080", proxyURL.String())

	trackedRequest, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	trackedRequest = trackedRequest.WithContext(context.WithValue(
		trackedRequest.Context(),
		proxySelectionTrackerContextKey{},
		&proxySelectionTracker{requestID: "fallback-selection"},
	))
	proxyURL, err = trackSelectedProxyURL(trackedRequest, func(*http.Request) (*url.URL, error) {
		return url.Parse("http://fallback-proxy.example:8080")
	})
	require.NoError(t, err)
	require.Equal(t, "http://fallback-proxy.example:8080", proxyURL.String())
	storedSelection, found := trackedProxySelections.Load("fallback-selection")
	require.True(t, found)
	require.Equal(t, "http://fallback-proxy.example:8080", storedSelection.(ProxySelection).ProxyURL)
}

func TestApplyTrackedProxySelectionBoundaryBranches(t *testing.T) {
	applyTrackedProxySelection(nil)
	applyTrackedProxySelection(&colly.Response{})

	headers := http.Header{}
	request := &colly.Request{Headers: &headers, Ctx: colly.NewContext()}
	applyTrackedProxySelection(&colly.Response{Request: request})

	headers.Set(proxySelectionTrackingHeader, "missing")
	applyTrackedProxySelection(&colly.Response{Request: request})
	require.Empty(t, request.ProxyURL)

	trackedProxySelections.Store("empty-selection", ProxySelection{})
	headers.Set(proxySelectionTrackingHeader, "empty-selection")
	applyTrackedProxySelection(&colly.Response{Request: request})
	require.Empty(t, request.ProxyURL)

	trackedProxySelections.Store("empty-string", "")
	headers.Set(proxySelectionTrackingHeader, "empty-string")
	applyTrackedProxySelection(&colly.Response{Request: request})
	require.Empty(t, request.ProxyURL)

	trackedProxySelections.Store("string-selection", "http://string-proxy.example:8080")
	headers.Set(proxySelectionTrackingHeader, "string-selection")
	applyTrackedProxySelection(&colly.Response{Request: request})
	require.Equal(t, "http://string-proxy.example:8080", request.ProxyURL)

	request.ProxyURL = "http://existing-proxy.example:8080"
	trackedProxySelections.Store("lease-selection", ProxySelection{
		ProviderName: "provider-one",
		UserName:     "user-one",
		ProxyURL:     "http://lease-proxy.example:8080",
	})
	headers.Set(proxySelectionTrackingHeader, "lease-selection")
	applyTrackedProxySelection(&colly.Response{Request: request})
	require.Equal(t, "http://existing-proxy.example:8080", request.ProxyURL)
	selection, found := selectedProxySelectionFromCollyRequest(request)
	require.True(t, found)
	require.Equal(t, "http://lease-proxy.example:8080", selection.ProxyURL)
}

func TestProxySelectionTrackingIDAndLinkBranches(t *testing.T) {
	assignProxySelectionTrackingID(nil)
	assignProxySelectionTrackingID(&colly.Request{})

	headers := http.Header{}
	request := &colly.Request{Headers: &headers}
	assignProxySelectionTrackingID(request)
	require.NotEmpty(t, headers.Get(proxySelectionTrackingHeader))
	require.NotEmpty(t, headers.Get(proxySelectionTrackingLinkHeader))
	require.NotEmpty(t, request.Ctx.Get(proxySelectionTrackingLinkContextKey))

	linkID := request.Ctx.Get(proxySelectionTrackingLinkContextKey)
	headers.Del(proxySelectionTrackingHeader)
	trackedProxySelectionLinks.Store(linkID, 42)
	require.Empty(t, requestProxySelectionTrackingID(request))

	trackedProxySelectionLinks.Store(linkID, " linked-request ")
	require.Equal(t, "linked-request", requestProxySelectionTrackingID(request))
	require.Equal(t, linkID, requestProxySelectionTrackingLinkID(request))

	clearTrackedProxySelection(request)
	require.Empty(t, request.Ctx.Get(proxySelectionTrackingLinkContextKey))
	require.Empty(t, headers.Get(proxySelectionTrackingHeader))
	require.Empty(t, headers.Get(proxySelectionTrackingLinkHeader))
	clearTrackedProxySelection(nil)

	require.Empty(t, requestProxySelectionTrackingID(nil))
	require.Empty(t, requestProxySelectionTrackingLinkID(nil))
	require.Empty(t, requestProxySelectionTrackingLinkID(&colly.Request{}))
	require.Empty(t, ensureProxySelectionTrackingLinkID(nil))

	requestWithoutHeaders := &colly.Request{Ctx: colly.NewContext()}
	requestWithoutHeaders.Ctx.Put(proxySelectionTrackingLinkContextKey, "ctx-link")
	require.Empty(t, requestProxySelectionTrackingID(requestWithoutHeaders))
	trackedProxySelectionLinks.Store("ctx-link", "ctx-request")
	require.Equal(t, "ctx-request", requestProxySelectionTrackingID(requestWithoutHeaders))

	require.Equal(t, "ctx-link", ensureProxySelectionTrackingLinkID(requestWithoutHeaders))

	directHeaderRequest := &colly.Request{Headers: &http.Header{}}
	directHeaderRequest.Headers.Set(proxySelectionTrackingHeader, "direct-request")
	require.Equal(t, "direct-request", requestProxySelectionTrackingID(directHeaderRequest))

	require.Empty(t, requestProxySelectionTrackingID(&colly.Request{}))
}

func TestAttachAndReadTrackedProxySelectionBoundaryBranches(t *testing.T) {
	attachTrackedProxySelection(nil, ProxySelection{ProxyURL: "http://proxy.example:8080"})
	request := &colly.Request{}
	attachTrackedProxySelection(request, ProxySelection{ProxyURL: "http://proxy.example:8080"})
	selection, found := selectedProxySelectionFromCollyRequest(request)
	require.True(t, found)
	require.Equal(t, "http://proxy.example:8080", selection.ProxyURL)

	selection, found = selectedProxySelectionFromCollyRequest(nil)
	require.False(t, found)
	require.Empty(t, selection)
	selection, found = selectedProxySelectionFromCollyRequest(&colly.Request{Ctx: colly.NewContext()})
	require.False(t, found)
	require.Empty(t, selection)
	selection, found = selectedProxySelectionFromResponse(nil)
	require.False(t, found)
	require.Empty(t, selection)
}
