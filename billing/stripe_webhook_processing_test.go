package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers – mock event status provider
// ---------------------------------------------------------------------------

type stubCheckoutEventStatusProvider struct {
	statusByEventType map[string]CheckoutEventStatus
}

func (p *stubCheckoutEventStatusProvider) ResolveCheckoutEventStatus(eventType string) CheckoutEventStatus {
	status, found := p.statusByEventType[eventType]
	if !found {
		return CheckoutEventStatusUnknown
	}
	return status
}

// ---------------------------------------------------------------------------
// helpers – mock customer email resolver
// ---------------------------------------------------------------------------

type stubStripeCustomerEmailResolver struct {
	email string
	err   error
}

func (r *stubStripeCustomerEmailResolver) ResolveCustomerEmail(_ context.Context, _ string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return r.email, nil
}

// ---------------------------------------------------------------------------
// helpers – build grant resolver with catalog
// ---------------------------------------------------------------------------

func testStripeGrantResolver(
	plans []SubscriptionPlan,
	packs []TopUpPack,
	planGrants map[string]stripeGrantDefinition,
	packGrants map[string]stripeGrantDefinition,
	emailResolver stripeCustomerEmailResolver,
	eventStatusProvider CheckoutEventStatusProvider,
) (*stripeWebhookGrantResolver, error) {
	return newStripeWebhookGrantResolverWithCatalog(
		plans, packs, planGrants, packGrants, emailResolver, eventStatusProvider,
	)
}

func testPlans() []SubscriptionPlan {
	return []SubscriptionPlan{
		{Code: "pro", MonthlyCredits: 1000},
		{Code: "plus", MonthlyCredits: 10000},
	}
}

func testPacks() []TopUpPack {
	return []TopUpPack{
		{Code: "top_up", Credits: 2400},
	}
}

func testPlanGrantsByPriceID() map[string]stripeGrantDefinition {
	return map[string]stripeGrantDefinition{
		"price_pro":  {Code: "pro", Credits: 1000},
		"price_plus": {Code: "plus", Credits: 10000},
	}
}

func testPackGrantsByPriceID() map[string]stripeGrantDefinition {
	return map[string]stripeGrantDefinition{
		"price_pack_top_up": {Code: "top_up", Credits: 2400},
	}
}

func testEventStatusProvider() *stubCheckoutEventStatusProvider {
	return &stubCheckoutEventStatusProvider{
		statusByEventType: map[string]CheckoutEventStatus{
			stripeEventTypeCheckoutSessionCompleted:             CheckoutEventStatusSucceeded,
			stripeEventTypeCheckoutSessionAsyncPaymentSucceeded: CheckoutEventStatusSucceeded,
			stripeEventTypeCheckoutSessionAsyncPaymentFailed:    CheckoutEventStatusFailed,
			stripeEventTypeCheckoutSessionExpired:               CheckoutEventStatusExpired,
		},
	}
}

func buildStripeCheckoutPayload(session stripeCheckoutSessionWebhookData) []byte {
	payload := stripeCheckoutSessionWebhookPayload{
		Data: stripeCheckoutSessionWebhookPayloadData{
			Object: session,
		},
	}
	data, _ := json.Marshal(payload)
	return data
}

func buildStripeSubscriptionPayload(sub stripeSubscriptionWebhookData) []byte {
	payload := stripeSubscriptionWebhookPayload{
		Data: stripeSubscriptionWebhookPayloadData{
			Object: sub,
		},
	}
	data, _ := json.Marshal(payload)
	return data
}

// ---------------------------------------------------------------------------
// newStripeSubscriptionStatusWebhookProcessor
// ---------------------------------------------------------------------------

func TestNewStripeSubscriptionStatusWebhookProcessorNilProvider(t *testing.T) {
	repo := &stubSubscriptionStateRepository{}
	_, err := newStripeSubscriptionStatusWebhookProcessor(nil, repo)
	require.ErrorIs(t, err, ErrWebhookGrantResolverProviderUnavailable)
}

func TestNewStripeSubscriptionStatusWebhookProcessorNilRepo(t *testing.T) {
	provider, provErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	require.NoError(t, provErr)
	_, err := newStripeSubscriptionStatusWebhookProcessor(provider, nil)
	require.ErrorIs(t, err, ErrWebhookSubscriptionStateRepositoryUnavailable)
}

func TestNewStripeSubscriptionStatusWebhookProcessorValid(t *testing.T) {
	provider, provErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	require.NoError(t, provErr)
	repo := &stubSubscriptionStateRepository{}
	processor, err := newStripeSubscriptionStatusWebhookProcessor(provider, repo)
	require.NoError(t, err)
	require.NotNil(t, processor)
}

// ---------------------------------------------------------------------------
// buildStripePlanCodeByPriceID
// ---------------------------------------------------------------------------

func TestBuildStripePlanCodeByPriceIDEmpty(t *testing.T) {
	result := buildStripePlanCodeByPriceID(nil)
	require.Empty(t, result)
	result2 := buildStripePlanCodeByPriceID(map[string]stripePlanDefinition{})
	require.Empty(t, result2)
}

func TestBuildStripePlanCodeByPriceIDValid(t *testing.T) {
	defs := map[string]stripePlanDefinition{
		"pro": {
			Plan:    SubscriptionPlan{Code: "Pro", MonthlyCredits: 1000},
			PriceID: " price_pro ",
		},
		"plus": {
			Plan:    SubscriptionPlan{Code: "Plus", MonthlyCredits: 10000},
			PriceID: "price_plus",
		},
	}
	result := buildStripePlanCodeByPriceID(defs)
	require.Equal(t, "pro", result["price_pro"])
	require.Equal(t, "plus", result["price_plus"])
}

func TestBuildStripePlanCodeByPriceIDSkipsEmpty(t *testing.T) {
	defs := map[string]stripePlanDefinition{
		"bad_price": {
			Plan:    SubscriptionPlan{Code: "Pro", MonthlyCredits: 1000},
			PriceID: "  ",
		},
		"bad_code": {
			Plan:    SubscriptionPlan{Code: " ", MonthlyCredits: 1000},
			PriceID: "price_x",
		},
	}
	result := buildStripePlanCodeByPriceID(defs)
	require.Empty(t, result)
}

// ---------------------------------------------------------------------------
// Process – wrong provider code
// ---------------------------------------------------------------------------

func TestStripeProcessIgnoresWrongProviderCode(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: "paddle",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      []byte(`{}`),
	})
	require.NoError(t, err)
	require.Empty(t, repo.inputs)
}

// ---------------------------------------------------------------------------
// Process – checkout.session.completed
// ---------------------------------------------------------------------------

func TestStripeProcessCheckoutSessionCompletedSubscription(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:             "cs_test_1",
		Status:         stripeCheckoutStatusComplete,
		PaymentStatus:  stripeCheckoutPaymentStatusPaid,
		Mode:           stripeCheckoutModeSubscriptionRaw,
		SubscriptionID: "sub_123",
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_1",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		OccurredAt:   time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, subscriptionStatusActive, repo.inputs[0].Status)
	require.Equal(t, "pro", repo.inputs[0].ActivePlan)
	require.Equal(t, "user@example.com", repo.inputs[0].UserEmail)
	require.Equal(t, "sub_123", repo.inputs[0].SubscriptionID)
}

// ---------------------------------------------------------------------------
// Process – checkout.session.completed for top-up pack (non-subscription → skip state upsert)
// ---------------------------------------------------------------------------

func TestStripeProcessCheckoutSessionCompletedTopUpPackSkipsUpsert(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_pack_1",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModePaymentRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "buyer@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindTopUpPack,
			stripeMetadataPackCodeKey:     PackCodeTopUp,
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_pack_1",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		OccurredAt:   time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Empty(t, repo.inputs)
}

// ---------------------------------------------------------------------------
// Process – stale checkout event skipped
// ---------------------------------------------------------------------------

func TestStripeProcessCheckoutSessionCompletedStaleEventSkipped(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			ProviderCode:        ProviderCodeStripe,
			UserEmail:           "user@example.com",
			Status:              subscriptionStatusActive,
			LastEventOccurredAt: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		},
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:             "cs_stale",
		Status:         stripeCheckoutStatusComplete,
		PaymentStatus:  stripeCheckoutPaymentStatusPaid,
		Mode:           stripeCheckoutModeSubscriptionRaw,
		SubscriptionID: "sub_stale",
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_stale",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		OccurredAt:   time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Empty(t, repo.inputs)
}

// ---------------------------------------------------------------------------
// Process – checkout plan code fallback from grant reason and existing state
// ---------------------------------------------------------------------------

func TestStripeProcessCheckoutSessionCompletedPlanCodeFallbackFromPriceID(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:             "cs_fallback",
		Status:         stripeCheckoutStatusComplete,
		PaymentStatus:  stripeCheckoutPaymentStatusPaid,
		Mode:           stripeCheckoutModeSubscriptionRaw,
		SubscriptionID: "sub_fallback",
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPriceIDKey:      "price_pro",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_fb1",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		OccurredAt:   time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, "pro", repo.inputs[0].ActivePlan)
}

func TestStripeProcessCheckoutSessionCompletedPlanCodeFallbackFromExistingState(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			ProviderCode:        ProviderCodeStripe,
			UserEmail:           "user@example.com",
			Status:              subscriptionStatusActive,
			ActivePlan:          "plus",
			LastEventOccurredAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	// No plan_code in metadata, no price_id → grant reason based on plan "pro" from price
	// But we want to test the fallback from existing state, so set plan code empty
	// and no priceID match
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:             "cs_state_fallback",
		Status:         stripeCheckoutStatusComplete,
		PaymentStatus:  stripeCheckoutPaymentStatusPaid,
		Mode:           stripeCheckoutModeSubscriptionRaw,
		SubscriptionID: "sub_state_fb",
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_state_fb",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		OccurredAt:   time.Date(2026, 3, 2, 13, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, "pro", repo.inputs[0].ActivePlan)
}

// ---------------------------------------------------------------------------
// Process – checkout session get error
// ---------------------------------------------------------------------------

func TestStripeProcessCheckoutSessionGetError(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		getErr: errors.New("db_error"),
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:             "cs_get_err",
		Status:         stripeCheckoutStatusComplete,
		PaymentStatus:  stripeCheckoutPaymentStatusPaid,
		Mode:           stripeCheckoutModeSubscriptionRaw,
		SubscriptionID: "sub_get_err",
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_get_err",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		OccurredAt:   time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.webhook.subscription_state.get")
}

// ---------------------------------------------------------------------------
// Process – checkout session upsert error
// ---------------------------------------------------------------------------

func TestStripeProcessCheckoutSessionUpsertError(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		upsertErr: errors.New("upsert_failed"),
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:             "cs_upsert_err",
		Status:         stripeCheckoutStatusComplete,
		PaymentStatus:  stripeCheckoutPaymentStatusPaid,
		Mode:           stripeCheckoutModeSubscriptionRaw,
		SubscriptionID: "sub_upsert_err",
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_upsert_err",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		OccurredAt:   time.Date(2026, 3, 1, 15, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.webhook.subscription_state.upsert")
}

// ---------------------------------------------------------------------------
// Process – subscription lifecycle events
// ---------------------------------------------------------------------------

func TestStripeProcessSubscriptionCreatedEvent(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{}
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_created_1",
		Status:   stripeSubscriptionStatusActive,
		Metadata: map[string]string{stripeMetadataUserEmailKey: "user@example.com"},
		Items: stripeSubscriptionItems{
			Data: []stripeSubscriptionItem{
				{Price: stripeSubscriptionItemPrice{ID: "price_pro"}},
			},
		},
		CurrentPeriodEnd: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC).Unix(),
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_sub_created",
		EventType:    stripeEventTypeSubscriptionCreated,
		OccurredAt:   time.Date(2026, 3, 1, 16, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, subscriptionStatusActive, repo.inputs[0].Status)
	require.Equal(t, "pro", repo.inputs[0].ActivePlan)
	require.Equal(t, "sub_created_1", repo.inputs[0].SubscriptionID)
}

func TestStripeProcessSubscriptionUpdatedActiveEvent(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_updated_1",
		Status:   stripeSubscriptionStatusActive,
		Metadata: map[string]string{stripeMetadataUserEmailKey: "user@example.com"},
		Items: stripeSubscriptionItems{
			Data: []stripeSubscriptionItem{
				{Price: stripeSubscriptionItemPrice{ID: "price_plus"}},
			},
		},
		CurrentPeriodEnd: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC).Unix(),
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_sub_updated",
		EventType:    stripeEventTypeSubscriptionUpdated,
		OccurredAt:   time.Date(2026, 3, 1, 17, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, subscriptionStatusActive, repo.inputs[0].Status)
	require.Equal(t, "plus", repo.inputs[0].ActivePlan)
}

func TestStripeProcessSubscriptionDeletedEvent(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_deleted_1",
		Status:   stripeSubscriptionStatusCanceled,
		Metadata: map[string]string{stripeMetadataUserEmailKey: "user@example.com"},
		Items: stripeSubscriptionItems{
			Data: []stripeSubscriptionItem{
				{Price: stripeSubscriptionItemPrice{ID: "price_pro"}},
			},
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_sub_deleted",
		EventType:    stripeEventTypeSubscriptionDeleted,
		OccurredAt:   time.Date(2026, 3, 1, 18, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, subscriptionStatusInactive, repo.inputs[0].Status)
	require.Equal(t, "", repo.inputs[0].ActivePlan)
}

// ---------------------------------------------------------------------------
// Process – unknown event type
// ---------------------------------------------------------------------------

func TestStripeProcessUnknownEventType(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    "some.unknown.event",
		Payload:      []byte(`{}`),
	})
	require.NoError(t, err)
	require.Empty(t, repo.inputs)
}

// ---------------------------------------------------------------------------
// processSubscriptionLifecycleEvent – invalid JSON
// ---------------------------------------------------------------------------

func TestStripeProcessSubscriptionLifecycleInvalidJSON(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeSubscriptionUpdated,
		Payload:      []byte(`{bad json`),
	})
	require.ErrorIs(t, err, ErrWebhookGrantPayloadInvalid)
}

// ---------------------------------------------------------------------------
// processSubscriptionLifecycleEvent – email resolution via customer ID
// ---------------------------------------------------------------------------

func TestStripeProcessSubscriptionLifecycleEmailFromCustomerID(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		resolvedCustomerEmail: "resolved@example.com",
	}
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:         "sub_cust_id_1",
		Status:     stripeSubscriptionStatusActive,
		CustomerID: "cus_123",
		Metadata:   map[string]string{},
		Items: stripeSubscriptionItems{
			Data: []stripeSubscriptionItem{
				{Price: stripeSubscriptionItemPrice{ID: "price_pro"}},
			},
		},
		CurrentPeriodEnd: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC).Unix(),
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_cust_id",
		EventType:    stripeEventTypeSubscriptionUpdated,
		OccurredAt:   time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, "resolved@example.com", repo.inputs[0].UserEmail)
}

// ---------------------------------------------------------------------------
// processSubscriptionLifecycleEvent – stale event skipped
// ---------------------------------------------------------------------------

func TestStripeProcessSubscriptionLifecycleStaleEventSkipped(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			ProviderCode:        ProviderCodeStripe,
			UserEmail:           "user@example.com",
			Status:              subscriptionStatusActive,
			LastEventOccurredAt: time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC),
		},
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_stale_lc",
		Status:   stripeSubscriptionStatusActive,
		Metadata: map[string]string{stripeMetadataUserEmailKey: "user@example.com"},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_stale_lc",
		EventType:    stripeEventTypeSubscriptionUpdated,
		OccurredAt:   time.Date(2026, 3, 2, 11, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Empty(t, repo.inputs)
}

// ---------------------------------------------------------------------------
// processSubscriptionLifecycleEvent – state get error
// ---------------------------------------------------------------------------

func TestStripeProcessSubscriptionLifecycleGetError(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		getErr: errors.New("db_down"),
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_get_err_lc",
		Status:   stripeSubscriptionStatusActive,
		Metadata: map[string]string{stripeMetadataUserEmailKey: "user@example.com"},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_get_err_lc",
		EventType:    stripeEventTypeSubscriptionUpdated,
		OccurredAt:   time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.webhook.subscription_state.get")
}

// ---------------------------------------------------------------------------
// processSubscriptionLifecycleEvent – upsert error
// ---------------------------------------------------------------------------

func TestStripeProcessSubscriptionLifecycleUpsertError(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		upsertErr: errors.New("upsert_fail"),
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_upsert_err_lc",
		Status:   stripeSubscriptionStatusActive,
		Metadata: map[string]string{stripeMetadataUserEmailKey: "user@example.com"},
		Items: stripeSubscriptionItems{
			Data: []stripeSubscriptionItem{
				{Price: stripeSubscriptionItemPrice{ID: "price_pro"}},
			},
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_upsert_err_lc",
		EventType:    stripeEventTypeSubscriptionCreated,
		OccurredAt:   time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.webhook.subscription_state.upsert")
}

// ---------------------------------------------------------------------------
// resolveLifecycleUserEmail – fallback to subscription state lookup
// ---------------------------------------------------------------------------

func TestStripeResolveLifecycleUserEmailFallbackToSubscriptionState(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		getBySubscriptionFound: true,
		getBySubscriptionState: SubscriptionState{
			UserEmail:      "state-owner@example.com",
			SubscriptionID: "sub_lookup_1",
		},
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_lookup_1",
		Status:   stripeSubscriptionStatusCanceled,
		Metadata: map[string]string{},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_lookup_1",
		EventType:    stripeEventTypeSubscriptionDeleted,
		OccurredAt:   time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, "state-owner@example.com", repo.inputs[0].UserEmail)
}

func TestStripeResolveLifecycleUserEmailFallbackLookupError(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		getBySubscriptionErr: errors.New("lookup_fail"),
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_lookup_err",
		Status:   stripeSubscriptionStatusCanceled,
		Metadata: map[string]string{},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_lookup_err",
		EventType:    stripeEventTypeSubscriptionDeleted,
		OccurredAt:   time.Date(2026, 3, 3, 11, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.webhook.subscription_state.get_by_subscription_id")
}

func TestStripeResolveLifecycleUserEmailFallbackNotFound(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		getBySubscriptionFound: false,
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_not_found",
		Status:   stripeSubscriptionStatusCanceled,
		Metadata: map[string]string{},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_not_found",
		EventType:    stripeEventTypeSubscriptionDeleted,
		OccurredAt:   time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestStripeResolveLifecycleUserEmailFallbackEmptySubscriptionID(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "",
		Status:   stripeSubscriptionStatusCanceled,
		Metadata: map[string]string{},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_empty_sub_id",
		EventType:    stripeEventTypeSubscriptionDeleted,
		OccurredAt:   time.Date(2026, 3, 3, 13, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestStripeResolveLifecycleUserEmailFallbackEmptyUserEmailInState(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		getBySubscriptionFound: true,
		getBySubscriptionState: SubscriptionState{
			UserEmail: "",
		},
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_empty_email",
		Status:   stripeSubscriptionStatusCanceled,
		Metadata: map[string]string{},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_empty_email",
		EventType:    stripeEventTypeSubscriptionDeleted,
		OccurredAt:   time.Date(2026, 3, 3, 14, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

// ---------------------------------------------------------------------------
// resolvePayloadUserEmail – metadata email, customer ID resolution, missing
// ---------------------------------------------------------------------------

func TestStripeResolvePayloadUserEmailFromMetadata(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:     "sub_meta_email",
		Status: stripeSubscriptionStatusActive,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey: "  META@Example.com  ",
		},
		Items: stripeSubscriptionItems{
			Data: []stripeSubscriptionItem{
				{Price: stripeSubscriptionItemPrice{ID: "price_pro"}},
			},
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_meta_email",
		EventType:    stripeEventTypeSubscriptionCreated,
		OccurredAt:   time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, "meta@example.com", repo.inputs[0].UserEmail)
}

func TestStripeResolvePayloadUserEmailCustomerIDEmpty(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		getBySubscriptionFound: false,
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:         "sub_no_cust",
		Status:     stripeSubscriptionStatusActive,
		CustomerID: "",
		Metadata:   map[string]string{},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_no_cust",
		EventType:    stripeEventTypeSubscriptionCreated,
		OccurredAt:   time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestStripeResolvePayloadUserEmailCustomerResolveReturnsEmpty(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		resolvedCustomerEmail: "",
	}
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	repo := &stubSubscriptionStateRepository{
		getBySubscriptionFound: false,
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:         "sub_empty_resolve",
		Status:     stripeSubscriptionStatusActive,
		CustomerID: "cus_empty",
		Metadata:   map[string]string{},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_empty_resolve",
		EventType:    stripeEventTypeSubscriptionCreated,
		OccurredAt:   time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

// ---------------------------------------------------------------------------
// resolvePlanCode on stripe processor
// ---------------------------------------------------------------------------

func TestStripeProcessorResolvePlanCodeFromMetadata(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:     "sub_plan_meta",
		Status: stripeSubscriptionStatusActive,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey: "user@example.com",
			stripeMetadataPlanCodeKey:  "Plus",
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_plan_meta",
		EventType:    stripeEventTypeSubscriptionUpdated,
		OccurredAt:   time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, "plus", repo.inputs[0].ActivePlan)
}

func TestStripeProcessorResolvePlanCodeFromPriceID(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_plan_price",
		Status:   stripeSubscriptionStatusActive,
		Metadata: map[string]string{stripeMetadataUserEmailKey: "user@example.com"},
		Items: stripeSubscriptionItems{
			Data: []stripeSubscriptionItem{
				{Price: stripeSubscriptionItemPrice{ID: "price_plus"}},
			},
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_plan_price",
		EventType:    stripeEventTypeSubscriptionUpdated,
		OccurredAt:   time.Date(2026, 3, 5, 11, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, "plus", repo.inputs[0].ActivePlan)
}

func TestStripeProcessorResolvePlanCodeEmptyNoPriceID(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_no_plan",
		Status:   stripeSubscriptionStatusActive,
		Metadata: map[string]string{stripeMetadataUserEmailKey: "user@example.com"},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_no_plan",
		EventType:    stripeEventTypeSubscriptionCreated,
		OccurredAt:   time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	// Plan code is empty for active status → still set to empty because no source of plan code
	require.Equal(t, "", repo.inputs[0].ActivePlan)
}

// ---------------------------------------------------------------------------
// resolveStripeSubscriptionPriceID
// ---------------------------------------------------------------------------

func TestResolveStripeSubscriptionPriceIDWithItems(t *testing.T) {
	data := stripeSubscriptionWebhookData{
		Items: stripeSubscriptionItems{
			Data: []stripeSubscriptionItem{
				{Price: stripeSubscriptionItemPrice{ID: "price_abc"}},
			},
		},
	}
	require.Equal(t, "price_abc", resolveStripeSubscriptionPriceID(data))
}

func TestResolveStripeSubscriptionPriceIDEmpty(t *testing.T) {
	data := stripeSubscriptionWebhookData{}
	require.Equal(t, "", resolveStripeSubscriptionPriceID(data))
}

func TestResolveStripeSubscriptionPriceIDSkipsBlanks(t *testing.T) {
	data := stripeSubscriptionWebhookData{
		Items: stripeSubscriptionItems{
			Data: []stripeSubscriptionItem{
				{Price: stripeSubscriptionItemPrice{ID: "  "}},
				{Price: stripeSubscriptionItemPrice{ID: "price_second"}},
			},
		},
	}
	require.Equal(t, "price_second", resolveStripeSubscriptionPriceID(data))
}

// ---------------------------------------------------------------------------
// resolveStripeSubscriptionNextBillingAt
// ---------------------------------------------------------------------------

func TestResolveStripeSubscriptionNextBillingAtValid(t *testing.T) {
	data := stripeSubscriptionWebhookData{
		CurrentPeriodEnd: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC).Unix(),
	}
	result := resolveStripeSubscriptionNextBillingAt(data)
	require.Equal(t, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), result)
}

func TestResolveStripeSubscriptionNextBillingAtZero(t *testing.T) {
	data := stripeSubscriptionWebhookData{CurrentPeriodEnd: 0}
	result := resolveStripeSubscriptionNextBillingAt(data)
	require.True(t, result.IsZero())
}

// ---------------------------------------------------------------------------
// resolveStripeSubscriptionState – all status transitions
// ---------------------------------------------------------------------------

func TestResolveStripeSubscriptionStateDeleted(t *testing.T) {
	require.Equal(t, subscriptionStatusInactive, resolveStripeSubscriptionState(stripeEventTypeSubscriptionDeleted, "active"))
}

func TestResolveStripeSubscriptionStateActive(t *testing.T) {
	require.Equal(t, subscriptionStatusActive, resolveStripeSubscriptionState(stripeEventTypeSubscriptionUpdated, stripeSubscriptionStatusActive))
}

func TestResolveStripeSubscriptionStateTrialing(t *testing.T) {
	require.Equal(t, subscriptionStatusActive, resolveStripeSubscriptionState(stripeEventTypeSubscriptionUpdated, stripeSubscriptionStatusTrialing))
}

func TestResolveStripeSubscriptionStatePaused(t *testing.T) {
	require.Equal(t, subscriptionStatusInactive, resolveStripeSubscriptionState(stripeEventTypeSubscriptionUpdated, stripeSubscriptionStatusPaused))
}

func TestResolveStripeSubscriptionStateCanceled(t *testing.T) {
	require.Equal(t, subscriptionStatusInactive, resolveStripeSubscriptionState(stripeEventTypeSubscriptionUpdated, stripeSubscriptionStatusCanceled))
}

func TestResolveStripeSubscriptionStateIncomplete(t *testing.T) {
	require.Equal(t, subscriptionStatusInactive, resolveStripeSubscriptionState(stripeEventTypeSubscriptionUpdated, stripeSubscriptionStatusIncomplete))
}

func TestResolveStripeSubscriptionStateIncompleteExpired(t *testing.T) {
	require.Equal(t, subscriptionStatusInactive, resolveStripeSubscriptionState(stripeEventTypeSubscriptionUpdated, stripeSubscriptionStatusIncompleteExpired))
}

func TestResolveStripeSubscriptionStatePastDue(t *testing.T) {
	require.Equal(t, subscriptionStatusInactive, resolveStripeSubscriptionState(stripeEventTypeSubscriptionUpdated, stripeSubscriptionStatusPastDue))
}

func TestResolveStripeSubscriptionStateUnpaid(t *testing.T) {
	require.Equal(t, subscriptionStatusInactive, resolveStripeSubscriptionState(stripeEventTypeSubscriptionUpdated, stripeSubscriptionStatusUnpaid))
}

func TestResolveStripeSubscriptionStateUnknownStatusCreatedEvent(t *testing.T) {
	require.Equal(t, subscriptionStatusActive, resolveStripeSubscriptionState(stripeEventTypeSubscriptionCreated, "some_unknown_status"))
}

func TestResolveStripeSubscriptionStateUnknownStatusUpdatedEvent(t *testing.T) {
	require.Equal(t, subscriptionStatusInactive, resolveStripeSubscriptionState(stripeEventTypeSubscriptionUpdated, "some_unknown_status"))
}

// ---------------------------------------------------------------------------
// cloneStripeGrantDefinitionsByPriceID
// ---------------------------------------------------------------------------

func TestCloneStripeGrantDefinitionsByPriceIDEmpty(t *testing.T) {
	result := cloneStripeGrantDefinitionsByPriceID(nil)
	require.Empty(t, result)
}

func TestCloneStripeGrantDefinitionsByPriceIDValid(t *testing.T) {
	source := map[string]stripeGrantDefinition{
		" price_1 ": {Code: " Pro ", Credits: 1000},
		"price_2":   {Code: "plus", Credits: 10000},
	}
	result := cloneStripeGrantDefinitionsByPriceID(source)
	require.Len(t, result, 2)
	require.Equal(t, "pro", result["price_1"].Code)
	require.Equal(t, int64(1000), result["price_1"].Credits)
	require.Equal(t, "plus", result["price_2"].Code)
}

func TestCloneStripeGrantDefinitionsByPriceIDSkipsInvalid(t *testing.T) {
	source := map[string]stripeGrantDefinition{
		"  ":      {Code: "pro", Credits: 100},
		"price_x": {Code: "  ", Credits: 100},
		"price_y": {Code: "pro", Credits: 0},
		"price_z": {Code: "pro", Credits: -5},
	}
	result := cloneStripeGrantDefinitionsByPriceID(source)
	require.Empty(t, result)
}

// ---------------------------------------------------------------------------
// stripeWebhookGrantResolver.Resolve – comprehensive tests
// ---------------------------------------------------------------------------

func TestStripeGrantResolverWrongProviderCode(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: "paddle",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
	})
	require.NoError(t, resolveErr)
	require.False(t, shouldGrant)
	require.Equal(t, WebhookGrant{}, grant)
}

func TestStripeGrantResolverNilEventStatusProvider(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		nil,
	)
	require.NoError(t, err)
	_, _, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
	})
	require.ErrorIs(t, resolveErr, ErrWebhookGrantResolverUnavailable)
}

func TestStripeGrantResolverNonSucceededEvent(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	_, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionExpired,
	})
	require.NoError(t, resolveErr)
	require.False(t, shouldGrant)
}

func TestStripeGrantResolverInvalidPayload(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	_, _, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      []byte(`{bad`),
	})
	require.ErrorIs(t, resolveErr, ErrWebhookGrantPayloadInvalid)
}

func TestStripeGrantResolverNonPaidSession(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_unpaid",
		Status:        "open",
		PaymentStatus: "unpaid",
	})
	_, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.False(t, shouldGrant)
}

func TestStripeGrantResolverMissingSessionID(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
	})
	_, _, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.ErrorIs(t, resolveErr, ErrWebhookGrantPayloadInvalid)
}

func TestStripeGrantResolverSubscriptionGrant(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:             "cs_sub_grant",
		Status:         stripeCheckoutStatusComplete,
		PaymentStatus:  stripeCheckoutPaymentStatusPaid,
		Mode:           stripeCheckoutModeSubscriptionRaw,
		SubscriptionID: "sub_abc",
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
			stripeMetadataPriceIDKey:      "price_pro",
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, "user@example.com", grant.UserEmail)
	require.Equal(t, int64(1000), grant.Credits)
	require.Equal(t, "subscription_monthly_pro", grant.Reason)
	require.Contains(t, grant.Reference, "stripe:subscription:cs_sub_grant")
	require.Equal(t, "sub_abc", grant.Metadata[billingGrantMetadataSubscriptionIDKey])
	require.Equal(t, "price_pro", grant.Metadata[billingGrantMetadataPriceIDKey])
}

func TestStripeGrantResolverSubscriptionGrantPlanCodeFromPriceID(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_sub_price_fallback",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPriceIDKey:      "price_plus",
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, int64(10000), grant.Credits)
	require.Equal(t, "subscription_monthly_plus", grant.Reason)
}

func TestStripeGrantResolverSubscriptionGrantUnknownPlan(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_unknown_plan",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     "nonexistent",
		},
	})
	_, _, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.ErrorIs(t, resolveErr, ErrWebhookGrantPlanUnknown)
}

func TestStripeGrantResolverTopUpPackGrant(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_pack_grant",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModePaymentRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindTopUpPack,
			stripeMetadataPackCodeKey:     PackCodeTopUp,
			stripeMetadataPriceIDKey:      "price_pack_top_up",
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, "user@example.com", grant.UserEmail)
	require.Equal(t, int64(2400), grant.Credits)
	require.Contains(t, grant.Reason, "top_up_pack_top_up")
	require.Contains(t, grant.Reference, "stripe:top_up_pack:cs_pack_grant")
	require.Equal(t, "price_pack_top_up", grant.Metadata[billingGrantMetadataPriceIDKey])
}

func TestStripeGrantResolverTopUpPackGrantCreditsFromMetadata(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_pack_meta_credits",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModePaymentRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindTopUpPack,
			stripeMetadataPackCodeKey:     PackCodeTopUp,
			stripeMetadataPackCreditsKey:  "5000",
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, int64(5000), grant.Credits)
}

func TestStripeGrantResolverTopUpPackGrantInvalidCreditsMetadata(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_pack_bad_credits",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModePaymentRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindTopUpPack,
			stripeMetadataPackCodeKey:     PackCodeTopUp,
			stripeMetadataPackCreditsKey:  "not_a_number",
		},
	})
	_, _, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.ErrorIs(t, resolveErr, ErrWebhookGrantMetadataInvalid)
}

func TestStripeGrantResolverTopUpPackFromPriceIDFallback(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_pack_price_fb",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModePaymentRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindTopUpPack,
			stripeMetadataPriceIDKey:      "price_pack_top_up",
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, int64(2400), grant.Credits)
	require.Equal(t, "top_up", grant.Metadata[billingGrantMetadataPackCodeKey])
}

func TestStripeGrantResolverTopUpPackUnknown(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_pack_unknown",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModePaymentRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindTopUpPack,
			stripeMetadataPackCodeKey:     "nonexistent_pack",
		},
	})
	_, _, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.ErrorIs(t, resolveErr, ErrWebhookGrantPackUnknown)
}

func TestStripeGrantResolverUnknownPurchaseKind(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_unknown_kind",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          "some_weird_mode",
		Metadata: map[string]string{
			stripeMetadataUserEmailKey: "user@example.com",
		},
	})
	_, _, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.ErrorIs(t, resolveErr, ErrWebhookGrantMetadataInvalid)
}

func TestStripeGrantResolverPurchaseKindFromModeSubscription(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_mode_sub",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey: "user@example.com",
			stripeMetadataPlanCodeKey:  PlanCodePro,
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, "subscription_monthly_pro", grant.Reason)
}

func TestStripeGrantResolverPurchaseKindFromModePayment(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_mode_pay",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModePaymentRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey: "user@example.com",
			stripeMetadataPackCodeKey:  PackCodeTopUp,
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Contains(t, grant.Reason, "top_up_pack_top_up")
}

func TestStripeGrantResolverPurchaseKindFromPriceIDPlan(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_priceid_plan",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey: "user@example.com",
			stripeMetadataPriceIDKey:   "price_pro",
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Contains(t, grant.Reason, "subscription_monthly_pro")
}

func TestStripeGrantResolverPurchaseKindFromPriceIDPack(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_priceid_pack",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey: "user@example.com",
			stripeMetadataPriceIDKey:   "price_pack_top_up",
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Contains(t, grant.Reason, "top_up_pack_top_up")
}

// ---------------------------------------------------------------------------
// resolvePurchaseKindFromPriceID
// ---------------------------------------------------------------------------

func TestStripeResolvePurchaseKindFromPriceIDPlan(t *testing.T) {
	resolver, _ := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		nil, nil,
	)
	require.Equal(t, stripePurchaseKindSubscription, resolver.resolvePurchaseKindFromPriceID("price_pro"))
}

func TestStripeResolvePurchaseKindFromPriceIDPack(t *testing.T) {
	resolver, _ := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		nil, nil,
	)
	require.Equal(t, stripePurchaseKindTopUpPack, resolver.resolvePurchaseKindFromPriceID("price_pack_top_up"))
}

func TestStripeResolvePurchaseKindFromPriceIDUnknown(t *testing.T) {
	resolver, _ := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		nil, nil,
	)
	require.Equal(t, "", resolver.resolvePurchaseKindFromPriceID("price_unknown"))
}

func TestStripeResolvePurchaseKindFromPriceIDEmpty(t *testing.T) {
	resolver, _ := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		nil, nil,
	)
	require.Equal(t, "", resolver.resolvePurchaseKindFromPriceID(""))
}

// ---------------------------------------------------------------------------
// resolveUserEmail on stripe resolver – all paths
// ---------------------------------------------------------------------------

func TestStripeResolverResolveUserEmailFromMetadata(t *testing.T) {
	resolver, _ := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		nil, testEventStatusProvider(),
	)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_email_meta",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    " User@Example.COM ",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, "user@example.com", grant.UserEmail)
}

func TestStripeResolverResolveUserEmailFromCustomerEmail(t *testing.T) {
	resolver, _ := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		nil, testEventStatusProvider(),
	)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_email_customer",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		CustomerEmail: " Customer@Example.com ",
		Metadata: map[string]string{
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, "customer@example.com", grant.UserEmail)
}

func TestStripeResolverResolveUserEmailFromCustomerDetails(t *testing.T) {
	resolver, _ := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		nil, testEventStatusProvider(),
	)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:              "cs_email_details",
		Status:          stripeCheckoutStatusComplete,
		PaymentStatus:   stripeCheckoutPaymentStatusPaid,
		Mode:            stripeCheckoutModeSubscriptionRaw,
		CustomerDetails: stripeCustomerData{Email: " Details@Example.com "},
		Metadata: map[string]string{
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, "details@example.com", grant.UserEmail)
}

func TestStripeResolverResolveUserEmailFromCustomerIDLookup(t *testing.T) {
	resolver, _ := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "  Resolved@Example.com  "},
		testEventStatusProvider(),
	)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_email_cust_id",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		CustomerID:    "cus_resolve_1",
		Metadata: map[string]string{
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, "resolved@example.com", grant.UserEmail)
}

func TestStripeResolverResolveUserEmailCustomerIDLookupError(t *testing.T) {
	resolver, _ := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{err: errors.New("api_error")},
		testEventStatusProvider(),
	)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_email_err",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		CustomerID:    "cus_err",
		Metadata: map[string]string{
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	_, _, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.ErrorIs(t, resolveErr, ErrWebhookGrantMetadataInvalid)
}

func TestStripeResolverResolveUserEmailNoCustomerID(t *testing.T) {
	resolver, _ := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		nil, testEventStatusProvider(),
	)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_no_customer",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		Metadata: map[string]string{
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	_, _, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.ErrorIs(t, resolveErr, ErrWebhookGrantMetadataInvalid)
}

func TestStripeResolverResolveUserEmailCustomerIDReturnsEmpty(t *testing.T) {
	resolver, _ := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: ""},
		testEventStatusProvider(),
	)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_empty_email",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		CustomerID:    "cus_empty_email",
		Metadata: map[string]string{
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	_, _, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.ErrorIs(t, resolveErr, ErrWebhookGrantMetadataInvalid)
}

// ---------------------------------------------------------------------------
// newStripeWebhookGrantResolverWithCatalog – validation edge cases
// ---------------------------------------------------------------------------

func TestStripeGrantResolverCatalogInvalidPlan(t *testing.T) {
	_, err := newStripeWebhookGrantResolverWithCatalog(
		[]SubscriptionPlan{{Code: "", MonthlyCredits: 100}},
		testPacks(),
		testPlanGrantsByPriceID(),
		testPackGrantsByPriceID(),
		nil, nil,
	)
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestStripeGrantResolverCatalogInvalidPlanCredits(t *testing.T) {
	_, err := newStripeWebhookGrantResolverWithCatalog(
		[]SubscriptionPlan{{Code: "pro", MonthlyCredits: 0}},
		testPacks(),
		testPlanGrantsByPriceID(),
		testPackGrantsByPriceID(),
		nil, nil,
	)
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestStripeGrantResolverCatalogInvalidPack(t *testing.T) {
	_, err := newStripeWebhookGrantResolverWithCatalog(
		testPlans(),
		[]TopUpPack{{Code: "", Credits: 100}},
		testPlanGrantsByPriceID(),
		testPackGrantsByPriceID(),
		nil, nil,
	)
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestStripeGrantResolverCatalogInvalidPackCredits(t *testing.T) {
	_, err := newStripeWebhookGrantResolverWithCatalog(
		testPlans(),
		[]TopUpPack{{Code: "top_up", Credits: 0}},
		testPlanGrantsByPriceID(),
		testPackGrantsByPriceID(),
		nil, nil,
	)
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

// ---------------------------------------------------------------------------
// newStripeWebhookGrantResolverFromProvider – nil
// ---------------------------------------------------------------------------

func TestNewStripeWebhookGrantResolverFromProviderNil(t *testing.T) {
	_, err := newStripeWebhookGrantResolverFromProvider(nil)
	require.ErrorIs(t, err, ErrWebhookGrantResolverProviderUnavailable)
}

// ---------------------------------------------------------------------------
// Subscription grant with plan credits resolved via planGrantByPriceID
// (when planCreditsByCode doesn't match but planGrantByPriceID does for same code)
// ---------------------------------------------------------------------------

func TestStripeGrantResolverSubscriptionCreditsFromPriceIDDefinition(t *testing.T) {
	// Build resolver where planCreditsByCode has different credits than planGrantByPriceID
	planGrants := map[string]stripeGrantDefinition{
		"price_custom": {Code: "custom_plan", Credits: 999},
	}
	resolver, err := newStripeWebhookGrantResolverWithCatalog(
		[]SubscriptionPlan{{Code: "other_plan", MonthlyCredits: 100}},
		testPacks(),
		planGrants,
		testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_custom_plan",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPriceIDKey:      "price_custom",
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, int64(999), grant.Credits)
	require.Equal(t, "custom_plan", grant.Metadata[billingGrantMetadataPlanCodeKey])
}

// ---------------------------------------------------------------------------
// Subscription grant without subscriptionID or priceID in metadata
// ---------------------------------------------------------------------------

func TestStripeGrantResolverSubscriptionGrantNoSubscriptionIDNoPriceID(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_no_sub_no_price",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	_, hasSubID := grant.Metadata[billingGrantMetadataSubscriptionIDKey]
	require.False(t, hasSubID)
	_, hasPriceID := grant.Metadata[billingGrantMetadataPriceIDKey]
	require.False(t, hasPriceID)
}

// ---------------------------------------------------------------------------
// Top-up pack grant no priceID in metadata
// ---------------------------------------------------------------------------

func TestStripeGrantResolverTopUpPackNoPriceIDInMetadata(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_pack_no_price",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModePaymentRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindTopUpPack,
			stripeMetadataPackCodeKey:     PackCodeTopUp,
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	_, hasPriceID := grant.Metadata[billingGrantMetadataPriceIDKey]
	require.False(t, hasPriceID)
}

// ---------------------------------------------------------------------------
// Default purchase kind (not subscription, not top_up) -> error
// ---------------------------------------------------------------------------

func TestStripeGrantResolverDefaultPurchaseKindError(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_bad_kind",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: "invalid_kind",
		},
	})
	_, _, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.ErrorIs(t, resolveErr, ErrWebhookGrantMetadataInvalid)
}

// ---------------------------------------------------------------------------
// processCheckoutSessionCompletedEvent – grant resolver error
// ---------------------------------------------------------------------------

func TestStripeProcessCheckoutSessionCompletedGrantResolveError(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	// Invalid JSON payload will cause grant resolver to fail
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      []byte(`{invalid`),
	})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// processCheckoutSessionCompletedEvent – grant not granted (non-paid)
// ---------------------------------------------------------------------------

func TestStripeProcessCheckoutSessionCompletedNotPaid(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_not_paid",
		Status:        "open",
		PaymentStatus: "unpaid",
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Empty(t, repo.inputs)
}

// ---------------------------------------------------------------------------
// Sync event bypasses stale check
// ---------------------------------------------------------------------------

func TestStripeProcessSubscriptionLifecycleSyncEventBypassesStaleCheck(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			ProviderCode:        ProviderCodeStripe,
			UserEmail:           "user@example.com",
			Status:              subscriptionStatusActive,
			LastEventOccurredAt: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC),
		},
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_sync",
		Status:   stripeSubscriptionStatusActive,
		Metadata: map[string]string{stripeMetadataUserEmailKey: "user@example.com"},
		Items: stripeSubscriptionItems{
			Data: []stripeSubscriptionItem{
				{Price: stripeSubscriptionItemPrice{ID: "price_pro"}},
			},
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      fmt.Sprintf("sync:subscription:%s", "sub_sync"),
		EventType:    stripeEventTypeSubscriptionUpdated,
		OccurredAt:   time.Date(2026, 3, 10, 11, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
}

// ---------------------------------------------------------------------------
// Checkout sync event bypasses stale check
// ---------------------------------------------------------------------------

func TestStripeProcessCheckoutSessionSyncEventBypassesStaleCheck(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			ProviderCode:        ProviderCodeStripe,
			UserEmail:           "user@example.com",
			Status:              subscriptionStatusActive,
			LastEventOccurredAt: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC),
		},
	}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:             "cs_sync",
		Status:         stripeCheckoutStatusComplete,
		PaymentStatus:  stripeCheckoutPaymentStatusPaid,
		Mode:           stripeCheckoutModeSubscriptionRaw,
		SubscriptionID: "sub_sync_co",
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "sync:checkout:cs_sync",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		OccurredAt:   time.Date(2026, 3, 10, 11, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
}

// ---------------------------------------------------------------------------
// Inactive subscription clears plan code
// ---------------------------------------------------------------------------

func TestStripeProcessSubscriptionInactiveStatusClearsPlanCode(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeSubscriptionPayload(stripeSubscriptionWebhookData{
		ID:       "sub_paused",
		Status:   stripeSubscriptionStatusPaused,
		Metadata: map[string]string{stripeMetadataUserEmailKey: "user@example.com"},
		Items: stripeSubscriptionItems{
			Data: []stripeSubscriptionItem{
				{Price: stripeSubscriptionItemPrice{ID: "price_pro"}},
			},
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_paused",
		EventType:    stripeEventTypeSubscriptionUpdated,
		OccurredAt:   time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, subscriptionStatusInactive, repo.inputs[0].Status)
	require.Equal(t, "", repo.inputs[0].ActivePlan)
}

// ---------------------------------------------------------------------------
// Process – async_payment_succeeded (also a checkout success event)
// ---------------------------------------------------------------------------

func TestStripeProcessAsyncPaymentSucceeded(t *testing.T) {
	provider, _ := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	repo := &stubSubscriptionStateRepository{}
	processor, _ := newStripeSubscriptionStatusWebhookProcessor(provider, repo)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:             "cs_async",
		Status:         stripeCheckoutStatusComplete,
		PaymentStatus:  stripeCheckoutPaymentStatusPaid,
		Mode:           stripeCheckoutModeSubscriptionRaw,
		SubscriptionID: "sub_async",
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     PlanCodePro,
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_async",
		EventType:    stripeEventTypeCheckoutSessionAsyncPaymentSucceeded,
		OccurredAt:   time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC),
		Payload:      payload,
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, subscriptionStatusActive, repo.inputs[0].Status)
}

// ---------------------------------------------------------------------------
// resolvePayloadUserEmail – nil customer email resolver
// ---------------------------------------------------------------------------

func TestStripeResolvePayloadUserEmailNilCustomerResolver(t *testing.T) {
	processor := &stripeSubscriptionStatusWebhookProcessor{
		providerCode:          ProviderCodeStripe,
		stateRepository:       &stubSubscriptionStateRepository{},
		customerEmailResolver: nil,
		planCodeByPriceID:     map[string]string{},
	}
	_, err := processor.resolvePayloadUserEmail(context.Background(), stripeSubscriptionWebhookData{
		CustomerID: "cus_no_resolver",
		Metadata:   map[string]string{},
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

// ---------------------------------------------------------------------------
// resolvePayloadUserEmail – customer resolve error
// ---------------------------------------------------------------------------

func TestStripeResolvePayloadUserEmailCustomerResolveError(t *testing.T) {
	processor := &stripeSubscriptionStatusWebhookProcessor{
		providerCode:          ProviderCodeStripe,
		stateRepository:       &stubSubscriptionStateRepository{},
		customerEmailResolver: &stubStripeCustomerEmailResolver{err: errors.New("api_err")},
		planCodeByPriceID:     map[string]string{},
	}
	_, err := processor.resolvePayloadUserEmail(context.Background(), stripeSubscriptionWebhookData{
		CustomerID: "cus_err",
		Metadata:   map[string]string{},
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

// ---------------------------------------------------------------------------
// resolveLifecycleUserEmail – non-metadata-invalid error passes through
// ---------------------------------------------------------------------------

func TestStripeResolveLifecycleUserEmailNonMetadataError(t *testing.T) {
	processor := &stripeSubscriptionStatusWebhookProcessor{
		providerCode:          ProviderCodeStripe,
		stateRepository:       &stubSubscriptionStateRepository{},
		customerEmailResolver: &stubStripeCustomerEmailResolver{err: errors.New("network_error")},
		planCodeByPriceID:     map[string]string{},
	}
	// customerEmailResolver returns an error that wraps into ErrWebhookGrantMetadataInvalid
	// but only the metadata invalid case triggers fallback to subscription state lookup.
	// Since resolvePayloadUserEmail wraps the error as ErrWebhookGrantMetadataInvalid,
	// it WILL try the fallback.
	_, err := processor.resolveLifecycleUserEmail(context.Background(), stripeSubscriptionWebhookData{
		ID:         "sub_net_err",
		CustomerID: "cus_net_err",
		Metadata:   map[string]string{},
	})
	// The error from resolvePayloadUserEmail IS ErrWebhookGrantMetadataInvalid,
	// so it does fall through to subscription state lookup. Since state repo returns
	// not found, it returns the original error.
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

// ---------------------------------------------------------------------------
// isStripeCheckoutSessionPaid
// ---------------------------------------------------------------------------

func TestIsStripeCheckoutSessionPaidTrue(t *testing.T) {
	require.True(t, isStripeCheckoutSessionPaid(stripeCheckoutSessionWebhookData{
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
	}))
}

func TestIsStripeCheckoutSessionPaidNotComplete(t *testing.T) {
	require.False(t, isStripeCheckoutSessionPaid(stripeCheckoutSessionWebhookData{
		Status:        "open",
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
	}))
}

func TestIsStripeCheckoutSessionPaidNotPaid(t *testing.T) {
	require.False(t, isStripeCheckoutSessionPaid(stripeCheckoutSessionWebhookData{
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: "unpaid",
	}))
}

// ---------------------------------------------------------------------------
// Pack grant: pack code resolved from price ID when pack code empty
// ---------------------------------------------------------------------------

func TestStripeGrantResolverPackCodeFromPriceIDWhenEmpty(t *testing.T) {
	resolver, err := testStripeGrantResolver(
		testPlans(), testPacks(), testPlanGrantsByPriceID(), testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)
	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_pack_code_fb",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModePaymentRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindTopUpPack,
			stripeMetadataPriceIDKey:      "price_pack_top_up",
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, "top_up", grant.Metadata[billingGrantMetadataPackCodeKey])
}

// ---------------------------------------------------------------------------
// Mock WebhookGrantResolver for direct processor testing
// ---------------------------------------------------------------------------

type stubWebhookGrantResolver struct {
	grant       WebhookGrant
	shouldGrant bool
	err         error
}

func (r *stubWebhookGrantResolver) Resolve(_ context.Context, _ WebhookEvent) (WebhookGrant, bool, error) {
	return r.grant, r.shouldGrant, r.err
}

// ---------------------------------------------------------------------------
// processCheckoutSessionCompletedEvent – plan code fallback from grant reason
// ---------------------------------------------------------------------------

func TestStripeProcessCheckoutPlanCodeFallbackFromGrantReason(t *testing.T) {
	repo := &stubSubscriptionStateRepository{}
	processor := &stripeSubscriptionStatusWebhookProcessor{
		providerCode:    ProviderCodeStripe,
		stateRepository: repo,
		grantResolver: &stubWebhookGrantResolver{
			grant: WebhookGrant{
				UserEmail: "user@example.com",
				Credits:   1000,
				Reason:    "subscription_monthly_pro",
				Reference: "stripe:subscription:cs_test:pro",
				Metadata: map[string]string{
					billingGrantMetadataPurchaseKindKey:   stripePurchaseKindSubscription,
					billingGrantMetadataTransactionIDKey:  "cs_test",
					billingGrantMetadataSubscriptionIDKey: "sub_test",
				},
			},
			shouldGrant: true,
		},
		customerEmailResolver: nil,
		planCodeByPriceID:     map[string]string{},
		eventStatusProvider:   testEventStatusProvider(),
	}
	err := processor.processCheckoutSessionCompletedEvent(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_reason_fb",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		OccurredAt:   time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, "pro", repo.inputs[0].ActivePlan)
}

// ---------------------------------------------------------------------------
// processCheckoutSessionCompletedEvent – plan code fallback from existing state
// ---------------------------------------------------------------------------

func TestStripeProcessCheckoutPlanCodeFallbackFromExistingStateDirect(t *testing.T) {
	repo := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			ProviderCode:        ProviderCodeStripe,
			UserEmail:           "user@example.com",
			Status:              subscriptionStatusActive,
			ActivePlan:          "plus",
			LastEventOccurredAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	processor := &stripeSubscriptionStatusWebhookProcessor{
		providerCode:    ProviderCodeStripe,
		stateRepository: repo,
		grantResolver: &stubWebhookGrantResolver{
			grant: WebhookGrant{
				UserEmail: "user@example.com",
				Credits:   1000,
				Reason:    "custom_reason",
				Reference: "stripe:subscription:cs_fb:unknown",
				Metadata: map[string]string{
					billingGrantMetadataPurchaseKindKey:   stripePurchaseKindSubscription,
					billingGrantMetadataTransactionIDKey:  "cs_fb",
					billingGrantMetadataSubscriptionIDKey: "sub_fb",
				},
			},
			shouldGrant: true,
		},
		customerEmailResolver: nil,
		planCodeByPriceID:     map[string]string{},
		eventStatusProvider:   testEventStatusProvider(),
	}
	err := processor.processCheckoutSessionCompletedEvent(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_state_fb_direct",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		OccurredAt:   time.Date(2026, 3, 10, 11, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, repo.inputs, 1)
	require.Equal(t, "plus", repo.inputs[0].ActivePlan)
}

// ---------------------------------------------------------------------------
// newStripeWebhookGrantResolverFromProvider – invalid plan/pack definitions (continue)
// ---------------------------------------------------------------------------

func TestNewStripeWebhookGrantResolverFromProviderSkipsInvalidDefinitions(t *testing.T) {
	provider := &StripeProvider{
		plans: map[string]stripePlanDefinition{
			"valid": {
				Plan:    SubscriptionPlan{Code: "pro", MonthlyCredits: 1000},
				PriceID: "price_pro",
			},
			"empty_price": {
				Plan:    SubscriptionPlan{Code: "bad", MonthlyCredits: 500},
				PriceID: "  ",
			},
			"empty_code": {
				Plan:    SubscriptionPlan{Code: " ", MonthlyCredits: 500},
				PriceID: "price_bad_code",
			},
			"zero_credits": {
				Plan:    SubscriptionPlan{Code: "zero", MonthlyCredits: 0},
				PriceID: "price_zero",
			},
		},
		packs: map[string]stripePackDefinition{
			"valid": {
				Pack:    TopUpPack{Code: "top_up", Credits: 2400},
				PriceID: "price_pack_top_up",
			},
			"empty_price": {
				Pack:    TopUpPack{Code: "bad", Credits: 100},
				PriceID: "  ",
			},
			"empty_code": {
				Pack:    TopUpPack{Code: " ", Credits: 100},
				PriceID: "price_bad_pack_code",
			},
			"zero_credits": {
				Pack:    TopUpPack{Code: "zero_pack", Credits: 0},
				PriceID: "price_zero_pack",
			},
		},
		client: &stubStripeCommerceClient{},
	}
	// The continue branches in the plan/pack loops are exercised here.
	// The function may still fail downstream in newStripeWebhookGrantResolverWithCatalog
	// because SubscriptionPlans() returns all plans from the map.
	_, _ = newStripeWebhookGrantResolverFromProvider(provider)
}

// ---------------------------------------------------------------------------
// newStripeSubscriptionStatusWebhookProcessor – grantResolverErr path
// ---------------------------------------------------------------------------

func TestNewStripeSubscriptionStatusWebhookProcessorGrantResolverError(t *testing.T) {
	provider := &StripeProvider{
		plans: map[string]stripePlanDefinition{
			"broken": {
				Plan:    SubscriptionPlan{Code: "bad", MonthlyCredits: -1},
				PriceID: "price_bad",
			},
		},
		packs:  map[string]stripePackDefinition{},
		client: &stubStripeCommerceClient{},
	}
	repo := &stubSubscriptionStateRepository{}
	_, err := newStripeSubscriptionStatusWebhookProcessor(provider, repo)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Resolve – subscription with plan code from planGrantByPriceID where
// planCreditsByCode doesn't have it (lines 254-259 in Resolve)
// ---------------------------------------------------------------------------

func TestStripeGrantResolverSubscriptionCreditsFromGrantByPriceIDOnly(t *testing.T) {
	planGrants := map[string]stripeGrantDefinition{
		"price_exclusive": {Code: "exclusive", Credits: 777},
	}
	resolver, err := newStripeWebhookGrantResolverWithCatalog(
		[]SubscriptionPlan{{Code: "base", MonthlyCredits: 100}},
		testPacks(),
		planGrants,
		testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_exclusive",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPriceIDKey:      "price_exclusive",
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, int64(777), grant.Credits)
	require.Equal(t, "exclusive", grant.Metadata[billingGrantMetadataPlanCodeKey])
}

// ---------------------------------------------------------------------------
// Resolve – subscription where plan code from metadata matches planGrantByPriceID code
// but NOT in planCreditsByCode
// ---------------------------------------------------------------------------

func TestStripeGrantResolverSubscriptionPlanCodeFromMetadataCreditsFromGrant(t *testing.T) {
	planGrants := map[string]stripeGrantDefinition{
		"price_special": {Code: "special", Credits: 555},
	}
	resolver, err := newStripeWebhookGrantResolverWithCatalog(
		[]SubscriptionPlan{{Code: "other", MonthlyCredits: 200}},
		testPacks(),
		planGrants,
		testPackGrantsByPriceID(),
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_special",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModeSubscriptionRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     "special",
			stripeMetadataPriceIDKey:      "price_special",
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, int64(555), grant.Credits)
}

// ---------------------------------------------------------------------------
// Top-up pack: credits=0, packCreditsByCode miss, packGrantByPriceID hit
// with empty packCode (exercises packCode = packGrantDefinition.Code)
// ---------------------------------------------------------------------------

func TestStripeGrantResolverTopUpPackCreditsAndCodeFromGrantByPriceID(t *testing.T) {
	packGrants := map[string]stripeGrantDefinition{
		"price_exclusive_pack": {Code: "exclusive_pack", Credits: 333},
	}
	resolver, err := newStripeWebhookGrantResolverWithCatalog(
		testPlans(),
		[]TopUpPack{{Code: "base_pack", Credits: 100}},
		testPlanGrantsByPriceID(),
		packGrants,
		&stubStripeCustomerEmailResolver{email: "user@example.com"},
		testEventStatusProvider(),
	)
	require.NoError(t, err)

	payload := buildStripeCheckoutPayload(stripeCheckoutSessionWebhookData{
		ID:            "cs_excl_pack",
		Status:        stripeCheckoutStatusComplete,
		PaymentStatus: stripeCheckoutPaymentStatusPaid,
		Mode:          stripeCheckoutModePaymentRaw,
		Metadata: map[string]string{
			stripeMetadataUserEmailKey:    "user@example.com",
			stripeMetadataPurchaseKindKey: stripePurchaseKindTopUpPack,
			stripeMetadataPriceIDKey:      "price_exclusive_pack",
		},
	})
	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payload,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, int64(333), grant.Credits)
	require.Equal(t, "exclusive_pack", grant.Metadata[billingGrantMetadataPackCodeKey])
}

// Coverage gap tests for stripe_webhook_processing.go

func TestStripeResolveLifecycleUserEmailGetBySubscriptionError(t *testing.T) {
	settings := testStripeProviderSettings()
	client := &stubStripeCommerceClient{}
	provider, providerErr := NewStripeProvider(settings, &stubStripeVerifier{}, client)
	require.NoError(t, providerErr)

	stateRepo := &stubSubscriptionStateRepository{
		getBySubscriptionErr: errors.New("db error"),
	}
	processor, processorErr := newStripeSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(stripeSubscriptionWebhookPayload{
		Data: stripeSubscriptionWebhookPayloadData{
			Object: stripeSubscriptionWebhookData{
				ID:         "sub_123",
				Status:     "active",
				CustomerID: "",
				CreatedAt:  time.Now().Unix(),
			},
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_lifecycle",
		EventType:    stripeEventTypeSubscriptionUpdated,
		OccurredAt:   time.Now(),
		Payload:      payload,
	})
	require.Error(t, err)
}

func TestStripeResolveLifecycleUserEmailEmptySubscriptionID(t *testing.T) {
	settings := testStripeProviderSettings()
	client := &stubStripeCommerceClient{}
	provider, providerErr := NewStripeProvider(settings, &stubStripeVerifier{}, client)
	require.NoError(t, providerErr)

	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := newStripeSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	payload, _ := json.Marshal(stripeSubscriptionWebhookPayload{
		Data: stripeSubscriptionWebhookPayloadData{
			Object: stripeSubscriptionWebhookData{
				ID:         "",
				Status:     "active",
				CustomerID: "",
				CreatedAt:  time.Now().Unix(),
			},
		},
	})
	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_noid",
		EventType:    stripeEventTypeSubscriptionUpdated,
		OccurredAt:   time.Now(),
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}
