package billing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type stubWebhookProvider struct {
	code                string
	signatureHeaderName string
	verifyErr           error
	parseErr            error
	parseMetadata       WebhookEventMetadata
}

func (provider *stubWebhookProvider) Code() string {
	return provider.code
}

func (provider *stubWebhookProvider) SignatureHeaderName() string {
	return provider.signatureHeaderName
}

func (provider *stubWebhookProvider) VerifySignature(signatureHeader string, payload []byte) error {
	return provider.verifyErr
}

func (provider *stubWebhookProvider) ParseWebhookEvent(payload []byte) (WebhookEventMetadata, error) {
	if provider.parseErr != nil {
		return WebhookEventMetadata{}, provider.parseErr
	}
	return provider.parseMetadata, nil
}

type stubWebhookProcessor struct {
	err   error
	event WebhookEvent
	calls int
}

func (processor *stubWebhookProcessor) Process(_ context.Context, event WebhookEvent) error {
	processor.calls++
	processor.event = event
	return processor.err
}

func TestWebhookHandlerRejectsNonPost(t *testing.T) {
	handler := NewWebhookHandler(&stubWebhookProvider{
		code:                "stub",
		signatureHeaderName: "X-Webhook-Signature",
		parseMetadata:       WebhookEventMetadata{EventID: "evt_1", EventType: "event.type"},
	}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/billing/webhook", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
}

func TestWebhookHandlerRejectsWhenProviderUnavailable(t *testing.T) {
	handler := NewWebhookHandler(nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(`{}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestWebhookHandlerRejectsMissingSignature(t *testing.T) {
	handler := NewWebhookHandler(&stubWebhookProvider{
		code:                "stub",
		signatureHeaderName: "X-Webhook-Signature",
		parseMetadata:       WebhookEventMetadata{EventID: "evt_1", EventType: "event.type"},
	}, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(`{"id":"evt_1"}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestWebhookHandlerRejectsInvalidSignature(t *testing.T) {
	handler := NewWebhookHandler(&stubWebhookProvider{
		code:                "stub",
		signatureHeaderName: "X-Webhook-Signature",
		verifyErr:           errors.New("invalid signature"),
		parseMetadata:       WebhookEventMetadata{EventID: "evt_1", EventType: "event.type"},
	}, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(`{"id":"evt_1"}`))
	request.Header.Set("X-Webhook-Signature", "invalid")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestWebhookHandlerRejectsInvalidPayloadAfterVerification(t *testing.T) {
	handler := NewWebhookHandler(&stubWebhookProvider{
		code:                "stub",
		signatureHeaderName: "X-Webhook-Signature",
		parseErr:            errors.New("invalid payload"),
	}, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(`{"id":"evt_1"}`))
	request.Header.Set("X-Webhook-Signature", "ok")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestWebhookHandlerRejectsWhenProcessingFails(t *testing.T) {
	processor := &stubWebhookProcessor{
		err: errors.New("temporary database error"),
	}
	handler := NewWebhookHandler(&stubWebhookProvider{
		code:                "stub",
		signatureHeaderName: "X-Webhook-Signature",
		parseMetadata:       WebhookEventMetadata{EventID: "evt_1", EventType: "event.type"},
	}, processor)
	request := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(`{"id":"evt_1"}`))
	request.Header.Set("X-Webhook-Signature", "ok")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, 1, processor.calls)
}

func TestWebhookHandlerAcceptsWhenProcessingFailsWithNonRetryableError(t *testing.T) {
	testCases := []struct {
		name         string
		processorErr error
	}{
		{
			name:         "metadata invalid",
			processorErr: fmt.Errorf("billing.webhook.grant.resolve: %w", ErrWebhookGrantMetadataInvalid),
		},
		{
			name:         "payload invalid",
			processorErr: fmt.Errorf("billing.webhook.grant.resolve: %w", ErrWebhookGrantPayloadInvalid),
		},
		{
			name:         "unknown plan",
			processorErr: fmt.Errorf("billing.webhook.grant.resolve: %w", ErrWebhookGrantPlanUnknown),
		},
		{
			name:         "unknown pack",
			processorErr: fmt.Errorf("billing.webhook.grant.resolve: %w", ErrWebhookGrantPackUnknown),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			processor := &stubWebhookProcessor{
				err: testCase.processorErr,
			}
			handler := NewWebhookHandler(&stubWebhookProvider{
				code:                "stub",
				signatureHeaderName: "X-Webhook-Signature",
				parseMetadata:       WebhookEventMetadata{EventID: "evt_1", EventType: "event.type"},
			}, processor)
			request := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(`{"id":"evt_1"}`))
			request.Header.Set("X-Webhook-Signature", "ok")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, webhookAcceptedResponse, recorder.Body.String())
			require.Equal(t, 1, processor.calls)
		})
	}
}

func TestWebhookHandlerAcceptsValidRequest(t *testing.T) {
	processor := &stubWebhookProcessor{}
	handler := NewWebhookHandler(&stubWebhookProvider{
		code:                "stub",
		signatureHeaderName: "X-Webhook-Signature",
		parseMetadata:       WebhookEventMetadata{EventID: "evt_1", EventType: "event.type"},
	}, processor)
	requestPayload := `{"id":"evt_1","type":"event.type"}`
	request := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(requestPayload))
	request.Header.Set("X-Webhook-Signature", "ok")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, webhookAcceptedResponse, recorder.Body.String())
	require.Equal(t, 1, processor.calls)
	require.Equal(t, "stub", processor.event.ProviderCode)
	require.Equal(t, "evt_1", processor.event.EventID)
	require.Equal(t, "event.type", processor.event.EventType)
	require.Equal(t, []byte(requestPayload), processor.event.Payload)
}

func TestWebhookHandlerRejectsOversizedBody(t *testing.T) {
	handler := NewWebhookHandler(&stubWebhookProvider{
		code:                "stub",
		signatureHeaderName: "X-Webhook-Signature",
		parseMetadata:       WebhookEventMetadata{EventID: "evt_1", EventType: "event.type"},
	}, nil)
	oversizedBody := strings.Repeat("x", 1024*1024+1)
	request := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(oversizedBody))
	request.Header.Set("X-Webhook-Signature", "ok")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

// Coverage gap tests for webhook_handler.go

func TestWebhookHandlerReadBodyError(t *testing.T) {
	provider := &stubWebhookProvider{
		code:                "paddle",
		signatureHeaderName: "Paddle-Signature",
	}
	handler := NewWebhookHandler(provider, nil)

	req := httptest.NewRequest(http.MethodPost, "/webhook", &errorReader{err: errors.New("read error")})
	req.Header.Set("Paddle-Signature", "valid")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

type errorReader struct {
	err error
}

func (r *errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestWebhookHandlerMaxBodySize(t *testing.T) {
	provider := &stubWebhookProvider{
		code:                "paddle",
		signatureHeaderName: "Paddle-Signature",
	}
	processor := WebhookProcessorFunc(func(_ context.Context, _ WebhookEvent) error {
		return nil
	})
	handler := NewWebhookHandler(provider, processor)

	// Create a payload larger than 1MB
	largeBody := strings.NewReader(strings.Repeat("x", 1024*1024+1))
	req := httptest.NewRequest(http.MethodPost, "/webhook", largeBody)
	req.Header.Set("Paddle-Signature", "valid")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}
