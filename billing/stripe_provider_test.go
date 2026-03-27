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

type stubStripeVerifier struct{}

func (verifier *stubStripeVerifier) Verify(string, []byte) error {
	return nil
}

type stubStripeCommerceClient struct {
	resolvedCustomerID    string
	resolvedCustomerEmail string
	createdCheckoutID     string
	createdPortalURL      string
	foundCustomerID       string
	subscriptions         []stripeSubscriptionWebhookData
	findCustomerIDErr     error
	listSubscriptionsErr  error
	receivedFindEmail     string
	receivedListCustomer  string
	receivedResolveEmail  string
	receivedCheckoutInput stripeCheckoutSessionInput
	reconcileSession      stripeCheckoutSessionWebhookData
	priceByID             map[string]stripePriceResponse
}

func (client *stubStripeCommerceClient) FindCustomerID(
	_ context.Context,
	email string,
) (string, error) {
	client.receivedFindEmail = email
	if client.findCustomerIDErr != nil {
		return "", client.findCustomerIDErr
	}
	return client.foundCustomerID, nil
}

func (client *stubStripeCommerceClient) ResolveCustomerID(
	_ context.Context,
	email string,
) (string, error) {
	client.receivedResolveEmail = email
	return client.resolvedCustomerID, nil
}

func (client *stubStripeCommerceClient) ResolveCustomerEmail(
	_ context.Context,
	_ string,
) (string, error) {
	return client.resolvedCustomerEmail, nil
}

func (client *stubStripeCommerceClient) CreateCheckoutSession(
	_ context.Context,
	input stripeCheckoutSessionInput,
) (string, error) {
	client.receivedCheckoutInput = input
	return client.createdCheckoutID, nil
}

func (client *stubStripeCommerceClient) GetCheckoutSession(
	_ context.Context,
	_ string,
) (stripeCheckoutSessionWebhookData, error) {
	return client.reconcileSession, nil
}

func (client *stubStripeCommerceClient) CreateCustomerPortalURL(
	_ context.Context,
	_ stripePortalSessionInput,
) (string, error) {
	return client.createdPortalURL, nil
}

func (client *stubStripeCommerceClient) GetPrice(
	_ context.Context,
	priceID string,
) (stripePriceResponse, error) {
	priceResponse, hasPriceResponse := client.priceByID[priceID]
	if !hasPriceResponse {
		return stripePriceResponse{}, ErrStripeAPIPriceNotFound
	}
	return priceResponse, nil
}

func (client *stubStripeCommerceClient) ListSubscriptions(
	_ context.Context,
	customerID string,
) ([]stripeSubscriptionWebhookData, error) {
	client.receivedListCustomer = customerID
	if client.listSubscriptionsErr != nil {
		return nil, client.listSubscriptionsErr
	}
	return client.subscriptions, nil
}

func TestStripeProviderCreateSubscriptionCheckout(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		resolvedCustomerID: "cus_test_1",
		createdCheckoutID:  "cs_test_1",
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	checkoutSession, checkoutErr := provider.CreateSubscriptionCheckout(
		context.Background(),
		"User@Example.com",
		PlanCodePro,
	)
	require.NoError(t, checkoutErr)
	require.Equal(t, ProviderCodeStripe, checkoutSession.ProviderCode)
	require.Equal(t, "cs_test_1", checkoutSession.TransactionID)
	require.Equal(t, CheckoutModeOverlay, checkoutSession.CheckoutMode)
	require.Equal(t, "user@example.com", commerceClient.receivedResolveEmail)
	require.Equal(t, stripeCheckoutModeSubscription, commerceClient.receivedCheckoutInput.Mode)
	require.Equal(
		t,
		"user@example.com",
		commerceClient.receivedCheckoutInput.Metadata[stripeMetadataUserEmailKey],
	)
	require.Equal(
		t,
		stripePurchaseKindSubscription,
		commerceClient.receivedCheckoutInput.Metadata[stripeMetadataPurchaseKindKey],
	)
	require.Equal(t, PlanCodePro, commerceClient.receivedCheckoutInput.Metadata[stripeMetadataPlanCodeKey])
}

func TestStripeProviderCreateTopUpCheckout(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		resolvedCustomerID: "cus_test_2",
		createdCheckoutID:  "cs_test_2",
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	checkoutSession, checkoutErr := provider.CreateTopUpCheckout(
		context.Background(),
		"buyer@example.com",
		PackCodeTopUp,
	)
	require.NoError(t, checkoutErr)
	require.Equal(t, ProviderCodeStripe, checkoutSession.ProviderCode)
	require.Equal(t, "cs_test_2", checkoutSession.TransactionID)
	require.Equal(t, stripeCheckoutModePayment, commerceClient.receivedCheckoutInput.Mode)
	require.Equal(
		t,
		stripePurchaseKindTopUpPack,
		commerceClient.receivedCheckoutInput.Metadata[stripeMetadataPurchaseKindKey],
	)
	require.Equal(t, PackCodeTopUp, commerceClient.receivedCheckoutInput.Metadata[stripeMetadataPackCodeKey])
	require.Equal(t, "2400", commerceClient.receivedCheckoutInput.Metadata[stripeMetadataPackCreditsKey])
}

func TestStripeProviderCatalogIncludesPriceMetadata(t *testing.T) {
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	require.NoError(t, providerErr)

	plans := provider.SubscriptionPlans()
	require.Len(t, plans, 2)
	require.Equal(t, "$170", plans[0].PriceDisplay)
	require.Equal(t, "monthly", plans[0].BillingPeriod)
	require.Equal(t, "$27", plans[1].PriceDisplay)
	require.Equal(t, "monthly", plans[1].BillingPeriod)

	packs := provider.TopUpPacks()
	require.Len(t, packs, 1)
	require.Equal(t, packLabelTopUp, packs[0].Label)
	require.Equal(t, "$10", packs[0].PriceDisplay)
	require.Equal(t, "one-time", packs[0].BillingPeriod)
}

func TestStripeProviderBuildCheckoutReconcileEventPending(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		reconcileSession: stripeCheckoutSessionWebhookData{
			ID:            "cs_test_pending",
			Status:        "open",
			PaymentStatus: "unpaid",
			Metadata: map[string]string{
				stripeMetadataUserEmailKey: "buyer@example.com",
			},
			CreatedAt: time.Date(2026, time.February, 24, 11, 0, 0, 0, time.UTC).Unix(),
		},
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	webhookEvent, checkoutUserEmail, reconcileErr := provider.BuildCheckoutReconcileEvent(
		context.Background(),
		"cs_test_pending",
	)
	require.NoError(t, reconcileErr)
	require.Equal(t, stripeEventTypeCheckoutSessionPending, webhookEvent.EventType)
	require.Equal(t, "buyer@example.com", checkoutUserEmail)
}

func TestStripeWebhookGrantResolverResolvesSubscriptionGrant(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		resolvedCustomerID: "cus_test_3",
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	resolver, resolverErr := provider.NewWebhookGrantResolver()
	require.NoError(t, resolverErr)

	payloadBytes, marshalErr := json.Marshal(stripeCheckoutSessionWebhookPayload{
		Data: stripeCheckoutSessionWebhookPayloadData{
			Object: stripeCheckoutSessionWebhookData{
				ID:            "cs_test_subscription",
				Status:        stripeCheckoutStatusComplete,
				PaymentStatus: stripeCheckoutPaymentStatusPaid,
				Mode:          stripeCheckoutModeSubscriptionRaw,
				Metadata: map[string]string{
					stripeMetadataUserEmailKey:    "subscriber@example.com",
					stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
					stripeMetadataPlanCodeKey:     PlanCodePro,
				},
			},
		},
	})
	require.NoError(t, marshalErr)

	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_stripe_1",
		EventType:    stripeEventTypeCheckoutSessionCompleted,
		Payload:      payloadBytes,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, "subscriber@example.com", grant.UserEmail)
	require.EqualValues(t, 1000, grant.Credits)
	require.Equal(t, "subscription_monthly_pro", grant.Reason)
}

func TestStripeWebhookGrantResolverResolvesSubscriptionGrantFromAsyncSucceededEvent(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		resolvedCustomerID: "cus_test_3",
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	resolver, resolverErr := provider.NewWebhookGrantResolver()
	require.NoError(t, resolverErr)

	payloadBytes, marshalErr := json.Marshal(stripeCheckoutSessionWebhookPayload{
		Data: stripeCheckoutSessionWebhookPayloadData{
			Object: stripeCheckoutSessionWebhookData{
				ID:            "cs_test_async_subscription",
				Status:        stripeCheckoutStatusComplete,
				PaymentStatus: stripeCheckoutPaymentStatusPaid,
				Mode:          stripeCheckoutModeSubscriptionRaw,
				Metadata: map[string]string{
					stripeMetadataUserEmailKey:    "subscriber@example.com",
					stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
					stripeMetadataPlanCodeKey:     PlanCodePro,
				},
			},
		},
	})
	require.NoError(t, marshalErr)

	grant, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_stripe_async_1",
		EventType:    stripeEventTypeCheckoutSessionAsyncPaymentSucceeded,
		Payload:      payloadBytes,
	})
	require.NoError(t, resolveErr)
	require.True(t, shouldGrant)
	require.Equal(t, "subscriber@example.com", grant.UserEmail)
	require.EqualValues(t, 1000, grant.Credits)
}

func TestStripeWebhookGrantResolverSkipsAsyncFailedEvent(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		resolvedCustomerID: "cus_test_3",
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	resolver, resolverErr := provider.NewWebhookGrantResolver()
	require.NoError(t, resolverErr)

	payloadBytes, marshalErr := json.Marshal(stripeCheckoutSessionWebhookPayload{
		Data: stripeCheckoutSessionWebhookPayloadData{
			Object: stripeCheckoutSessionWebhookData{
				ID:            "cs_test_async_failed",
				Status:        "open",
				PaymentStatus: "unpaid",
				Mode:          stripeCheckoutModeSubscriptionRaw,
				Metadata: map[string]string{
					stripeMetadataUserEmailKey:    "subscriber@example.com",
					stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
					stripeMetadataPlanCodeKey:     PlanCodePro,
				},
			},
		},
	})
	require.NoError(t, marshalErr)

	_, shouldGrant, resolveErr := resolver.Resolve(context.Background(), WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      "evt_stripe_async_failed_1",
		EventType:    stripeEventTypeCheckoutSessionAsyncPaymentFailed,
		Payload:      payloadBytes,
	})
	require.NoError(t, resolveErr)
	require.False(t, shouldGrant)
}

func TestStripeProviderValidateCatalogAcceptsRecurringAndOneOffPrices(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		priceByID: map[string]stripePriceResponse{
			"price_pro": {
				ID:         "price_pro",
				Type:       "recurring",
				UnitAmount: 2700,
				Recurring: &stripePriceRecurring{
					Interval: "month",
				},
			},
			"price_plus": {
				ID:         "price_plus",
				Type:       "recurring",
				UnitAmount: 17000,
				Recurring: &stripePriceRecurring{
					Interval: "month",
				},
			},
			"price_pack_top_up": {
				ID:         "price_pack_top_up",
				Type:       "one_time",
				UnitAmount: 1000,
			},
		},
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	require.NoError(t, provider.ValidateCatalog(context.Background()))
}

func TestStripeProviderValidateCatalogRejectsNonRecurringPlanPrice(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		priceByID: map[string]stripePriceResponse{
			"price_pro": {
				ID:         "price_pro",
				Type:       "one_time",
				UnitAmount: 2700,
			},
			"price_plus": {
				ID:         "price_plus",
				Type:       "recurring",
				UnitAmount: 17000,
				Recurring: &stripePriceRecurring{
					Interval: "month",
				},
			},
			"price_pack_top_up": {
				ID:         "price_pack_top_up",
				Type:       "one_time",
				UnitAmount: 1000,
			},
		},
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	validateErr := provider.ValidateCatalog(context.Background())
	require.Error(t, validateErr)
	require.ErrorIs(t, validateErr, ErrStripeProviderPriceRecurringInvalid)
}

func TestStripeProviderValidateCatalogRejectsPlanPriceAmountDrift(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		priceByID: map[string]stripePriceResponse{
			"price_pro": {
				ID:         "price_pro",
				Type:       "recurring",
				UnitAmount: 2800,
				Recurring: &stripePriceRecurring{
					Interval: "month",
				},
			},
			"price_plus": {
				ID:         "price_plus",
				Type:       "recurring",
				UnitAmount: 17000,
				Recurring: &stripePriceRecurring{
					Interval: "month",
				},
			},
			"price_pack_top_up": {
				ID:         "price_pack_top_up",
				Type:       "one_time",
				UnitAmount: 1000,
			},
		},
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	validateErr := provider.ValidateCatalog(context.Background())
	require.Error(t, validateErr)
	require.ErrorIs(t, validateErr, ErrStripeProviderPriceAmountMismatch)
}

func TestStripeProviderValidateCatalogRejectsPackPriceAmountDrift(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		priceByID: map[string]stripePriceResponse{
			"price_pro": {
				ID:         "price_pro",
				Type:       "recurring",
				UnitAmount: 2700,
				Recurring: &stripePriceRecurring{
					Interval: "month",
				},
			},
			"price_plus": {
				ID:         "price_plus",
				Type:       "recurring",
				UnitAmount: 17000,
				Recurring: &stripePriceRecurring{
					Interval: "month",
				},
			},
			"price_pack_top_up": {
				ID:         "price_pack_top_up",
				Type:       "one_time",
				UnitAmount: 900,
			},
		},
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	validateErr := provider.ValidateCatalog(context.Background())
	require.Error(t, validateErr)
	require.ErrorIs(t, validateErr, ErrStripeProviderPriceAmountMismatch)
}

func TestStripeProviderBuildUserSyncEventsReturnsInactiveEventWhenCustomerMissing(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "",
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), " User@Example.com ")
	require.NoError(t, syncErr)
	require.Len(t, syncEvents, 1)
	require.Equal(t, ProviderCodeStripe, syncEvents[0].ProviderCode)
	require.Equal(t, stripeEventTypeSubscriptionUpdated, syncEvents[0].EventType)
	require.True(t, strings.HasPrefix(syncEvents[0].EventID, "sync:subscription:none:"))
	require.Equal(t, "user@example.com", commerceClient.receivedFindEmail)

	payload := stripeSubscriptionWebhookPayload{}
	require.NoError(t, json.Unmarshal(syncEvents[0].Payload, &payload))
	require.Equal(t, stripeSubscriptionStatusCanceled, strings.ToLower(strings.TrimSpace(payload.Object().Status)))
	require.Equal(
		t,
		"user@example.com",
		strings.ToLower(strings.TrimSpace(payload.Object().Metadata[stripeMetadataUserEmailKey])),
	)
}

func TestStripeProviderBuildUserSyncEventsSortsInactiveBeforeActive(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "cus_sync_1",
		subscriptions: []stripeSubscriptionWebhookData{
			{
				ID:         "sub_active",
				Status:     stripeSubscriptionStatusActive,
				CustomerID: "cus_sync_1",
				CreatedAt:  time.Date(2026, time.February, 24, 9, 0, 0, 0, time.UTC).Unix(),
			},
			{
				ID:         "sub_canceled",
				Status:     stripeSubscriptionStatusCanceled,
				CustomerID: "cus_sync_1",
				CreatedAt:  time.Date(2026, time.February, 24, 10, 0, 0, 0, time.UTC).Unix(),
			},
		},
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	require.Len(t, syncEvents, 2)
	require.Equal(t, "sync:subscription:sub_canceled:canceled", syncEvents[0].EventID)
	require.Equal(t, "sync:subscription:sub_active:active", syncEvents[1].EventID)
	require.Equal(t, "cus_sync_1", commerceClient.receivedListCustomer)
}

func TestStripeProviderSignatureHeaderName(t *testing.T) {
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	require.NoError(t, providerErr)
	require.Equal(t, "Stripe-Signature", provider.SignatureHeaderName())
}

func TestStripeProviderVerifySignature(t *testing.T) {
	t.Run("delegates to verifier", func(t *testing.T) {
		provider, providerErr := NewStripeProvider(
			testStripeProviderSettings(),
			&stubStripeVerifier{},
			&stubStripeCommerceClient{},
		)
		require.NoError(t, providerErr)
		require.NoError(t, provider.VerifySignature("sig_header", []byte(`{}`)))
	})

	t.Run("nil verifier returns error", func(t *testing.T) {
		provider := &StripeProvider{}
		require.ErrorIs(t, provider.VerifySignature("sig", []byte(`{}`)), ErrStripeProviderVerifierUnavailable)
	})

	t.Run("nil provider returns error", func(t *testing.T) {
		var provider *StripeProvider
		require.ErrorIs(t, provider.VerifySignature("sig", []byte(`{}`)), ErrStripeProviderVerifierUnavailable)
	})
}

func TestStripeProviderParseWebhookEvent(t *testing.T) {
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	require.NoError(t, providerErr)

	t.Run("valid checkout event", func(t *testing.T) {
		metadata, parseErr := provider.ParseWebhookEvent(
			[]byte(`{"id":"evt_1","type":"checkout.session.completed","created":1708787200}`),
		)
		require.NoError(t, parseErr)
		require.Equal(t, "evt_1", metadata.EventID)
		require.Equal(t, "checkout.session.completed", metadata.EventType)
		require.False(t, metadata.OccurredAt.IsZero())
	})

	t.Run("valid subscription event", func(t *testing.T) {
		metadata, parseErr := provider.ParseWebhookEvent(
			[]byte(`{"id":"evt_2","type":"customer.subscription.updated","created":1708787200}`),
		)
		require.NoError(t, parseErr)
		require.Equal(t, "evt_2", metadata.EventID)
		require.Equal(t, "customer.subscription.updated", metadata.EventType)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, parseErr := provider.ParseWebhookEvent([]byte(`not-json`))
		require.ErrorIs(t, parseErr, ErrStripeWebhookPayloadInvalid)
	})

	t.Run("missing ID", func(t *testing.T) {
		_, parseErr := provider.ParseWebhookEvent(
			[]byte(`{"type":"checkout.session.completed","created":1708787200}`),
		)
		require.ErrorIs(t, parseErr, ErrStripeWebhookPayloadInvalid)
	})

	t.Run("missing type", func(t *testing.T) {
		_, parseErr := provider.ParseWebhookEvent(
			[]byte(`{"id":"evt_1","created":1708787200}`),
		)
		require.ErrorIs(t, parseErr, ErrStripeWebhookPayloadInvalid)
	})

	t.Run("zero timestamp", func(t *testing.T) {
		_, parseErr := provider.ParseWebhookEvent(
			[]byte(`{"id":"evt_1","type":"checkout.session.completed","created":0}`),
		)
		require.ErrorIs(t, parseErr, ErrStripeWebhookPayloadInvalid)
	})
}

func TestStripeProviderPublicConfig(t *testing.T) {
	t.Run("returns correct config", func(t *testing.T) {
		provider, providerErr := NewStripeProvider(
			testStripeProviderSettings(),
			&stubStripeVerifier{},
			&stubStripeCommerceClient{},
		)
		require.NoError(t, providerErr)
		config := provider.PublicConfig()
		require.Equal(t, ProviderCodeStripe, config.ProviderCode)
		require.Equal(t, "sandbox", config.Environment)
		require.Equal(t, "pk_test_123", config.ClientToken)
	})

	t.Run("nil provider returns empty", func(t *testing.T) {
		var provider *StripeProvider
		config := provider.PublicConfig()
		require.Equal(t, PublicConfig{}, config)
	})
}

func TestStripeProviderCreateCustomerPortalSession(t *testing.T) {
	t.Run("valid flow", func(t *testing.T) {
		commerceClient := &stubStripeCommerceClient{
			resolvedCustomerID: "cus_portal_1",
			createdPortalURL:   "https://billing.stripe.com/session/portal_abc",
		}
		provider, providerErr := NewStripeProvider(
			testStripeProviderSettings(),
			&stubStripeVerifier{},
			commerceClient,
		)
		require.NoError(t, providerErr)
		portalSession, portalErr := provider.CreateCustomerPortalSession(context.Background(), "user@example.com")
		require.NoError(t, portalErr)
		require.Equal(t, ProviderCodeStripe, portalSession.ProviderCode)
		require.Equal(t, "https://billing.stripe.com/session/portal_abc", portalSession.URL)
	})

	t.Run("empty email", func(t *testing.T) {
		provider, providerErr := NewStripeProvider(
			testStripeProviderSettings(),
			&stubStripeVerifier{},
			&stubStripeCommerceClient{},
		)
		require.NoError(t, providerErr)
		_, portalErr := provider.CreateCustomerPortalSession(context.Background(), "  ")
		require.ErrorIs(t, portalErr, ErrBillingUserEmailInvalid)
	})

	t.Run("nil client", func(t *testing.T) {
		provider := &StripeProvider{}
		_, portalErr := provider.CreateCustomerPortalSession(context.Background(), "user@example.com")
		require.ErrorIs(t, portalErr, ErrStripeProviderClientUnavailable)
	})
}

func TestStripeProviderNewSubscriptionStatusWebhookProcessor(t *testing.T) {
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	require.NoError(t, providerErr)
	processor, processorErr := provider.NewSubscriptionStatusWebhookProcessor(&stubStripeStateRepository{})
	require.NoError(t, processorErr)
	require.NotNil(t, processor)
}

type stubStripeStateRepository struct{}

func (r *stubStripeStateRepository) Upsert(_ context.Context, _ SubscriptionStateUpsertInput) error {
	return nil
}

func (r *stubStripeStateRepository) Get(_ context.Context, _ string, _ string) (SubscriptionState, bool, error) {
	return SubscriptionState{}, false, nil
}

func (r *stubStripeStateRepository) GetBySubscriptionID(_ context.Context, _ string, _ string) (SubscriptionState, bool, error) {
	return SubscriptionState{}, false, nil
}

func TestStripeProviderResolveCheckoutEventStatus(t *testing.T) {
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	require.NoError(t, providerErr)

	require.Equal(t, CheckoutEventStatusSucceeded, provider.ResolveCheckoutEventStatus("checkout.session.completed"))
	require.Equal(t, CheckoutEventStatusSucceeded, provider.ResolveCheckoutEventStatus("checkout.session.async_payment_succeeded"))
	require.Equal(t, CheckoutEventStatusPending, provider.ResolveCheckoutEventStatus("checkout.session.pending"))
	require.Equal(t, CheckoutEventStatusFailed, provider.ResolveCheckoutEventStatus("checkout.session.async_payment_failed"))
	require.Equal(t, CheckoutEventStatusExpired, provider.ResolveCheckoutEventStatus("checkout.session.expired"))
	require.Equal(t, CheckoutEventStatusUnknown, provider.ResolveCheckoutEventStatus("unknown.event"))
}

func TestStripeProviderBuildUserSyncEventsWithActiveSubscription(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "cus_active_1",
		subscriptions: []stripeSubscriptionWebhookData{
			{
				ID:         "sub_active_only",
				Status:     stripeSubscriptionStatusActive,
				CustomerID: "cus_active_1",
				CreatedAt:  time.Date(2026, time.February, 24, 9, 0, 0, 0, time.UTC).Unix(),
			},
		},
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "active@example.com")
	require.NoError(t, syncErr)
	require.Len(t, syncEvents, 1)
	require.Equal(t, "sync:subscription:sub_active_only:active", syncEvents[0].EventID)
	require.Equal(t, stripeEventTypeSubscriptionUpdated, syncEvents[0].EventType)
}

func TestStripeProviderBuildCheckoutReconcileEventCompleted(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		reconcileSession: stripeCheckoutSessionWebhookData{
			ID:            "cs_test_completed",
			Status:        stripeCheckoutStatusComplete,
			PaymentStatus: stripeCheckoutPaymentStatusPaid,
			Metadata: map[string]string{
				stripeMetadataUserEmailKey: "buyer@example.com",
			},
			CreatedAt: time.Date(2026, time.February, 24, 11, 0, 0, 0, time.UTC).Unix(),
		},
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	webhookEvent, checkoutUserEmail, reconcileErr := provider.BuildCheckoutReconcileEvent(
		context.Background(),
		"cs_test_completed",
	)
	require.NoError(t, reconcileErr)
	require.Equal(t, stripeEventTypeCheckoutSessionCompleted, webhookEvent.EventType)
	require.Equal(t, "buyer@example.com", checkoutUserEmail)
	require.Contains(t, webhookEvent.EventID, "reconcile:cs_test_completed")
}

func TestStripeProviderResolveCheckoutUserEmail(t *testing.T) {
	t.Run("from metadata", func(t *testing.T) {
		commerceClient := &stubStripeCommerceClient{
			reconcileSession: stripeCheckoutSessionWebhookData{
				ID:            "cs_meta",
				Status:        "open",
				PaymentStatus: "unpaid",
				Metadata: map[string]string{
					stripeMetadataUserEmailKey: "meta@example.com",
				},
				CreatedAt: time.Date(2026, time.February, 24, 11, 0, 0, 0, time.UTC).Unix(),
			},
		}
		provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
		_, email, err := provider.BuildCheckoutReconcileEvent(context.Background(), "cs_meta")
		require.NoError(t, err)
		require.Equal(t, "meta@example.com", email)
	})

	t.Run("from CustomerEmail", func(t *testing.T) {
		commerceClient := &stubStripeCommerceClient{
			reconcileSession: stripeCheckoutSessionWebhookData{
				ID:            "cs_custmail",
				Status:        "open",
				PaymentStatus: "unpaid",
				CustomerEmail: "custmail@example.com",
				CreatedAt:     time.Date(2026, time.February, 24, 11, 0, 0, 0, time.UTC).Unix(),
			},
		}
		provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
		_, email, err := provider.BuildCheckoutReconcileEvent(context.Background(), "cs_custmail")
		require.NoError(t, err)
		require.Equal(t, "custmail@example.com", email)
	})

	t.Run("from CustomerDetails", func(t *testing.T) {
		commerceClient := &stubStripeCommerceClient{
			reconcileSession: stripeCheckoutSessionWebhookData{
				ID:              "cs_custdetails",
				Status:          "open",
				PaymentStatus:   "unpaid",
				CustomerDetails: stripeCustomerData{Email: "details@example.com"},
				CreatedAt:       time.Date(2026, time.February, 24, 11, 0, 0, 0, time.UTC).Unix(),
			},
		}
		provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
		_, email, err := provider.BuildCheckoutReconcileEvent(context.Background(), "cs_custdetails")
		require.NoError(t, err)
		require.Equal(t, "details@example.com", email)
	})

	t.Run("from customer ID lookup", func(t *testing.T) {
		commerceClient := &stubStripeCommerceClient{
			resolvedCustomerEmail: "resolved@example.com",
			reconcileSession: stripeCheckoutSessionWebhookData{
				ID:            "cs_custid",
				Status:        "open",
				PaymentStatus: "unpaid",
				CustomerID:    "cus_lookup_1",
				CreatedAt:     time.Date(2026, time.February, 24, 11, 0, 0, 0, time.UTC).Unix(),
			},
		}
		provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
		_, email, err := provider.BuildCheckoutReconcileEvent(context.Background(), "cs_custid")
		require.NoError(t, err)
		require.Equal(t, "resolved@example.com", email)
	})

	t.Run("no email and no customer ID returns error", func(t *testing.T) {
		commerceClient := &stubStripeCommerceClient{
			reconcileSession: stripeCheckoutSessionWebhookData{
				ID:            "cs_noemail",
				Status:        "open",
				PaymentStatus: "unpaid",
				CreatedAt:     time.Date(2026, time.February, 24, 11, 0, 0, 0, time.UTC).Unix(),
			},
		}
		provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
		_, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "cs_noemail")
		require.Error(t, err)
	})
}

func TestNewStripeProviderRejectsInvalidURLs(t *testing.T) {
	t.Run("empty success URL", func(t *testing.T) {
		settings := testStripeProviderSettings()
		settings.CheckoutSuccessURL = ""
		_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
		require.ErrorIs(t, err, ErrStripeProviderURLInvalid)
	})

	t.Run("no host in URL", func(t *testing.T) {
		settings := testStripeProviderSettings()
		settings.CheckoutSuccessURL = "notaurl"
		_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
		require.ErrorIs(t, err, ErrStripeProviderURLInvalid)
	})

	t.Run("non-http scheme", func(t *testing.T) {
		settings := testStripeProviderSettings()
		settings.CheckoutCancelURL = "ftp://example.com/cancel"
		_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
		require.ErrorIs(t, err, ErrStripeProviderURLInvalid)
	})

	t.Run("empty portal return URL", func(t *testing.T) {
		settings := testStripeProviderSettings()
		settings.PortalReturnURL = ""
		_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
		require.ErrorIs(t, err, ErrStripeProviderURLInvalid)
	})
}

func TestStripeProviderCreateSubscriptionCheckoutErrors(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		provider := &StripeProvider{}
		_, err := provider.CreateSubscriptionCheckout(context.Background(), "user@example.com", PlanCodePro)
		require.ErrorIs(t, err, ErrStripeProviderClientUnavailable)
	})

	t.Run("empty email", func(t *testing.T) {
		provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, &stubStripeCommerceClient{})
		_, err := provider.CreateSubscriptionCheckout(context.Background(), "  ", PlanCodePro)
		require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
	})

	t.Run("unsupported plan", func(t *testing.T) {
		provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, &stubStripeCommerceClient{})
		_, err := provider.CreateSubscriptionCheckout(context.Background(), "user@example.com", "enterprise")
		require.ErrorIs(t, err, ErrBillingPlanUnsupported)
	})
}

func TestStripeProviderCreateTopUpCheckoutErrors(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		provider := &StripeProvider{}
		_, err := provider.CreateTopUpCheckout(context.Background(), "user@example.com", PackCodeTopUp)
		require.ErrorIs(t, err, ErrStripeProviderClientUnavailable)
	})

	t.Run("empty email", func(t *testing.T) {
		provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, &stubStripeCommerceClient{})
		_, err := provider.CreateTopUpCheckout(context.Background(), "  ", PackCodeTopUp)
		require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
	})

	t.Run("unknown pack", func(t *testing.T) {
		provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, &stubStripeCommerceClient{})
		_, err := provider.CreateTopUpCheckout(context.Background(), "user@example.com", "unknown")
		require.ErrorIs(t, err, ErrBillingTopUpPackUnknown)
	})
}

func TestStripeProviderSubscriptionPlansNilProvider(t *testing.T) {
	var provider *StripeProvider
	plans := provider.SubscriptionPlans()
	require.Empty(t, plans)
}

func TestStripeProviderTopUpPacksNilProvider(t *testing.T) {
	var provider *StripeProvider
	packs := provider.TopUpPacks()
	require.Empty(t, packs)
}

func TestStripeProviderBuildUserSyncEventsNilClient(t *testing.T) {
	provider := &StripeProvider{}
	_, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrStripeProviderClientUnavailable)
}

func TestStripeProviderBuildUserSyncEventsEmptyEmail(t *testing.T) {
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, &stubStripeCommerceClient{})
	_, err := provider.BuildUserSyncEvents(context.Background(), "  ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestStripeProviderBuildCheckoutReconcileEventEmptyTransactionID(t *testing.T) {
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, &stubStripeCommerceClient{})
	_, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "  ")
	require.ErrorIs(t, err, ErrStripeAPICheckoutSessionNotFound)
}

func TestStripeProviderBuildCheckoutReconcileEventNilClient(t *testing.T) {
	provider := &StripeProvider{}
	_, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "cs_test")
	require.ErrorIs(t, err, ErrStripeProviderClientUnavailable)
}

func TestStripeProviderValidateCatalogNilClient(t *testing.T) {
	provider := &StripeProvider{}
	err := provider.ValidateCatalog(context.Background())
	require.ErrorIs(t, err, ErrStripeProviderClientUnavailable)
}

func TestStripeProviderBuildUserSyncEventsCustomerNotFoundOnList(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		foundCustomerID:      "cus_exists",
		listSubscriptionsErr: ErrStripeAPICustomerNotFound,
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	require.Len(t, syncEvents, 1)
	require.Contains(t, syncEvents[0].EventID, "sync:subscription:none:")
}

func TestStripeProviderBuildUserSyncEventsEmptySubscriptions(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "cus_empty_subs",
		subscriptions:   []stripeSubscriptionWebhookData{},
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	require.Len(t, syncEvents, 1)
	require.Contains(t, syncEvents[0].EventID, "sync:subscription:none:")
}

func TestNewStripeProviderRejectsNilVerifier(t *testing.T) {
	_, err := NewStripeProvider(testStripeProviderSettings(), nil, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderVerifierUnavailable)
}

func TestNewStripeProviderRejectsEmptyAPIKey(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.APIKey = ""
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderAPIKeyEmpty)
}

func TestNewStripeProviderRejectsEmptyClientToken(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.ClientToken = ""
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderClientTokenEmpty)
}

func TestNewStripeProviderRejectsInvalidPlanCredits(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.SubscriptionMonthlyCredits["pro"] = -1
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderPlanCreditsInvalid)
}

func TestNewStripeProviderRejectsInvalidPlanPrice(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.SubscriptionMonthlyPrices["pro"] = -1
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderPlanPriceInvalid)
}

func TestNewStripeProviderRejectsMissingPlanCredits(t *testing.T) {
	settings := testStripeProviderSettings()
	delete(settings.SubscriptionMonthlyCredits, "pro")
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderPlanCreditsMissing)
}

func TestNewStripeProviderRejectsMissingPlanPrice(t *testing.T) {
	settings := testStripeProviderSettings()
	delete(settings.SubscriptionMonthlyPrices, "pro")
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderPlanPriceMissing)
}

func TestNewStripeProviderRejectsEmptyPriceID(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.ProMonthlyPriceID = ""
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderPriceIDEmpty)
}

func TestNewStripeProviderRejectsInvalidPackCredits(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.TopUpPackCredits[PackCodeTopUp] = -1
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderPackCreditsInvalid)
}

func TestNewStripeProviderRejectsInvalidPackPrice(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.TopUpPackPrices[PackCodeTopUp] = -1
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderPackPriceInvalid)
}

func TestNewStripeProviderRejectsMissingPackCredits(t *testing.T) {
	settings := testStripeProviderSettings()
	delete(settings.TopUpPackCredits, PackCodeTopUp)
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderPackCreditsMissing)
}

func TestNewStripeProviderRejectsMissingPackPrice(t *testing.T) {
	settings := testStripeProviderSettings()
	delete(settings.TopUpPackPrices, PackCodeTopUp)
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderPackPriceMissing)
}

func TestNewStripeProviderRejectsPackPriceIDMissing(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.TopUpPackCredits["extra_pack"] = 100
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderPackPriceIDMissing)
}

func TestNewStripeProviderRejectsEmptyPackPriceID(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.TopUpPackPriceIDs[PackCodeTopUp] = ""
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.ErrorIs(t, err, ErrStripeProviderPriceIDEmpty)
}

func TestStripeProviderCode(t *testing.T) {
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.Equal(t, ProviderCodeStripe, provider.Code())
}

func TestStripeProviderBuildUserSyncEventsListSubscriptionsError(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		foundCustomerID:      "cus_err",
		listSubscriptionsErr: errors.New("network error"),
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.stripe.sync.subscription.list")
}

func TestStripeProviderBuildUserSyncEventsFindCustomerError(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		findCustomerIDErr: errors.New("find error"),
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.stripe.sync.customer.find")
}

func TestStripeProviderBuildUserSyncEventsSubscriptionWithEmptyID(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "cus_nosubid",
		subscriptions: []stripeSubscriptionWebhookData{
			{
				ID:        "",
				Status:    stripeSubscriptionStatusActive,
				CreatedAt: time.Date(2026, time.February, 24, 9, 0, 0, 0, time.UTC).Unix(),
			},
		},
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	// Empty ID subscription should be skipped, resulting in inactive sync event
	require.Len(t, syncEvents, 1)
	require.Contains(t, syncEvents[0].EventID, "sync:subscription:none:")
}

func TestStripeProviderBuildCheckoutReconcileEventGetSessionError(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{}
	// GetCheckoutSession returns zero-value session with empty ID, which triggers error
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "cs_nonexistent")
	require.Error(t, err)
}

func TestStripeProviderValidateCatalogRejectsPriceNotFound(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		priceByID: map[string]stripePriceResponse{},
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	err := provider.ValidateCatalog(context.Background())
	require.Error(t, err)
}

func TestStripeProviderValidateCatalogRejectsOneOffPlanPrice(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		priceByID: map[string]stripePriceResponse{
			"price_pro": {
				ID:         "price_pro",
				Type:       "one_time",
				UnitAmount: 2700,
			},
			"price_plus": {
				ID:         "price_plus",
				Type:       "recurring",
				UnitAmount: 17000,
				Recurring:  &stripePriceRecurring{Interval: "month"},
			},
			"price_pack_top_up": {
				ID:         "price_pack_top_up",
				Type:       "one_time",
				UnitAmount: 1000,
			},
		},
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	err := provider.ValidateCatalog(context.Background())
	require.ErrorIs(t, err, ErrStripeProviderPriceRecurringInvalid)
}

func TestStripeProviderValidateCatalogRejectsNonOneOffPackPrice(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		priceByID: map[string]stripePriceResponse{
			"price_pro": {
				ID:         "price_pro",
				Type:       "recurring",
				UnitAmount: 2700,
				Recurring:  &stripePriceRecurring{Interval: "month"},
			},
			"price_plus": {
				ID:         "price_plus",
				Type:       "recurring",
				UnitAmount: 17000,
				Recurring:  &stripePriceRecurring{Interval: "month"},
			},
			"price_pack_top_up": {
				ID:         "price_pack_top_up",
				Type:       "recurring",
				UnitAmount: 1000,
				Recurring:  &stripePriceRecurring{Interval: "month"},
			},
		},
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	err := provider.ValidateCatalog(context.Background())
	require.ErrorIs(t, err, ErrStripeProviderPriceOneOffInvalid)
}

func TestStripeProviderCreateSubscriptionCheckoutClientError(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		resolvedCustomerID: "cus_test",
		createdCheckoutID:  "",
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	session, err := provider.CreateSubscriptionCheckout(context.Background(), "user@example.com", PlanCodePro)
	require.NoError(t, err)
	require.Equal(t, "", session.TransactionID)
}

func TestStripeProviderCreateTopUpCheckoutClientError(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		resolvedCustomerID: "cus_test",
		createdCheckoutID:  "",
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	session, err := provider.CreateTopUpCheckout(context.Background(), "user@example.com", PackCodeTopUp)
	require.NoError(t, err)
	require.Equal(t, "", session.TransactionID)
}

func TestStripeProviderResolveCheckoutOccurredAtZeroTimestamp(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		reconcileSession: stripeCheckoutSessionWebhookData{
			ID:            "cs_zero_ts",
			Status:        stripeCheckoutStatusComplete,
			PaymentStatus: stripeCheckoutPaymentStatusPaid,
			Metadata: map[string]string{
				stripeMetadataUserEmailKey: "user@example.com",
			},
			CreatedAt: 0,
		},
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	event, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "cs_zero_ts")
	require.NoError(t, err)
	require.False(t, event.OccurredAt.IsZero())
}

func TestStripeProviderIsRecurringMonthlyPriceNilRecurring(t *testing.T) {
	require.False(t, isStripeRecurringMonthlyPrice(stripePriceResponse{
		Type: "recurring",
	}))
}

func TestStripeProviderIsRecurringMonthlyPriceYearlyInterval(t *testing.T) {
	require.False(t, isStripeRecurringMonthlyPrice(stripePriceResponse{
		Type:      "recurring",
		Recurring: &stripePriceRecurring{Interval: "year"},
	}))
}

type errorStripeCommerceClient struct {
	stubStripeCommerceClient
	resolveCustomerIDErr       error
	createCheckoutSessionErr   error
	createCustomerPortalURLErr error
	getCheckoutSessionErr      error
}

func (client *errorStripeCommerceClient) ResolveCustomerID(_ context.Context, _ string) (string, error) {
	if client.resolveCustomerIDErr != nil {
		return "", client.resolveCustomerIDErr
	}
	return client.stubStripeCommerceClient.resolvedCustomerID, nil
}

func (client *errorStripeCommerceClient) CreateCheckoutSession(_ context.Context, _ stripeCheckoutSessionInput) (string, error) {
	if client.createCheckoutSessionErr != nil {
		return "", client.createCheckoutSessionErr
	}
	return client.stubStripeCommerceClient.createdCheckoutID, nil
}

func (client *errorStripeCommerceClient) CreateCustomerPortalURL(_ context.Context, _ stripePortalSessionInput) (string, error) {
	if client.createCustomerPortalURLErr != nil {
		return "", client.createCustomerPortalURLErr
	}
	return client.stubStripeCommerceClient.createdPortalURL, nil
}

func (client *errorStripeCommerceClient) GetCheckoutSession(_ context.Context, _ string) (stripeCheckoutSessionWebhookData, error) {
	if client.getCheckoutSessionErr != nil {
		return stripeCheckoutSessionWebhookData{}, client.getCheckoutSessionErr
	}
	return client.stubStripeCommerceClient.reconcileSession, nil
}

func TestStripeProviderCreateSubscriptionCheckoutResolveError(t *testing.T) {
	commerceClient := &errorStripeCommerceClient{
		resolveCustomerIDErr: errors.New("resolve error"),
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, err := provider.CreateSubscriptionCheckout(context.Background(), "user@example.com", PlanCodePro)
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.stripe.customer.resolve")
}

func TestStripeProviderCreateSubscriptionCheckoutCheckoutError(t *testing.T) {
	commerceClient := &errorStripeCommerceClient{
		stubStripeCommerceClient: stubStripeCommerceClient{resolvedCustomerID: "cus_test"},
		createCheckoutSessionErr: errors.New("checkout error"),
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, err := provider.CreateSubscriptionCheckout(context.Background(), "user@example.com", PlanCodePro)
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.stripe.checkout.subscription")
}

func TestStripeProviderCreateTopUpCheckoutResolveError(t *testing.T) {
	commerceClient := &errorStripeCommerceClient{
		resolveCustomerIDErr: errors.New("resolve error"),
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, err := provider.CreateTopUpCheckout(context.Background(), "user@example.com", PackCodeTopUp)
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.stripe.customer.resolve")
}

func TestStripeProviderCreateTopUpCheckoutCheckoutError(t *testing.T) {
	commerceClient := &errorStripeCommerceClient{
		stubStripeCommerceClient: stubStripeCommerceClient{resolvedCustomerID: "cus_test"},
		createCheckoutSessionErr: errors.New("checkout error"),
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, err := provider.CreateTopUpCheckout(context.Background(), "user@example.com", PackCodeTopUp)
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.stripe.checkout.credits")
}

func TestStripeProviderCreateCustomerPortalSessionResolveError(t *testing.T) {
	commerceClient := &errorStripeCommerceClient{
		resolveCustomerIDErr: errors.New("resolve error"),
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, err := provider.CreateCustomerPortalSession(context.Background(), "user@example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.stripe.customer.resolve")
}

func TestStripeProviderCreateCustomerPortalSessionPortalError(t *testing.T) {
	commerceClient := &errorStripeCommerceClient{
		stubStripeCommerceClient:   stubStripeCommerceClient{resolvedCustomerID: "cus_test"},
		createCustomerPortalURLErr: errors.New("portal error"),
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, err := provider.CreateCustomerPortalSession(context.Background(), "user@example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.stripe.portal.create")
}

func TestStripeProviderBuildCheckoutReconcileEventSessionError(t *testing.T) {
	commerceClient := &errorStripeCommerceClient{
		getCheckoutSessionErr: errors.New("session error"),
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "cs_test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.stripe.checkout.session.get")
}

func TestStripeProviderBuildUserSyncEventsWithZeroTimestampSubscription(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "cus_zero_ts",
		subscriptions: []stripeSubscriptionWebhookData{
			{
				ID:        "sub_zero_ts",
				Status:    stripeSubscriptionStatusActive,
				CreatedAt: 0,
			},
		},
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrStripeWebhookPayloadInvalid)
}

func TestStripeProviderBuildCheckoutReconcileEventEmailFromCustomerIDError(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		reconcileSession: stripeCheckoutSessionWebhookData{
			ID:            "cs_cust_err",
			Status:        "open",
			PaymentStatus: "unpaid",
			CustomerID:    "cus_err",
			CreatedAt:     time.Date(2026, time.February, 24, 11, 0, 0, 0, time.UTC).Unix(),
		},
		resolvedCustomerEmail: "",
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "cs_cust_err")
	require.Error(t, err)
}

func TestStripeProviderNewWebhookGrantResolver(t *testing.T) {
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, &stubStripeCommerceClient{})
	resolver, err := provider.NewWebhookGrantResolver()
	require.NoError(t, err)
	require.NotNil(t, resolver)
}

func TestNewStripeProviderSkipsEmptyPlanCreditKey(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.SubscriptionMonthlyCredits["  "] = 500
	provider, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestNewStripeProviderSkipsEmptyPlanPriceKey(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.SubscriptionMonthlyPrices["  "] = 500
	provider, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestNewStripeProviderSkipsEmptyPackCreditKey(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.TopUpPackCredits["  "] = 500
	provider, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestNewStripeProviderSkipsEmptyPackPriceKey(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.TopUpPackPrices["  "] = 500
	provider, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestNewStripeProviderSkipsEmptyPackPriceIDKey(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.TopUpPackPriceIDs["  "] = "price_empty_key"
	provider, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestStripeProviderBuildUserSyncEventsInactiveSortWithMultipleSubs(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "cus_multi",
		subscriptions: []stripeSubscriptionWebhookData{
			{
				ID:        "sub_paused",
				Status:    stripeSubscriptionStatusPaused,
				CreatedAt: time.Date(2026, time.February, 24, 9, 0, 0, 0, time.UTC).Unix(),
			},
			{
				ID:        "sub_active_a",
				Status:    stripeSubscriptionStatusActive,
				CreatedAt: time.Date(2026, time.February, 24, 8, 0, 0, 0, time.UTC).Unix(),
			},
			{
				ID:        "sub_unpaid",
				Status:    stripeSubscriptionStatusUnpaid,
				CreatedAt: time.Date(2026, time.February, 24, 9, 0, 0, 0, time.UTC).Unix(),
			},
		},
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	require.Len(t, syncEvents, 3)
	// Inactive subs should come first, then active last
	require.Contains(t, syncEvents[2].EventID, "sub_active_a")
}

func TestStripeProviderBuildUserSyncEventsTiesByEventID(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "cus_tie",
		subscriptions: []stripeSubscriptionWebhookData{
			{
				ID:        "sub_b",
				Status:    stripeSubscriptionStatusCanceled,
				CreatedAt: time.Date(2026, time.February, 24, 9, 0, 0, 0, time.UTC).Unix(),
			},
			{
				ID:        "sub_a",
				Status:    stripeSubscriptionStatusCanceled,
				CreatedAt: time.Date(2026, time.February, 24, 9, 0, 0, 0, time.UTC).Unix(),
			},
		},
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	require.Len(t, syncEvents, 2)
	require.Equal(t, "sync:subscription:sub_a:canceled", syncEvents[0].EventID)
	require.Equal(t, "sync:subscription:sub_b:canceled", syncEvents[1].EventID)
}

func TestNewStripeProviderNilClientFallsBackToAPIClient(t *testing.T) {
	settings := testStripeProviderSettings()
	provider, err := NewStripeProvider(settings, &stubStripeVerifier{}, nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestStripeProviderBuildUserSyncEventsEmptyEmail2(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "cus_test",
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	_, err := provider.BuildUserSyncEvents(context.Background(), "")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestStripeProviderValidateCatalogPackPriceNotFound(t *testing.T) {
	commerceClient := &stubStripeCommerceClient{
		priceByID: map[string]stripePriceResponse{
			"price_pro": {
				ID:         "price_pro",
				Type:       "recurring",
				UnitAmount: 2700,
				Recurring:  &stripePriceRecurring{Interval: "month"},
			},
			"price_plus": {
				ID:         "price_plus",
				Type:       "recurring",
				UnitAmount: 17000,
				Recurring:  &stripePriceRecurring{Interval: "month"},
			},
			// Missing price_pack_top_up
		},
	}
	provider, _ := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, commerceClient)
	err := provider.ValidateCatalog(context.Background())
	require.Error(t, err)
}

func testStripeProviderSettings() StripeProviderSettings {
	return StripeProviderSettings{
		Environment:        "sandbox",
		APIKey:             "sk_test_123",
		ClientToken:        "pk_test_123",
		CheckoutSuccessURL: "https://app.local/success",
		CheckoutCancelURL:  "https://app.local/cancel",
		PortalReturnURL:    "https://app.local/settings/billing",
		ProMonthlyPriceID:  "price_pro",
		PlusMonthlyPriceID: "price_plus",
		SubscriptionMonthlyCredits: map[string]int64{
			"pro":  1000,
			"plus": 10000,
		},
		SubscriptionMonthlyPrices: map[string]int64{
			"pro":  2700,
			"plus": 17000,
		},
		TopUpPackPriceIDs: map[string]string{
			PackCodeTopUp: "price_pack_top_up",
		},
		TopUpPackCredits: map[string]int64{
			PackCodeTopUp: 2400,
		},
		TopUpPackPrices: map[string]int64{
			PackCodeTopUp: 1000,
		},
	}
}

// Coverage gap tests for stripe_provider.go

func TestNewStripeProviderNilClient(t *testing.T) {
	settings := testStripeProviderSettings()
	provider, err := NewStripeProvider(settings, &stubStripeVerifier{}, nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestBuildStripeInactiveSyncEventSuccess(t *testing.T) {
	now := time.Now().UTC()
	event, err := buildStripeInactiveSyncEvent("user@example.com", now)
	require.NoError(t, err)
	require.Equal(t, ProviderCodeStripe, event.ProviderCode)
	require.Contains(t, event.EventID, "sync:subscription:none:")
}

func TestBuildStripeSyncSubscriptionEventsEmptyIDs(t *testing.T) {
	now := time.Now().UTC()
	subscriptions := []stripeSubscriptionWebhookData{
		{ID: "", Status: "active", CreatedAt: now.Unix()},
	}
	events, err := buildStripeSyncSubscriptionEvents("user@example.com", subscriptions, now)
	require.NoError(t, err)
	// Empty ID subscription is skipped, falls back to inactive event
	require.Len(t, events, 1)
	require.Contains(t, events[0].EventID, "sync:subscription:none:")
}

func TestBuildStripeSyncSubscriptionEventsZeroCreatedAt(t *testing.T) {
	now := time.Now().UTC()
	subscriptions := []stripeSubscriptionWebhookData{
		{ID: "sub_123", Status: "active", CreatedAt: 0},
	}
	_, err := buildStripeSyncSubscriptionEvents("user@example.com", subscriptions, now)
	require.ErrorIs(t, err, ErrStripeWebhookPayloadInvalid)
}

func TestStripeProviderBuildUserSyncEventsListSubscriptionsCustomerNotFound(t *testing.T) {
	settings := testStripeProviderSettings()
	client := &stubStripeCommerceClient{
		foundCustomerID:      "cus_123",
		listSubscriptionsErr: ErrStripeAPICustomerNotFound,
	}
	provider, providerErr := NewStripeProvider(settings, &stubStripeVerifier{}, client)
	require.NoError(t, providerErr)

	events, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Contains(t, events[0].EventID, "sync:subscription:none:")
}

func TestStripeBuildCheckoutReconcileEventEmpty(t *testing.T) {
	settings := testStripeProviderSettings()
	client := &stubStripeCommerceClient{}
	provider, providerErr := NewStripeProvider(settings, &stubStripeVerifier{}, client)
	require.NoError(t, providerErr)

	_, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), " ")
	require.ErrorIs(t, err, ErrStripeAPICheckoutSessionNotFound)
}

func TestStripeResolveCheckoutUserEmailFallbackToCustomerID(t *testing.T) {
	settings := testStripeProviderSettings()
	client := &stubStripeCommerceClient{
		resolvedCustomerEmail: "resolved@example.com",
		reconcileSession: stripeCheckoutSessionWebhookData{
			ID:            "cs_123",
			Status:        "complete",
			PaymentStatus: "paid",
			CustomerID:    "cus_123",
		},
	}
	provider, providerErr := NewStripeProvider(settings, &stubStripeVerifier{}, client)
	require.NoError(t, providerErr)

	email, err := provider.resolveCheckoutUserEmail(context.Background(), stripeCheckoutSessionWebhookData{
		ID:         "cs_123",
		CustomerID: "cus_123",
	})
	require.NoError(t, err)
	require.Equal(t, "resolved@example.com", email)
}

func TestStripeResolveCheckoutUserEmailEmpty(t *testing.T) {
	settings := testStripeProviderSettings()
	client := &stubStripeCommerceClient{}
	provider, providerErr := NewStripeProvider(settings, &stubStripeVerifier{}, client)
	require.NoError(t, providerErr)

	_, err := provider.resolveCheckoutUserEmail(context.Background(), stripeCheckoutSessionWebhookData{
		ID:         "cs_123",
		CustomerID: "",
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestStripeResolveCheckoutUserEmailResolvedEmpty(t *testing.T) {
	settings := testStripeProviderSettings()
	client := &stubStripeCommerceClient{
		resolvedCustomerEmail: "",
	}
	provider, providerErr := NewStripeProvider(settings, &stubStripeVerifier{}, client)
	require.NoError(t, providerErr)

	_, err := provider.resolveCheckoutUserEmail(context.Background(), stripeCheckoutSessionWebhookData{
		ID:         "cs_123",
		CustomerID: "cus_123",
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestStripeProviderBuildUserSyncEventsEmptyCustomerID(t *testing.T) {
	settings := testStripeProviderSettings()
	client := &stubStripeCommerceClient{
		foundCustomerID: "",
	}
	provider, providerErr := NewStripeProvider(settings, &stubStripeVerifier{}, client)
	require.NoError(t, providerErr)

	events, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Contains(t, events[0].EventID, "sync:subscription:none:")
}

func TestStripeProviderBuildUserSyncEventsListError(t *testing.T) {
	settings := testStripeProviderSettings()
	client := &stubStripeCommerceClient{
		foundCustomerID:      "cus_123",
		listSubscriptionsErr: errors.New("api error"),
	}
	provider, providerErr := NewStripeProvider(settings, &stubStripeVerifier{}, client)
	require.NoError(t, providerErr)

	_, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, err)
}

func TestBuildStripeSyncSubscriptionEventsActiveSortedLast(t *testing.T) {
	now := time.Now().UTC()
	subscriptions := []stripeSubscriptionWebhookData{
		{ID: "sub_active", Status: "active", CreatedAt: now.Unix()},
		{ID: "sub_canceled", Status: "canceled", CreatedAt: now.Add(-time.Hour).Unix()},
	}
	events, err := buildStripeSyncSubscriptionEvents("user@example.com", subscriptions, now)
	require.NoError(t, err)
	require.Len(t, events, 2)
	// Inactive first, then active
	require.Contains(t, events[0].EventID, "sub_canceled")
	require.Contains(t, events[1].EventID, "sub_active")
}

func TestNewStripeProviderCustomPackLabel(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.TopUpPackPriceIDs["custom_pack"] = "price_custom"
	settings.TopUpPackCredits["custom_pack"] = 500
	settings.TopUpPackPrices["custom_pack"] = 800
	provider, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.NoError(t, err)
	packs := provider.TopUpPacks()
	found := false
	for _, pack := range packs {
		if pack.Code == "custom_pack" {
			found = true
			require.Equal(t, "Custom Pack", pack.Label)
		}
	}
	require.True(t, found)
}

func TestNewStripeProviderPackCreditsMissingForDefinedPriceID(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.TopUpPackCredits["orphan_pack"] = 500
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeProviderPackPriceIDMissing)
}

func TestNewStripeProviderNilClientCreatesDefault(t *testing.T) {
	settings := testStripeProviderSettings()
	provider, err := NewStripeProvider(settings, &stubStripeVerifier{}, nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestBuildStripeSyncSubscriptionEventsSortEqualTimestamps(t *testing.T) {
	now := time.Now().UTC()
	subscriptions := []stripeSubscriptionWebhookData{
		{ID: "sub_b", Status: "canceled", CreatedAt: now.Unix()},
		{ID: "sub_a", Status: "canceled", CreatedAt: now.Unix()},
	}
	events, err := buildStripeSyncSubscriptionEvents("user@example.com", subscriptions, now)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Contains(t, events[0].EventID, "sub_a")
	require.Contains(t, events[1].EventID, "sub_b")
}

func TestStripeResolveCheckoutUserEmailFromCustomerDetails(t *testing.T) {
	settings := testStripeProviderSettings()
	client := &stubStripeCommerceClient{}
	provider, providerErr := NewStripeProvider(settings, &stubStripeVerifier{}, client)
	require.NoError(t, providerErr)

	email, err := provider.resolveCheckoutUserEmail(context.Background(), stripeCheckoutSessionWebhookData{
		ID:              "cs_123",
		CustomerDetails: stripeCustomerData{Email: "details@example.com"},
	})
	require.NoError(t, err)
	require.Equal(t, "details@example.com", email)
}

func TestStripeResolveCheckoutUserEmailResolveError(t *testing.T) {
	settings := testStripeProviderSettings()
	client := &stubStripeCommerceClientWithResolveError{
		resolveErr: errors.New("api error"),
	}
	provider, providerErr := NewStripeProvider(settings, &stubStripeVerifier{}, client)
	require.NoError(t, providerErr)

	_, err := provider.resolveCheckoutUserEmail(context.Background(), stripeCheckoutSessionWebhookData{
		ID:         "cs_123",
		CustomerID: "cus_123",
	})
	require.Error(t, err)
}

type stubStripeCommerceClientWithResolveError struct {
	stubStripeCommerceClient
	resolveErr error
}

func (c *stubStripeCommerceClientWithResolveError) ResolveCustomerEmail(_ context.Context, _ string) (string, error) {
	return "", c.resolveErr
}

func TestNewStripeProviderPackPriceMissing(t *testing.T) {
	settings := testStripeProviderSettings()
	settings.TopUpPackPriceIDs["extra_pack"] = "price_extra"
	settings.TopUpPackCredits["extra_pack"] = 500
	// Deliberately don't add to TopUpPackPrices
	_, err := NewStripeProvider(settings, &stubStripeVerifier{}, &stubStripeCommerceClient{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeProviderPackPriceMissing)
}
