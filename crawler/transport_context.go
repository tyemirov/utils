package crawler

import (
	"context"
	"net/http"

	"github.com/gocolly/colly/v2"
)

// NewContextAwareTransport wraps a transport with run-context cancellation.
func NewContextAwareTransport(base http.RoundTripper, ctxProvider func() context.Context) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &contextAwareTransport{base: base, ctxFactory: ctxProvider}
}

type contextAwareTransport struct {
	base       http.RoundTripper
	ctxFactory func() context.Context
}

func (t *contextAwareTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	requestCtx := req.Context()
	var cleanup func()

	if t.ctxFactory != nil {
		if runCtx := t.ctxFactory(); runCtx != nil {
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

	updated := req.WithContext(requestCtx)
	response, err := t.base.RoundTrip(updated)
	propagateProxyURLContext(req, updated)
	return response, err
}

func propagateProxyURLContext(original, updated *http.Request) {
	if original == nil || updated == nil {
		return
	}
	proxyURL, ok := updated.Context().Value(colly.ProxyURLKey).(string)
	if !ok || proxyURL == "" {
		return
	}
	if existing, ok := original.Context().Value(colly.ProxyURLKey).(string); ok && existing == proxyURL {
		return
	}
	*original = *original.WithContext(context.WithValue(original.Context(), colly.ProxyURLKey, proxyURL))
}
