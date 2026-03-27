package billing

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const (
	webhookUnavailableMessage                = "Billing webhook unavailable"
	webhookInvalidPayloadMessage             = "Invalid payload"
	webhookUnauthorizedMessage               = "Unauthorized"
	webhookAcceptedResponse                  = "ok"
	webhookPayloadProcessFailedMessage       = "Webhook processing failed"
	webhookMaxBodyBytes                int64 = 1024 * 1024
)

type WebhookHandler struct {
	Provider  WebhookProvider
	Processor WebhookProcessor
}

func NewWebhookHandler(provider WebhookProvider, processor WebhookProcessor) *WebhookHandler {
	return &WebhookHandler{
		Provider:  provider,
		Processor: resolveWebhookProcessor(processor),
	}
}

func (handler *WebhookHandler) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(responseWriter, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if handler == nil || handler.Provider == nil {
		http.Error(responseWriter, webhookUnavailableMessage, http.StatusServiceUnavailable)
		return
	}

	requestPayload, payloadErr := io.ReadAll(http.MaxBytesReader(responseWriter, request.Body, webhookMaxBodyBytes))
	if payloadErr != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(payloadErr, &maxBytesErr) {
			http.Error(responseWriter, webhookInvalidPayloadMessage, http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(responseWriter, webhookInvalidPayloadMessage, http.StatusBadRequest)
		return
	}

	signatureHeader := strings.TrimSpace(request.Header.Get(handler.Provider.SignatureHeaderName()))
	if signatureHeader == "" {
		http.Error(responseWriter, webhookUnauthorizedMessage, http.StatusUnauthorized)
		return
	}

	if verifyErr := handler.Provider.VerifySignature(signatureHeader, requestPayload); verifyErr != nil {
		slog.Warn("billing webhook signature verification failed", "provider", handler.Provider.Code(), "error", verifyErr)
		http.Error(responseWriter, webhookUnauthorizedMessage, http.StatusUnauthorized)
		return
	}

	eventMetadata, eventErr := handler.Provider.ParseWebhookEvent(requestPayload)
	if eventErr != nil {
		http.Error(responseWriter, webhookInvalidPayloadMessage, http.StatusBadRequest)
		return
	}

	webhookEvent := WebhookEvent{
		ProviderCode: handler.Provider.Code(),
		EventID:      eventMetadata.EventID,
		EventType:    eventMetadata.EventType,
		OccurredAt:   eventMetadata.OccurredAt,
		Payload:      requestPayload,
	}

	processor := resolveWebhookProcessor(handler.Processor)
	if processErr := processor.Process(request.Context(), webhookEvent); processErr != nil {
		if isWebhookProcessErrorNonRetryable(processErr) {
			slog.Warn(
				"billing webhook skipped",
				"provider", webhookEvent.ProviderCode,
				"event_id", webhookEvent.EventID,
				"event_type", webhookEvent.EventType,
				"error", processErr,
			)
			writeWebhookAcceptedResponse(responseWriter)
			return
		}
		slog.Warn(
			"billing webhook processing failed",
			"provider", webhookEvent.ProviderCode,
			"event_id", webhookEvent.EventID,
			"event_type", webhookEvent.EventType,
			"error", processErr,
		)
		http.Error(responseWriter, webhookPayloadProcessFailedMessage, http.StatusInternalServerError)
		return
	}

	slog.Info(
		"billing webhook accepted",
		"provider", webhookEvent.ProviderCode,
		"event_id", webhookEvent.EventID,
		"event_type", webhookEvent.EventType,
	)
	writeWebhookAcceptedResponse(responseWriter)
}

func isWebhookProcessErrorNonRetryable(processErr error) bool {
	return errors.Is(processErr, ErrWebhookGrantPayloadInvalid) ||
		errors.Is(processErr, ErrWebhookGrantMetadataInvalid) ||
		errors.Is(processErr, ErrWebhookGrantPlanUnknown) ||
		errors.Is(processErr, ErrWebhookGrantPackUnknown)
}

func writeWebhookAcceptedResponse(responseWriter http.ResponseWriter) {
	responseWriter.WriteHeader(http.StatusOK)
	_, _ = responseWriter.Write([]byte(webhookAcceptedResponse))
}
