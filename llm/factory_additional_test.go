package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFactoryChatFailsWhenFactoryIsNil(t *testing.T) {
	var factory *Factory
	_, err := factory.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "llm factory is not configured") {
		t.Fatalf("expected configured error, got %v", err)
	}
}

func TestFactoryChatSurfacesContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`{"error":"temporary"}`))
	}))
	defer server.Close()

	factory, err := NewFactory(
		Config{
			BaseURL:        server.URL,
			APIKey:         "token",
			Model:          "gpt-4o-mini",
			HTTPClient:     server.Client(),
			RequestTimeout: time.Second,
			RetryAttempts:  2,
		},
		WithSleepFunc(func(ctx context.Context, duration time.Duration) error { return nil }),
	)
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}

	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = factory.Chat(cancelledContext, ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "llm chat cancelled") {
		t.Fatalf("expected cancelled error, got %v", err)
	}
}

func TestFactoryChatStopsWhenSleepFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`{"error":"temporary"}`))
	}))
	defer server.Close()

	factory, err := NewFactory(
		Config{
			BaseURL:             server.URL,
			APIKey:              "token",
			Model:               "gpt-4o-mini",
			HTTPClient:          server.Client(),
			RequestTimeout:      time.Second,
			RetryAttempts:       2,
			RetryInitialBackoff: 10 * time.Millisecond,
		},
		WithSleepFunc(func(ctx context.Context, duration time.Duration) error { return errors.New("sleep failed") }),
	)
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}

	_, err = factory.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "retry interrupted") {
		t.Fatalf("expected retry interrupted error, got %v", err)
	}
}

func TestFactoryChatNormalisesRetryPolicyFields(t *testing.T) {
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
			MaxAttempts:       2,
			InitialBackoff:    -1,
			MaxBackoff:        1 * time.Millisecond,
			BackoffMultiplier: 0,
		},
		sleep: func(ctx context.Context, duration time.Duration) error { return nil },
	}

	_, err := factory.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNormaliseRetryPolicySetsDefaults(t *testing.T) {
	policy := normaliseRetryPolicy(RetryPolicy{
		MaxAttempts:       0,
		InitialBackoff:    20 * time.Millisecond,
		MaxBackoff:        10 * time.Millisecond,
		BackoffMultiplier: 0,
	})
	if policy.MaxAttempts != defaultRetryAttempts {
		t.Fatalf("expected default attempts, got %d", policy.MaxAttempts)
	}
	if policy.MaxBackoff != policy.InitialBackoff {
		t.Fatalf("expected max backoff to clamp to initial, got %v", policy.MaxBackoff)
	}
	if policy.BackoffMultiplier != defaultRetryMultiplier {
		t.Fatalf("expected default multiplier, got %v", policy.BackoffMultiplier)
	}
}

func TestDefaultSleepReturnsContextErrorWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := defaultSleep(ctx, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestDefaultSleepReturnsNilForNonPositiveDurations(t *testing.T) {
	if err := defaultSleep(context.Background(), 0); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if err := defaultSleep(context.Background(), -1); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestDefaultSleepWaitsForTimer(t *testing.T) {
	if err := defaultSleep(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}
