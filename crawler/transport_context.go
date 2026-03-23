package crawler

import (
	"context"
	"net/http"

	"github.com/gocolly/colly/v2"
)

func newContextAwareTransport(base http.RoundTripper, ctxProvider func() context.Context) http.RoundTripper {
	effectiveBase := base
	if effectiveBase == nil {
		effectiveBase = http.DefaultTransport
	}
	return &contextAwareTransport{
		base:       effectiveBase,
		ctxFactory: ctxProvider,
	}
}

type contextAwareTransport struct {
	base       http.RoundTripper
	ctxFactory func() context.Context
}

func (transport *contextAwareTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	requestCtx := req.Context()
	var cleanup func()

	if transport.ctxFactory != nil {
		if runCtx := transport.ctxFactory(); runCtx != nil {
			if err := runCtx.Err(); err != nil {
				return nil, err
			}
			if done := runCtx.Done(); done != nil {
				requestCtx, cancel := context.WithCancel(requestCtx)
				stop := make(chan struct{})
				go func(parent context.Context, notify <-chan struct{}, cancel context.CancelFunc, reqCtx context.Context) {
					select {
					case <-parent.Done():
						cancel()
					case <-notify:
					case <-reqCtx.Done():
					}
				}(runCtx, stop, cancel, requestCtx)
				cleanup = func() {
					close(stop)
					cancel()
				}
			}
		}
	}

	if cleanup != nil {
		defer cleanup()
	}

	updatedRequest := req.WithContext(requestCtx)
	response, err := transport.base.RoundTrip(updatedRequest)
	propagateProxyURLContext(req, updatedRequest)
	return response, err
}

func propagateProxyURLContext(originalRequest *http.Request, updatedRequest *http.Request) {
	if originalRequest == nil || updatedRequest == nil {
		return
	}
	proxyURL, ok := updatedRequest.Context().Value(colly.ProxyURLKey).(string)
	if !ok || proxyURL == "" {
		return
	}
	if existingProxyURL, ok := originalRequest.Context().Value(colly.ProxyURLKey).(string); ok && existingProxyURL == proxyURL {
		return
	}
	*originalRequest = *originalRequest.WithContext(context.WithValue(originalRequest.Context(), colly.ProxyURLKey, proxyURL))
}
