package llm

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWithRetryPolicyOverridesDefaults(t *testing.T) {
	factory, err := NewFactory(
		Config{
			APIKey: "token",
			Model:  "model",
		},
		WithRetryPolicy(RetryPolicy{
			MaxAttempts:       5,
			InitialBackoff:    10 * time.Millisecond,
			MaxBackoff:        20 * time.Millisecond,
			BackoffMultiplier: 1.5,
		}),
	)
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	if factory.retryPolicy.MaxAttempts != 5 {
		t.Fatalf("expected retry attempts=5, got %d", factory.retryPolicy.MaxAttempts)
	}
	if factory.retryPolicy.InitialBackoff != 10*time.Millisecond {
		t.Fatalf("expected initial backoff=10ms, got %v", factory.retryPolicy.InitialBackoff)
	}
	if factory.retryPolicy.MaxBackoff != 20*time.Millisecond {
		t.Fatalf("expected max backoff=20ms, got %v", factory.retryPolicy.MaxBackoff)
	}
	if factory.retryPolicy.BackoffMultiplier != 1.5 {
		t.Fatalf("expected multiplier=1.5, got %v", factory.retryPolicy.BackoffMultiplier)
	}
}

func TestNewFactoryReturnsClientConstructionErrors(t *testing.T) {
	_, err := NewFactory(Config{})
	if err == nil || !strings.Contains(err.Error(), "llm api key is required") {
		t.Fatalf("expected api key error, got %v", err)
	}
}

func TestFactoryChatClampsWaitToMaxBackoff(t *testing.T) {
	baseClient := &Client{
		baseURL: "http://example.com",
		apiKey:  "token",
		model:   "model",
		httpClient: stubHTTPClient{do: func(request *http.Request) (*http.Response, error) {
			return nil, errors.New("transport failed")
		}},
		timeout: time.Second,
	}

	var slept time.Duration
	factory := &Factory{
		baseClient: baseClient,
		retryPolicy: RetryPolicy{
			MaxAttempts:       2,
			InitialBackoff:    50 * time.Millisecond,
			MaxBackoff:        10 * time.Millisecond,
			BackoffMultiplier: 2,
		},
		sleep: func(ctx context.Context, duration time.Duration) error {
			slept = duration
			return nil
		},
	}

	_, err := factory.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if slept != 10*time.Millisecond {
		t.Fatalf("expected sleep to clamp to max backoff, got %v", slept)
	}
}

func TestFactoryChatUsesSingleAttemptWhenPolicyZero(t *testing.T) {
	baseClient := &Client{
		baseURL: "http://example.com",
		apiKey:  "token",
		model:   "model",
		httpClient: stubHTTPClient{do: func(request *http.Request) (*http.Response, error) {
			return nil, errors.New("transport failed")
		}},
		timeout: time.Second,
	}

	factory := &Factory{
		baseClient: baseClient,
		retryPolicy: RetryPolicy{
			MaxAttempts:       0,
			InitialBackoff:    0,
			MaxBackoff:        0,
			BackoffMultiplier: 0,
		},
		sleep: func(ctx context.Context, duration time.Duration) error { return nil },
	}

	_, err := factory.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "after 1 attempts") {
		t.Fatalf("expected single-attempt error, got %v", err)
	}
}

