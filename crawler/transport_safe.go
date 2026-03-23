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

func newPanicSafeTransport(base http.RoundTripper, logger Logger) http.RoundTripper {
	effectiveBase := base
	if effectiveBase == nil {
		effectiveBase = http.DefaultTransport
	}
	return &panicSafeTransport{
		base:   effectiveBase,
		logger: ensureLogger(logger),
	}
}

type panicSafeTransport struct {
	base   http.RoundTripper
	logger Logger
}

func (transport *panicSafeTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			transport.logger.Error("http transport panic for %s: %v", req.URL.String(), recovered)
			resp = nil
			err = fmt.Errorf("%w: %s transport panic", errResponseBodyPanic, req.URL.String())
		}
	}()

	resp, err = transport.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}

	if resp.Body == nil {
		transport.logger.Error("nil http response body for %s", req.URL.String())
		return nil, fmt.Errorf("%w: %s", errNilResponseBody, req.URL.String())
	}

	resp.Body = newPanicSafeReadCloser(resp.Body, req.URL.String(), transport.logger)
	return resp, nil
}

func newPanicSafeReadCloser(body io.ReadCloser, url string, logger Logger) *panicSafeReadCloser {
	return &panicSafeReadCloser{
		body:   body,
		url:    url,
		logger: ensureLogger(logger),
	}
}

type panicSafeReadCloser struct {
	body     io.ReadCloser
	url      string
	logger   Logger
	panicErr error
	mu       sync.Mutex
}

func (reader *panicSafeReadCloser) Read(p []byte) (n int, err error) {
	if err := reader.currentError(); err != nil {
		return 0, err
	}
	if reader.body == nil {
		return 0, fmt.Errorf("%w: %s", errNilResponseBody, reader.url)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			n = 0
			err = reader.recordPanic("read", recovered)
		}
	}()

	return reader.body.Read(p)
}

func (reader *panicSafeReadCloser) Close() (err error) {
	panicErr := reader.currentError()
	if reader.body == nil {
		if panicErr != nil {
			return panicErr
		}
		return fmt.Errorf("%w: %s", errNilResponseBody, reader.url)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = reader.recordPanic("close", recovered)
		}
	}()

	closeErr := reader.body.Close()
	if panicErr != nil {
		return panicErr
	}
	return closeErr
}

func (reader *panicSafeReadCloser) currentError() error {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.panicErr
}

func (reader *panicSafeReadCloser) recordPanic(action string, recovered interface{}) error {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.panicErr == nil {
		reader.panicErr = fmt.Errorf("%w: %s panic for %s (%v)", errResponseBodyPanic, action, reader.url, recovered)
		reader.logger.Error("response body %s panic for %s: %v", action, reader.url, recovered)
	}
	return reader.panicErr
}
