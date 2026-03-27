package billing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const httpRetryAfterHeader = "Retry-After"

var ErrHTTPRetryAttemptsExhausted = errors.New("billing.http.retry.attempts.exhausted")

type httpRetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

type httpRetryWaitError struct {
	StatusCode int
	Err        error
}

func (retryWaitErr *httpRetryWaitError) Error() string {
	if retryWaitErr == nil || retryWaitErr.Err == nil {
		return ""
	}
	if retryWaitErr.StatusCode <= 0 {
		return retryWaitErr.Err.Error()
	}
	return fmt.Sprintf("status=%d: %v", retryWaitErr.StatusCode, retryWaitErr.Err)
}

func (retryWaitErr *httpRetryWaitError) Unwrap() error {
	if retryWaitErr == nil {
		return nil
	}
	return retryWaitErr.Err
}

func retryWaitStatusCode(err error) (int, bool) {
	waitErr := &httpRetryWaitError{}
	if !errors.As(err, &waitErr) {
		return 0, false
	}
	return waitErr.StatusCode, true
}

func doHTTPRequestWithRetry(
	httpClient *http.Client,
	requestFactory func() (*http.Request, error),
	retryConfig httpRetryConfig,
) (*http.Response, error) {
	for attemptIndex := 0; attemptIndex < retryConfig.MaxAttempts; attemptIndex++ {
		request, requestErr := requestFactory()
		if requestErr != nil {
			return nil, requestErr
		}

		response, responseErr := httpClient.Do(request)
		if responseErr != nil {
			if !canRetryHTTPRequest(request.Method, attemptIndex, 0, retryConfig.MaxAttempts) {
				return nil, responseErr
			}
			if waitErr := waitForHTTPRetry(request.Context(), "", attemptIndex, retryConfig); waitErr != nil {
				return nil, &httpRetryWaitError{
					StatusCode: 0,
					Err:        waitErr,
				}
			}
			continue
		}

		if !canRetryHTTPRequest(request.Method, attemptIndex, response.StatusCode, retryConfig.MaxAttempts) {
			return response, nil
		}
		if _, copyErr := io.Copy(io.Discard, response.Body); copyErr != nil {
			_ = response.Body.Close()
			return nil, copyErr
		}
		if closeErr := response.Body.Close(); closeErr != nil {
			return nil, closeErr
		}
		if waitErr := waitForHTTPRetry(
			request.Context(),
			response.Header.Get(httpRetryAfterHeader),
			attemptIndex,
			retryConfig,
		); waitErr != nil {
			return nil, &httpRetryWaitError{
				StatusCode: response.StatusCode,
				Err:        waitErr,
			}
		}
	}

	return nil, ErrHTTPRetryAttemptsExhausted
}

func canRetryHTTPRequest(method string, attemptIndex int, statusCode int, maxAttempts int) bool {
	if attemptIndex >= maxAttempts-1 {
		return false
	}
	if !isRetryableHTTPMethod(method) {
		return false
	}
	if statusCode == 0 {
		return true
	}
	if statusCode == http.StatusTooManyRequests {
		return true
	}
	return statusCode >= http.StatusInternalServerError && statusCode < 600
}

func isRetryableHTTPMethod(method string) bool {
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	switch normalizedMethod {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func waitForHTTPRetry(
	ctx context.Context,
	retryAfterHeader string,
	attemptIndex int,
	retryConfig httpRetryConfig,
) error {
	retryDelay, hasRetryDelay := parseHTTPRetryAfter(retryAfterHeader, time.Now().UTC())
	if !hasRetryDelay {
		retryDelay = retryConfig.BaseDelay * time.Duration(1<<attemptIndex)
		if retryDelay > retryConfig.MaxDelay {
			retryDelay = retryConfig.MaxDelay
		}
	}
	if retryDelay < 0 {
		retryDelay = 0
	}
	timer := time.NewTimer(retryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseHTTPRetryAfter(rawHeader string, now time.Time) (time.Duration, bool) {
	normalizedHeader := strings.TrimSpace(rawHeader)
	if normalizedHeader == "" {
		return 0, false
	}
	seconds, secondsErr := strconv.ParseInt(normalizedHeader, 10, 64)
	if secondsErr == nil {
		return time.Duration(seconds) * time.Second, true
	}
	retryAt, parseErr := http.ParseTime(normalizedHeader)
	if parseErr != nil {
		return 0, false
	}
	return retryAt.Sub(now), true
}
