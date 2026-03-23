package crawler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

var (
	errResponseBodyPanic = errors.New("crawler: response body panic")
	errNilResponseBody   = errors.New("crawler: nil http response body")
)

// NewPanicSafeTransport wraps a transport with panic recovery.
func NewPanicSafeTransport(base http.RoundTripper, logger Logger) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &panicSafeTransport{base: base, logger: ensureLogger(logger)}
}

type panicSafeTransport struct {
	base   http.RoundTripper
	logger Logger
}

func (t *panicSafeTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.logger.Error("http transport panic for %s: %v", req.URL.String(), recovered)
			resp = nil
			err = fmt.Errorf("%w: %s transport panic", errResponseBodyPanic, req.URL.String())
		}
	}()

	resp, err = t.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if resp.Body == nil {
		t.logger.Error("nil http response body for %s", req.URL.String())
		return nil, fmt.Errorf("%w: %s", errNilResponseBody, req.URL.String())
	}
	resp.Body = newPanicSafeReadCloser(resp.Body, req.URL.String(), t.logger)
	return resp, nil
}

func newPanicSafeReadCloser(body io.ReadCloser, url string, logger Logger) *panicSafeReadCloser {
	return &panicSafeReadCloser{body: body, url: url, logger: ensureLogger(logger)}
}

type panicSafeReadCloser struct {
	body     io.ReadCloser
	url      string
	logger   Logger
	panicErr error
	mu       sync.Mutex
}

func (r *panicSafeReadCloser) Read(p []byte) (n int, err error) {
	if err := r.currentError(); err != nil {
		return 0, err
	}
	if r.body == nil {
		return 0, fmt.Errorf("%w: %s", errNilResponseBody, r.url)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			n = 0
			err = r.recordPanic("read", recovered)
		}
	}()
	return r.body.Read(p)
}

func (r *panicSafeReadCloser) Close() (err error) {
	panicErr := r.currentError()
	if r.body == nil {
		if panicErr != nil {
			return panicErr
		}
		return fmt.Errorf("%w: %s", errNilResponseBody, r.url)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = r.recordPanic("close", recovered)
		}
	}()
	closeErr := r.body.Close()
	if panicErr != nil {
		return panicErr
	}
	return closeErr
}

func (r *panicSafeReadCloser) currentError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.panicErr
}

func (r *panicSafeReadCloser) recordPanic(action string, recovered interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.panicErr == nil {
		r.panicErr = fmt.Errorf("%w: %s panic for %s (%v)", errResponseBodyPanic, action, r.url, recovered)
		r.logger.Error("response body %s panic for %s: %v", action, r.url, recovered)
	}
	return r.panicErr
}
