package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stubHTTPClient struct {
	do func(request *http.Request) (*http.Response, error)
}

func (client stubHTTPClient) Do(request *http.Request) (*http.Response, error) {
	return client.do(request)
}

type errorReadCloser struct{}

func (errorReadCloser) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (errorReadCloser) Close() error             { return nil }

func TestClientChatFailsWhenClientIsNil(t *testing.T) {
	var client *Client
	_, err := client.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "llm client is not configured") {
		t.Fatalf("expected configured error, got %v", err)
	}
}

func TestClientChatUsesBackgroundWhenContextNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:        server.URL,
		APIKey:         "token",
		Model:          "model",
		HTTPClient:     server.Client(),
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	response, err := client.Chat(nil, ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if response != "ok" {
		t.Fatalf("expected ok response, got %q", response)
	}
}

func TestClientChatFailsWithInvalidRequestURL(t *testing.T) {
	client, err := NewClient(Config{
		BaseURL: "http://[::1",
		APIKey:  "token",
		Model:   "model",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "construct llm request") {
		t.Fatalf("expected construct request error, got %v", err)
	}
}

func TestClientChatFailsWhenMarshallingRequestPayload(t *testing.T) {
	client, err := NewClient(Config{
		BaseURL: "http://example.com",
		APIKey:  "token",
		Model:   "model",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
		ResponseFormat: &ResponseFormat{
			Type:   "json_schema",
			Name:   "example",
			Schema: []byte("{invalid-json"),
			Strict: true,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "encode llm request") {
		t.Fatalf("expected encode request error, got %v", err)
	}
}

func TestClientChatSurfacesHTTPClientFailures(t *testing.T) {
	client := &Client{
		baseURL: "http://example.com",
		apiKey:  "token",
		model:   "model",
		httpClient: stubHTTPClient{do: func(request *http.Request) (*http.Response, error) {
			return nil, errors.New("transport failed")
		}},
		timeout: time.Second,
	}

	_, err := client.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "send llm request") {
		t.Fatalf("expected send request error, got %v", err)
	}
}

func TestClientChatSurfacesBodyReadFailures(t *testing.T) {
	client := &Client{
		baseURL: "http://example.com",
		apiKey:  "token",
		model:   "model",
		httpClient: stubHTTPClient{do: func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       errorReadCloser{},
			}, nil
		}},
		timeout: time.Second,
	}

	_, err := client.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "read llm response") {
		t.Fatalf("expected read response error, got %v", err)
	}
}

func TestClientChatSurfacesDecodeFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, "{not-json")
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:        server.URL,
		APIKey:         "token",
		Model:          "model",
		HTTPClient:     server.Client(),
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "decode llm response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestClientChatFailsWhenNoChoicesReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"choices":[]}`)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:        server.URL,
		APIKey:         "token",
		Model:          "model",
		HTTPClient:     server.Client(),
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "returned no choices") {
		t.Fatalf("expected no choices error, got %v", err)
	}
}

func TestClientChatReturnsEmptyOnLengthFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"choices":[{"finish_reason":"length","message":{"content":""}}]}`)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:        server.URL,
		APIKey:         "token",
		Model:          "model",
		HTTPClient:     server.Client(),
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	response, err := client.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if response != "" {
		t.Fatalf("expected empty response, got %q", response)
	}
}

func TestClientChatSurfacesRefusalsWhenContentIsNull(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"content":null,"refusal":{"content":"nope"}}}]}`)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:        server.URL,
		APIKey:         "token",
		Model:          "model",
		HTTPClient:     server.Client(),
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "llm refusal") {
		t.Fatalf("expected refusal error, got %v", err)
	}
}

func TestClientChatSurfacesRefusalsWhenContentIsEmptyString(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"content":"","refusal":{"content":"nope"}}}]}`)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:        server.URL,
		APIKey:         "token",
		Model:          "model",
		HTTPClient:     server.Client(),
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil || !strings.Contains(err.Error(), "llm refusal") {
		t.Fatalf("expected refusal error, got %v", err)
	}
}

func TestTruncateForLogHandlesMultiByteCharacters(t *testing.T) {
	if value := truncateForLog("éé", 2); value != "éé" {
		t.Fatalf("expected multi-byte string to remain unchanged, got %q", value)
	}
	if value := truncateForLog("hello", 3); value != "hel…" {
		t.Fatalf("expected truncation, got %q", value)
	}
}

func TestExtractMessageContentErrorsOnUnsupportedPayload(t *testing.T) {
	content, err := extractMessageContent(chatMessageResponse{Content: []byte(`{"unexpected":true}`)})
	if err == nil || !strings.Contains(err.Error(), "unsupported llm content") {
		t.Fatalf("expected unsupported content error, got content=%q err=%v", content, err)
	}
}

func TestExtractMessageContentErrorsOnToolCalls(t *testing.T) {
	content, err := extractMessageContent(chatMessageResponse{
		Content:   []byte(`123`),
		ToolCalls: []byte(`[{"id":"t1"}]`),
	})
	if err == nil || !strings.Contains(err.Error(), "tool calls") {
		t.Fatalf("expected tool calls error, got content=%q err=%v", content, err)
	}
}

func TestExtractRichTextRejectsInvalidJSON(t *testing.T) {
	text, ok := extractRichText([]byte("{"))
	if ok || text != "" {
		t.Fatalf("expected invalid json to be rejected, got ok=%v text=%q", ok, text)
	}
}

func TestExtractRichTextRejectsWhitespaceOnlyStrings(t *testing.T) {
	text, ok := extractRichText([]byte(`"   "`))
	if ok || text != "" {
		t.Fatalf("expected whitespace-only content to be rejected, got ok=%v text=%q", ok, text)
	}
}

func TestExtractRichTextUnderstandsValueObjects(t *testing.T) {
	text, ok := extractRichText([]byte(`{"value":"Line A"}`))
	if !ok || text != "Line A" {
		t.Fatalf("expected value content, got ok=%v text=%q", ok, text)
	}
}

func TestExtractMessageContentReturnsEmptyWhenContentIsNullAndNoRefusal(t *testing.T) {
	content, err := extractMessageContent(chatMessageResponse{Content: []byte("null")})
	if err != nil || content != "" {
		t.Fatalf("expected empty content with no error, got content=%q err=%v", content, err)
	}
}

func TestExtractMessageContentReturnsEmptyWhenContentIsMissing(t *testing.T) {
	content, err := extractMessageContent(chatMessageResponse{})
	if err != nil || content != "" {
		t.Fatalf("expected empty content with no error, got content=%q err=%v", content, err)
	}
}

func TestDecodeRefusalSupportsArrayContent(t *testing.T) {
	refusal := decodeRefusal([]byte(`{"content":[{"text":{"value":"Line A"}},{"content":"Line B"}]}`))
	if refusal != "Line A\nLine B" {
		t.Fatalf("expected refusal text, got %q", refusal)
	}
}

func TestDecodeRefusalReturnsEmptyOnNilOrInvalidPayload(t *testing.T) {
	if value := decodeRefusal(nil); value != "" {
		t.Fatalf("expected empty refusal for nil raw, got %q", value)
	}
	if value := decodeRefusal([]byte("null")); value != "" {
		t.Fatalf("expected empty refusal for null payload, got %q", value)
	}
	if value := decodeRefusal([]byte("{")); value != "" {
		t.Fatalf("expected empty refusal for invalid payload, got %q", value)
	}
	if value := decodeRefusal([]byte(`{"other":"ignored"}`)); value != "" {
		t.Fatalf("expected empty refusal for missing content, got %q", value)
	}
	if value := decodeRefusal([]byte(`{"content":1}`)); value != "" {
		t.Fatalf("expected empty refusal for non-text content, got %q", value)
	}
}

func TestExtractMessageContentErrorsOnRefusalWhenContentUnsupported(t *testing.T) {
	content, err := extractMessageContent(chatMessageResponse{
		Content:  []byte(`123`),
		Refusal:  []byte(`{"content":"nope"}`),
		ToolCalls: nil,
	})
	if err == nil || !strings.Contains(err.Error(), "llm refusal") {
		t.Fatalf("expected refusal error, got content=%q err=%v", content, err)
	}
}
