package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultRetryAttempts       = 3
	defaultRetryInitialBackoff = 200 * time.Millisecond
	defaultRetryMaxBackoff     = 2 * time.Second
	defaultRetryMultiplier     = 2.0
)

// ErrEmptyResponse indicates the LLM returned only whitespace or an empty payload.
var ErrEmptyResponse = errors.New("llm returned empty response")

// SleepFunc waits for the provided duration or returns early if the context is cancelled.
type SleepFunc func(ctx context.Context, duration time.Duration) error

// RetryPolicy controls retry behaviour for chat completion requests.
type RetryPolicy struct {
	MaxAttempts       int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
}

// FactoryOption customises factory behaviour.
type FactoryOption func(*Factory)

// WithRetryPolicy overrides the retry policy applied by the factory.
func WithRetryPolicy(policy RetryPolicy) FactoryOption {
	return func(factory *Factory) {
		factory.retryPolicy = normaliseRetryPolicy(policy)
	}
}

// WithSleepFunc overrides the sleep function used between retries (intended for tests).
func WithSleepFunc(fn SleepFunc) FactoryOption {
	return func(factory *Factory) {
		if fn != nil {
			factory.sleep = fn
		}
	}
}

// Factory constructs chat clients and enforces retry/backoff behaviour.
type Factory struct {
	baseClient  *Client
	retryPolicy RetryPolicy
	sleep       SleepFunc
}

// NewFactory builds a Factory using the provided configuration.
func NewFactory(configuration Config, options ...FactoryOption) (*Factory, error) {
	client, clientErr := NewClient(configuration)
	if clientErr != nil {
		return nil, clientErr
	}

	factory := &Factory{
		baseClient: client,
		retryPolicy: normaliseRetryPolicy(RetryPolicy{
			MaxAttempts:       configuration.RetryAttempts,
			InitialBackoff:    configuration.RetryInitialBackoff,
			MaxBackoff:        configuration.RetryMaxBackoff,
			BackoffMultiplier: configuration.RetryBackoffFactor,
		}),
		sleep: defaultSleep,
	}

	for _, option := range options {
		option(factory)
	}

	return factory, nil
}

// Chat executes the chat request with retry/backoff safeguards.
func (factory *Factory) Chat(ctx context.Context, request ChatRequest) (string, error) {
	if factory == nil || factory.baseClient == nil {
		return "", errors.New("llm factory is not configured")
	}

	requestContext := ctx
	if requestContext == nil {
		requestContext = context.Background()
	}

	attempts := factory.retryPolicy.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}

	delay := factory.retryPolicy.InitialBackoff
	if delay < 0 {
		delay = 0
	}

	var lastError error
	for attempt := 1; ; attempt++ {
		response, callErr := factory.baseClient.Chat(requestContext, request)
		if callErr == nil {
			trimmed := strings.TrimSpace(response)
			if trimmed != "" {
				return trimmed, nil
			}
			callErr = ErrEmptyResponse
		}
		lastError = callErr

		if attempt >= attempts {
			return "", fmt.Errorf("llm chat failed after %d attempts: %w", attempts, lastError)
		}

		if ctxErr := requestContext.Err(); ctxErr != nil {
			return "", fmt.Errorf("llm chat cancelled: %w", ctxErr)
		}

		wait := delay
		if factory.retryPolicy.MaxBackoff > 0 && wait > factory.retryPolicy.MaxBackoff {
			wait = factory.retryPolicy.MaxBackoff
		}

		if wait > 0 {
			if sleepErr := factory.sleep(requestContext, wait); sleepErr != nil {
				return "", fmt.Errorf("llm chat retry interrupted: %w", sleepErr)
			}
		}

		next := time.Duration(float64(delay) * factory.retryPolicy.BackoffMultiplier)
		if next <= 0 {
			next = delay
		}
		if factory.retryPolicy.MaxBackoff > 0 && next > factory.retryPolicy.MaxBackoff {
			next = factory.retryPolicy.MaxBackoff
		}
		delay = next
	}
}

func normaliseRetryPolicy(policy RetryPolicy) RetryPolicy {
	normalised := policy
	if normalised.MaxAttempts <= 0 {
		normalised.MaxAttempts = defaultRetryAttempts
	}
	if normalised.InitialBackoff <= 0 {
		normalised.InitialBackoff = defaultRetryInitialBackoff
	}
	if normalised.MaxBackoff <= 0 {
		normalised.MaxBackoff = defaultRetryMaxBackoff
	}
	if normalised.MaxBackoff < normalised.InitialBackoff {
		normalised.MaxBackoff = normalised.InitialBackoff
	}
	if normalised.BackoffMultiplier <= 0 {
		normalised.BackoffMultiplier = defaultRetryMultiplier
	}
	return normalised
}

func defaultSleep(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
