package billing

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ─── subscription_status_webhook_processor.go gaps ──────────────────────────

// Line 76: newPaddleSubscriptionStatusWebhookProcessor — grantResolver creation
// error. We build a PaddleProvider directly with a plan whose MonthlyCredits <=
// 0 so that newPaddleWebhookGrantResolverWithCatalog (called inside
// NewWebhookGrantResolver) returns ErrWebhookGrantMetadataInvalid.
func TestNewPaddleSubscriptionStatusWebhookProcessorGrantResolverCreationError(t *testing.T) {
	// Build the provider bypassing NewPaddleProvider validation so we can inject
	// a plan with zero credits (which makes newPaddleWebhookGrantResolverWithCatalog
	// fail).
	provider := &PaddleProvider{
		environment: "sandbox",
		clientToken: "test_token",
		verifier:    &stubPaddleVerifier{},
		client:      &stubPaddleCommerceClient{},
		plans: map[string]paddlePlanDefinition{
			"bad_plan": {
				Plan:    SubscriptionPlan{Code: "bad_plan", MonthlyCredits: 0},
				PriceID: "pri_bad_plan",
			},
		},
		packs: map[string]paddlePackDefinition{},
	}
	stateRepo := &stubSubscriptionStateRepository{}
	_, err := newPaddleSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

// Lines 153-155: processTransactionEvent — stateRepository.Get returns an error.
func TestProcessTransactionEventStateRepositoryGetError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{
		getErr: errors.New("db_unavailable"),
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_get_err_1",
		map[string]string{
			paddleMetadataUserEmailKey:    "subscriber@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_get_err_1",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.Error(t, processErr)
	require.Contains(t, processErr.Error(), "billing.webhook.subscription_state.get")
}

// Lines 156-158: processTransactionEvent — existing state has a later
// OccurredAt than the incoming event, so the event is considered stale and
// silently skipped (returns nil without upserting).
func TestProcessTransactionEventSkipsStaleEventWithExistingHigherOccurrence(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	// The stored state is from the future relative to the incoming event.
	stateRepo := &stubSubscriptionStateRepository{
		getFound: true,
		getState: SubscriptionState{
			ProviderCode:        ProviderCodePaddle,
			UserEmail:           "subscriber@example.com",
			Status:              subscriptionStatusActive,
			ActivePlan:          PlanCodePro,
			LastEventOccurredAt: time.Date(2026, time.March, 20, 12, 0, 0, 0, time.UTC),
		},
	}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload := createPaddleTransactionEventPayload(
		"transaction.completed",
		"txn_stale_tx_1",
		map[string]string{
			paddleMetadataUserEmailKey:    "subscriber@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_stale_tx_1",
		EventType:    "transaction.completed",
		// Event is older than the stored state.
		OccurredAt: time.Date(2026, time.March, 10, 8, 0, 0, 0, time.UTC),
		Payload:    eventPayload,
	})
	require.NoError(t, processErr)
	// No upsert should have been called because the event is stale.
	require.Empty(t, stateRepo.inputs)
}

// Line 174: processSubscriptionLifecycleEvent — resolveCustomerEmail fails.
// We set the payload so that there is no email in metadata, no customer email,
// the customer ID is present (so the email resolver is called), and the
// resolver returns an error.
func TestProcessSubscriptionLifecycleEventResolveCustomerEmailError(t *testing.T) {
	commerceClient := &stubPaddleCommerceClient{
		resolveCustomerEmailErr: errors.New("customer_api_error"),
	}
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	eventPayload, marshalErr := json.Marshal(map[string]interface{}{
		"event_id":   "evt_lifecycle_email_err",
		"event_type": "subscription.updated",
		"data": map[string]interface{}{
			"id":          "",
			"status":      "active",
			"customer_id": "ctm_bad_1",
			"custom_data": map[string]interface{}{},
		},
	})
	require.NoError(t, marshalErr)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_lifecycle_email_err",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	// resolveCustomerEmail fails → resolvePayloadUserEmail returns
	// ErrWebhookGrantMetadataInvalid, which is NOT errors.Is(err,
	// ErrWebhookGrantMetadataInvalid) == true at line 298 (the check for
	// metadata-invalid to try subscription-ID fallback), so it propagates up.
	require.Error(t, processErr)
}

// Line 298: resolveLifecycleUserEmail — metadata is invalid AND payloadData.ID
// is empty. In this branch, metadata-invalid error is returned unchanged
// because there is no subscription ID to fall back to.
func TestResolveLifecycleUserEmailMetadataInvalidAndEmptySubscriptionID(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	// Payload has no email anywhere and an empty subscription ID ("id": "").
	// resolvePayloadUserEmail → ErrWebhookGrantMetadataInvalid (metadata invalid).
	// subscriptionID is "" → code returns the original metadata-invalid error.
	eventPayload, marshalErr := json.Marshal(map[string]interface{}{
		"event_id":   "evt_lifecycle_meta_err",
		"event_type": "subscription.updated",
		"data": map[string]interface{}{
			"id":          "",
			"status":      "active",
			"customer_id": "",
			"custom_data": map[string]interface{}{},
		},
	})
	require.NoError(t, marshalErr)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_lifecycle_meta_err",
		EventType:    "subscription.updated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.Error(t, processErr)
	require.ErrorIs(t, processErr, ErrWebhookGrantMetadataInvalid)
	// Should not have tried the repository fallback.
	require.Empty(t, stateRepo.receivedGetSubscriptionID)
}

// Lines 366-368: resolvePaddleSubscriptionPriceID — item.PriceID is empty but
// item.Price.ID has a value, so the fallback to item.Price.ID is used.
// (The subscription lifecycle path calls resolvePlanCode → resolvePaddleSubscriptionPriceID.)
func TestProcessSubscriptionLifecycleEventResolvePriceIDFromNestedItemPrice(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)
	stateRepo := &stubSubscriptionStateRepository{}
	processor, processorErr := NewSubscriptionStatusWebhookProcessor(provider, stateRepo)
	require.NoError(t, processorErr)

	// item.price_id is empty; item.price.id = "pri_pro" which maps to PlanCodePro.
	eventPayload, marshalErr := json.Marshal(map[string]interface{}{
		"event_id":   "evt_price_nested",
		"event_type": "subscription.activated",
		"data": map[string]interface{}{
			"id":     "sub_price_nested_1",
			"status": "active",
			"customer": map[string]interface{}{
				"email": "subscriber@example.com",
			},
			"custom_data": map[string]interface{}{},
			"items": []map[string]interface{}{
				{
					"price_id": "",
					"price": map[string]interface{}{
						"id": "pri_pro",
					},
				},
			},
		},
	})
	require.NoError(t, marshalErr)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_price_nested",
		EventType:    "subscription.activated",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.Len(t, stateRepo.inputs, 1)
	require.Equal(t, PlanCodePro, stateRepo.inputs[0].ActivePlan)
}

// ─── webhook_grant_processor.go gaps ─────────────────────────────────────────

// Lines 168-169: newPaddleWebhookGrantResolverFromProvider — plan entry with
// empty priceID is silently skipped (continue).
func TestNewPaddleWebhookGrantResolverFromProviderSkipsPlanWithEmptyPriceID(t *testing.T) {
	provider := &PaddleProvider{
		environment: "sandbox",
		clientToken: "test_token",
		verifier:    &stubPaddleVerifier{},
		client:      &stubPaddleCommerceClient{},
		plans: map[string]paddlePlanDefinition{
			// PriceID is empty → skipped at line 168
			"empty_price_plan": {
				Plan:    SubscriptionPlan{Code: "empty_price_plan", MonthlyCredits: 500},
				PriceID: "",
			},
			// Valid plan so the resolver can be constructed successfully.
			PlanCodePro: {
				Plan:    SubscriptionPlan{Code: PlanCodePro, MonthlyCredits: 1000},
				PriceID: "pri_pro",
			},
		},
		packs: map[string]paddlePackDefinition{},
	}
	resolver, err := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, err)
	require.NotNil(t, resolver)
	// The invalid plan must NOT appear in the resolver's price map.
	_, hasBadPlan := resolver.planGrantByPriceID[""]
	require.False(t, hasBadPlan)
}

// Lines 168-169 (zero credits variant): plan entry with non-positive credits is
// skipped in the planGrantByPriceID building loop. The catalog validation in
// newPaddleWebhookGrantResolverWithCatalog also fails for the same entry, so
// the resolver returns an error — but the continue on line 168 is still
// executed before that failure.
func TestNewPaddleWebhookGrantResolverFromProviderSkipsPlanWithZeroCredits(t *testing.T) {
	provider := &PaddleProvider{
		environment: "sandbox",
		clientToken: "test_token",
		verifier:    &stubPaddleVerifier{},
		client:      &stubPaddleCommerceClient{},
		plans: map[string]paddlePlanDefinition{
			// MonthlyCredits <= 0 → continue at line 168; catalog validation
			// also fails on this entry.
			"zero_credits_plan": {
				Plan:    SubscriptionPlan{Code: "zero_credits_plan", MonthlyCredits: 0},
				PriceID: "pri_zero_credits",
			},
		},
		packs: map[string]paddlePackDefinition{},
	}
	// The resolver creation fails because SubscriptionPlans() returns the
	// zero-credit plan, which the catalog validator rejects.
	_, err := newPaddleWebhookGrantResolverFromProvider(provider)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

// Lines 180-181: pack entry with empty priceID is silently skipped (continue).
func TestNewPaddleWebhookGrantResolverFromProviderSkipsPackWithEmptyPriceID(t *testing.T) {
	provider := &PaddleProvider{
		environment: "sandbox",
		clientToken: "test_token",
		verifier:    &stubPaddleVerifier{},
		client:      &stubPaddleCommerceClient{},
		plans: map[string]paddlePlanDefinition{
			PlanCodePro: {
				Plan:    SubscriptionPlan{Code: PlanCodePro, MonthlyCredits: 1000},
				PriceID: "pri_pro",
			},
		},
		packs: map[string]paddlePackDefinition{
			// PriceID is empty → skipped at line 180
			"empty_price_pack": {
				Pack:    TopUpPack{Code: "empty_price_pack", Credits: 500},
				PriceID: "",
			},
		},
	}
	resolver, err := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, err)
	require.NotNil(t, resolver)
	_, hasBadPack := resolver.packGrantByPriceID[""]
	require.False(t, hasBadPack)
}

// Lines 180-181 (zero credits variant): pack entry with non-positive credits is
// skipped in the packGrantByPriceID building loop. The catalog validation in
// newPaddleWebhookGrantResolverWithCatalog also fails for the same entry, so
// the resolver returns an error — but the continue on line 180 is still
// executed before that failure.
func TestNewPaddleWebhookGrantResolverFromProviderSkipsPackWithZeroCredits(t *testing.T) {
	provider := &PaddleProvider{
		environment: "sandbox",
		clientToken: "test_token",
		verifier:    &stubPaddleVerifier{},
		client:      &stubPaddleCommerceClient{},
		plans: map[string]paddlePlanDefinition{
			PlanCodePro: {
				Plan:    SubscriptionPlan{Code: PlanCodePro, MonthlyCredits: 1000},
				PriceID: "pri_pro",
			},
		},
		packs: map[string]paddlePackDefinition{
			// Credits <= 0 → continue at line 180; catalog validation also
			// fails on this entry.
			"zero_credits_pack": {
				Pack:    TopUpPack{Code: "zero_credits_pack", Credits: 0},
				PriceID: "pri_zero_credits_pack",
			},
		},
	}
	// The resolver creation fails because TopUpPacks() returns the zero-credit
	// pack, which the catalog validator rejects.
	_, err := newPaddleWebhookGrantResolverFromProvider(provider)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

// Lines 311-314: Resolve — subscription purchase kind, planCode is NOT in
// planCreditsByCode but IS found via the planGrantByPriceID catalog with a
// matching code, so credits come from planGrantByPriceID (the fallback).
func TestPaddleWebhookGrantResolverResolvePlanCreditsFromPriceCatalogFallback(t *testing.T) {
	// planCreditsByCode does NOT include "special_pro", so hasPlanCredits is
	// initially false. planGrantByPriceID has an entry for "pri_special" whose
	// Code == "special_pro", so lines 311-314 run and set hasPlanCredits = true.
	resolver := &paddleWebhookGrantResolver{
		planCreditsByCode:  map[string]int64{},
		packCreditsByCode:  map[string]int64{},
		planGrantByPriceID: map[string]paddleGrantDefinition{
			"pri_special": {Code: "special_pro", Credits: 750},
		},
		packGrantByPriceID:    map[string]paddleGrantDefinition{},
		customerEmailResolver: nil,
		eventStatusProvider:   &paddleGrantTestCheckoutEventStatusProvider{status: CheckoutEventStatusSucceeded},
	}

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_catalog_fallback",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
				// planCode from metadata is empty; will be resolved from planGrantByPriceID
			},
			Items: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_special"},
			},
		},
	})
	grant, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_catalog_fallback",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.NoError(t, err)
	require.True(t, shouldGrant)
	require.EqualValues(t, 750, grant.Credits)
}

// Lines 360-362: Resolve — transaction.updated event where the status is NOT
// paid/completed (pending + non-grantable check returns false, grant skipped).
// This is tested via the full Resolve call through a PaddleProvider so that
// the CheckoutEventStatusPending path is exercised.
func TestPaddleWebhookGrantResolverResolveTransactionUpdatedNonPaidStatusSkipped(t *testing.T) {
	provider, providerErr := NewPaddleProvider(
		testPaddleProviderSettings(),
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, providerErr)

	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)

	// Status is "pending" (not "paid" or "completed") → isGrantablePaddleTransactionStatus
	// returns false → Resolve returns (_, false, nil) at lines 360-362 area.
	eventPayload := createPaddleTransactionEventPayloadWithStatus(
		"transaction.updated",
		"pending",
		"txn_updated_pending",
		map[string]string{
			paddleMetadataUserEmailKey:    "user@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)
	_, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_updated_pending",
		EventType:    "transaction.updated",
		Payload:      eventPayload,
	})
	require.NoError(t, err)
	require.False(t, shouldGrant)
}

// Line 392-393: resolvePurchaseKindFromPriceID returns "" when the price ID is
// found in neither planGrantByPriceID nor packGrantByPriceID.  Exercised
// through a full Resolve call so that the return "" branch is reached in
// context.
func TestPaddleWebhookGrantResolverResolveUnknownPriceIDReturnsMetadataInvalid(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{
		planCreditsByCode:  map[string]int64{PlanCodePro: 1000},
		packCreditsByCode:  map[string]int64{},
		planGrantByPriceID: map[string]paddleGrantDefinition{},
		packGrantByPriceID: map[string]paddleGrantDefinition{},
		// customerEmailResolver not needed because email is in metadata
		customerEmailResolver: nil,
		eventStatusProvider:   &paddleGrantTestCheckoutEventStatusProvider{status: CheckoutEventStatusSucceeded},
	}

	// No purchase_kind in metadata, and the price ID "pri_unknown_xyz" is in
	// neither map → resolvePurchaseKindFromPriceID returns "" →
	// purchaseKind stays "" → ErrWebhookGrantMetadataInvalid.
	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_unknown_price",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
			},
			Items: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_unknown_xyz"},
			},
		},
	})
	_, _, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_unknown_price",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

// Lines 420-422: resolveUserEmail — customerEmailResolver is nil AND there is
// no email in metadata or customer field, but customerID IS present.  The nil
// check at lines 420-422 returns ErrWebhookGrantMetadataInvalid.
// This is exercised through the full Resolve path so that the branch is
// reached in context (unlike the existing unit test that calls
// resolveUserEmail directly).
func TestPaddleWebhookGrantResolverResolveNilEmailResolverWithCustomerID(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{
		planCreditsByCode:  map[string]int64{PlanCodePro: 1000},
		packCreditsByCode:  map[string]int64{},
		planGrantByPriceID: map[string]paddleGrantDefinition{
			"pri_pro": {Code: PlanCodePro, Credits: 1000},
		},
		packGrantByPriceID:    map[string]paddleGrantDefinition{},
		customerEmailResolver: nil, // ← key: nil resolver
		eventStatusProvider:   &paddleGrantTestCheckoutEventStatusProvider{status: CheckoutEventStatusSucceeded},
	}

	// No email in metadata or customer, but customer_id is present.  With nil
	// customerEmailResolver, lines 420-422 fire and return
	// ErrWebhookGrantMetadataInvalid.
	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:         "txn_nil_resolver",
			Status:     "completed",
			CustomerID: "ctm_nil_resolver",
			CustomData: map[string]interface{}{
				paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
				paddleMetadataPlanCodeKey:     PlanCodePro,
			},
			Items: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_pro"},
			},
		},
	})
	_, _, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_nil_resolver",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}
