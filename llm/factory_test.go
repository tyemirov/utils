package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestFactoryRetriesOnHTTPError(t *testing.T) {
	t.Helper()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		current := atomic.AddInt32(&attempts, 1)
		writer.Header().Set("Content-Type", "application/json")
		if current == 1 {
			writer.WriteHeader(http.StatusBadGateway)
			_, _ = writer.Write([]byte(`{"error":"temporary"}`))
			return
		}
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"feat: retry success"}}]}`))
	}))
	defer server.Close()

	factory, err := NewFactory(
		Config{
			BaseURL:             server.URL,
			APIKey:              "token",
			Model:               "gpt-4o-mini",
			HTTPClient:          server.Client(),
			RequestTimeout:      2 * time.Second,
			RetryAttempts:       3,
			RetryInitialBackoff: 10 * time.Millisecond,
		},
		WithSleepFunc(func(context.Context, time.Duration) error { return nil }),
	)
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}

	result, chatErr := factory.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "summarise"}},
	})
	if chatErr != nil {
		t.Fatalf("chat: %v", chatErr)
	}
	if result != "feat: retry success" {
		t.Fatalf("unexpected chat result %q", result)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestFactoryDetectsEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
	}))
	defer server.Close()

	factory, err := NewFactory(
		Config{
			BaseURL:        server.URL,
			APIKey:         "token",
			Model:          "gpt-4o-mini",
			HTTPClient:     server.Client(),
			RequestTimeout: time.Second,
			RetryAttempts:  1,
		},
	)
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}

	_, chatErr := factory.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "summarise"}},
	})
	if chatErr == nil {
		t.Fatal("expected error for empty response, got nil")
	}
	if !errors.Is(chatErr, ErrEmptyResponse) {
		t.Fatalf("expected ErrEmptyResponse, got %v", chatErr)
	}
}

func TestFactoryRetriesWhenClientReturnsEmptySuccess(t *testing.T) {
	t.Helper()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		current := atomic.AddInt32(&attempts, 1)
		writer.Header().Set("Content-Type", "application/json")
		if current == 1 {
			_, _ = writer.Write([]byte(`{"choices":[{"finish_reason":"length","message":{"content":""}}]}`))
			return
		}
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
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
		WithSleepFunc(func(context.Context, time.Duration) error { return nil }),
	)
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}

	result, chatErr := factory.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "summarise"}},
	})
	if chatErr != nil {
		t.Fatalf("chat: %v", chatErr)
	}
	if result != "ok" {
		t.Fatalf("unexpected chat result %q", result)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}
