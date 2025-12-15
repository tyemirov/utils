package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientChatRequestPayloadSupportsResponseFormat(t *testing.T) {
	var capturedRequest []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var err error
		capturedRequest, err = io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:        server.URL,
		APIKey:         "token",
		Model:          "model-default",
		HTTPClient:     server.Client(),
		RequestTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{
		Model:     "model-override",
		MaxTokens: 0,
		Messages:  []Message{{Role: "user", Content: "hello"}},
		Temperature: func() *float64 {
			value := 1.2
			return &value
		}(),
		ResponseFormat: &ResponseFormat{
			Type:   "json_schema",
			Name:   "example",
			Schema: json.RawMessage(`{"type":"object"}`),
			Strict: true,
		},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(capturedRequest, &payload); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if payload["model"].(string) != "model-override" {
		t.Fatalf("expected model override, got %#v", payload["model"])
	}
	if payload["max_completion_tokens"].(float64) != float64(defaultMaxCompletionTokens) {
		t.Fatalf("expected default max tokens, got %#v", payload["max_completion_tokens"])
	}
	if payload["temperature"].(float64) != 1.2 {
		t.Fatalf("expected temperature override, got %#v", payload["temperature"])
	}

	responseFormat, ok := payload["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("expected response format object, got %#v", payload["response_format"])
	}
	if responseFormat["type"].(string) != "json_schema" {
		t.Fatalf("expected json_schema response format, got %#v", responseFormat["type"])
	}

	jsonSchema, ok := responseFormat["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected json_schema wrapper, got %#v", responseFormat["json_schema"])
	}
	if jsonSchema["name"].(string) != "example" {
		t.Fatalf("expected schema name, got %#v", jsonSchema["name"])
	}
	if jsonSchema["strict"].(bool) != true {
		t.Fatalf("expected strict=true, got %#v", jsonSchema["strict"])
	}
}

