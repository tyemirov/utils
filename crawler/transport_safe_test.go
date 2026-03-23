package crawler

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const exampleProductURL = "https://example.com/product/B0TEST"

func TestPanicSafeReadCloserReadRecovers(t *testing.T) {
	logger := &recordingLogger{}
	reader := &panicReadCloser{panicOnRead: true}

	safe := newPanicSafeReadCloser(reader, exampleProductURL, logger)

	_, err := safe.Read(make([]byte, 8))
	require.Error(t, err)
	require.ErrorIs(t, err, errResponseBodyPanic)
	require.Contains(t, logger.lastError, exampleProductURL)

	_, err = safe.Read(make([]byte, 8))
	require.ErrorIs(t, err, errResponseBodyPanic)
}

func TestPanicSafeReadCloserCloseRecovers(t *testing.T) {
	logger := &recordingLogger{}
	reader := &panicReadCloser{panicOnClose: true}

	safe := newPanicSafeReadCloser(reader, exampleProductURL, logger)

	require.ErrorIs(t, safe.Close(), errResponseBodyPanic)
	require.Contains(t, logger.lastError, "body close panic")
}

func TestPanicSafeReadCloserCloseAfterReadPanicClosesBody(t *testing.T) {
	logger := &recordingLogger{}
	reader := &panicReadCloser{panicOnRead: true}

	safe := newPanicSafeReadCloser(reader, exampleProductURL, logger)

	_, readErr := safe.Read(make([]byte, 16))
	require.ErrorIs(t, readErr, errResponseBodyPanic)

	closeErr := safe.Close()
	require.ErrorIs(t, closeErr, errResponseBodyPanic)
	require.True(t, reader.wasClosed)
	require.Equal(t, 1, reader.closeCallCount)
}

func TestPanicSafeTransportWrapsResponseBody(t *testing.T) {
	logger := &recordingLogger{}
	transport := newPanicSafeTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	}), logger)

	resp, err := transport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", nil))
	require.NoError(t, err)
	require.IsType(t, &panicSafeReadCloser{}, resp.Body)
	require.NoError(t, resp.Body.Close())
}

func TestPanicSafeTransportRejectsNilBodies(t *testing.T) {
	logger := &recordingLogger{}
	transport := newPanicSafeTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: nil}, nil
	}), logger)

	resp, err := transport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", nil))
	require.Nil(t, resp)
	require.ErrorIs(t, err, errNilResponseBody)
	require.Contains(t, logger.lastError, "nil http response body")
}

type panicReadCloser struct {
	panicOnRead    bool
	panicOnClose   bool
	wasClosed      bool
	closeCallCount int
}

func (reader *panicReadCloser) Read(p []byte) (int, error) {
	if reader.panicOnRead {
		panic("body read panic")
	}
	copy(p, "ok")
	if len(p) >= 2 {
		return 2, io.EOF
	}
	return len(p), io.EOF
}

func (reader *panicReadCloser) Close() error {
	reader.closeCallCount++
	reader.wasClosed = true
	if reader.panicOnClose {
		panic("body close panic")
	}
	return nil
}

type recordingLogger struct {
	lastError string
}

func (logger *recordingLogger) Debug(string, ...interface{})   {}
func (logger *recordingLogger) Info(string, ...interface{})    {}
func (logger *recordingLogger) Warning(string, ...interface{}) {}
func (logger *recordingLogger) Error(format string, args ...interface{}) {
	logger.lastError = fmt.Sprintf(format, args...)
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
