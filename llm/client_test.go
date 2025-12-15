package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClientValidatesConfiguration(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil || err.Error() != "llm api key is required" {
		t.Fatalf("expected api key error, got %v", err)
	}

	_, err = NewClient(Config{APIKey: "token"})
	if err == nil || err.Error() != "llm model identifier is required" {
		t.Fatalf("expected model error, got %v", err)
	}
}

func TestClientChatSuccess(t *testing.T) {
	var capturedRequest []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", request.Method)
		}
		if request.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("unexpected authorization header %q", request.Header.Get("Authorization"))
		}
		var err error
		capturedRequest, err = io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"feat: add API"}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:             server.URL,
		APIKey:              "token",
		Model:               "gpt-4.1-mini",
		MaxCompletionTokens: 320,
		Temperature:         0.2,
		HTTPClient:          server.Client(),
		RequestTimeout:      5 * time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	response, err := client.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "Instruction"},
			{Role: "user", Content: "Diff summary"},
		},
		MaxTokens: 128,
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if response != "feat: add API" {
		t.Fatalf("unexpected response %q", response)
	}

	var payload map[string]any
	if err := json.Unmarshal(capturedRequest, &payload); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if payload["model"] != "gpt-4.1-mini" {
		t.Fatalf("unexpected model %#v", payload["model"])
	}
	if payload["max_completion_tokens"].(float64) != 128 {
		t.Fatalf("unexpected max tokens %#v", payload["max_completion_tokens"])
	}
	if temperature, ok := payload["temperature"]; !ok || temperature.(float64) != 0.2 {
		t.Fatalf("missing temperature override: %#v", temperature)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("expected two messages, got %#v", payload["messages"])
	}
}

func TestClientChatParsesRichContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Write([]byte(`{"choices":[{"message":{"content":[{"type":"text","text":{"value":"Line A"}},{"type":"text","text":{"value":"Line B"}}]}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:        server.URL,
		APIKey:         "token",
		Model:          "gpt-4",
		HTTPClient:     server.Client(),
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	result, err := client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Example"}},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if result != "Line A\nLine B" {
		t.Fatalf("unexpected result %q", result)
	}
}

func TestClientChatHandlesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		writer.Write([]byte(`{"error":"temporary"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:        server.URL,
		APIKey:         "token",
		Model:          "gpt-4",
		HTTPClient:     server.Client(),
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Example"}},
	})
	if err == nil || !strings.Contains(err.Error(), "llm http error") {
		t.Fatalf("expected http error, got %v", err)
	}
}

func TestClientChatRejectsEmptyMessages(t *testing.T) {
	client, _ := NewClient(Config{
		APIKey: "token",
		Model:  "foo",
	})
	_, err := client.Chat(context.Background(), ChatRequest{})
	if err == nil || !strings.Contains(err.Error(), "requires at least one message") {
		t.Fatalf("expected message validation error, got %v", err)
	}
}

