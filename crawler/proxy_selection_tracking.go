package crawler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gocolly/colly/v2"
)

type proxySelectionTrackerContextKey struct{}

type proxySelectionTracker struct {
	requestID string
}

type proxySelectionTrackingTransport struct {
	base http.RoundTripper
}

var trackedProxySelections sync.Map
var trackedProxySelectionLinks sync.Map
var proxySelectionRequestSequence atomic.Uint64

const proxySelectionTrackingHeader = "X-Crawler-Proxy-Tracking"
const proxySelectionTrackingLinkHeader = "X-Crawler-Proxy-Tracking-Link"
const proxySelectionCollyContextKey = "crawler_proxy_selection"
const proxySelectionTrackingLinkContextKey = "crawler_proxy_selection_tracking_link"

func installProxySelectionTracking(collector *colly.Collector, transport *http.Transport, runtimeTransport http.RoundTripper) {
	if collector == nil || transport == nil || runtimeTransport == nil || transport.Proxy == nil {
		return
	}
	wrapHTTPTransportProxySelector(transport)
	collector.OnRequest(func(request *colly.Request) {
		assignProxySelectionTrackingID(request)
	})
	collector.OnError(func(response *colly.Response, _ error) {
		applyTrackedProxySelection(response)
		clearTrackedProxySelection(response.Request)
	})
	collector.OnResponse(func(response *colly.Response) {
		applyTrackedProxySelection(response)
		clearTrackedProxySelection(response.Request)
	})
	collector.WithTransport(&proxySelectionTrackingTransport{base: runtimeTransport})
}

func (transport *proxySelectionTrackingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return transport.base.RoundTrip(request)
	}
	requestID := strings.TrimSpace(request.Header.Get(proxySelectionTrackingHeader))
	linkID := strings.TrimSpace(request.Header.Get(proxySelectionTrackingLinkHeader))
	if requestID == "" && linkID == "" {
		return transport.base.RoundTrip(request)
	}
	updatedContext := request.Context()
	if requestID != "" {
		tracker := &proxySelectionTracker{requestID: requestID}
		updatedContext = context.WithValue(updatedContext, proxySelectionTrackerContextKey{}, tracker)
	}
	if requestID != "" && linkID != "" {
		trackedProxySelectionLinks.Store(linkID, requestID)
	}
	updatedRequest := request.Clone(updatedContext)
	attachStoredProxySelection(updatedRequest, requestID)
	updatedRequest.Header.Del(proxySelectionTrackingHeader)
	updatedRequest.Header.Del(proxySelectionTrackingLinkHeader)
	return transport.base.RoundTrip(updatedRequest)
}

func attachStoredProxySelection(request *http.Request, requestID string) {
	if request == nil || strings.TrimSpace(requestID) == "" {
		return
	}
	storedSelection, found := trackedProxySelections.Load(strings.TrimSpace(requestID))
	if !found {
		return
	}
	switch selection := storedSelection.(type) {
	case ProxySelection:
		AttachProxySelection(request, selection)
	case string:
		AttachProxySelection(request, ProxySelection{ProxyURL: strings.TrimSpace(selection)})
	}
}

func wrapHTTPTransportProxySelector(transport *http.Transport) {
	if transport == nil || transport.Proxy == nil {
		return
	}
	delegate := transport.Proxy
	transport.Proxy = func(request *http.Request) (*url.URL, error) {
		return trackSelectedProxyURL(request, delegate)
	}
}

func trackSelectedProxyURL(
	request *http.Request,
	delegate func(*http.Request) (*url.URL, error),
) (*url.URL, error) {
	if delegate == nil {
		return nil, nil
	}
	if request == nil {
		return nil, errors.New("crawler: cannot select proxy for nil request")
	}

	proxyURL, err := delegate(request)
	if err != nil {
		return proxyURL, err
	}
	if proxyURL == nil {
		return nil, nil
	}

	tracker, _ := request.Context().Value(proxySelectionTrackerContextKey{}).(*proxySelectionTracker)
	if tracker != nil && tracker.requestID != "" {
		selection, found := SelectedProxySelection(request)
		if !found {
			selection = ProxySelection{ProxyURL: proxyURL.String()}
		}
		trackedProxySelections.Store(tracker.requestID, selection)
	}
	return proxyURL, nil
}

func assignProxySelectionTrackingID(request *colly.Request) {
	if request == nil || request.Headers == nil {
		return
	}
	requestID := strings.TrimSpace(request.Headers.Get(proxySelectionTrackingHeader))
	if requestID == "" {
		requestID = nextProxySelectionTrackingID()
		request.Headers.Set(proxySelectionTrackingHeader, requestID)
	}
	linkID := ensureProxySelectionTrackingLinkID(request)
	if linkID != "" {
		request.Headers.Set(proxySelectionTrackingLinkHeader, linkID)
	}
}

func nextProxySelectionTrackingID() string {
	return strconv.FormatUint(proxySelectionRequestSequence.Add(1), 10)
}

func applyTrackedProxySelection(response *colly.Response) {
	if response == nil || response.Request == nil {
		return
	}
	requestID := requestProxySelectionTrackingID(response.Request)
	if requestID == "" {
		return
	}
	defer clearTrackedProxySelection(response.Request)
	proxyValue, ok := trackedProxySelections.Load(requestID)
	if !ok {
		return
	}
	switch trackedSelection := proxyValue.(type) {
	case ProxySelection:
		if strings.TrimSpace(trackedSelection.ProxyURL) == "" {
			return
		}
		if response.Request.ProxyURL == "" {
			response.Request.ProxyURL = trackedSelection.ProxyURL
		}
		attachTrackedProxySelection(response.Request, trackedSelection)
	case string:
		proxyURL := strings.TrimSpace(trackedSelection)
		if proxyURL == "" {
			return
		}
		if response.Request.ProxyURL == "" {
			response.Request.ProxyURL = proxyURL
		}
	}
}

func clearTrackedProxySelection(request *colly.Request) {
	requestID := requestProxySelectionTrackingID(request)
	linkID := requestProxySelectionTrackingLinkID(request)
	if requestID != "" {
		trackedProxySelections.Delete(requestID)
	}
	if linkID != "" {
		trackedProxySelectionLinks.Delete(linkID)
		if request != nil && request.Ctx != nil {
			request.Ctx.Put(proxySelectionTrackingLinkContextKey, "")
		}
	}
	if request == nil || request.Headers == nil {
		return
	}
	request.Headers.Del(proxySelectionTrackingHeader)
	request.Headers.Del(proxySelectionTrackingLinkHeader)
}

func requestProxySelectionTrackingID(request *colly.Request) string {
	if request == nil {
		return ""
	}
	if request.Headers != nil {
		requestID := strings.TrimSpace(request.Headers.Get(proxySelectionTrackingHeader))
		if requestID != "" {
			return requestID
		}
	}
	linkID := requestProxySelectionTrackingLinkID(request)
	if linkID == "" {
		return ""
	}
	trackedRequestID, ok := trackedProxySelectionLinks.Load(linkID)
	if !ok {
		return ""
	}
	requestID, ok := trackedRequestID.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(requestID)
}

func requestProxySelectionTrackingLinkID(request *colly.Request) string {
	if request == nil {
		return ""
	}
	if request.Ctx != nil {
		linkID := strings.TrimSpace(request.Ctx.Get(proxySelectionTrackingLinkContextKey))
		if linkID != "" {
			return linkID
		}
	}
	if request.Headers == nil {
		return ""
	}
	return strings.TrimSpace(request.Headers.Get(proxySelectionTrackingLinkHeader))
}

func ensureProxySelectionTrackingLinkID(request *colly.Request) string {
	if request == nil {
		return ""
	}
	if request.Ctx == nil {
		request.Ctx = colly.NewContext()
	}
	linkID := strings.TrimSpace(request.Ctx.Get(proxySelectionTrackingLinkContextKey))
	if linkID != "" {
		return linkID
	}
	linkID = nextProxySelectionTrackingID()
	request.Ctx.Put(proxySelectionTrackingLinkContextKey, linkID)
	return linkID
}

func attachTrackedProxySelection(request *colly.Request, selection ProxySelection) {
	if request == nil {
		return
	}
	if request.Ctx == nil {
		request.Ctx = colly.NewContext()
	}
	request.Ctx.Put(proxySelectionCollyContextKey, selection)
}

func selectedProxySelectionFromCollyRequest(request *colly.Request) (ProxySelection, bool) {
	if request == nil || request.Ctx == nil {
		return ProxySelection{}, false
	}
	selection, ok := request.Ctx.GetAny(proxySelectionCollyContextKey).(ProxySelection)
	if !ok || strings.TrimSpace(selection.ProxyURL) == "" {
		return ProxySelection{}, false
	}
	return selection, true
}
