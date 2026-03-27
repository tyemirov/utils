package billing

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type stubSubscriptionStateRepository struct {
	inputs                    []SubscriptionStateUpsertInput
	upsertErr                 error
	getState                  SubscriptionState
	getFound                  bool
	getErr                    error
	getBySubscriptionState    SubscriptionState
	getBySubscriptionFound    bool
	getBySubscriptionErr      error
	receivedGetSubscriptionID string
}

func (repository *stubSubscriptionStateRepository) Upsert(
	_ context.Context,
	input SubscriptionStateUpsertInput,
) error {
	repository.inputs = append(repository.inputs, input)
	return repository.upsertErr
}

func (repository *stubSubscriptionStateRepository) Get(
	_ context.Context,
	_ string,
	_ string,
) (SubscriptionState, bool, error) {
	if repository.getErr != nil {
		return SubscriptionState{}, false, repository.getErr
	}
	return repository.getState, repository.getFound, nil
}

func (repository *stubSubscriptionStateRepository) GetBySubscriptionID(
	_ context.Context,
	_ string,
	subscriptionID string,
) (SubscriptionState, bool, error) {
	repository.receivedGetSubscriptionID = subscriptionID
	if repository.getBySubscriptionErr != nil {
		return SubscriptionState{}, false, repository.getBySubscriptionErr
	}
	return repository.getBySubscriptionState, repository.getBySubscriptionFound, nil
}

func TestSubscriptionStatusWebhookProcessorTracksActiveStateFromTransactionGrant(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepository := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepository)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_subscription_1",
		map[string]string{
			paddleMetadataUserEmailKey:    "subscriber@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_subscription_1",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.February, 19, 16, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.Len(t, stateRepository.inputs, 1)
	require.Equal(t, subscriptionStatusActive, stateRepository.inputs[0].Status)
	require.Equal(t, PlanCodePro, stateRepository.inputs[0].ActivePlan)
	require.Equal(t, "subscriber@example.com", stateRepository.inputs[0].UserEmail)
	require.Equal(
		t,
		time.Date(2026, time.February, 19, 16, 0, 0, 0, time.UTC),
		stateRepository.inputs[0].EventOccurredAt,
	)
}

func TestSubscriptionStatusWebhookProcessorTracksInactiveStateFromCanceledEvent(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepository := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepository)
	require.NoError(t, processorErr)

	eventPayload := map[string]interface{}{
		"event_id":   "evt_subscription_canceled_1",
		"event_type": "subscription.canceled",
		"data": map[string]interface{}{
			"id":     "sub_123",
			"status": "canceled",
			"customer": map[string]interface{}{
				"email": "subscriber@example.com",
			},
			"custom_data": map[string]interface{}{
				paddleMetadataPlanCodeKey: PlanCodePlus,
			},
		},
	}
	eventPayloadBytes, marshalErr := json.Marshal(eventPayload)
	require.NoError(t, marshalErr)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_subscription_canceled_1",
		EventType:    "subscription.canceled",
		Payload:      eventPayloadBytes,
	})
	require.NoError(t, processErr)
	require.Len(t, stateRepository.inputs, 1)
	require.Equal(t, subscriptionStatusInactive, stateRepository.inputs[0].Status)
	require.Equal(t, "", stateRepository.inputs[0].ActivePlan)
	require.Equal(t, "subscriber@example.com", stateRepository.inputs[0].UserEmail)
}

func TestSubscriptionStatusWebhookProcessorIgnoresStaleTransactionGrantEvent(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepository := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			ProviderCode:        ProviderCodePaddle,
			UserEmail:           "subscriber@example.com",
			Status:              subscriptionStatusInactive,
			ActivePlan:          "",
			LastEventOccurredAt: time.Date(2026, time.February, 19, 18, 0, 0, 0, time.UTC),
		},
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepository)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_subscription_1",
		map[string]string{
			paddleMetadataUserEmailKey:    "subscriber@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_subscription_1",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.February, 19, 17, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.Empty(t, stateRepository.inputs)
}

func TestSubscriptionStatusWebhookProcessorAppliesSyncEventEvenWhenOlderThanStoredState(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepository := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			ProviderCode:        ProviderCodePaddle,
			UserEmail:           "subscriber@example.com",
			Status:              subscriptionStatusInactive,
			ActivePlan:          "",
			LastEventOccurredAt: time.Date(2026, time.February, 19, 18, 0, 0, 0, time.UTC),
		},
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepository)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_subscription_sync_1",
		map[string]string{
			paddleMetadataUserEmailKey:    "subscriber@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "sync:transaction:txn_subscription_sync_1",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.February, 19, 17, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.Len(t, stateRepository.inputs, 1)
	require.Equal(t, subscriptionStatusActive, stateRepository.inputs[0].Status)
	require.Equal(t, PlanCodePro, stateRepository.inputs[0].ActivePlan)
}

func TestSubscriptionStatusWebhookProcessorResolvesCustomerEmailByCustomerID(t *testing.T) {
	commerceClient := &stubPaddleCommerceClient{
		resolvedCustomerEmail: "subscriber@example.com",
	}
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)
	stateRepository := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepository)
	require.NoError(t, processorErr)

	eventPayload := map[string]interface{}{
		"event_id":   "evt_subscription_updated_1",
		"event_type": "subscription.updated",
		"data": map[string]interface{}{
			"id":          "sub_123",
			"status":      "active",
			"customer_id": "ctm_123",
			"custom_data": map[string]interface{}{},
			"items": []map[string]interface{}{
				{
					"price": map[string]interface{}{
						"id": "pri_plus",
					},
				},
			},
		},
	}
	eventPayloadBytes, marshalErr := json.Marshal(eventPayload)
	require.NoError(t, marshalErr)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_subscription_updated_1",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.February, 19, 17, 30, 0, 0, time.UTC),
		Payload:      eventPayloadBytes,
	})
	require.NoError(t, processErr)
	require.Len(t, stateRepository.inputs, 1)
	require.Equal(t, "subscriber@example.com", stateRepository.inputs[0].UserEmail)
	require.Equal(t, PlanCodePlus, stateRepository.inputs[0].ActivePlan)
	require.Equal(t, "ctm_123", commerceClient.receivedResolveCustomerID)
}

func TestSubscriptionStatusWebhookProcessorReactivatesInactiveStateFromNewTransaction(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepository := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			ProviderCode:        ProviderCodePaddle,
			UserEmail:           "subscriber@example.com",
			Status:              subscriptionStatusInactive,
			ActivePlan:          "",
			LastEventOccurredAt: time.Date(2026, time.February, 19, 16, 0, 0, 0, time.UTC),
		},
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepository)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_subscription_reactivate",
		map[string]string{
			paddleMetadataUserEmailKey:    "subscriber@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePlus,
		},
	)
	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_subscription_reactivate",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.February, 19, 17, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.Len(t, stateRepository.inputs, 1)
	require.Equal(t, subscriptionStatusActive, stateRepository.inputs[0].Status)
	require.Equal(t, PlanCodePlus, stateRepository.inputs[0].ActivePlan)
}

func TestSubscriptionStatusWebhookProcessorContinuesWhenSubscriptionLookupFails(t *testing.T) {
	commerceClient := &stubPaddleCommerceClient{
		getSubscriptionErr: errors.New("upstream_unavailable"),
	}
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)
	stateRepository := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepository)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_subscription_lookup_fails",
		map[string]string{
			paddleMetadataUserEmailKey:    "subscriber@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_subscription_lookup_fails",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.February, 19, 18, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})

	require.NoError(t, processErr)
	require.Equal(t, "sub_test", commerceClient.receivedSubscriptionID)
	require.Len(t, stateRepository.inputs, 1)
	require.Equal(t, subscriptionStatusActive, stateRepository.inputs[0].Status)
	require.Equal(t, paddleSubscriptionStatusActive, stateRepository.inputs[0].ProviderStatus)
	require.Equal(t, PlanCodePro, stateRepository.inputs[0].ActivePlan)
}

func TestSubscriptionStatusWebhookProcessorResolvesLifecycleUserFromSubscriptionState(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepository := &stubSubscriptionStateRepository{
		getBySubscriptionFound: true,
		getBySubscriptionState: SubscriptionState{
			ProviderCode:   ProviderCodePaddle,
			UserEmail:      "state-owner@example.com",
			SubscriptionID: "sub_existing_1",
			Status:         subscriptionStatusActive,
			ActivePlan:     PlanCodePro,
		},
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepository)
	require.NoError(t, processorErr)

	eventPayload := map[string]interface{}{
		"event_id":   "evt_subscription_canceled_lookup_1",
		"event_type": "subscription.canceled",
		"data": map[string]interface{}{
			"id":          "sub_existing_1",
			"status":      "canceled",
			"custom_data": map[string]interface{}{},
		},
	}
	eventPayloadBytes, marshalErr := json.Marshal(eventPayload)
	require.NoError(t, marshalErr)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_subscription_canceled_lookup_1",
		EventType:    "subscription.canceled",
		OccurredAt:   time.Date(2026, time.February, 19, 18, 30, 0, 0, time.UTC),
		Payload:      eventPayloadBytes,
	})
	require.NoError(t, processErr)
	require.Equal(t, "sub_existing_1", stateRepository.receivedGetSubscriptionID)
	require.Len(t, stateRepository.inputs, 1)
	require.Equal(t, "state-owner@example.com", stateRepository.inputs[0].UserEmail)
	require.Equal(t, subscriptionStatusInactive, stateRepository.inputs[0].Status)
}

func TestSubscriptionStatusWebhookProcessorReturnsErrorWhenSubscriptionLookupFails(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepository := &stubSubscriptionStateRepository{
		getBySubscriptionErr: errors.New("repository_unavailable"),
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepository)
	require.NoError(t, processorErr)

	eventPayload := map[string]interface{}{
		"event_id":   "evt_subscription_updated_lookup_err_1",
		"event_type": "subscription.updated",
		"data": map[string]interface{}{
			"id":          "sub_lookup_err_1",
			"status":      "active",
			"custom_data": map[string]interface{}{},
		},
	}
	eventPayloadBytes, marshalErr := json.Marshal(eventPayload)
	require.NoError(t, marshalErr)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_subscription_updated_lookup_err_1",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.February, 19, 18, 35, 0, 0, time.UTC),
		Payload:      eventPayloadBytes,
	})
	require.Error(t, processErr)
	require.Contains(t, processErr.Error(), "billing.webhook.subscription_state.get_by_subscription_id")
	require.Equal(t, "sub_lookup_err_1", stateRepository.receivedGetSubscriptionID)
	require.Empty(t, stateRepository.inputs)
}

func TestNewSubscriptionStatusWebhookProcessorRejectsProviderWithoutProcessorCapability(t *testing.T) {
	stateRepository := &stubSubscriptionStateRepository{}
	_, processorErr := NewSubscriptionStatusWebhookProcessor(
		unsupportedWebhookGrantResolverProvider{},
		stateRepository,
	)
	require.Error(t, processorErr)
	require.ErrorIs(t, processorErr, ErrWebhookSubscriptionStateProviderUnsupported)
}

func TestResolveSubscriptionPlanCodeFromGrantReasonExtractsCode(t *testing.T) {
	planCode := resolveSubscriptionPlanCodeFromGrantReason("subscription_monthly_pro")
	require.Equal(t, "pro", planCode)

	planCode = resolveSubscriptionPlanCodeFromGrantReason("subscription_monthly_plus")
	require.Equal(t, "plus", planCode)
}

func TestResolveSubscriptionPlanCodeFromGrantReasonReturnsEmptyForInvalidPrefix(t *testing.T) {
	planCode := resolveSubscriptionPlanCodeFromGrantReason("top_up_pack_gold")
	require.Equal(t, "", planCode)

	planCode = resolveSubscriptionPlanCodeFromGrantReason("random_string")
	require.Equal(t, "", planCode)
}

func TestResolveSubscriptionPlanCodeFromGrantReasonReturnsEmptyForEmptyString(t *testing.T) {
	planCode := resolveSubscriptionPlanCodeFromGrantReason("")
	require.Equal(t, "", planCode)

	planCode = resolveSubscriptionPlanCodeFromGrantReason("   ")
	require.Equal(t, "", planCode)
}

// Coverage gap tests for subscription_status_webhook_processor.go

func TestNewSubscriptionStatusWebhookProcessorNilState(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	_, err := NewSubscriptionStatusWebhookProcessor(provider, nil)
	require.ErrorIs(t, err, ErrWebhookSubscriptionStateRepositoryUnavailable)
}

func TestNewSubscriptionStatusWebhookProcessorNilProvider(t *testing.T) {
	stateRepo := &stubSubscriptionStateRepository{}
	_, err := NewSubscriptionStatusWebhookProcessor(nil, stateRepo)
	require.ErrorIs(t, err, ErrWebhookGrantResolverProviderUnavailable)
}

func TestNewSubscriptionStatusWebhookProcessorUnsupportedProvider(t *testing.T) {
	stateRepo := &stubSubscriptionStateRepository{}
	_, err := NewSubscriptionStatusWebhookProcessor(unsupportedWebhookGrantResolverProvider{}, stateRepo)
	require.ErrorIs(t, err, ErrWebhookSubscriptionStateProviderUnsupported)
}

func TestNewPaddleSubscriptionStatusWebhookProcessorNilProvider(t *testing.T) {
	_, err := newPaddleSubscriptionStatusWebhookProcessor(nil, &stubSubscriptionStateRepository{})
	require.ErrorIs(t, err, ErrWebhookGrantResolverProviderUnavailable)
}

func TestNewPaddleSubscriptionStatusWebhookProcessorNilState(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	_, err := newPaddleSubscriptionStatusWebhookProcessor(provider, nil)
	require.ErrorIs(t, err, ErrWebhookSubscriptionStateRepositoryUnavailable)
}

func TestBuildPaddlePlanCodeByPriceIDEmptyDefinitions(t *testing.T) {
	result := buildPaddlePlanCodeByPriceID(nil)
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestBuildPaddlePlanCodeByPriceIDEmptyPriceID(t *testing.T) {
	defs := map[string]paddlePlanDefinition{
		"pro": {PriceID: "", Plan: SubscriptionPlan{Code: "pro"}},
	}
	result := buildPaddlePlanCodeByPriceID(defs)
	require.Empty(t, result)
}

func TestProcessIgnoresDifferentProvider(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: "other_provider",
		EventType:    "transaction.completed",
		Payload:      []byte(`{}`),
	})
	require.NoError(t, err)
	require.Empty(t, stateRepo.inputs)
}

func TestResolvePaddleSubscriptionStateSpecialCases(t *testing.T) {
	// canceled event -> inactive
	require.Equal(t, subscriptionStatusInactive, resolvePaddleSubscriptionState("subscription.canceled", "active"))

	// trialing status -> active
	require.Equal(t, subscriptionStatusActive, resolvePaddleSubscriptionState("subscription.updated", "trialing"))

	// paused status -> inactive
	require.Equal(t, subscriptionStatusInactive, resolvePaddleSubscriptionState("subscription.updated", "paused"))

	// created event with unknown status -> active
	require.Equal(t, subscriptionStatusActive, resolvePaddleSubscriptionState("subscription.created", "unknown"))

	// resumed event with unknown status -> active
	require.Equal(t, subscriptionStatusActive, resolvePaddleSubscriptionState("subscription.resumed", "unknown"))

	// activated event with unknown status -> active
	require.Equal(t, subscriptionStatusActive, resolvePaddleSubscriptionState("subscription.activated", "unknown"))

	// unknown event with unknown status -> inactive
	require.Equal(t, subscriptionStatusInactive, resolvePaddleSubscriptionState("unknown", "unknown"))

	// past_due status -> inactive
	require.Equal(t, subscriptionStatusInactive, resolvePaddleSubscriptionState("subscription.updated", "past_due"))
}

func TestResolveLifecycleUserEmailFallbackToSubscriptionState(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getBySubscriptionState: SubscriptionState{UserEmail: "user@example.com"},
		getBySubscriptionFound: true,
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:         "sub_123",
			Status:     "active",
			CustomerID: "",
			UpdatedAt:  "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_lifecycle",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
}

func TestResolveLifecycleUserEmailGetBySubscriptionError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getBySubscriptionErr: errors.New("db error"),
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:         "sub_123",
			Status:     "active",
			CustomerID: "",
			UpdatedAt:  "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_lifecycle",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.Error(t, err)
}

func TestResolvePayloadUserEmailWithCustomerEmail(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:     "sub_123",
			Status: "active",
			Customer: paddleTransactionCompletedCustomer{
				Email: "customer@example.com",
			},
			UpdatedAt: "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_lifecycle",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, stateRepo.inputs, 1)
	require.Equal(t, "customer@example.com", stateRepo.inputs[0].UserEmail)
}

func TestResolvePaddleSubscriptionPriceIDFromNestedPrice(t *testing.T) {
	priceID := resolvePaddleSubscriptionPriceID(paddleSubscriptionWebhookData{
		Items: []paddleSubscriptionWebhookItem{
			{PriceID: "", Price: paddleSubscriptionWebhookItemPrice{ID: "pri_nested"}},
		},
	})
	require.Equal(t, "pri_nested", priceID)
}

func TestResolvePaddleSubscriptionNextBillingAtFallback(t *testing.T) {
	result := resolvePaddleSubscriptionNextBillingAt(paddleSubscriptionWebhookData{
		NextBilledAt: "",
		CurrentBillingPeriod: paddleSubscriptionBillingPeriod{
			EndsAt: "2026-04-15T10:30:00Z",
		},
	})
	require.False(t, result.IsZero())
}

func TestResolvePaddleSubscriptionNextBillingAtBothEmpty(t *testing.T) {
	result := resolvePaddleSubscriptionNextBillingAt(paddleSubscriptionWebhookData{})
	require.True(t, result.IsZero())
}

func TestIsStaleSubscriptionEventBothZero(t *testing.T) {
	require.False(t, isStaleSubscriptionEvent(time.Time{}, time.Time{}))
}

func TestIsStaleSubscriptionEventExistingZero(t *testing.T) {
	require.False(t, isStaleSubscriptionEvent(time.Time{}, time.Now()))
}

func TestIsStaleSubscriptionEventIncomingZero(t *testing.T) {
	require.False(t, isStaleSubscriptionEvent(time.Now(), time.Time{}))
}

func TestIsStaleSubscriptionEventIncomingBefore(t *testing.T) {
	existing := time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC)
	incoming := time.Date(2026, time.March, 14, 10, 0, 0, 0, time.UTC)
	require.True(t, isStaleSubscriptionEvent(existing, incoming))
}

func TestIsStaleSubscriptionEventIncomingAfter(t *testing.T) {
	existing := time.Date(2026, time.March, 14, 10, 0, 0, 0, time.UTC)
	incoming := time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC)
	require.False(t, isStaleSubscriptionEvent(existing, incoming))
}

func TestProcessTransactionEventStaleIsSkipped(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			LastEventOccurredAt: time.Date(2026, time.March, 20, 10, 0, 0, 0, time.UTC),
		},
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_old",
		map[string]string{
			paddleMetadataUserEmailKey:    "user@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_old",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 10, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, err)
	require.Empty(t, stateRepo.inputs)
}

func TestProcessTransactionEventResolvesFromExistingState(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			ActivePlan:          "pro",
			LastEventOccurredAt: time.Date(2026, time.March, 10, 10, 0, 0, 0, time.UTC),
		},
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	// Transaction with plan code in metadata, but no plan code from reason
	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_with_plan",
		map[string]string{
			paddleMetadataUserEmailKey:    "user@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "sync:evt_plan",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, err)
	require.Len(t, stateRepo.inputs, 1)
	require.Equal(t, PlanCodePro, stateRepo.inputs[0].ActivePlan)
}

func TestProcessSubscriptionLifecycleEventInactiveResetsPlan(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:     "sub_123",
			Status: "canceled",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
				paddleMetadataPlanCodeKey:  "pro",
			},
			UpdatedAt: "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_canceled",
		EventType:    "subscription.canceled",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, stateRepo.inputs, 1)
	require.Equal(t, "", stateRepo.inputs[0].ActivePlan)
	require.Equal(t, subscriptionStatusInactive, stateRepo.inputs[0].Status)
}

// Additional coverage gap tests

func TestProcessTransactionEventNonSubscriptionPurchaseKindSkipped(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_topup",
		map[string]string{
			paddleMetadataUserEmailKey:    "user@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindTopUpPack,
			paddleMetadataPackCodeKey:     PackCodeTopUp,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_topup",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, err)
	require.Empty(t, stateRepo.inputs)
}

func TestProcessTransactionEventGetStateError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getErr: errors.New("db error"),
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_dberr",
		map[string]string{
			paddleMetadataUserEmailKey:    "user@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_dberr",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.webhook.subscription_state.get")
}

func TestProcessTransactionEventUpsertError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		upsertErr: errors.New("upsert failed"),
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_upsert",
		map[string]string{
			paddleMetadataUserEmailKey:    "user@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_upsert",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.webhook.subscription_state.upsert")
}

func TestProcessTransactionEventWithSubscriptionResolver(t *testing.T) {
	client := &stubPaddleCommerceClient{
		subscription: paddleSubscriptionWebhookData{
			ID:           "sub_resolved",
			Status:       "active",
			NextBilledAt: "2026-04-15T10:30:00Z",
			Items: []paddleSubscriptionWebhookItem{
				{PriceID: "pri_pro"},
			},
		},
	}
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		client,
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_withsub",
		map[string]string{
			paddleMetadataUserEmailKey:    "user@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_withsub",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, err)
	require.Len(t, stateRepo.inputs, 1)
	require.Equal(t, subscriptionStatusActive, stateRepo.inputs[0].Status)
}

func TestProcessTransactionEventSubscriptionResolverError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		getSubscriptionErr: errors.New("api error"),
	}
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		client,
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_suberr",
		map[string]string{
			paddleMetadataUserEmailKey:    "user@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_suberr",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, err)
}

func TestProcessTransactionEventSubscriptionNotFound(t *testing.T) {
	client := &stubPaddleCommerceClient{
		getSubscriptionErr: ErrPaddleAPISubscriptionNotFound,
	}
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		client,
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_subnotfound",
		map[string]string{
			paddleMetadataUserEmailKey:    "user@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_subnotfound",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, err)
}

func TestProcessTransactionEventInactiveSubscriptionClearsPlan(t *testing.T) {
	client := &stubPaddleCommerceClient{
		subscription: paddleSubscriptionWebhookData{
			ID:     "sub_canceled",
			Status: "canceled",
		},
	}
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		client,
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_inactive",
		map[string]string{
			paddleMetadataUserEmailKey:    "user@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_inactive",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, err)
	require.Len(t, stateRepo.inputs, 1)
	require.Equal(t, "", stateRepo.inputs[0].ActivePlan)
}

func TestProcessSubscriptionLifecycleEventGetStateError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getErr: errors.New("db error"),
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:     "sub_123",
			Status: "active",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
			},
			UpdatedAt: "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_staterr",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.webhook.subscription_state.get")
}

func TestProcessSubscriptionLifecycleEventStaleSkipped(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			LastEventOccurredAt: time.Date(2026, time.March, 20, 10, 0, 0, 0, time.UTC),
		},
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:     "sub_123",
			Status: "active",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
			},
			UpdatedAt: "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_stale",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 10, 10, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Empty(t, stateRepo.inputs)
}

func TestProcessSubscriptionLifecycleEventUpsertError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		upsertErr: errors.New("upsert error"),
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:     "sub_123",
			Status: "active",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
			},
			UpdatedAt: "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_upserterr",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.webhook.subscription_state.upsert")
}

func TestResolveLifecycleUserEmailNotFoundFallback(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getBySubscriptionFound: false,
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:         "sub_123",
			Status:     "active",
			CustomerID: "",
			UpdatedAt:  "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_noemail",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestResolveLifecycleUserEmailEmptySubscriptionID(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:         "",
			Status:     "active",
			CustomerID: "",
			UpdatedAt:  "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_noid",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestResolvePayloadUserEmailViaCustomerEmailResolverEmpty(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerEmail: "  ",
	}
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		client,
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:         "sub_123",
			Status:     "active",
			CustomerID: "cus_123",
			UpdatedAt:  "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_emptyemail",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestProcessTransactionEventPendingCheckoutStatus(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayloadWithStatus(
		"transaction.updated",
		"paid",
		"txn_pending",
		map[string]string{
			paddleMetadataUserEmailKey:    "user@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_pending",
		EventType:    "transaction.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, err)
	require.Len(t, stateRepo.inputs, 1)
}

func TestResolveLifecycleUserEmailStateEmailEmpty(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getBySubscriptionFound: true,
		getBySubscriptionState: SubscriptionState{UserEmail: "  "},
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:         "sub_123",
			Status:     "active",
			CustomerID: "",
			UpdatedAt:  "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_emptystate",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestNewPaddleSubscriptionStatusWebhookProcessorNilStateRepo(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	_, err := newPaddleSubscriptionStatusWebhookProcessor(provider, nil)
	require.ErrorIs(t, err, ErrWebhookSubscriptionStateRepositoryUnavailable)
}

func TestProcessUnknownEventTypeIgnored(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_unknown",
		EventType:    "customer.created",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      []byte(`{}`),
	})
	require.NoError(t, err)
	require.Empty(t, stateRepo.inputs)
}

func TestProcessTransactionEventGrantResolveError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_bad_payload",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      []byte(`not json`),
	})
	require.Error(t, err)
}

func TestProcessTransactionEventStateGetError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getErr: errors.New("db error"),
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_sub_stateErr",
		map[string]string{
			paddleMetadataUserEmailKey:    "subscriber@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_stateErr",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "subscription_state.get")
}

func TestProcessTransactionEventStaleEvent(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			LastEventOccurredAt: time.Date(2026, time.March, 20, 10, 0, 0, 0, time.UTC),
		},
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_sub_stale",
		map[string]string{
			paddleMetadataUserEmailKey:    "subscriber@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_stale",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, err)
	require.Empty(t, stateRepo.inputs)
}

func TestProcessTransactionEventSubscriptionResolverNonNotFoundError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{
			getSubscriptionErr: errors.New("subscription api error"),
		},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_sub_resolver_err2",
		map[string]string{
			paddleMetadataUserEmailKey:    "subscriber@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_resolver_err2",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, err)
	require.Len(t, stateRepo.inputs, 1)
}

func TestResolvePaddleSubscriptionPriceIDFromPriceObject(t *testing.T) {
	priceID := resolvePaddleSubscriptionPriceID(paddleSubscriptionWebhookData{
		Items: []paddleSubscriptionWebhookItem{
			{PriceID: "", Price: paddleSubscriptionWebhookItemPrice{ID: "pri_from_price_obj"}},
		},
	})
	require.Equal(t, "pri_from_price_obj", priceID)
}

func TestResolvePayloadUserEmailFromCustomerEmailResolver(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{
			resolvedCustomerEmail: "resolved@example.com",
		},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:         "sub_resolve",
			Status:     "active",
			CustomerID: "cus_123",
			UpdatedAt:  "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_resolve_email",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, stateRepo.inputs, 1)
	require.Equal(t, "resolved@example.com", stateRepo.inputs[0].UserEmail)
}

func TestResolvePayloadUserEmailResolverError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{
			resolveCustomerEmailErr: errors.New("api error"),
		},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:         "sub_err",
			Status:     "active",
			CustomerID: "cus_123",
			UpdatedAt:  "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_resolve_err",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestResolveLifecycleUserEmailFallsBackToStateRepo(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getBySubscriptionErr: errors.New("db error"),
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:         "sub_nouser",
			Status:     "active",
			CustomerID: "",
			UpdatedAt:  "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_nouser",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "subscription_state.get_by_subscription_id")
}

func TestResolveLifecycleUserEmailNoSubscriptionID(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleSubscriptionWebhookPayload{
		Data: paddleSubscriptionWebhookData{
			ID:         "",
			Status:     "active",
			CustomerID: "",
			UpdatedAt:  "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_nosub",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestProcessSubscriptionLifecycleEventDecodeError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_badjson",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      []byte(`not json`),
	})
	require.ErrorIs(t, err, ErrWebhookGrantPayloadInvalid)
}

func TestResolvePaddleSubscriptionPriceIDFromPriceField(t *testing.T) {
	result := resolvePaddleSubscriptionPriceID(paddleSubscriptionWebhookData{
		Items: []paddleSubscriptionWebhookItem{
			{PriceID: "", Price: paddleSubscriptionWebhookItemPrice{ID: "pri_from_price_field"}},
		},
	})
	require.Equal(t, "pri_from_price_field", result)
}

func TestResolvePaddleSubscriptionPriceIDEmptyItems(t *testing.T) {
	result := resolvePaddleSubscriptionPriceID(paddleSubscriptionWebhookData{
		Items: []paddleSubscriptionWebhookItem{},
	})
	require.Equal(t, "", result)
}

func TestProcessTransactionEventPlanCodeFallbackFromGrantReason(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{
			subscription: paddleSubscriptionWebhookData{
				ID:     "sub_123",
				Status: "active",
				Items: []paddleSubscriptionWebhookItem{
					{PriceID: "pri_pro"},
				},
				UpdatedAt: "2026-03-15T10:30:00Z",
			},
		},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:             "txn_plan_code_fallback",
			Status:         "completed",
			SubscriptionID: "sub_123",
			CustomerID:     "cus_123",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			},
			Items: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_pro"},
			},
			BilledAt: "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_plan_code_fallback",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, stateRepo.inputs, 1)
	require.Equal(t, PlanCodePro, stateRepo.inputs[0].ActivePlan)
}

func TestProcessTransactionEventPlanCodeFallbackFromExistingState(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{
			subscription: paddleSubscriptionWebhookData{
				ID:        "sub_123",
				Status:    "active",
				UpdatedAt: "2026-03-15T10:30:00Z",
			},
		},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getState: SubscriptionState{
			ActivePlan: PlanCodePlus,
		},
		getFound: true,
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:             "txn_state_fallback",
			Status:         "completed",
			SubscriptionID: "sub_123",
			CustomerID:     "cus_123",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			},
			Items: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_pro"},
			},
			BilledAt: "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_state_fallback",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, stateRepo.inputs, 1)
}

func TestProcessTransactionEventSubscriptionResolverTransientError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{
			getSubscriptionErr: errors.New("transient error"),
		},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:             "txn_sub_err",
			Status:         "completed",
			SubscriptionID: "sub_123",
			CustomerID:     "cus_123",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
				paddleMetadataPlanCodeKey:     PlanCodePro,
			},
			Items: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_pro"},
			},
			BilledAt: "2026-03-15T10:30:00Z",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_sub_err",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 30, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, stateRepo.inputs, 1)
}
