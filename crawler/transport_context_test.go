package crawler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/require"
)

func TestContextAwareTransportPropagatesContext(t *testing.T) {
	t.Parallel()

	reqCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	reqCtx = context.WithValue(reqCtx, contextKey("request-key"), "request-context")
	runCtx := context.WithValue(context.Background(), contextKey("run-key"), "run-context")

	transport := newContextAwareTransport(contextRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		value := req.Context().Value(contextKey("request-key"))
		require.Equal(t, "request-context", value)
		deadline, ok := req.Context().Deadline()
		require.True(t, ok)
		expectedDeadline, _ := reqCtx.Deadline()
		require.WithinDuration(t, expectedDeadline, deadline, time.Second)
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       http.NoBody,
			Request:    req,
		}, nil
	}), func() context.Context {
		return runCtx
	})

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)
	req = req.WithContext(reqCtx)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestContextAwareTransportHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	transport := newContextAwareTransport(http.DefaultTransport, func() context.Context {
		return ctx
	})

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	require.ErrorIs(t, err, context.Canceled)
}

func TestContextAwareTransportPreservesSelectedProxyURL(t *testing.T) {
	t.Parallel()

	transport := newContextAwareTransport(contextRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		attachProxyURL(req, "http://proxy-one.test:8080")
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       http.NoBody,
			Request:    req,
		}, nil
	}), nil)

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "http://proxy-one.test:8080", req.Context().Value(colly.ProxyURLKey))
}

type contextRoundTripFunc func(req *http.Request) (*http.Response, error)

func (fn contextRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type contextKey string
