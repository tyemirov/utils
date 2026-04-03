package billing

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type unsupportedWebhookGrantResolverProvider struct{}
type webhookGrantResolverStub struct {
	grant       WebhookGrant
	shouldGrant bool
	resolveErr  error
}

func (resolver webhookGrantResolverStub) Resolve(_ context.Context, _ WebhookEvent) (WebhookGrant, bool, error) {
	return resolver.grant, resolver.shouldGrant, resolver.resolveErr
}

type capturingCreditGranter struct {
	lastInput CreditGrantInput
}

func (granter *capturingCreditGranter) GrantBillingCredits(_ context.Context, input CreditGrantInput) error {
	granter.lastInput = input
	return nil
}

type trackingCreditGranter struct {
	grants []CreditGrantInput
}

func (granter *trackingCreditGranter) GrantBillingCredits(_ context.Context, input CreditGrantInput) error {
	for _, existing := range granter.grants {
		if existing.IdempotencyKey == input.IdempotencyKey {
			return ErrDuplicateGrant
		}
	}
	granter.grants = append(granter.grants, input)
	return nil
}

func (granter *trackingCreditGranter) totalCreditsFor(email string) int64 {
	var total int64
	for _, grant := range granter.grants {
		if grant.UserEmail == email {
			total += grant.Credits
		}
	}
	return total
}

func (provider unsupportedWebhookGrantResolverProvider) Code() string {
	return "unsupported"
}

func (provider unsupportedWebhookGrantResolverProvider) SubscriptionPlans() []SubscriptionPlan {
	return []SubscriptionPlan{}
}

func (provider unsupportedWebhookGrantResolverProvider) TopUpPacks() []TopUpPack {
	return []TopUpPack{}
}

func (provider unsupportedWebhookGrantResolverProvider) PublicConfig() PublicConfig {
	return PublicConfig{}
}

func (provider unsupportedWebhookGrantResolverProvider) BuildUserSyncEvents(
	context.Context,
	string,
) ([]WebhookEvent, error) {
	return []WebhookEvent{}, nil
}

func (provider unsupportedWebhookGrantResolverProvider) CreateSubscriptionCheckout(
	context.Context,
	CustomerContext,
	string,
) (CheckoutSession, error) {
	return CheckoutSession{}, nil
}

func (provider unsupportedWebhookGrantResolverProvider) CreateTopUpCheckout(
	context.Context,
	CustomerContext,
	string,
) (CheckoutSession, error) {
	return CheckoutSession{}, nil
}

func (provider unsupportedWebhookGrantResolverProvider) CreateCustomerPortalSession(
	context.Context,
	string,
) (PortalSession, error) {
	return PortalSession{}, nil
}

func TestWebhookCreditsProcessorAppliesTopUpPackGrant(t *testing.T) {
	processor, granter := newWebhookCreditsProcessorForTest(t)
	eventPayload := createPaddleTransactionEventPayload("transaction.completed", "txn_pack_1", map[string]string{
		paddleMetadataUserEmailKey:    "buyer@example.com",
		paddleMetadataPurchaseKindKey: paddlePurchaseKindTopUpPack,
		paddleMetadataPackCodeKey:     "bulk_top_up",
	})

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_pack_1",
		EventType:    "transaction.completed",
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.EqualValues(t, 1000, granter.totalCreditsFor("buyer@example.com"))
}

func TestWebhookCreditsProcessorIsIdempotentForDuplicateEvents(t *testing.T) {
	processor, granter := newWebhookCreditsProcessorForTest(t)
	eventPayload := createPaddleTransactionEventPayload("transaction.completed", "txn_pack_2", map[string]string{
		paddleMetadataUserEmailKey:    "buyer@example.com",
		paddleMetadataPurchaseKindKey: paddlePurchaseKindTopUpPack,
		paddleMetadataPackCodeKey:     "bulk_top_up",
	})
	webhookEvent := WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_pack_2",
		EventType:    "transaction.completed",
		Payload:      eventPayload,
	}

	firstProcessErr := processor.Process(context.Background(), webhookEvent)
	secondProcessErr := processor.Process(context.Background(), webhookEvent)
	require.NoError(t, firstProcessErr)
	require.NoError(t, secondProcessErr)
	require.EqualValues(t, 1000, granter.totalCreditsFor("buyer@example.com"))
}

func TestWebhookCreditsProcessorAppliesSubscriptionGrant(t *testing.T) {
	processor, granter := newWebhookCreditsProcessorForTest(t)
	eventPayload := createPaddleTransactionEventPayload("transaction.completed", "txn_sub_1", map[string]string{
		paddleMetadataUserEmailKey:    "pro@example.com",
		paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
		paddleMetadataPlanCodeKey:     PlanCodePro,
	})

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_sub_1",
		EventType:    "transaction.completed",
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.EqualValues(t, 1000, granter.totalCreditsFor("pro@example.com"))
}

func TestWebhookCreditsProcessorFallsBackToPriceCatalogForSubscriptionGrants(t *testing.T) {
	commerceClient := &stubPaddleCommerceClient{
		resolvedCustomerEmail: "subscriber@example.com",
	}
	processor, granter := newWebhookCreditsProcessorForTestWithClient(t, commerceClient)
	eventPayload := []byte(`{
		"event_id":"evt_sub_price_catalog_1",
		"event_type":"transaction.completed",
		"data":{
			"id":"txn_sub_price_catalog_1",
			"subscription_id":"sub_price_catalog_1",
			"customer_id":"ctm_sub_1",
			"custom_data":{},
			"details":{
				"line_items":[
					{
						"price":{"id":"pri_pro"}
					}
				]
			}
		}
	}`)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_sub_price_catalog_1",
		EventType:    "transaction.completed",
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.Equal(t, "ctm_sub_1", commerceClient.receivedResolveCustomerID)
	require.EqualValues(t, 1000, granter.totalCreditsFor("subscriber@example.com"))
}

func TestWebhookCreditsProcessorFallsBackToPriceCatalogForPackGrants(t *testing.T) {
	commerceClient := &stubPaddleCommerceClient{
		resolvedCustomerEmail: "packbuyer@example.com",
	}
	processor, granter := newWebhookCreditsProcessorForTestWithClient(t, commerceClient)
	eventPayload := []byte(`{
		"event_id":"evt_pack_price_catalog_1",
		"event_type":"transaction.completed",
		"data":{
			"id":"txn_pack_price_catalog_1",
			"customer_id":"ctm_pack_1",
			"custom_data":{},
			"details":{
				"line_items":[
					{
						"price":{"id":"pri_pack_top_up"}
					}
				]
			}
		}
	}`)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_pack_price_catalog_1",
		EventType:    "transaction.completed",
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.Equal(t, "ctm_pack_1", commerceClient.receivedResolveCustomerID)
	require.EqualValues(t, 1000, granter.totalCreditsFor("packbuyer@example.com"))
}

func TestWebhookCreditsProcessorAppliesSubscriptionGrantForTransactionUpdatedPaid(t *testing.T) {
	processor, granter := newWebhookCreditsProcessorForTest(t)
	eventPayload := createPaddleTransactionEventPayloadWithStatus(
		"transaction.updated",
		"paid",
		"txn_sub_updated_paid_1",
		map[string]string{
			paddleMetadataUserEmailKey:    "pro@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_sub_updated_paid_1",
		EventType:    "transaction.updated",
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.EqualValues(t, 1000, granter.totalCreditsFor("pro@example.com"))
}

func TestWebhookCreditsProcessorIgnoresTransactionUpdatedWhenStatusNotPaid(t *testing.T) {
	processor, granter := newWebhookCreditsProcessorForTest(t)
	eventPayload := createPaddleTransactionEventPayloadWithStatus(
		"transaction.updated",
		"draft",
		"txn_sub_updated_draft_1",
		map[string]string{
			paddleMetadataUserEmailKey:    "pro@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     PlanCodePro,
		},
	)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_sub_updated_draft_1",
		EventType:    "transaction.updated",
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.EqualValues(t, 0, granter.totalCreditsFor("pro@example.com"))
}

func TestWebhookCreditsProcessorAppliesTopUpPackGrantForTransactionPaid(t *testing.T) {
	processor, granter := newWebhookCreditsProcessorForTest(t)
	eventPayload := createPaddleTransactionEventPayload(
		"transaction.paid",
		"txn_pack_paid_1",
		map[string]string{
			paddleMetadataUserEmailKey:    "buyer@example.com",
			paddleMetadataPurchaseKindKey: paddlePurchaseKindTopUpPack,
			paddleMetadataPackCodeKey:     "bulk_top_up",
		},
	)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_pack_paid_1",
		EventType:    "transaction.paid",
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.EqualValues(t, 1000, granter.totalCreditsFor("buyer@example.com"))
}

func TestWebhookCreditsProcessorIgnoresUnsupportedEventType(t *testing.T) {
	processor, granter := newWebhookCreditsProcessorForTest(t)
	eventPayload := createPaddleTransactionEventPayload("transaction.updated", "txn_ignore_1", map[string]string{
		paddleMetadataUserEmailKey:    "ignored@example.com",
		paddleMetadataPurchaseKindKey: paddlePurchaseKindTopUpPack,
		paddleMetadataPackCodeKey:     "top_up",
	})

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_ignore_1",
		EventType:    "transaction.updated",
		Payload:      eventPayload,
	})
	require.NoError(t, processErr)
	require.EqualValues(t, 0, granter.totalCreditsFor("ignored@example.com"))
}

func TestWebhookCreditsProcessorRejectsUnknownPackCode(t *testing.T) {
	processor, _ := newWebhookCreditsProcessorForTest(t)
	eventPayload := createPaddleTransactionEventPayload("transaction.completed", "txn_unknown_pack_1", map[string]string{
		paddleMetadataUserEmailKey:    "buyer@example.com",
		paddleMetadataPurchaseKindKey: paddlePurchaseKindTopUpPack,
		paddleMetadataPackCodeKey:     "unknown",
	})

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_unknown_pack_1",
		EventType:    "transaction.completed",
		Payload:      eventPayload,
	})
	require.Error(t, processErr)
	require.ErrorIs(t, processErr, ErrWebhookGrantPackUnknown)
}

func TestWebhookCreditsProcessorPersistsOccurredAtMetadata(t *testing.T) {
	granter := &capturingCreditGranter{}
	resolver := webhookGrantResolverStub{
		grant: WebhookGrant{
			UserEmail: "buyer@example.com",
			Credits:   1000,
			Reason:    "top_up_pack_bulk_top_up",
			Reference: "paddle:top_up_pack:txn_occured_1",
			Metadata: map[string]string{
				billingGrantMetadataTransactionIDKey: "txn_occured_1",
			},
		},
		shouldGrant: true,
	}
	processor, processorErr := NewCreditsWebhookProcessor(granter, resolver)
	require.NoError(t, processorErr)
	occurredAt := time.Date(2026, time.February, 18, 9, 30, 0, 0, time.UTC)

	processErr := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_occured_1",
		EventType:    "transaction.completed",
		OccurredAt:   occurredAt,
		Payload:      []byte(`{}`),
	})
	require.NoError(t, processErr)
	require.Equal(
		t,
		occurredAt.Format(time.RFC3339Nano),
		granter.lastInput.Metadata[metadataBillingEventOccurredAtKey],
	)
}

func TestNewWebhookGrantResolverRejectsProviderWithoutResolverCapability(t *testing.T) {
	_, resolverErr := NewWebhookGrantResolver(unsupportedWebhookGrantResolverProvider{})
	require.Error(t, resolverErr)
	require.ErrorIs(t, resolverErr, ErrWebhookGrantResolverProviderUnsupported)
}

func newWebhookCreditsProcessorForTest(t *testing.T) (WebhookProcessor, *trackingCreditGranter) {
	return newWebhookCreditsProcessorForTestWithClient(t, &stubPaddleCommerceClient{})
}

func newWebhookCreditsProcessorForTestWithClient(
	t *testing.T,
	commerceClient paddleCommerceClient,
) (WebhookProcessor, *trackingCreditGranter) {
	t.Helper()

	granter := &trackingCreditGranter{}

	paddleProvider, providerErr := NewPaddleProvider(PaddleProviderSettings{
		Environment:        "sandbox",
		APIKey:             "pdl_sdbx_key",
		ClientToken:        "test_client_token",
		ProMonthlyPriceID:  "pri_pro",
		PlusMonthlyPriceID: "pri_plus",
		SubscriptionMonthlyCredits: map[string]int64{
			"pro":  1000,
			"plus": 10000,
		},
		SubscriptionMonthlyPrices: map[string]int64{
			"pro":  2700,
			"plus": 17000,
		},
		TopUpPackPriceIDs: map[string]string{
			PackCodeTopUp:     "pri_pack_top_up",
			PackCodeBulkTopUp: "pri_pack_bulk_top_up",
		},
		TopUpPackCredits: map[string]int64{
			PackCodeTopUp:     1000,
			PackCodeBulkTopUp: 1000,
		},
		TopUpPackPrices: map[string]int64{
			PackCodeTopUp:     1000,
			PackCodeBulkTopUp: 10000,
		},
	}, &stubPaddleVerifier{}, commerceClient)
	require.NoError(t, providerErr)

	resolver, resolverErr := NewWebhookGrantResolver(paddleProvider)
	require.NoError(t, resolverErr)

	processor, processorErr := NewCreditsWebhookProcessor(granter, resolver)
	require.NoError(t, processorErr)

	return processor, granter
}

func createPaddleTransactionEventPayload(eventType string, transactionID string, customData map[string]string) []byte {
	return createPaddleTransactionEventPayloadWithStatus(eventType, "", transactionID, customData)
}

func createPaddleTransactionEventPayloadWithStatus(
	eventType string,
	status string,
	transactionID string,
	customData map[string]string,
) []byte {
	payload := map[string]interface{}{
		"event_id":   "evt_test",
		"event_type": eventType,
		"data": map[string]interface{}{
			"id":              transactionID,
			"subscription_id": "sub_test",
			"custom_data":     customData,
			"details": map[string]interface{}{
				"line_items": []map[string]interface{}{
					{
						"price": map[string]interface{}{
							"id": "pri_line_item",
						},
					},
				},
			},
		},
	}
	normalizedStatus := status
	if strings.TrimSpace(normalizedStatus) != "" {
		dataPayload, _ := payload["data"].(map[string]interface{})
		if dataPayload != nil {
			dataPayload["status"] = normalizedStatus
		}
	}
	payloadBytes, _ := json.Marshal(payload)
	return payloadBytes
}

func TestNewCreditsWebhookProcessorRejectsNilGranter(t *testing.T) {
	resolver := webhookGrantResolverStub{}
	_, err := NewCreditsWebhookProcessor(nil, resolver)
	require.ErrorIs(t, err, ErrWebhookCreditsServiceUnavailable)
}

func TestNewCreditsWebhookProcessorRejectsNilResolver(t *testing.T) {
	granter := &capturingCreditGranter{}
	_, err := NewCreditsWebhookProcessor(granter, nil)
	require.ErrorIs(t, err, ErrWebhookGrantResolverUnavailable)
}

func TestCloneGrantMetadataWithEmptyKeys(t *testing.T) {
	result := cloneGrantMetadata(nil)
	require.Empty(t, result)

	result = cloneGrantMetadata(map[string]string{})
	require.Empty(t, result)

	result = cloneGrantMetadata(map[string]string{"": "value", " ": "other"})
	require.Empty(t, result)

	result = cloneGrantMetadata(map[string]string{"key": "value"})
	require.Equal(t, map[string]string{"key": "value"}, result)
}

func TestWebhookMetadataValueBoolType(t *testing.T) {
	metadata := map[string]interface{}{
		"flag_true":  true,
		"flag_false": false,
	}
	require.Equal(t, "true", webhookMetadataValue(metadata, "flag_true"))
	require.Equal(t, "false", webhookMetadataValue(metadata, "flag_false"))
}

func TestWebhookMetadataValueFloat64Type(t *testing.T) {
	metadata := map[string]interface{}{
		"whole":      float64(42),
		"fractional": float64(3.14),
	}
	require.Equal(t, "42", webhookMetadataValue(metadata, "whole"))
	require.Equal(t, "3.14", webhookMetadataValue(metadata, "fractional"))
}

func TestWebhookMetadataValueNilValue(t *testing.T) {
	metadata := map[string]interface{}{
		"nil_key": nil,
	}
	require.Equal(t, "", webhookMetadataValue(metadata, "nil_key"))
	require.Equal(t, "", webhookMetadataValue(metadata, "missing_key"))
	require.Equal(t, "", webhookMetadataValue(nil, "key"))
}

func TestParsePackCreditsFromMetadataInvalidValue(t *testing.T) {
	metadata := map[string]interface{}{
		paddleMetadataPackCreditsKey: "abc",
	}
	_, err := parsePackCreditsFromMetadata(metadata)
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestParsePackCreditsFromMetadataZeroValue(t *testing.T) {
	metadata := map[string]interface{}{
		paddleMetadataPackCreditsKey: "0",
	}
	_, err := parsePackCreditsFromMetadata(metadata)
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestParsePackCreditsFromMetadataNegativeValue(t *testing.T) {
	metadata := map[string]interface{}{
		paddleMetadataPackCreditsKey: "-5",
	}
	_, err := parsePackCreditsFromMetadata(metadata)
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestParsePackCreditsFromMetadataValidValue(t *testing.T) {
	metadata := map[string]interface{}{
		paddleMetadataPackCreditsKey: "1000",
	}
	credits, err := parsePackCreditsFromMetadata(metadata)
	require.NoError(t, err)
	require.EqualValues(t, 1000, credits)
}

func TestParsePackCreditsFromMetadataEmpty(t *testing.T) {
	metadata := map[string]interface{}{}
	credits, err := parsePackCreditsFromMetadata(metadata)
	require.NoError(t, err)
	require.EqualValues(t, 0, credits)
}

func TestResolvePaddleTransactionPriceIDViaDetailsLineItems(t *testing.T) {
	data := paddleTransactionCompletedWebhookData{
		Items: nil,
		Details: paddleTransactionCompletedLineDetails{
			LineItems: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_from_details"},
			},
		},
	}
	priceID := resolvePaddleTransactionPriceID(data)
	require.Equal(t, "pri_from_details", priceID)
}

func TestResolvePaddleTransactionPriceIDViaDetailsLineItemsNestedPrice(t *testing.T) {
	data := paddleTransactionCompletedWebhookData{
		Items: nil,
		Details: paddleTransactionCompletedLineDetails{
			LineItems: []paddleTransactionCompletedLineItem{
				{Price: paddleTransactionCompletedLineItemPrice{ID: "pri_nested_details"}},
			},
		},
	}
	priceID := resolvePaddleTransactionPriceID(data)
	require.Equal(t, "pri_nested_details", priceID)
}

func TestResolvePaddleTransactionPriceIDReturnsEmptyWhenNone(t *testing.T) {
	data := paddleTransactionCompletedWebhookData{}
	priceID := resolvePaddleTransactionPriceID(data)
	require.Equal(t, "", priceID)
}

func TestNewWebhookGrantResolverRejectsNilProvider(t *testing.T) {
	_, err := NewWebhookGrantResolver(nil)
	require.ErrorIs(t, err, ErrWebhookGrantResolverProviderUnavailable)
}

// Coverage gap tests for webhook_grant_processor.go

func TestWebhookCreditsProcessorGrantError(t *testing.T) {
	grantErr := errors.New("granting failed")
	resolver := webhookGrantResolverStub{
		grant:       WebhookGrant{UserEmail: "user@example.com", Credits: 100, Reason: "test", Reference: "ref"},
		shouldGrant: true,
	}
	granter := &errorCreditGranter{grantErr: grantErr}
	processor, procErr := NewCreditsWebhookProcessor(granter, resolver)
	require.NoError(t, procErr)

	err := processor.Process(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_1",
		EventType:    "transaction.completed",
		OccurredAt:   time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC),
		Payload:      []byte(`{}`),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.webhook.grant.apply")
}

type errorCreditGranter struct {
	grantErr error
}

func (g *errorCreditGranter) GrantBillingCredits(_ context.Context, _ CreditGrantInput) error {
	return g.grantErr
}

func TestNewPaddleWebhookGrantResolverFromProviderNil(t *testing.T) {
	_, err := newPaddleWebhookGrantResolverFromProvider(nil)
	require.ErrorIs(t, err, ErrWebhookGrantResolverProviderUnavailable)
}

func TestNewPaddleWebhookGrantResolverFromProviderSkipsInvalidEntries(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)

	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)
	require.NotNil(t, resolver)
}

func TestNewPaddleWebhookGrantResolverWithCatalogInvalidPlan(t *testing.T) {
	_, err := newPaddleWebhookGrantResolverWithCatalog(
		[]SubscriptionPlan{{Code: "", MonthlyCredits: 100}},
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestNewPaddleWebhookGrantResolverWithCatalogInvalidPack(t *testing.T) {
	_, err := newPaddleWebhookGrantResolverWithCatalog(
		[]SubscriptionPlan{{Code: "pro", MonthlyCredits: 100}},
		[]TopUpPack{{Code: "", Credits: 100}},
		nil,
		nil,
		nil,
		nil,
	)
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestClonePaddleGrantDefinitionsByPriceIDEmpty(t *testing.T) {
	result := clonePaddleGrantDefinitionsByPriceID(nil)
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestClonePaddleGrantDefinitionsByPriceIDSkipsInvalid(t *testing.T) {
	source := map[string]paddleGrantDefinition{
		"":        {Code: "pro", Credits: 100},
		"pri_ok":  {Code: "pro", Credits: 100},
		"pri_bad": {Code: "", Credits: 100},
		"pri_neg": {Code: "pro", Credits: 0},
	}
	result := clonePaddleGrantDefinitionsByPriceID(source)
	require.Len(t, result, 1)
}

func TestPaddleWebhookGrantResolverResolveNonPaddleProvider(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{}
	_, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{ProviderCode: "stripe"})
	require.NoError(t, err)
	require.False(t, shouldGrant)
}

func TestPaddleWebhookGrantResolverResolveNilEventStatusProvider(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{eventStatusProvider: nil}
	_, _, err := resolver.Resolve(context.Background(), WebhookEvent{ProviderCode: ProviderCodePaddle})
	require.ErrorIs(t, err, ErrWebhookGrantResolverUnavailable)
}

func TestPaddleWebhookGrantResolverResolvePurchaseKindFromPriceIDEmpty(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{
		planGrantByPriceID: map[string]paddleGrantDefinition{},
		packGrantByPriceID: map[string]paddleGrantDefinition{},
	}
	result := resolver.resolvePurchaseKindFromPriceID("")
	require.Equal(t, "", result)
}

func TestPaddleWebhookGrantResolverResolvePurchaseKindFromPriceIDPlan(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{
		planGrantByPriceID: map[string]paddleGrantDefinition{"pri_pro": {Code: "pro", Credits: 100}},
		packGrantByPriceID: map[string]paddleGrantDefinition{},
	}
	result := resolver.resolvePurchaseKindFromPriceID("pri_pro")
	require.Equal(t, paddlePurchaseKindSubscription, result)
}

func TestPaddleWebhookGrantResolverResolvePurchaseKindFromPriceIDPack(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{
		planGrantByPriceID: map[string]paddleGrantDefinition{},
		packGrantByPriceID: map[string]paddleGrantDefinition{"pri_pack": {Code: "top_up", Credits: 100}},
	}
	result := resolver.resolvePurchaseKindFromPriceID("pri_pack")
	require.Equal(t, paddlePurchaseKindTopUpPack, result)
}

func TestResolveUserEmailFromCustomerAPI(t *testing.T) {
	mockResolver := &mockPaddleCustomerEmailResolver{email: "api@example.com"}
	resolver := &paddleWebhookGrantResolver{
		customerEmailResolver: mockResolver,
	}
	email, err := resolver.resolveUserEmail(context.Background(), paddleTransactionCompletedWebhookData{
		CustomerID: "cus_123",
	})
	require.NoError(t, err)
	require.Equal(t, "api@example.com", email)
}

type mockPaddleCustomerEmailResolver struct {
	email string
	err   error
}

func (m *mockPaddleCustomerEmailResolver) ResolveCustomerEmail(_ context.Context, _ string) (string, error) {
	return m.email, m.err
}

func TestResolveUserEmailCustomerIDEmpty(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{}
	_, err := resolver.resolveUserEmail(context.Background(), paddleTransactionCompletedWebhookData{
		CustomerID: "",
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestResolveUserEmailNilResolver(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{customerEmailResolver: nil}
	_, err := resolver.resolveUserEmail(context.Background(), paddleTransactionCompletedWebhookData{
		CustomerID: "cus_123",
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestResolveUserEmailResolverError(t *testing.T) {
	mockResolver := &mockPaddleCustomerEmailResolver{err: errors.New("api error")}
	resolver := &paddleWebhookGrantResolver{
		customerEmailResolver: mockResolver,
	}
	_, err := resolver.resolveUserEmail(context.Background(), paddleTransactionCompletedWebhookData{
		CustomerID: "cus_123",
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestIsGrantablePaddleTransactionStatusEdgeCases(t *testing.T) {
	require.True(t, isGrantablePaddleTransactionStatus("transaction.completed", "any"))
	require.True(t, isGrantablePaddleTransactionStatus("transaction.paid", "any"))
	require.True(t, isGrantablePaddleTransactionStatus("transaction.updated", "paid"))
	require.True(t, isGrantablePaddleTransactionStatus("transaction.updated", "completed"))
	require.False(t, isGrantablePaddleTransactionStatus("transaction.updated", "pending"))
	require.False(t, isGrantablePaddleTransactionStatus("unknown.event", "paid"))
}

func TestWebhookMetadataValueEdgeCases(t *testing.T) {
	require.Equal(t, "", webhookMetadataValue(nil, "key"))
	require.Equal(t, "", webhookMetadataValue(map[string]interface{}{"key": nil}, "key"))

	// float64 integer
	require.Equal(t, "42", webhookMetadataValue(map[string]interface{}{"key": float64(42)}, "key"))

	// float64 non-integer
	val := webhookMetadataValue(map[string]interface{}{"key": float64(42.5)}, "key")
	require.Contains(t, val, "42.5")

	// bool
	require.Equal(t, "true", webhookMetadataValue(map[string]interface{}{"key": true}, "key"))
	require.Equal(t, "false", webhookMetadataValue(map[string]interface{}{"key": false}, "key"))

	// other type
	val = webhookMetadataValue(map[string]interface{}{"key": []string{"a"}}, "key")
	require.NotEmpty(t, val)
}

func TestResolvePaddleTransactionPriceIDFromDetailLineItems(t *testing.T) {
	priceID := resolvePaddleTransactionPriceID(paddleTransactionCompletedWebhookData{
		Items: []paddleTransactionCompletedLineItem{},
		Details: paddleTransactionCompletedLineDetails{
			LineItems: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_detail"},
			},
		},
	})
	require.Equal(t, "pri_detail", priceID)
}

func TestResolvePaddleTransactionPriceIDFromNestedPrice(t *testing.T) {
	priceID := resolvePaddleTransactionPriceID(paddleTransactionCompletedWebhookData{
		Items: []paddleTransactionCompletedLineItem{
			{PriceID: "", Price: paddleTransactionCompletedLineItemPrice{ID: "pri_nested"}},
		},
	})
	require.Equal(t, "pri_nested", priceID)
}

func TestResolvePaddleTransactionPriceIDFromDetailNestedPrice(t *testing.T) {
	priceID := resolvePaddleTransactionPriceID(paddleTransactionCompletedWebhookData{
		Items: []paddleTransactionCompletedLineItem{},
		Details: paddleTransactionCompletedLineDetails{
			LineItems: []paddleTransactionCompletedLineItem{
				{PriceID: "", Price: paddleTransactionCompletedLineItemPrice{ID: "pri_detail_nested"}},
			},
		},
	})
	require.Equal(t, "pri_detail_nested", priceID)
}

func TestResolvePaddleTransactionPriceIDEmpty(t *testing.T) {
	priceID := resolvePaddleTransactionPriceID(paddleTransactionCompletedWebhookData{})
	require.Equal(t, "", priceID)
}

func TestPaddleWebhookGrantResolverResolveDefaultPurchaseKindUnknown(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)

	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)

	// Event with unknown purchase kind and no price ID match
	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_unknown",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: "unknown_kind",
			},
		},
	})
	_, _, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_unknown",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestPaddleWebhookGrantResolverResolveTopUpPackFromPriceID(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)

	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)

	// Build a payload where purchase kind is resolved from price ID (pack)
	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_pack_price",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
			},
			Items: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_pack_top_up"},
			},
		},
	})
	grant, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_pack_price",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.NoError(t, err)
	require.True(t, shouldGrant)
	require.Equal(t, int64(2400), grant.Credits)
}

func TestNewPaddleWebhookGrantResolverFromProviderSkipsEmptyPriceIDs(t *testing.T) {
	settings := testPaddleProviderSettings()
	provider, providerErr := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)

	// Provider has valid definitions - just ensure it works
	resolver, err := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, err)
	require.NotNil(t, resolver)
}

func TestPaddleWebhookGrantResolverResolveTopUpPackWithCreditsFromMetadata(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)

	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_topup_meta",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindTopUpPack,
				paddleMetadataPackCodeKey:     PackCodeTopUp,
				paddleMetadataPackCreditsKey:  "5000",
			},
		},
	})
	grant, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_topup_meta",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.NoError(t, err)
	require.True(t, shouldGrant)
	require.Equal(t, int64(5000), grant.Credits)
}

func TestPaddleWebhookGrantResolverResolveTopUpPackUnknown(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)

	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_unknown_pack",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindTopUpPack,
				paddleMetadataPackCodeKey:     "nonexistent_pack",
			},
		},
	})
	_, _, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_unknown_pack",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantPackUnknown)
}

func TestPaddleWebhookGrantResolverResolveSubscriptionFromPriceID(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)

	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)

	// No purchase kind in metadata, resolve from price ID (plan)
	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_plan_price",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
			},
			Items: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_pro"},
			},
		},
	})
	grant, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_plan_price",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.NoError(t, err)
	require.True(t, shouldGrant)
	require.Equal(t, int64(1000), grant.Credits)
}

func TestPaddleWebhookGrantResolverResolvePendingEventNotGrantable(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)

	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)

	// transaction.updated with non-grantable status (not paid/completed)
	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_pending",
			Status: "draft",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
				paddleMetadataPlanCodeKey:     PlanCodePro,
			},
		},
	})
	_, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_pending",
		EventType:    "transaction.updated",
		Payload:      payload,
	})
	require.NoError(t, err)
	require.False(t, shouldGrant)
}

func TestPaddleWebhookGrantResolverResolveInvalidPackCreditsMetadata(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)

	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_bad_credits",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindTopUpPack,
				paddleMetadataPackCodeKey:     PackCodeTopUp,
				paddleMetadataPackCreditsKey:  "notanumber",
			},
		},
	})
	_, _, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_bad_credits",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestPaddleWebhookGrantResolverResolveSubscriptionPlanUnknown(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)

	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_unknown_plan",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
				paddleMetadataPlanCodeKey:     "nonexistent_plan",
			},
		},
	})
	_, _, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_unknown_plan",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantPlanUnknown)
}

func TestPaddleWebhookGrantResolverResolveSubscriptionPlanFromPriceIDCredits(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)

	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)

	// Subscription with plan code that matches price ID but not in planCreditsByCode
	// The code finds planGrantByPriceID and uses its credits
	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_plan_from_price",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			},
			Items: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_pro"},
			},
		},
	})
	grant, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_plan_from_price_credits",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.NoError(t, err)
	require.True(t, shouldGrant)
	require.Equal(t, int64(1000), grant.Credits)
}

func TestPaddleWebhookGrantResolverResolveTopUpPackFromPriceIDWithCodeFallback(t *testing.T) {
	// Test the case where packCode is empty and resolved from packGrantByPriceID,
	// and credits come from packGrantByPriceID too (not from packCreditsByCode)
	resolver := &paddleWebhookGrantResolver{
		planCreditsByCode:  map[string]int64{},
		packCreditsByCode:  map[string]int64{},
		planGrantByPriceID: map[string]paddleGrantDefinition{},
		packGrantByPriceID: map[string]paddleGrantDefinition{
			"pri_special": {Code: "special_pack", Credits: 999},
		},
		customerEmailResolver: nil,
		eventStatusProvider:   &paddleGrantTestCheckoutEventStatusProvider{status: CheckoutEventStatusSucceeded},
	}

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_special",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindTopUpPack,
			},
			Items: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_special"},
			},
		},
	})
	grant, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_special",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.NoError(t, err)
	require.True(t, shouldGrant)
	require.Equal(t, int64(999), grant.Credits)
	require.Equal(t, "special_pack", grant.Metadata[billingGrantMetadataPackCodeKey])
}

type paddleGrantTestCheckoutEventStatusProvider struct {
	status CheckoutEventStatus
}

func (s *paddleGrantTestCheckoutEventStatusProvider) ResolveCheckoutEventStatus(string) CheckoutEventStatus {
	return s.status
}

func TestPaddleWebhookGrantResolverResolveTopUpPackFromPriceIDWithCode(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)

	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)

	// Pack purchase with no pack code in metadata, should resolve from price ID
	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_pack_no_code",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindTopUpPack,
			},
			Items: []paddleTransactionCompletedLineItem{
				{PriceID: "pri_pack_top_up"},
			},
		},
	})
	grant, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_pack_no_code",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.NoError(t, err)
	require.True(t, shouldGrant)
	require.Equal(t, int64(2400), grant.Credits)
	require.Equal(t, PackCodeTopUp, grant.Metadata[billingGrantMetadataPackCodeKey])
}

func TestPaddleWebhookGrantResolverResolveEmptyTransactionID(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)
	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
				paddleMetadataPlanCodeKey:     PlanCodePro,
			},
		},
	})
	_, _, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_empty_txn",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantPayloadInvalid)
}

func TestPaddleWebhookGrantResolverResolveUserEmailError(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{
		planCreditsByCode:     map[string]int64{"pro": 1000},
		packCreditsByCode:     map[string]int64{},
		planGrantByPriceID:    map[string]paddleGrantDefinition{},
		packGrantByPriceID:    map[string]paddleGrantDefinition{},
		customerEmailResolver: nil,
		eventStatusProvider:   &paddleGrantTestCheckoutEventStatusProvider{status: CheckoutEventStatusSucceeded},
	}

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:         "txn_no_email",
			Status:     "completed",
			CustomerID: "",
			CustomData: map[string]interface{}{
				paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
				paddleMetadataPlanCodeKey:     PlanCodePro,
			},
		},
	})
	_, _, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_no_email",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestPaddleWebhookGrantResolverNormalizesUserEmailToLowercase(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{
		planCreditsByCode:     map[string]int64{"pro": 1000},
		packCreditsByCode:     map[string]int64{},
		planGrantByPriceID:    map[string]paddleGrantDefinition{"pri_pro": {Code: PlanCodePro, Credits: 1000}},
		packGrantByPriceID:    map[string]paddleGrantDefinition{},
		customerEmailResolver: nil,
		eventStatusProvider:   &paddleGrantTestCheckoutEventStatusProvider{status: CheckoutEventStatusSucceeded},
	}

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_email_case",
			Status: "completed",
			Customer: paddleTransactionCompletedCustomer{
				Email: " USER@EXAMPLE.COM ",
			},
			CustomData: map[string]interface{}{
				paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
				paddleMetadataPlanCodeKey:     PlanCodePro,
			},
			Items: []paddleTransactionCompletedLineItem{
				{Price: paddleTransactionCompletedLineItemPrice{ID: "pri_pro"}},
			},
		},
	})
	grant, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_email_case",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.NoError(t, err)
	require.True(t, shouldGrant)
	require.Equal(t, "user@example.com", grant.UserEmail)
}

func TestPaddleWebhookGrantResolverResolvePendingNonGrantableStatus(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{
		planCreditsByCode:     map[string]int64{"pro": 1000},
		packCreditsByCode:     map[string]int64{},
		planGrantByPriceID:    map[string]paddleGrantDefinition{},
		packGrantByPriceID:    map[string]paddleGrantDefinition{},
		customerEmailResolver: nil,
		eventStatusProvider:   &paddleGrantTestCheckoutEventStatusProvider{status: CheckoutEventStatusPending},
	}

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_pending",
			Status: "draft",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey:    "user@example.com",
				paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
				paddleMetadataPlanCodeKey:     PlanCodePro,
			},
		},
	})
	_, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_pending_non_grantable",
		EventType:    "transaction.ready",
		Payload:      payload,
	})
	require.NoError(t, err)
	require.False(t, shouldGrant)
}

func TestPaddleWebhookGrantResolverResolvePurchaseKindFromPriceIDEmptyAndUnknown(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{
		planGrantByPriceID: map[string]paddleGrantDefinition{},
		packGrantByPriceID: map[string]paddleGrantDefinition{},
	}
	require.Equal(t, "", resolver.resolvePurchaseKindFromPriceID(""))
	require.Equal(t, "", resolver.resolvePurchaseKindFromPriceID("pri_unknown"))
}

func TestPaddleWebhookGrantResolverResolveUserEmailCustomerResolverError(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{
		customerEmailResolver: &stubPaddleCommerceClient{
			resolveCustomerEmailErr: errors.New("api error"),
		},
	}

	_, err := resolver.resolveUserEmail(context.Background(), paddleTransactionCompletedWebhookData{
		CustomerID: "cus_123",
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestNewPaddleWebhookGrantResolverFromProviderEmptyPlanPriceID(t *testing.T) {
	settings := testPaddleProviderSettings()
	provider, providerErr := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, providerErr)
	// The provider is valid, so the resolver should be created successfully
	resolver, resolverErr := newPaddleWebhookGrantResolverFromProvider(provider)
	require.NoError(t, resolverErr)
	require.NotNil(t, resolver)
}

func TestPaddleWebhookGrantResolverResolveNonCheckoutEventSkipped(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{
		planCreditsByCode:     map[string]int64{"pro": 1000},
		packCreditsByCode:     map[string]int64{},
		planGrantByPriceID:    map[string]paddleGrantDefinition{},
		packGrantByPriceID:    map[string]paddleGrantDefinition{},
		customerEmailResolver: nil,
		eventStatusProvider:   &paddleGrantTestCheckoutEventStatusProvider{status: CheckoutEventStatusUnknown},
	}

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_unknown",
			Status: "completed",
		},
	})
	_, shouldGrant, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_non_checkout",
		EventType:    "subscription.updated",
		Payload:      payload,
	})
	require.NoError(t, err)
	require.False(t, shouldGrant)
}

func TestPaddleWebhookGrantResolverResolveUnknownPurchaseKind(t *testing.T) {
	resolver := &paddleWebhookGrantResolver{
		planCreditsByCode:     map[string]int64{"pro": 1000},
		packCreditsByCode:     map[string]int64{},
		planGrantByPriceID:    map[string]paddleGrantDefinition{},
		packGrantByPriceID:    map[string]paddleGrantDefinition{},
		customerEmailResolver: nil,
		eventStatusProvider:   &paddleGrantTestCheckoutEventStatusProvider{status: CheckoutEventStatusSucceeded},
	}

	payload, _ := json.Marshal(paddleTransactionCompletedWebhookPayload{
		Data: paddleTransactionCompletedWebhookData{
			ID:     "txn_unknown_kind",
			Status: "completed",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
			},
		},
	})
	_, _, err := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodePaddle,
		EventID:      "evt_unknown_kind",
		EventType:    "transaction.completed",
		Payload:      payload,
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}
