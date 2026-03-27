package billing

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPRetryWaitErrorUnwrapReturnsWrappedError(t *testing.T) {
	inner := errors.New("connection refused")
	retryErr := &httpRetryWaitError{StatusCode: 429, Err: inner}
	require.Equal(t, inner, retryErr.Unwrap())
}

func TestHTTPRetryWaitErrorUnwrapNilReturnsNil(t *testing.T) {
	var retryErr *httpRetryWaitError
	require.Nil(t, retryErr.Unwrap())
}

func TestHTTPRetryWaitErrorErrorWithZeroStatusCode(t *testing.T) {
	inner := errors.New("network error")
	retryErr := &httpRetryWaitError{StatusCode: 0, Err: inner}
	require.Equal(t, "network error", retryErr.Error())
}

func TestHTTPRetryWaitErrorErrorNilErr(t *testing.T) {
	var retryErr *httpRetryWaitError
	require.Equal(t, "", retryErr.Error())

	retryErr2 := &httpRetryWaitError{StatusCode: 500, Err: nil}
	require.Equal(t, "", retryErr2.Error())
}

func TestParseHTTPRetryAfterDateFormat(t *testing.T) {
	now := time.Date(2026, time.March, 26, 12, 0, 0, 0, time.UTC)
	futureDate := now.Add(60 * time.Second)
	header := futureDate.UTC().Format(http.TimeFormat)

	duration, ok := parseHTTPRetryAfter(header, now)
	require.True(t, ok)
	require.InDelta(t, 60.0, duration.Seconds(), 1.0)
}

func TestParseHTTPRetryAfterInvalidHeader(t *testing.T) {
	_, ok := parseHTTPRetryAfter("", time.Now())
	require.False(t, ok)

	_, ok = parseHTTPRetryAfter("not-a-number-or-date", time.Now())
	require.False(t, ok)
}

func TestDoHTTPRequestWithRetryRetriesOnConnectionError(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestCount++
		_, writeErr := responseWriter.Write([]byte(`ok`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	callCount := 0
	resp, err := doHTTPRequestWithRetry(
		server.Client(),
		func() (*http.Request, error) {
			callCount++
			if callCount == 1 {
				req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:1/bad", nil)
				return req, nil
			}
			return http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
		},
		httpRetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 1, requestCount)
	_ = resp.Body.Close()
}

func TestCanRetryHTTPRequestReturnsFalseForPOST(t *testing.T) {
	require.False(t, canRetryHTTPRequest(http.MethodPost, 0, http.StatusTooManyRequests, 4))
	require.False(t, canRetryHTTPRequest(http.MethodPost, 0, 0, 4))
}

func TestCanRetryHTTPRequestReturnsTrueForRetryableConditions(t *testing.T) {
	require.True(t, canRetryHTTPRequest(http.MethodGet, 0, http.StatusTooManyRequests, 4))
	require.True(t, canRetryHTTPRequest(http.MethodGet, 0, http.StatusInternalServerError, 4))
	require.True(t, canRetryHTTPRequest(http.MethodGet, 0, 0, 4))
	require.False(t, canRetryHTTPRequest(http.MethodGet, 3, 0, 4))
	require.False(t, canRetryHTTPRequest(http.MethodGet, 0, http.StatusBadRequest, 4))
}

func TestWaitForHTTPRetryContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForHTTPRetry(ctx, "", 0, httpRetryConfig{
		MaxAttempts: 4,
		BaseDelay:   1 * time.Hour,
		MaxDelay:    1 * time.Hour,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRetryWaitStatusCodeExtractsCode(t *testing.T) {
	retryErr := &httpRetryWaitError{StatusCode: 429, Err: errors.New("rate limited")}
	code, ok := retryWaitStatusCode(retryErr)
	require.True(t, ok)
	require.Equal(t, 429, code)

	_, ok = retryWaitStatusCode(errors.New("plain error"))
	require.False(t, ok)
}

// Coverage gap tests for doHTTPRequestWithRetry and waitForHTTPRetry

func TestDoHTTPRequestWithRetryRequestFactoryError(t *testing.T) {
	resp, err := doHTTPRequestWithRetry(
		http.DefaultClient,
		func() (*http.Request, error) {
			return nil, errors.New("factory error")
		},
		httpRetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)
	require.Nil(t, resp)
	require.Error(t, err)
	require.Contains(t, err.Error(), "factory error")
}

func TestDoHTTPRequestWithRetryExhaustsAllAttempts(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		callCount++
		responseWriter.WriteHeader(http.StatusInternalServerError)
		_, _ = responseWriter.Write([]byte(`error`))
	}))
	defer server.Close()

	resp, err := doHTTPRequestWithRetry(
		server.Client(),
		func() (*http.Request, error) {
			return http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
		},
		httpRetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)
	// On the last attempt, the non-retryable status returns the response
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	_ = resp.Body.Close()
	require.Equal(t, 2, callCount)
}

func TestDoHTTPRequestWithRetryNonRetryableGetsNormalResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.WriteHeader(http.StatusOK)
		_, _ = responseWriter.Write([]byte(`ok`))
	}))
	defer server.Close()

	resp, err := doHTTPRequestWithRetry(
		server.Client(),
		func() (*http.Request, error) {
			return http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
		},
		httpRetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()
}

func TestDoHTTPRequestWithRetryContextCancelDuringWait(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		callCount++
		responseWriter.WriteHeader(http.StatusTooManyRequests)
		_, _ = responseWriter.Write([]byte(`rate limited`))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := doHTTPRequestWithRetry(
		server.Client(),
		func() (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		},
		httpRetryConfig{MaxAttempts: 3, BaseDelay: time.Hour, MaxDelay: time.Hour},
	)
	require.Error(t, err)
}

func TestDoHTTPRequestWithRetryConnectionErrorNonRetryableMethod(t *testing.T) {
	// POST is non-retryable, so connection errors should fail immediately.
	// Use a server that is immediately closed so connection fails fast.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	serverURL := server.URL
	server.Close() // close immediately so connections are refused

	_, err := doHTTPRequestWithRetry(
		&http.Client{Timeout: 1 * time.Second},
		func() (*http.Request, error) {
			return http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL, nil)
		},
		httpRetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)
	require.Error(t, err)
}

func TestWaitForHTTPRetryMaxDelayClamp(t *testing.T) {
	start := time.Now()
	err := waitForHTTPRetry(context.Background(), "", 10, httpRetryConfig{
		MaxAttempts: 20,
		BaseDelay:   time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
	})
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Less(t, elapsed, 50*time.Millisecond)
}

func TestWaitForHTTPRetryWithRetryAfterHeader(t *testing.T) {
	start := time.Now()
	err := waitForHTTPRetry(context.Background(), "0", 0, httpRetryConfig{
		MaxAttempts: 4,
		BaseDelay:   time.Hour,
		MaxDelay:    time.Hour,
	})
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Less(t, elapsed, 50*time.Millisecond)
}

func TestWaitForHTTPRetryNegativeDelay(t *testing.T) {
	// Use a date far in the past to get a negative retry delay
	pastDate := time.Now().Add(-10 * time.Minute).UTC().Format(http.TimeFormat)
	start := time.Now()
	err := waitForHTTPRetry(context.Background(), pastDate, 0, httpRetryConfig{
		MaxAttempts: 4,
		BaseDelay:   time.Hour,
		MaxDelay:    time.Hour,
	})
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Less(t, elapsed, 50*time.Millisecond)
}

func TestDoHTTPRequestWithRetryDrainsBodyOnRetryableStatus(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		callCount++
		if callCount == 1 {
			responseWriter.WriteHeader(http.StatusInternalServerError)
			_, _ = responseWriter.Write([]byte(`server error body to drain`))
			return
		}
		responseWriter.WriteHeader(http.StatusOK)
		_, _ = responseWriter.Write([]byte(`success`))
	}))
	defer server.Close()

	resp, err := doHTTPRequestWithRetry(
		server.Client(),
		func() (*http.Request, error) {
			return http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
		},
		httpRetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
	require.Equal(t, 2, callCount)
}

// errorReadCloser is a fake io.ReadCloser used to simulate body read/close errors.
type errorReadCloser struct {
	readErr  error
	closeErr error
	readDone bool
}

func (e *errorReadCloser) Read(p []byte) (int, error) {
	if e.readErr != nil {
		return 0, e.readErr
	}
	if !e.readDone {
		e.readDone = true
		return 0, nil // return 0 bytes then EOF on next call
	}
	return 0, io.EOF
}

func (e *errorReadCloser) Close() error {
	return e.closeErr
}

// roundTripFunc lets tests replace http.Client transport with a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDoHTTPRequestWithRetryBodyDrainErrorReturnsError(t *testing.T) {
	drainErr := errors.New("drain read error")
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     make(http.Header),
				Body:       &errorReadCloser{readErr: drainErr},
			}, nil
		}),
	}

	resp, err := doHTTPRequestWithRetry(
		client,
		func() (*http.Request, error) {
			return http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
		},
		httpRetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)
	require.Nil(t, resp)
	require.ErrorIs(t, err, drainErr)
}

func TestDoHTTPRequestWithRetryBodyCloseErrorReturnsError(t *testing.T) {
	closeErr := errors.New("body close error")
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     make(http.Header),
				// readErr nil so io.Copy succeeds (reads EOF immediately), closeErr set
				Body: &errorReadCloser{readErr: nil, closeErr: closeErr, readDone: true},
			}, nil
		}),
	}

	resp, err := doHTTPRequestWithRetry(
		client,
		func() (*http.Request, error) {
			return http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
		},
		httpRetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)
	require.Nil(t, resp)
	require.ErrorIs(t, err, closeErr)
}

func TestDoHTTPRequestWithRetryZeroMaxAttemptsReturnsExhaustedError(t *testing.T) {
	resp, err := doHTTPRequestWithRetry(
		http.DefaultClient,
		func() (*http.Request, error) {
			return http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
		},
		httpRetryConfig{MaxAttempts: 0, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)
	require.Nil(t, resp)
	require.ErrorIs(t, err, ErrHTTPRetryAttemptsExhausted)
}

func TestDoHTTPRequestWithRetryDrainsThenRetriesSuccessfully(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		callCount++
		if callCount <= 2 {
			responseWriter.WriteHeader(http.StatusTooManyRequests)
			_, _ = responseWriter.Write([]byte(`rate limited`))
			return
		}
		responseWriter.WriteHeader(http.StatusOK)
		_, _ = responseWriter.Write([]byte(`ok`))
	}))
	defer server.Close()

	resp, err := doHTTPRequestWithRetry(
		server.Client(),
		func() (*http.Request, error) {
			return http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
		},
		httpRetryConfig{MaxAttempts: 4, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
	require.Equal(t, 3, callCount)
}
