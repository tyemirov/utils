package crawler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetryHandlerShouldDelayWithProxyPool(t *testing.T) {
	handler := &retryHandler{proxyPoolSize: 3}

	require.False(t, handler.shouldDelay(1, RetryOptions{}))
	require.False(t, handler.shouldDelay(2, RetryOptions{}))
	require.True(t, handler.shouldDelay(3, RetryOptions{}))
	require.False(t, handler.shouldDelay(4, RetryOptions{}))
	require.False(t, handler.shouldDelay(5, RetryOptions{}))
	require.True(t, handler.shouldDelay(6, RetryOptions{}))
}

func TestRetryHandlerShouldDelayWithoutPool(t *testing.T) {
	handler := &retryHandler{proxyPoolSize: 0}

	require.True(t, handler.shouldDelay(1, RetryOptions{}))
	require.True(t, handler.shouldDelay(2, RetryOptions{}))
}

func TestRetryHandlerShouldDelaySkipsBackoffWhenRequested(t *testing.T) {
	handler := &retryHandler{proxyPoolSize: 3}

	require.False(t, handler.shouldDelay(3, RetryOptions{SkipDelay: true}))
}

func TestRetryHandlerEffectiveMaxRetriesUsesLowerLimit(t *testing.T) {
	handler := &retryHandler{maxRetries: 17}

	require.Equal(t, 2, handler.effectiveMaxRetries(RetryOptions{
		LimitRetries: true,
		MaxRetries:   2,
	}))
	require.Equal(t, 17, handler.effectiveMaxRetries(RetryOptions{
		LimitRetries: true,
		MaxRetries:   99,
	}))
}
