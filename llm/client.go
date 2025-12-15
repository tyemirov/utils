package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultBaseURL             = "https://api.openai.com/v1"
	defaultMaxCompletionTokens = 512
	bodyPreviewLimit           = 512
)

// HTTPClient issues HTTP requests.
type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

// ChatClient issues chat completion requests.
type ChatClient interface {
	Chat(ctx context.Context, request ChatRequest) (string, error)
}

// Config configures an LLM client.
type Config struct {
	BaseURL             string
	APIKey              string
	Model               string
	MaxCompletionTokens int
	Temperature         float64
	HTTPClient          HTTPClient
	RequestTimeout      time.Duration
	RetryAttempts       int
	RetryInitialBackoff time.Duration
	RetryMaxBackoff     time.Duration
	RetryBackoffFactor  float64
}

// Client communicates with an LLM chat completion endpoint.
type Client struct {
	baseURL             string
	apiKey              string
	model               string
	maxCompletionTokens int
	temperature         float64
	httpClient          HTTPClient
	timeout             time.Duration
}

// Message represents a chat message role/content pair.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ResponseFormat configures structured responses.
type ResponseFormat struct {
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema,omitempty"`
	Strict bool            `json:"strict,omitempty"`
}

// ChatRequest describes a chat completion request.
type ChatRequest struct {
	Model          string
	Messages       []Message
	MaxTokens      int
	Temperature    *float64
	ResponseFormat *ResponseFormat
}

type chatCompletionRequest struct {
	Model               string          `json:"model"`
	Messages            []Message       `json:"messages"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	ResponseFormat      *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type       string             `json:"type"`
	JSONSchema *jsonSchemaWrapper `json:"json_schema,omitempty"`
}

type jsonSchemaWrapper struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema,omitempty"`
	Strict bool            `json:"strict,omitempty"`
}

type chatMessageResponse struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Refusal   json.RawMessage `json:"refusal,omitempty"`
	ToolCalls json.RawMessage `json:"tool_calls,omitempty"`
}

type chatCompletionChoice struct {
	Message      chatMessageResponse `json:"message"`
	FinishReason string              `json:"finish_reason"`
}

type chatCompletionResponse struct {
	Choices []chatCompletionChoice `json:"choices"`
}

// NewClient constructs a client from configuration.
func NewClient(configuration Config) (*Client, error) {
	baseURL := strings.TrimSpace(configuration.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	apiKey := strings.TrimSpace(configuration.APIKey)
	if apiKey == "" {
		return nil, errors.New("llm api key is required")
	}
	model := strings.TrimSpace(configuration.Model)
	if model == "" {
		return nil, errors.New("llm model identifier is required")
	}
	maxTokens := configuration.MaxCompletionTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxCompletionTokens
	}
	httpClient := configuration.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	timeout := configuration.RequestTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Client{
		baseURL:             strings.TrimRight(baseURL, "/"),
		apiKey:              apiKey,
		model:               model,
		maxCompletionTokens: maxTokens,
		temperature:         configuration.Temperature,
		httpClient:          httpClient,
		timeout:             timeout,
	}, nil
}

// Chat returns a trimmed response string for the provided request.
func (client *Client) Chat(ctx context.Context, request ChatRequest) (string, error) {
	if client == nil {
		return "", errors.New("llm client is not configured")
	}
	if len(request.Messages) == 0 {
		return "", errors.New("llm request requires at least one message")
	}
	payload := client.buildRequestPayload(request)
	requestBytes, marshalError := json.Marshal(payload)
	if marshalError != nil {
		return "", fmt.Errorf("encode llm request: %w", marshalError)
	}
	requestContext := ctx
	if requestContext == nil {
		requestContext = context.Background()
	}
	var cancel context.CancelFunc
	requestContext, cancel = context.WithTimeout(requestContext, client.timeout)
	defer cancel()

	httpRequest, buildError := http.NewRequestWithContext(
		requestContext,
		http.MethodPost,
		client.baseURL+"/chat/completions",
		bytes.NewReader(requestBytes),
	)
	if buildError != nil {
		return "", fmt.Errorf("construct llm request: %w", buildError)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+client.apiKey)

	httpResponse, callError := client.httpClient.Do(httpRequest)
	if callError != nil {
		if httpResponse != nil && httpResponse.Body != nil {
			closeError := httpResponse.Body.Close()
			if closeError != nil {
				callError = errors.Join(callError, fmt.Errorf("close llm response body: %w", closeError))
			}
		}
		return "", fmt.Errorf("send llm request: %w", callError)
	}
	defer httpResponse.Body.Close()

	bodyBytes, readError := io.ReadAll(httpResponse.Body)
	if readError != nil {
		return "", fmt.Errorf("read llm response: %w", readError)
	}
	bodyPreview := truncateForLog(string(bodyBytes), bodyPreviewLimit)

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return "", fmt.Errorf("llm http error %d: %s", httpResponse.StatusCode, bodyPreview)
	}

	var completion chatCompletionResponse
	if decodeError := json.Unmarshal(bodyBytes, &completion); decodeError != nil {
		return "", fmt.Errorf("decode llm response: %w (body=%s)", decodeError, bodyPreview)
	}
	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("llm response returned no choices (status=%d body=%s)", httpResponse.StatusCode, bodyPreview)
	}

	choice := completion.Choices[0]
	content, extractError := extractMessageContent(choice.Message)
	if extractError != nil {
		return "", fmt.Errorf("parse llm content: %w (body=%s)", extractError, bodyPreview)
	}
	trimmed := strings.TrimSpace(content)
	if trimmed != "" {
		return trimmed, nil
	}
	if strings.EqualFold(strings.TrimSpace(choice.FinishReason), "length") {
		return "", nil
	}
	refusal := decodeRefusal(choice.Message.Refusal)
	if refusal != "" {
		return "", fmt.Errorf("llm refusal: %s (status=%d body=%s)", refusal, httpResponse.StatusCode, bodyPreview)
	}
	return "", fmt.Errorf("llm response empty (status=%d body=%s): %w", httpResponse.StatusCode, bodyPreview, ErrEmptyResponse)
}

func (client *Client) buildRequestPayload(request ChatRequest) chatCompletionRequest {
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = client.model
	}
	maxTokens := request.MaxTokens
	if maxTokens <= 0 {
		maxTokens = client.maxCompletionTokens
	}
	payload := chatCompletionRequest{
		Model:               model,
		Messages:            request.Messages,
		MaxCompletionTokens: maxTokens,
	}
	if request.Temperature != nil {
		payload.Temperature = request.Temperature
	} else if client.temperature > 0 {
		value := client.temperature
		payload.Temperature = &value
	}
	if request.ResponseFormat != nil {
		payload.ResponseFormat = &responseFormat{Type: request.ResponseFormat.Type}
		if payload.ResponseFormat.Type == "json_schema" {
			payload.ResponseFormat.JSONSchema = &jsonSchemaWrapper{
				Name:   request.ResponseFormat.Name,
				Schema: request.ResponseFormat.Schema,
				Strict: request.ResponseFormat.Strict,
			}
		}
	}
	return payload
}

func truncateForLog(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func extractMessageContent(message chatMessageResponse) (string, error) {
	if len(message.Content) == 0 || string(message.Content) == "null" {
		refusal := decodeRefusal(message.Refusal)
		if refusal != "" {
			return "", fmt.Errorf("llm refusal: %s", refusal)
		}
		return "", nil
	}

	var asString string
	if err := json.Unmarshal(message.Content, &asString); err == nil {
		return asString, nil
	}

	if text, ok := extractRichText(message.Content); ok {
		return text, nil
	}

	refusal := decodeRefusal(message.Refusal)
	if refusal != "" {
		return "", fmt.Errorf("llm refusal: %s", refusal)
	}

	if len(message.ToolCalls) > 0 && string(message.ToolCalls) != "null" {
		return "", fmt.Errorf("llm response contained tool calls")
	}

	return "", fmt.Errorf("unsupported llm content: %s", truncateForLog(string(message.Content), 240))
}

func extractRichText(raw json.RawMessage) (string, bool) {
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", false
	}
	fragments := flattenText(data)
	if len(fragments) == 0 {
		return "", false
	}
	combined := strings.TrimSpace(strings.Join(fragments, "\n"))
	return combined, combined != ""
}

func flattenText(value any) []string {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		return []string{trimmed}
	case []any:
		collected := make([]string, 0, len(typed))
		for _, item := range typed {
			collected = append(collected, flattenText(item)...)
		}
		return collected
	case map[string]any:
		if text, ok := typed["text"]; ok {
			return flattenText(text)
		}
		if content, ok := typed["content"]; ok {
			return flattenText(content)
		}
		if valuePart, ok := typed["value"]; ok {
			return flattenText(valuePart)
		}
		return nil
	default:
		return nil
	}
}

func decodeRefusal(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	text := payload["content"]
	var fragments []string
	switch typed := text.(type) {
	case string:
		fragments = append(fragments, strings.TrimSpace(typed))
	case []any:
		for _, element := range typed {
			fragments = append(fragments, flattenText(element)...)
		}
	}
	combined := strings.TrimSpace(strings.Join(fragments, "\n"))
	return combined
}
