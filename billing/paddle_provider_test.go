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

type stubPaddleVerifier struct {
	err                  error
	receivedHeader       string
	receivedPayloadBytes []byte
}

func (verifier *stubPaddleVerifier) Verify(signatureHeader string, payload []byte) error {
	verifier.receivedHeader = signatureHeader
	verifier.receivedPayloadBytes = payload
	return verifier.err
}

type stubPaddleCommerceClient struct {
	resolvedCustomerID        string
	resolveCustomerErr        error
	receivedResolveEmail      string
	receivedFindCustomerEmail string
	resolvedCustomerEmail     string
	resolveCustomerEmailErr   error
	receivedResolveCustomerID string
	transactionID             string
	createTransactionErr      error
	receivedTransaction       paddleTransactionInput
	portalURL                 string
	createPortalErr           error
	receivedPortalCustomer    string
	transaction               paddleTransactionCompletedWebhookData
	getTransactionErr         error
	receivedTransactionID     string
	subscription              paddleSubscriptionWebhookData
	getSubscriptionErr        error
	receivedSubscriptionID    string
	priceByID                 map[string]paddlePriceDetails
	getPriceErr               error
	receivedPriceIDs          []string
	listPricesErr             error
	receivedListPriceIDs      []string
	listTransactions          []paddleTransactionCompletedWebhookData
	listTransactionsErr       error
	receivedListTxnCustomerID string
	listSubscriptions         []paddleSubscriptionWebhookData
	listSubscriptionsErr      error
	receivedListSubCustomerID string
}

func (client *stubPaddleCommerceClient) ResolveCustomerID(_ context.Context, email string) (string, error) {
	client.receivedResolveEmail = email
	if client.resolveCustomerErr != nil {
		return "", client.resolveCustomerErr
	}
	return client.resolvedCustomerID, nil
}

func (client *stubPaddleCommerceClient) FindCustomerIDByEmail(
	_ context.Context,
	email string,
) (string, error) {
	client.receivedFindCustomerEmail = email
	if client.resolveCustomerErr != nil {
		return "", client.resolveCustomerErr
	}
	return client.resolvedCustomerID, nil
}

func (client *stubPaddleCommerceClient) ResolveCustomerEmail(_ context.Context, customerID string) (string, error) {
	client.receivedResolveCustomerID = customerID
	if client.resolveCustomerEmailErr != nil {
		return "", client.resolveCustomerEmailErr
	}
	return client.resolvedCustomerEmail, nil
}

func (client *stubPaddleCommerceClient) CreateTransaction(_ context.Context, input paddleTransactionInput) (string, error) {
	client.receivedTransaction = input
	if client.createTransactionErr != nil {
		return "", client.createTransactionErr
	}
	return client.transactionID, nil
}

func (client *stubPaddleCommerceClient) CreateCustomerPortalURL(_ context.Context, customerID string) (string, error) {
	client.receivedPortalCustomer = customerID
	if client.createPortalErr != nil {
		return "", client.createPortalErr
	}
	return client.portalURL, nil
}

func (client *stubPaddleCommerceClient) GetTransaction(
	_ context.Context,
	transactionID string,
) (paddleTransactionCompletedWebhookData, error) {
	client.receivedTransactionID = transactionID
	if client.getTransactionErr != nil {
		return paddleTransactionCompletedWebhookData{}, client.getTransactionErr
	}
	return client.transaction, nil
}

func (client *stubPaddleCommerceClient) ListCustomerTransactions(
	_ context.Context,
	customerID string,
) ([]paddleTransactionCompletedWebhookData, error) {
	client.receivedListTxnCustomerID = customerID
	if client.listTransactionsErr != nil {
		return nil, client.listTransactionsErr
	}
	return client.listTransactions, nil
}

func (client *stubPaddleCommerceClient) ListCustomerSubscriptions(
	_ context.Context,
	customerID string,
) ([]paddleSubscriptionWebhookData, error) {
	client.receivedListSubCustomerID = customerID
	if client.listSubscriptionsErr != nil {
		return nil, client.listSubscriptionsErr
	}
	return client.listSubscriptions, nil
}

func (client *stubPaddleCommerceClient) GetSubscription(
	_ context.Context,
	subscriptionID string,
) (paddleSubscriptionWebhookData, error) {
	client.receivedSubscriptionID = subscriptionID
	if client.getSubscriptionErr != nil {
		return paddleSubscriptionWebhookData{}, client.getSubscriptionErr
	}
	return client.subscription, nil
}

func (client *stubPaddleCommerceClient) GetPrice(
	_ context.Context,
	priceID string,
) (paddlePriceDetails, error) {
	client.receivedPriceIDs = append(client.receivedPriceIDs, priceID)
	if client.getPriceErr != nil {
		return paddlePriceDetails{}, client.getPriceErr
	}
	if client.priceByID == nil {
		if strings.Contains(priceID, "pack") {
			return paddlePriceDetails{
				ID: priceID,
			}, nil
		}
		return paddlePriceDetails{
			ID: priceID,
			BillingCycle: paddlePriceBillingCycle{
				Interval:  "month",
				Frequency: 1,
			},
		}, nil
	}
	priceDetails, hasPriceDetails := client.priceByID[priceID]
	if !hasPriceDetails {
		return paddlePriceDetails{}, ErrPaddleAPIPriceNotFound
	}
	return priceDetails, nil
}

func (client *stubPaddleCommerceClient) ListPrices(
	ctx context.Context,
	priceIDs []string,
) (map[string]paddlePriceDetails, error) {
	client.receivedListPriceIDs = append(client.receivedListPriceIDs, priceIDs...)
	if client.listPricesErr != nil {
		return nil, client.listPricesErr
	}
	resolvedPrices := make(map[string]paddlePriceDetails, len(priceIDs))
	for _, priceID := range priceIDs {
		priceDetails, getPriceErr := client.GetPrice(ctx, priceID)
		if getPriceErr != nil {
			return nil, getPriceErr
		}
		resolvedPrices[priceID] = priceDetails
	}
	return resolvedPrices, nil
}

func TestNewPaddleProviderRejectsNilVerifier(t *testing.T) {
	provider, err := NewPaddleProvider(PaddleProviderSettings{
		APIKey:             "api_key",
		ClientToken:        "client_token",
		ProMonthlyPriceID:  "pri_pro",
		PlusMonthlyPriceID: "pri_plus",
	}, nil, &stubPaddleCommerceClient{})

	require.Nil(t, provider)
	require.ErrorIs(t, err, ErrPaddleProviderVerifierUnavailable)
}

func TestNewPaddleProviderRejectsMissingAPIKey(t *testing.T) {
	provider, err := NewPaddleProvider(PaddleProviderSettings{
		ClientToken:        "client_token",
		ProMonthlyPriceID:  "pri_pro",
		PlusMonthlyPriceID: "pri_plus",
	}, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})

	require.Nil(t, provider)
	require.ErrorIs(t, err, ErrPaddleProviderAPIKeyEmpty)
}

func TestPaddleProviderParseWebhookEventAcceptsValidPayload(t *testing.T) {
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)

	eventMetadata, parseErr := provider.ParseWebhookEvent(
		[]byte(`{"event_id":"evt_123","event_type":"transaction.completed","occurred_at":"2026-02-19T17:00:00Z"}`),
	)
	require.NoError(t, parseErr)
	require.Equal(t, "evt_123", eventMetadata.EventID)
	require.Equal(t, "transaction.completed", eventMetadata.EventType)
	require.Equal(t, "2026-02-19T17:00:00Z", eventMetadata.OccurredAt.Format(time.RFC3339))
}

func TestPaddleProviderParseWebhookEventRejectsMissingFields(t *testing.T) {
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)

	_, parseErr := provider.ParseWebhookEvent([]byte(`{"event_type":"transaction.completed"}`))
	require.ErrorIs(t, parseErr, ErrPaddleWebhookPayloadInvalid)
}

func TestPaddleProviderParseWebhookEventRejectsMissingOccurredAt(t *testing.T) {
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)

	_, parseErr := provider.ParseWebhookEvent(
		[]byte(`{"event_id":"evt_123","event_type":"transaction.completed"}`),
	)
	require.ErrorIs(t, parseErr, ErrPaddleWebhookPayloadInvalid)
}

func TestPaddleProviderParseWebhookEventRejectsEmptyOccurredAt(t *testing.T) {
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)

	_, parseErr := provider.ParseWebhookEvent(
		[]byte(`{"event_id":"evt_123","event_type":"transaction.completed","occurred_at":"  "}`),
	)
	require.ErrorIs(t, parseErr, ErrPaddleWebhookPayloadInvalid)
}

func TestPaddleProviderVerifySignatureDelegatesToVerifier(t *testing.T) {
	verifier := &stubPaddleVerifier{}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), verifier, &stubPaddleCommerceClient{})
	require.NoError(t, err)

	verifyErr := provider.VerifySignature("ts=123;h1=abc", []byte(`{"event_id":"evt_123"}`))
	require.NoError(t, verifyErr)
	require.Equal(t, "ts=123;h1=abc", verifier.receivedHeader)
	require.Equal(t, []byte(`{"event_id":"evt_123"}`), verifier.receivedPayloadBytes)
}

func TestPaddleProviderSubscriptionCheckout(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		transactionID:      "txn_123",
	}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, err)

	session, checkoutErr := provider.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), PlanCodePro)
	require.NoError(t, checkoutErr)
	require.Equal(t, ProviderCodePaddle, session.ProviderCode)
	require.Equal(t, "txn_123", session.TransactionID)
	require.Equal(t, CheckoutModeOverlay, session.CheckoutMode)
	require.Equal(t, "user@example.com", client.receivedResolveEmail)
	require.Equal(t, "ctm_123", client.receivedTransaction.CustomerID)
	require.Equal(t, "pri_pro", client.receivedTransaction.PriceID)
	require.Equal(t, "subscription", client.receivedTransaction.Metadata[paddleMetadataPurchaseKindKey])
	require.Equal(t, "pro", client.receivedTransaction.Metadata[paddleMetadataPlanCodeKey])
}

func TestPaddleProviderSubscriptionPlansUseConfiguredCredits(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.SubscriptionMonthlyCredits = map[string]int64{
		"pro":  1200,
		"plus": 16000,
	}
	provider, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)

	plans := provider.SubscriptionPlans()
	require.Len(t, plans, 2)
	require.Equal(t, PlanCodePlus, plans[0].Code)
	require.EqualValues(t, 16000, plans[0].MonthlyCredits)
	require.Equal(t, "$170", plans[0].PriceDisplay)
	require.Equal(t, "monthly", plans[0].BillingPeriod)
	require.Equal(t, PlanCodePro, plans[1].Code)
	require.EqualValues(t, 1200, plans[1].MonthlyCredits)
	require.Equal(t, "$27", plans[1].PriceDisplay)
	require.Equal(t, "monthly", plans[1].BillingPeriod)

	packs := provider.TopUpPacks()
	require.Len(t, packs, 1)
	require.Equal(t, PackCodeTopUp, packs[0].Code)
	require.Equal(t, packLabelTopUp, packs[0].Label)
	require.Equal(t, "$10", packs[0].PriceDisplay)
	require.Equal(t, "one-time", packs[0].BillingPeriod)
}

func TestPaddleProviderSubscriptionCheckoutRejectsUnknownPlan(t *testing.T) {
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)

	_, checkoutErr := provider.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), "enterprise")
	require.ErrorIs(t, checkoutErr, ErrBillingPlanUnsupported)
}

func TestPaddleProviderTopUpCheckout(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		transactionID:      "txn_credits",
	}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, err)

	session, checkoutErr := provider.CreateTopUpCheckout(context.Background(), testCustomer("user@example.com"), PackCodeTopUp)
	require.NoError(t, checkoutErr)
	require.Equal(t, ProviderCodePaddle, session.ProviderCode)
	require.Equal(t, "txn_credits", session.TransactionID)
	require.Equal(t, "pri_pack_top_up", client.receivedTransaction.PriceID)
	require.Equal(t, "top_up_pack", client.receivedTransaction.Metadata[paddleMetadataPurchaseKindKey])
	require.Equal(t, PackCodeTopUp, client.receivedTransaction.Metadata[paddleMetadataPackCodeKey])
	require.Equal(t, "2400", client.receivedTransaction.Metadata[paddleMetadataPackCreditsKey])
}

func TestPaddleProviderTopUpCheckoutRejectsUnknownPack(t *testing.T) {
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)

	_, checkoutErr := provider.CreateTopUpCheckout(context.Background(), testCustomer("user@example.com"), "unknown")
	require.ErrorIs(t, checkoutErr, ErrBillingTopUpPackUnknown)
}

func TestPaddleProviderPortalSession(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		portalURL:          "https://portal.paddle.test/session",
	}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, err)

	portalSession, portalErr := provider.CreateCustomerPortalSession(context.Background(), "user@example.com")
	require.NoError(t, portalErr)
	require.Equal(t, ProviderCodePaddle, portalSession.ProviderCode)
	require.Equal(t, "https://portal.paddle.test/session", portalSession.URL)
	require.Equal(t, "ctm_123", client.receivedPortalCustomer)
}

func TestPaddleProviderVerifySignatureReturnsVerifierError(t *testing.T) {
	verifier := &stubPaddleVerifier{err: errors.New("verification failed")}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), verifier, &stubPaddleCommerceClient{})
	require.NoError(t, err)

	verifyErr := provider.VerifySignature("ts=123;h1=abc", []byte(`{"event_id":"evt_123"}`))
	require.EqualError(t, verifyErr, "verification failed")
}

func TestPaddleProviderBuildCheckoutReconcileEventUsesTransactionData(t *testing.T) {
	client := &stubPaddleCommerceClient{
		transaction: paddleTransactionCompletedWebhookData{
			ID:             "txn_checkout_123",
			Status:         paddleTransactionStatusCompleted,
			CustomerID:     "ctm_123",
			SubscriptionID: "sub_123",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
			},
			Details: paddleTransactionCompletedLineDetails{
				LineItems: []paddleTransactionCompletedLineItem{
					{
						PriceID: "pri_pro",
					},
				},
			},
		},
	}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, err)

	webhookEvent, checkoutUserEmail, reconcileErr := provider.BuildCheckoutReconcileEvent(
		context.Background(),
		"txn_checkout_123",
	)
	require.NoError(t, reconcileErr)
	require.Equal(t, "txn_checkout_123", client.receivedTransactionID)
	require.Equal(t, "user@example.com", checkoutUserEmail)
	require.Equal(t, "transaction.completed", webhookEvent.EventType)
	require.Equal(t, ProviderCodePaddle, webhookEvent.ProviderCode)
	require.Contains(t, webhookEvent.EventID, "reconcile:txn_checkout_123")
}

func TestNewPaddleProviderRejectsPackWithoutCreditsMapping(t *testing.T) {
	_, providerErr := NewPaddleProvider(PaddleProviderSettings{
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
			PackCodeTopUp: "pri_pack_top_up",
		},
	}, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})

	require.ErrorIs(t, providerErr, ErrPaddleProviderPackCreditsMissing)
}

func TestNewPaddleProviderRejectsMissingPlanCreditsMapping(t *testing.T) {
	_, providerErr := NewPaddleProvider(PaddleProviderSettings{
		Environment:        "sandbox",
		APIKey:             "pdl_sdbx_key",
		ClientToken:        "test_client_token",
		ProMonthlyPriceID:  "pri_pro",
		PlusMonthlyPriceID: "pri_plus",
	}, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})

	require.ErrorIs(t, providerErr, ErrPaddleProviderPlanCreditsMissing)
}

func TestNewPaddleProviderRejectsNonMonthlySubscriptionPrice(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{
			"pri_pro": {
				ID:         "pri_pro",
				PriceCents: 2700,
			},
			"pri_plus": {
				ID:         "pri_plus",
				PriceCents: 17000,
				BillingCycle: paddlePriceBillingCycle{
					Interval:  "month",
					Frequency: 1,
				},
			},
			"pri_pack_top_up": {
				ID:         "pri_pack_top_up",
				PriceCents: 1000,
			},
		},
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)
	require.ErrorIs(t, provider.ValidateCatalog(context.Background()), ErrPaddleProviderPriceRecurringInvalid)
}

func TestNewPaddleProviderRejectsRecurringPackPrice(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{
			"pri_pro": {
				ID:         "pri_pro",
				PriceCents: 2700,
				BillingCycle: paddlePriceBillingCycle{
					Interval:  "month",
					Frequency: 1,
				},
			},
			"pri_plus": {
				ID:         "pri_plus",
				PriceCents: 17000,
				BillingCycle: paddlePriceBillingCycle{
					Interval:  "month",
					Frequency: 1,
				},
			},
			"pri_pack_top_up": {
				ID:         "pri_pack_top_up",
				PriceCents: 1000,
				BillingCycle: paddlePriceBillingCycle{
					Interval:  "month",
					Frequency: 1,
				},
			},
		},
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)
	require.ErrorIs(t, provider.ValidateCatalog(context.Background()), ErrPaddleProviderPriceOneOffInvalid)
}

func TestPaddleProviderValidateCatalogLoadsPricesByID(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{
			"pri_pro": {
				ID:         "pri_pro",
				PriceCents: 2700,
				BillingCycle: paddlePriceBillingCycle{
					Interval:  "month",
					Frequency: 1,
				},
			},
			"pri_plus": {
				ID:         "pri_plus",
				PriceCents: 17000,
				BillingCycle: paddlePriceBillingCycle{
					Interval:  "month",
					Frequency: 1,
				},
			},
			"pri_pack_top_up": {
				ID:         "pri_pack_top_up",
				PriceCents: 1000,
			},
		},
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	require.NoError(t, provider.ValidateCatalog(context.Background()))
	require.ElementsMatch(t, []string{"pri_pro", "pri_plus", "pri_pack_top_up"}, client.receivedListPriceIDs)
	require.ElementsMatch(t, []string{"pri_pro", "pri_plus", "pri_pack_top_up"}, client.receivedPriceIDs)
}

func TestPaddleProviderValidateCatalogRejectsPlanPriceAmountDrift(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{
			"pri_pro": {
				ID:         "pri_pro",
				PriceCents: 2800,
				BillingCycle: paddlePriceBillingCycle{
					Interval:  "month",
					Frequency: 1,
				},
			},
			"pri_plus": {
				ID:         "pri_plus",
				PriceCents: 17000,
				BillingCycle: paddlePriceBillingCycle{
					Interval:  "month",
					Frequency: 1,
				},
			},
			"pri_pack_top_up": {
				ID:         "pri_pack_top_up",
				PriceCents: 1000,
			},
		},
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	validateErr := provider.ValidateCatalog(context.Background())
	require.Error(t, validateErr)
	require.ErrorIs(t, validateErr, ErrPaddleProviderPriceAmountMismatch)
}

func TestPaddleProviderValidateCatalogRejectsPackPriceAmountDrift(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{
			"pri_pro": {
				ID:         "pri_pro",
				PriceCents: 2700,
				BillingCycle: paddlePriceBillingCycle{
					Interval:  "month",
					Frequency: 1,
				},
			},
			"pri_plus": {
				ID:         "pri_plus",
				PriceCents: 17000,
				BillingCycle: paddlePriceBillingCycle{
					Interval:  "month",
					Frequency: 1,
				},
			},
			"pri_pack_top_up": {
				ID:         "pri_pack_top_up",
				PriceCents: 900,
			},
		},
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	validateErr := provider.ValidateCatalog(context.Background())
	require.Error(t, validateErr)
	require.ErrorIs(t, validateErr, ErrPaddleProviderPriceAmountMismatch)
}

func TestPaddleProviderBuildUserSyncEventsOrdersSubscriptionActivityWithActiveLast(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		listTransactions: []paddleTransactionCompletedWebhookData{
			{
				ID:       "txn_completed_1",
				Status:   paddleTransactionStatusCompleted,
				BilledAt: "2026-02-19T09:00:00Z",
			},
		},
		listSubscriptions: []paddleSubscriptionWebhookData{
			{
				ID:        "sub_canceled",
				Status:    paddleSubscriptionStatusCanceled,
				UpdatedAt: "2026-02-19T10:00:00Z",
				CustomData: map[string]interface{}{
					paddleMetadataUserEmailKey: "user@example.com",
				},
			},
			{
				ID:        "sub_active",
				Status:    paddleSubscriptionStatusActive,
				UpdatedAt: "2026-02-19T08:30:00Z",
				CustomData: map[string]interface{}{
					paddleMetadataUserEmailKey: "user@example.com",
				},
			},
		},
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	require.Equal(t, "user@example.com", client.receivedFindCustomerEmail)
	require.Len(t, syncEvents, 3)
	require.Equal(t, "sync:transaction:txn_completed_1", syncEvents[0].EventID)
	require.Equal(t, paddleEventTypeTransactionCompleted, syncEvents[0].EventType)
	require.Equal(t, "sync:subscription:sub_canceled:canceled", syncEvents[1].EventID)
	require.Equal(t, "sync:subscription:sub_active:active", syncEvents[2].EventID)
}

func TestPaddleProviderBuildUserSyncEventsEmitsInactiveEventWhenNoSubscriptions(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	require.Len(t, syncEvents, 1)
	require.Equal(t, paddleEventTypeSubscriptionUpdated, syncEvents[0].EventType)
	require.Contains(t, syncEvents[0].EventID, "sync:subscription:none:")
	require.False(t, syncEvents[0].OccurredAt.IsZero())
	payload := paddleSubscriptionWebhookPayload{}
	require.NoError(t, json.Unmarshal(syncEvents[0].Payload, &payload))
	require.Equal(t, paddleSubscriptionStatusInactive, payload.Data.Status)
	require.Equal(t, "user@example.com", webhookMetadataValue(payload.Data.CustomData, paddleMetadataUserEmailKey))
}

func TestPaddleProviderSignatureHeaderNameReturnsPaddleSignature(t *testing.T) {
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)
	require.Equal(t, "Paddle-Signature", provider.SignatureHeaderName())
}

func TestPaddleProviderPublicConfigReturnsProviderDetails(t *testing.T) {
	t.Run("returns correct config", func(t *testing.T) {
		provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
		require.NoError(t, err)
		config := provider.PublicConfig()
		require.Equal(t, ProviderCodePaddle, config.ProviderCode)
		require.Equal(t, "sandbox", config.Environment)
		require.Equal(t, "test_client_token", config.ClientToken)
	})

	t.Run("nil provider returns empty", func(t *testing.T) {
		var provider *PaddleProvider
		config := provider.PublicConfig()
		require.Equal(t, PublicConfig{}, config)
	})
}

func TestPaddleProviderEnvironmentReturnsConfiguredEnvironment(t *testing.T) {
	t.Run("returns value", func(t *testing.T) {
		provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
		require.NoError(t, err)
		require.Equal(t, "sandbox", provider.Environment())
	})

	t.Run("nil returns empty", func(t *testing.T) {
		var provider *PaddleProvider
		require.Equal(t, "", provider.Environment())
	})
}

func TestPaddleProviderClientTokenReturnsConfiguredToken(t *testing.T) {
	t.Run("returns value", func(t *testing.T) {
		provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
		require.NoError(t, err)
		require.Equal(t, "test_client_token", provider.ClientToken())
	})

	t.Run("nil returns empty", func(t *testing.T) {
		var provider *PaddleProvider
		require.Equal(t, "", provider.ClientToken())
	})
}

func TestToTitleCapitalizesUnderscoreDelimitedWords(t *testing.T) {
	require.Equal(t, "Top Up", toTitle("top_up"))
	require.Equal(t, "Bulk Top Up", toTitle("bulk_top_up"))
	require.Equal(t, "", toTitle(""))
}

func TestPaddleProviderBuildCheckoutReconcileEventPendingStatus(t *testing.T) {
	client := &stubPaddleCommerceClient{
		transaction: paddleTransactionCompletedWebhookData{
			ID:         "txn_pending_123",
			Status:     "ready",
			CustomerID: "ctm_123",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
			},
		},
	}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, err)

	webhookEvent, checkoutUserEmail, reconcileErr := provider.BuildCheckoutReconcileEvent(
		context.Background(),
		"txn_pending_123",
	)
	require.NoError(t, reconcileErr)
	require.Equal(t, "user@example.com", checkoutUserEmail)
	require.Equal(t, "transaction.updated", webhookEvent.EventType)
}

func TestPaddleProviderResolveCheckoutUserEmailFromCustomer(t *testing.T) {
	client := &stubPaddleCommerceClient{
		transaction: paddleTransactionCompletedWebhookData{
			ID:         "txn_cust_email",
			Status:     paddleTransactionStatusCompleted,
			CustomerID: "ctm_123",
			Customer: paddleTransactionCompletedCustomer{
				Email: "customer@example.com",
			},
		},
	}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, err)

	_, checkoutUserEmail, reconcileErr := provider.BuildCheckoutReconcileEvent(
		context.Background(),
		"txn_cust_email",
	)
	require.NoError(t, reconcileErr)
	require.Equal(t, "customer@example.com", checkoutUserEmail)
}

func TestPaddleProviderResolveCheckoutUserEmailFromCustomerID(t *testing.T) {
	client := &stubPaddleCommerceClient{
		transaction: paddleTransactionCompletedWebhookData{
			ID:         "txn_cust_id_lookup",
			Status:     paddleTransactionStatusCompleted,
			CustomerID: "ctm_lookup_1",
		},
		resolvedCustomerEmail: "looked-up@example.com",
	}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, err)

	_, checkoutUserEmail, reconcileErr := provider.BuildCheckoutReconcileEvent(
		context.Background(),
		"txn_cust_id_lookup",
	)
	require.NoError(t, reconcileErr)
	require.Equal(t, "looked-up@example.com", checkoutUserEmail)
	require.Equal(t, "ctm_lookup_1", client.receivedResolveCustomerID)
}

func TestPaddleProviderBuildUserTransactionSyncEventsFiltersNonGrantable(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		listTransactions: []paddleTransactionCompletedWebhookData{
			{
				ID:       "txn_draft",
				Status:   "draft",
				BilledAt: "2026-02-19T09:00:00Z",
			},
			{
				ID:       "txn_completed",
				Status:   paddleTransactionStatusCompleted,
				BilledAt: "2026-02-19T10:00:00Z",
			},
		},
	}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, err)

	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)

	transactionEventCount := 0
	for _, event := range syncEvents {
		if strings.HasPrefix(event.EventID, "sync:transaction:") {
			transactionEventCount++
			require.Equal(t, "sync:transaction:txn_completed", event.EventID)
		}
	}
	require.Equal(t, 1, transactionEventCount)
}

func TestNewPaddleProviderRejectsPackWithoutPriceIDMapping(t *testing.T) {
	_, providerErr := NewPaddleProvider(PaddleProviderSettings{
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
		TopUpPackCredits: map[string]int64{
			PackCodeTopUp: 2400,
		},
	}, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})

	require.ErrorIs(t, providerErr, ErrPaddleProviderPackPriceIDMissing)
}

func TestNewPaddleProviderRejectsPlanPriceMissing(t *testing.T) {
	_, providerErr := NewPaddleProvider(PaddleProviderSettings{
		Environment:        "sandbox",
		APIKey:             "pdl_sdbx_key",
		ClientToken:        "test_client_token",
		ProMonthlyPriceID:  "pri_pro",
		PlusMonthlyPriceID: "pri_plus",
		SubscriptionMonthlyCredits: map[string]int64{
			"pro":  1000,
			"plus": 10000,
		},
	}, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})

	require.ErrorIs(t, providerErr, ErrPaddleProviderPlanPriceMissing)
}

func TestParsePaddleTimestampWithRFC3339Nano(t *testing.T) {
	parsed, err := parsePaddleTimestamp("2026-02-19T17:00:00.123456789Z")
	require.NoError(t, err)
	require.False(t, parsed.IsZero())
	require.Equal(t, 2026, parsed.Year())
}

func TestPaddleProviderSubscriptionPlansNilProvider(t *testing.T) {
	var provider *PaddleProvider
	plans := provider.SubscriptionPlans()
	require.Empty(t, plans)
}

func TestPaddleProviderTopUpPacksNilProvider(t *testing.T) {
	var provider *PaddleProvider
	packs := provider.TopUpPacks()
	require.Empty(t, packs)
}

func TestPaddleProviderResolveCheckoutEventStatus(t *testing.T) {
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)

	require.Equal(t, CheckoutEventStatusSucceeded, provider.ResolveCheckoutEventStatus("transaction.completed"))
	require.Equal(t, CheckoutEventStatusSucceeded, provider.ResolveCheckoutEventStatus("transaction.paid"))
	require.Equal(t, CheckoutEventStatusPending, provider.ResolveCheckoutEventStatus("transaction.updated"))
	require.Equal(t, CheckoutEventStatusUnknown, provider.ResolveCheckoutEventStatus("unknown.event"))
}

func TestPaddleProviderBuildUserSyncEventsNilClient(t *testing.T) {
	provider := &PaddleProvider{}
	_, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)
}

func TestPaddleProviderBuildUserSyncEventsEmptyEmail(t *testing.T) {
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	_, err := provider.BuildUserSyncEvents(context.Background(), "  ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestPaddleProviderBuildUserSyncEventsNoCustomer(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "",
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	require.Empty(t, syncEvents)
}

func TestPaddleProviderBuildCheckoutReconcileEventNilClient(t *testing.T) {
	provider := &PaddleProvider{}
	_, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "txn_test")
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)
}

func TestPaddleProviderBuildCheckoutReconcileEventEmptyTransactionID(t *testing.T) {
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	_, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "  ")
	require.ErrorIs(t, err, ErrPaddleAPITransactionNotFound)
}

func TestPaddleProviderCreateSubscriptionCheckoutNilClient(t *testing.T) {
	provider := &PaddleProvider{}
	_, err := provider.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), PlanCodePro)
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)
}

func TestPaddleProviderCreateSubscriptionCheckoutEmptyEmail(t *testing.T) {
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	_, err := provider.CreateSubscriptionCheckout(context.Background(), testCustomer("  "), PlanCodePro)
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestPaddleProviderCreateTopUpCheckoutNilClient(t *testing.T) {
	provider := &PaddleProvider{}
	_, err := provider.CreateTopUpCheckout(context.Background(), testCustomer("user@example.com"), PackCodeTopUp)
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)
}

func TestPaddleProviderCreateTopUpCheckoutEmptyEmail(t *testing.T) {
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	_, err := provider.CreateTopUpCheckout(context.Background(), testCustomer("  "), PackCodeTopUp)
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestPaddleProviderCreateCustomerPortalSessionNilClient(t *testing.T) {
	provider := &PaddleProvider{}
	_, err := provider.CreateCustomerPortalSession(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)
}

func TestPaddleProviderCreateCustomerPortalSessionEmptyEmail(t *testing.T) {
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	_, err := provider.CreateCustomerPortalSession(context.Background(), "  ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestPaddleProviderValidateCatalogNilClient(t *testing.T) {
	provider := &PaddleProvider{}
	err := provider.ValidateCatalog(context.Background())
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)
}

func TestNewPaddleProviderRejectsEmptyClientToken(t *testing.T) {
	_, providerErr := NewPaddleProvider(PaddleProviderSettings{
		APIKey:             "pdl_sdbx_key",
		ProMonthlyPriceID:  "pri_pro",
		PlusMonthlyPriceID: "pri_plus",
	}, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.ErrorIs(t, providerErr, ErrPaddleProviderClientTokenEmpty)
}

func TestPaddleProviderResolveCheckoutUserEmailMissingAllReturnsError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		transaction: paddleTransactionCompletedWebhookData{
			ID:     "txn_no_email",
			Status: paddleTransactionStatusCompleted,
		},
	}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, err)

	_, _, reconcileErr := provider.BuildCheckoutReconcileEvent(context.Background(), "txn_no_email")
	require.Error(t, reconcileErr)
}

func TestPaddleProviderNewSubscriptionStatusWebhookProcessor(t *testing.T) {
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)
	processor, processorErr := provider.NewSubscriptionStatusWebhookProcessor(&stubPaddleStateRepository{})
	require.NoError(t, processorErr)
	require.NotNil(t, processor)
}

type stubPaddleStateRepository struct{}

func (r *stubPaddleStateRepository) Upsert(_ context.Context, _ SubscriptionStateUpsertInput) error {
	return nil
}

func (r *stubPaddleStateRepository) Get(_ context.Context, _ string, _ string) (SubscriptionState, bool, error) {
	return SubscriptionState{}, false, nil
}

func (r *stubPaddleStateRepository) GetBySubscriptionID(_ context.Context, _ string, _ string) (SubscriptionState, bool, error) {
	return SubscriptionState{}, false, nil
}

func TestPaddleProviderNewWebhookGrantResolver(t *testing.T) {
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)
	resolver, resolverErr := provider.NewWebhookGrantResolver()
	require.NoError(t, resolverErr)
	require.NotNil(t, resolver)
}

func TestNewPaddleProviderRejectsInvalidPlanCredits(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.SubscriptionMonthlyCredits["pro"] = -1
	_, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.ErrorIs(t, err, ErrPaddleProviderPlanCreditsInvalid)
}

func TestNewPaddleProviderRejectsInvalidPlanPrice(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.SubscriptionMonthlyPrices["pro"] = -1
	_, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.ErrorIs(t, err, ErrPaddleProviderPlanPriceInvalid)
}

func TestNewPaddleProviderRejectsInvalidPackCredits(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.TopUpPackCredits[PackCodeTopUp] = -1
	_, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.ErrorIs(t, err, ErrPaddleProviderPackCreditsInvalid)
}

func TestNewPaddleProviderRejectsInvalidPackPrice(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.TopUpPackPrices[PackCodeTopUp] = -1
	_, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.ErrorIs(t, err, ErrPaddleProviderPackPriceInvalid)
}

func TestNewPaddleProviderRejectsMissingPackPrice(t *testing.T) {
	settings := testPaddleProviderSettings()
	delete(settings.TopUpPackPrices, PackCodeTopUp)
	_, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.ErrorIs(t, err, ErrPaddleProviderPackPriceMissing)
}

func TestNewPaddleProviderRejectsEmptyPackPriceID(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.TopUpPackPriceIDs[PackCodeTopUp] = ""
	_, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.ErrorIs(t, err, ErrPaddleProviderPriceIDEmpty)
}

func TestNewPaddleProviderRejectsEmptyPriceID(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.ProMonthlyPriceID = ""
	_, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.ErrorIs(t, err, ErrPaddleProviderPriceIDEmpty)
}

func TestPaddleProviderCode(t *testing.T) {
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.Equal(t, ProviderCodePaddle, provider.Code())
}

func TestPaddleProviderVerifySignatureNilProvider(t *testing.T) {
	var provider *PaddleProvider
	require.ErrorIs(t, provider.VerifySignature("sig", []byte(`{}`)), ErrPaddleProviderVerifierUnavailable)
}

func TestPaddleProviderParseWebhookEventInvalidJSON(t *testing.T) {
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	_, err := provider.ParseWebhookEvent([]byte(`not-json`))
	require.ErrorIs(t, err, ErrPaddleWebhookPayloadInvalid)
}

func TestPaddleProviderBuildUserSyncEventsCustomerFindError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolveCustomerErr: errors.New("find error"),
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, err)
}

func TestPaddleProviderBuildUserSyncEventsTransactionsListError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID:  "ctm_123",
		listTransactionsErr: errors.New("txn list error"),
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, err)
}

func TestPaddleProviderBuildUserSyncEventsSubscriptionsListError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID:   "ctm_123",
		listSubscriptionsErr: errors.New("sub list error"),
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, err)
}

func TestPaddleProviderCreateSubscriptionCheckoutResolveError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolveCustomerErr: errors.New("resolve error"),
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, err := provider.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), PlanCodePro)
	require.Error(t, err)
}

func TestPaddleProviderCreateTopUpCheckoutResolveError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolveCustomerErr: errors.New("resolve error"),
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, err := provider.CreateTopUpCheckout(context.Background(), testCustomer("user@example.com"), PackCodeTopUp)
	require.Error(t, err)
}

func TestPaddleProviderCreateCustomerPortalSessionResolveError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolveCustomerErr: errors.New("resolve error"),
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, err := provider.CreateCustomerPortalSession(context.Background(), "user@example.com")
	require.Error(t, err)
}

func TestPaddleProviderCreateSubscriptionCheckoutTransactionError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID:   "ctm_123",
		createTransactionErr: errors.New("txn error"),
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, err := provider.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), PlanCodePro)
	require.Error(t, err)
}

func TestPaddleProviderCreateTopUpCheckoutTransactionError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID:   "ctm_123",
		createTransactionErr: errors.New("txn error"),
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, err := provider.CreateTopUpCheckout(context.Background(), testCustomer("user@example.com"), PackCodeTopUp)
	require.Error(t, err)
}

func TestPaddleProviderCreateCustomerPortalSessionPortalError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		createPortalErr:    errors.New("portal error"),
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, err := provider.CreateCustomerPortalSession(context.Background(), "user@example.com")
	require.Error(t, err)
}

func TestPaddleProviderBuildCheckoutReconcileEventTransactionError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		getTransactionErr: errors.New("get txn error"),
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "txn_test")
	require.Error(t, err)
}

func TestPaddleProviderBuildCheckoutReconcileEventEmailResolveError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		transaction: paddleTransactionCompletedWebhookData{
			ID:         "txn_no_email_resolve",
			Status:     paddleTransactionStatusCompleted,
			CustomerID: "ctm_resolve_err",
		},
		resolveCustomerEmailErr: errors.New("email resolve error"),
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "txn_no_email_resolve")
	require.Error(t, err)
}

func TestPaddleProviderBuildUserTransactionSyncEventsWithEmptyTransactionID(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		listTransactions: []paddleTransactionCompletedWebhookData{
			{
				ID:       "",
				Status:   paddleTransactionStatusCompleted,
				BilledAt: "2026-02-19T09:00:00Z",
			},
		},
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	transactionEvents := 0
	for _, e := range syncEvents {
		if strings.HasPrefix(e.EventID, "sync:transaction:") {
			transactionEvents++
		}
	}
	require.Equal(t, 0, transactionEvents)
}

func TestPaddleProviderBuildUserSubscriptionSyncEventsWithEmptySubID(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		listSubscriptions: []paddleSubscriptionWebhookData{
			{
				ID:        "",
				Status:    paddleSubscriptionStatusActive,
				UpdatedAt: "2026-02-19T10:00:00Z",
				CustomData: map[string]interface{}{
					paddleMetadataUserEmailKey: "user@example.com",
				},
			},
		},
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	// Empty ID subscription is skipped, results in empty subscription events
	require.Empty(t, syncEvents)
}

func TestParsePaddleTimestampEmptyString(t *testing.T) {
	parsed, err := parsePaddleTimestamp("")
	require.NoError(t, err)
	require.True(t, parsed.IsZero())
}

func TestParsePaddleTimestampInvalidFormat(t *testing.T) {
	_, err := parsePaddleTimestamp("not-a-timestamp")
	require.Error(t, err)
}

func TestPaddleProviderValidateCatalogListPricesError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		listPricesErr: errors.New("list prices error"),
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	err := provider.ValidateCatalog(context.Background())
	require.Error(t, err)
}

func TestPaddleProviderBuildCheckoutReconcileEventPaidStatus(t *testing.T) {
	client := &stubPaddleCommerceClient{
		transaction: paddleTransactionCompletedWebhookData{
			ID:         "txn_paid_123",
			Status:     paddleTransactionStatusPaid,
			CustomerID: "ctm_123",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
			},
			BilledAt: "2026-02-19T09:00:00Z",
		},
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	event, email, err := provider.BuildCheckoutReconcileEvent(context.Background(), "txn_paid_123")
	require.NoError(t, err)
	require.Equal(t, "user@example.com", email)
	require.Equal(t, "transaction.completed", event.EventType)
}

func TestNewPaddleProviderSkipsEmptyPlanCreditKey(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.SubscriptionMonthlyCredits["  "] = 500
	provider, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestNewPaddleProviderSkipsEmptyPlanPriceKey(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.SubscriptionMonthlyPrices["  "] = 500
	provider, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestNewPaddleProviderSkipsEmptyPackCreditKey(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.TopUpPackCredits["  "] = 500
	provider, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestNewPaddleProviderSkipsEmptyPackPriceKey(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.TopUpPackPrices["  "] = 500
	provider, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestNewPaddleProviderSkipsEmptyPackPriceIDKey(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.TopUpPackPriceIDs["  "] = "pri_empty_key"
	provider, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestNewPaddleProviderNilClientFallsBackToAPIClient(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.Environment = "sandbox"
	// Passing nil client should create API client; environment is sandbox so it should succeed
	provider, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestPaddleProviderBuildUserTransactionSyncEventsZeroTimestamp(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		listTransactions: []paddleTransactionCompletedWebhookData{
			{
				ID:     "txn_no_ts",
				Status: paddleTransactionStatusCompleted,
				// No timestamp fields set
			},
		},
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrPaddleWebhookPayloadInvalid)
}

func TestPaddleProviderBuildUserSubscriptionSyncEventsZeroTimestamp(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		listSubscriptions: []paddleSubscriptionWebhookData{
			{
				ID:     "sub_no_ts",
				Status: paddleSubscriptionStatusActive,
				// No timestamp fields set
			},
		},
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	_, err := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrPaddleWebhookPayloadInvalid)
}

func TestPaddleProviderBuildUserTransactionSyncEventsSortsTiesByEventID(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		listTransactions: []paddleTransactionCompletedWebhookData{
			{
				ID:       "txn_b",
				Status:   paddleTransactionStatusCompleted,
				BilledAt: "2026-02-19T09:00:00Z",
			},
			{
				ID:       "txn_a",
				Status:   paddleTransactionStatusCompleted,
				BilledAt: "2026-02-19T09:00:00Z",
			},
		},
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	txnEvents := []WebhookEvent{}
	for _, e := range syncEvents {
		if strings.HasPrefix(e.EventID, "sync:transaction:") {
			txnEvents = append(txnEvents, e)
		}
	}
	require.Len(t, txnEvents, 2)
	require.Equal(t, "sync:transaction:txn_a", txnEvents[0].EventID)
	require.Equal(t, "sync:transaction:txn_b", txnEvents[1].EventID)
}

func TestPaddleProviderBuildUserSubscriptionSyncEventsSortsTiesByEventID(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		listSubscriptions: []paddleSubscriptionWebhookData{
			{
				ID:        "sub_b",
				Status:    paddleSubscriptionStatusCanceled,
				UpdatedAt: "2026-02-19T10:00:00Z",
				CustomData: map[string]interface{}{
					paddleMetadataUserEmailKey: "user@example.com",
				},
			},
			{
				ID:        "sub_a",
				Status:    paddleSubscriptionStatusCanceled,
				UpdatedAt: "2026-02-19T10:00:00Z",
				CustomData: map[string]interface{}{
					paddleMetadataUserEmailKey: "user@example.com",
				},
			},
		},
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	require.Len(t, syncEvents, 2)
	require.Equal(t, "sync:subscription:sub_a:canceled", syncEvents[0].EventID)
	require.Equal(t, "sync:subscription:sub_b:canceled", syncEvents[1].EventID)
}

func TestPaddleProviderBuildCheckoutReconcileEventWithTransactionStatus(t *testing.T) {
	client := &stubPaddleCommerceClient{
		transaction: paddleTransactionCompletedWebhookData{
			ID:         "txn_with_status",
			Status:     paddleTransactionStatusCompleted,
			CustomerID: "ctm_123",
			BilledAt:   "2026-02-19T09:00:00Z",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
			},
		},
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	event, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "txn_with_status")
	require.NoError(t, err)
	require.Contains(t, event.EventID, ":completed")
}

func TestResolvePaddleTransactionOccurredAtFallbacks(t *testing.T) {
	// Test BilledAt is preferred
	txn := paddleTransactionCompletedWebhookData{
		BilledAt:    "2026-02-19T09:00:00Z",
		CompletedAt: "2026-02-19T10:00:00Z",
	}
	ts := resolvePaddleTransactionOccurredAt(txn)
	require.Equal(t, 9, ts.Hour())

	// Test CompletedAt fallback when BilledAt is invalid
	txn2 := paddleTransactionCompletedWebhookData{
		BilledAt:    "invalid-date",
		CompletedAt: "2026-02-19T10:00:00Z",
	}
	ts2 := resolvePaddleTransactionOccurredAt(txn2)
	require.Equal(t, 10, ts2.Hour())

	// Test UpdatedAt fallback when BilledAt and CompletedAt are invalid
	txn3 := paddleTransactionCompletedWebhookData{
		BilledAt:    "invalid",
		CompletedAt: "invalid",
		UpdatedAt:   "2026-02-19T11:00:00Z",
	}
	ts3 := resolvePaddleTransactionOccurredAt(txn3)
	require.Equal(t, 11, ts3.Hour())

	// Test CreatedAt fallback when all others invalid
	txn4 := paddleTransactionCompletedWebhookData{
		BilledAt:    "invalid",
		CompletedAt: "invalid",
		UpdatedAt:   "invalid",
		CreatedAt:   "2026-02-19T12:00:00Z",
	}
	ts4 := resolvePaddleTransactionOccurredAt(txn4)
	require.Equal(t, 12, ts4.Hour())

	// All invalid returns zero
	txn5 := paddleTransactionCompletedWebhookData{
		BilledAt:    "invalid",
		CompletedAt: "invalid",
		UpdatedAt:   "invalid",
		CreatedAt:   "invalid",
	}
	ts5 := resolvePaddleTransactionOccurredAt(txn5)
	require.True(t, ts5.IsZero())
}

func TestResolvePaddleSubscriptionOccurredAtFallbacks(t *testing.T) {
	// Test UpdatedAt is preferred
	sub := paddleSubscriptionWebhookData{
		UpdatedAt: "2026-02-19T09:00:00Z",
		CurrentBillingPeriod: paddleSubscriptionBillingPeriod{
			StartsAt: "2026-02-19T10:00:00Z",
		},
	}
	ts := resolvePaddleSubscriptionOccurredAt(sub)
	require.Equal(t, 9, ts.Hour())

	// Test CurrentBillingPeriod.StartsAt fallback
	sub2 := paddleSubscriptionWebhookData{
		UpdatedAt: "invalid",
		CurrentBillingPeriod: paddleSubscriptionBillingPeriod{
			StartsAt: "2026-02-19T10:00:00Z",
		},
	}
	ts2 := resolvePaddleSubscriptionOccurredAt(sub2)
	require.Equal(t, 10, ts2.Hour())

	// Test EndsAt fallback
	sub3 := paddleSubscriptionWebhookData{
		UpdatedAt: "invalid",
		CurrentBillingPeriod: paddleSubscriptionBillingPeriod{
			StartsAt: "invalid",
			EndsAt:   "2026-02-19T11:00:00Z",
		},
	}
	ts3 := resolvePaddleSubscriptionOccurredAt(sub3)
	require.Equal(t, 11, ts3.Hour())

	// Test NextBilledAt fallback
	sub4 := paddleSubscriptionWebhookData{
		UpdatedAt: "invalid",
		CurrentBillingPeriod: paddleSubscriptionBillingPeriod{
			StartsAt: "invalid",
			EndsAt:   "invalid",
		},
		NextBilledAt: "2026-02-19T12:00:00Z",
	}
	ts4 := resolvePaddleSubscriptionOccurredAt(sub4)
	require.Equal(t, 12, ts4.Hour())
}

func TestParseRequiredPaddleTimestampRejectsEmpty(t *testing.T) {
	_, err := parseRequiredPaddleTimestamp("")
	require.ErrorIs(t, err, ErrPaddleWebhookPayloadInvalid)
}

func TestParseRequiredPaddleTimestampRejectsInvalid(t *testing.T) {
	_, err := parseRequiredPaddleTimestamp("not-a-timestamp")
	require.Error(t, err)
}

func TestResolvePaddleCatalogPriceDetailsFromMapEmptyPriceID(t *testing.T) {
	_, err := resolvePaddleCatalogPriceDetailsFromMap(map[string]paddlePriceDetails{}, "  ")
	require.ErrorIs(t, err, ErrPaddleAPIPriceNotFound)
}

func TestResolvePaddleCatalogPriceDetailsFromMapMissingPrice(t *testing.T) {
	_, err := resolvePaddleCatalogPriceDetailsFromMap(map[string]paddlePriceDetails{
		"pri_other": {ID: "pri_other"},
	}, "pri_missing")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIPriceNotFound)
}

func TestValidatePaddleCatalogPricingNilClient(t *testing.T) {
	err := validatePaddleCatalogPricing(nil, context.Background(), nil, nil)
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)
}

func TestPaddleProviderBuildUserTransactionSyncEventsMultipleSorted(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_123",
		listTransactions: []paddleTransactionCompletedWebhookData{
			{
				ID:       "txn_later",
				Status:   paddleTransactionStatusCompleted,
				BilledAt: "2026-02-19T12:00:00Z",
			},
			{
				ID:       "txn_earlier",
				Status:   paddleTransactionStatusCompleted,
				BilledAt: "2026-02-19T09:00:00Z",
			},
		},
	}
	provider, _ := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	syncEvents, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.NoError(t, syncErr)
	txnEvents := []WebhookEvent{}
	for _, e := range syncEvents {
		if strings.HasPrefix(e.EventID, "sync:transaction:") {
			txnEvents = append(txnEvents, e)
		}
	}
	require.Len(t, txnEvents, 2)
	require.Equal(t, "sync:transaction:txn_earlier", txnEvents[0].EventID)
	require.Equal(t, "sync:transaction:txn_later", txnEvents[1].EventID)
}

func testPaddleProviderSettings() PaddleProviderSettings {
	return PaddleProviderSettings{
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
			PackCodeTopUp: "pri_pack_top_up",
		},
		TopUpPackCredits: map[string]int64{
			PackCodeTopUp: 2400,
		},
		TopUpPackPrices: map[string]int64{
			PackCodeTopUp: 1000,
		},
	}
}

// Coverage gap tests for paddle_provider.go

func TestNewPaddleProviderNilClient(t *testing.T) {
	settings := testPaddleProviderSettings()
	provider, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestParsePaddleTimestampRFC3339Nano(t *testing.T) {
	ts, err := parsePaddleTimestamp("2026-03-15T10:30:00.123456789Z")
	require.NoError(t, err)
	require.False(t, ts.IsZero())
}

func TestParsePaddleTimestampInvalid(t *testing.T) {
	_, err := parsePaddleTimestamp("not-a-timestamp")
	require.Error(t, err)
}

func TestParsePaddleTimestampEmpty(t *testing.T) {
	ts, err := parsePaddleTimestamp("")
	require.NoError(t, err)
	require.True(t, ts.IsZero())
}

func TestToTitleMultipleWords(t *testing.T) {
	require.Equal(t, "Hello World", toTitle("hello_world"))
	require.Equal(t, "My Custom Pack", toTitle("my_custom_pack"))
}

func TestToTitleSingleWord(t *testing.T) {
	require.Equal(t, "Test", toTitle("test"))
}

func TestToTitleEmptyString(t *testing.T) {
	require.Equal(t, "", toTitle(""))
}

func TestValidatePaddleCatalogPricingNilClientDirect(t *testing.T) {
	err := validatePaddleCatalogPricing(nil, context.Background(), nil, nil)
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)
}

func TestValidatePaddleCatalogPricingPlanNotRecurring(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{
			"pri_pro": {
				ID:           "pri_pro",
				PriceCents:   2700,
				BillingCycle: paddlePriceBillingCycle{Interval: "", Frequency: 0},
			},
		},
	}
	client.receivedListPriceIDs = nil
	planDefs := map[string]paddlePlanDefinition{
		"pro": {PriceID: "pri_pro", PriceCents: 2700, Plan: SubscriptionPlan{Code: "pro"}},
	}
	err := validatePaddleCatalogPricing(client, context.Background(), planDefs, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleProviderPriceRecurringInvalid)
}

func TestValidatePaddleCatalogPricingPlanAmountMismatch(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{
			"pri_pro": {
				ID:           "pri_pro",
				PriceCents:   9999,
				BillingCycle: paddlePriceBillingCycle{Interval: "month", Frequency: 1},
			},
		},
	}
	planDefs := map[string]paddlePlanDefinition{
		"pro": {PriceID: "pri_pro", PriceCents: 2700, Plan: SubscriptionPlan{Code: "pro"}},
	}
	err := validatePaddleCatalogPricing(client, context.Background(), planDefs, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleProviderPriceAmountMismatch)
}

func TestValidatePaddleCatalogPricingPackNotOneOff(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{
			"pri_pack": {
				ID:           "pri_pack",
				PriceCents:   1000,
				BillingCycle: paddlePriceBillingCycle{Interval: "month", Frequency: 1},
			},
		},
	}
	packDefs := map[string]paddlePackDefinition{
		"top_up": {PriceID: "pri_pack", PriceCents: 1000, Pack: TopUpPack{Code: "top_up"}},
	}
	err := validatePaddleCatalogPricing(client, context.Background(), nil, packDefs)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleProviderPriceOneOffInvalid)
}

func TestValidatePaddleCatalogPricingPackAmountMismatch(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{
			"pri_pack": {
				ID:           "pri_pack",
				PriceCents:   9999,
				BillingCycle: paddlePriceBillingCycle{Interval: "", Frequency: 0},
			},
		},
	}
	packDefs := map[string]paddlePackDefinition{
		"top_up": {PriceID: "pri_pack", PriceCents: 1000, Pack: TopUpPack{Code: "top_up"}},
	}
	err := validatePaddleCatalogPricing(client, context.Background(), nil, packDefs)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleProviderPriceAmountMismatch)
}

func TestBuildUserTransactionSyncEventsZeroOccurredAt(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{
		listTransactions: []paddleTransactionCompletedWebhookData{
			{
				ID:     "txn_nodate",
				Status: "completed",
			},
		},
	})
	require.NoError(t, providerErr)
	_, err := provider.buildUserTransactionSyncEvents(context.Background(), "cus_123")
	require.ErrorIs(t, err, ErrPaddleWebhookPayloadInvalid)
}

func TestBuildUserSubscriptionSyncEventsZeroOccurredAt(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{
		listSubscriptions: []paddleSubscriptionWebhookData{
			{
				ID:     "sub_nodate",
				Status: "active",
			},
		},
	})
	require.NoError(t, providerErr)
	_, err := provider.buildUserSubscriptionSyncEvents(context.Background(), "cus_123", "user@example.com")
	require.ErrorIs(t, err, ErrPaddleWebhookPayloadInvalid)
}

func TestBuildCheckoutReconcileEventResolveUserEmailFallbackToCustomerAPI(t *testing.T) {
	client := &stubPaddleCommerceClient{
		transaction: paddleTransactionCompletedWebhookData{
			ID:         "txn_123",
			Status:     "completed",
			CustomerID: "cus_123",
			BilledAt:   "2026-03-15T10:30:00Z",
		},
		resolvedCustomerEmail: "resolved@example.com",
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	event, userEmail, err := provider.BuildCheckoutReconcileEvent(context.Background(), "txn_123")
	require.NoError(t, err)
	require.Equal(t, "resolved@example.com", userEmail)
	require.NotEmpty(t, event.EventID)
}

func TestResolveCheckoutUserEmailCustomerIDEmpty(t *testing.T) {
	client := &stubPaddleCommerceClient{}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	_, err := provider.resolveCheckoutUserEmail(context.Background(), paddleTransactionCompletedWebhookData{
		ID:         "txn_123",
		CustomerID: "",
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestResolveCheckoutUserEmailResolveError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolveCustomerEmailErr: errors.New("api error"),
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	_, err := provider.resolveCheckoutUserEmail(context.Background(), paddleTransactionCompletedWebhookData{
		ID:         "txn_123",
		CustomerID: "cus_123",
	})
	require.Error(t, err)
}

func TestResolveCheckoutUserEmailResolvedEmpty(t *testing.T) {
	client := &stubPaddleCommerceClient{
		resolvedCustomerEmail: "  ",
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	_, err := provider.resolveCheckoutUserEmail(context.Background(), paddleTransactionCompletedWebhookData{
		ID:         "txn_123",
		CustomerID: "cus_123",
	})
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestValidatePaddleCatalogPricingListPricesError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		listPricesErr: errors.New("api error"),
	}
	planDefs := map[string]paddlePlanDefinition{
		"pro": {PriceID: "pri_pro", PriceCents: 2700, Plan: SubscriptionPlan{Code: "pro"}},
	}
	err := validatePaddleCatalogPricing(client, context.Background(), planDefs, nil)
	require.Error(t, err)
}

func TestValidatePaddleCatalogPricingPlanPriceNotFound(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{},
	}
	planDefs := map[string]paddlePlanDefinition{
		"pro": {PriceID: "pri_missing", PriceCents: 2700, Plan: SubscriptionPlan{Code: "pro"}},
	}
	err := validatePaddleCatalogPricing(client, context.Background(), planDefs, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIPriceNotFound)
}

func TestValidatePaddleCatalogPricingPackPriceNotFound(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{},
	}
	packDefs := map[string]paddlePackDefinition{
		"top_up": {PriceID: "pri_missing", PriceCents: 1000, Pack: TopUpPack{Code: "top_up"}},
	}
	err := validatePaddleCatalogPricing(client, context.Background(), nil, packDefs)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIPriceNotFound)
}

func TestBuildUserSubscriptionSyncEventsEmptySubscriptions(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{
		listSubscriptions: []paddleSubscriptionWebhookData{},
	})
	require.NoError(t, providerErr)

	events, err := provider.buildUserSubscriptionSyncEvents(context.Background(), "cus_123", "user@example.com")
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Contains(t, events[0].EventID, "sync:subscription:none:")
}

func TestBuildUserSubscriptionSyncEventsSkipsEmptyID(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{
		listSubscriptions: []paddleSubscriptionWebhookData{
			{ID: "", Status: "active", UpdatedAt: "2026-03-15T10:30:00Z"},
			{ID: "sub_valid", Status: "active", UpdatedAt: "2026-03-15T10:30:00Z"},
		},
	})
	require.NoError(t, providerErr)

	events, err := provider.buildUserSubscriptionSyncEvents(context.Background(), "cus_123", "user@example.com")
	require.NoError(t, err)
	require.Len(t, events, 1)
}

func TestBuildUserTransactionSyncEventsSkipsNonGrantable(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{
		listTransactions: []paddleTransactionCompletedWebhookData{
			{
				ID:        "txn_pending",
				Status:    "pending",
				CreatedAt: "2026-03-15T10:30:00Z",
			},
		},
	})
	require.NoError(t, providerErr)

	events, err := provider.buildUserTransactionSyncEvents(context.Background(), "cus_123")
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestToTitleWithMixedSpacesAndUnderscores(t *testing.T) {
	require.Equal(t, "A B C", toTitle("a_b_c"))
	require.Equal(t, "Hello", toTitle("HELLO"))
	require.Equal(t, "X", toTitle("x"))
}

func TestValidatePaddleCatalogPricingDuplicatePriceIDs(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{
			"pri_shared": {
				ID:           "pri_shared",
				PriceCents:   2700,
				BillingCycle: paddlePriceBillingCycle{Interval: "month", Frequency: 1},
			},
		},
	}
	planDefs := map[string]paddlePlanDefinition{
		"pro":  {PriceID: "pri_shared", PriceCents: 2700, Plan: SubscriptionPlan{Code: "pro"}},
		"plus": {PriceID: "pri_shared", PriceCents: 2700, Plan: SubscriptionPlan{Code: "plus"}},
	}
	err := validatePaddleCatalogPricing(client, context.Background(), planDefs, nil)
	require.NoError(t, err)
}

func TestBuildUserSubscriptionSyncEventsSortOrder(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{
		listSubscriptions: []paddleSubscriptionWebhookData{
			{ID: "sub_active", Status: "active", UpdatedAt: "2026-03-15T10:30:00Z"},
			{ID: "sub_canceled", Status: "canceled", UpdatedAt: "2026-03-14T10:30:00Z"},
		},
	})
	require.NoError(t, providerErr)

	events, err := provider.buildUserSubscriptionSyncEvents(context.Background(), "cus_123", "user@example.com")
	require.NoError(t, err)
	require.Len(t, events, 2)
	// Inactive first, then active
	require.Contains(t, events[0].EventID, "sub_canceled")
	require.Contains(t, events[1].EventID, "sub_active")
}

func TestBuildCheckoutReconcileEventTransactionGetError(t *testing.T) {
	client := &stubPaddleCommerceClient{
		getTransactionErr: errors.New("api error"),
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	_, _, err := provider.BuildCheckoutReconcileEvent(context.Background(), "txn_err")
	require.Error(t, err)
}

func TestNewPaddleProviderCustomPackLabel(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.TopUpPackPriceIDs["custom_pack"] = "pri_custom"
	settings.TopUpPackCredits["custom_pack"] = 500
	settings.TopUpPackPrices["custom_pack"] = 800
	provider, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
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

func TestNewPaddleProviderPackCreditsMissingForDefinedPriceID(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.TopUpPackCredits["orphan_pack"] = 500
	_, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleProviderPackPriceIDMissing)
}

func TestNewPaddleProviderNilClientWithInvalidEnvironment(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.Environment = "invalid_env"
	_, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, nil)
	require.Error(t, err)
}

func TestParsePaddleTimestampRFC3339NanoFallbackError(t *testing.T) {
	// This string fails RFC3339 parse but also fails RFC3339Nano, so original error is returned
	_, err := parsePaddleTimestamp("2026-03-15 10:30:00")
	require.Error(t, err)
}

func TestBuildUserTransactionSyncEventsEmptyTransactionID(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{
		listTransactions: []paddleTransactionCompletedWebhookData{
			{ID: "", Status: "completed", BilledAt: "2026-03-15T10:30:00Z"},
		},
	})
	require.NoError(t, providerErr)
	events, err := provider.buildUserTransactionSyncEvents(context.Background(), "cus_123")
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestBuildUserSubscriptionSyncEventsListError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{
		listSubscriptionsErr: errors.New("api error"),
	})
	require.NoError(t, providerErr)
	_, err := provider.buildUserSubscriptionSyncEvents(context.Background(), "cus_123", "user@example.com")
	require.Error(t, err)
}

func TestBuildUserTransactionSyncEventsListError(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{
		listTransactionsErr: errors.New("api error"),
	})
	require.NoError(t, providerErr)
	_, err := provider.buildUserTransactionSyncEvents(context.Background(), "cus_123")
	require.Error(t, err)
}

func TestBuildUserSubscriptionSyncEventsSortEqualTimestamps(t *testing.T) {
	ts := "2026-03-15T10:30:00Z"
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{
		listSubscriptions: []paddleSubscriptionWebhookData{
			{ID: "sub_b", Status: "canceled", UpdatedAt: ts},
			{ID: "sub_a", Status: "canceled", UpdatedAt: ts},
		},
	})
	require.NoError(t, providerErr)
	events, err := provider.buildUserSubscriptionSyncEvents(context.Background(), "cus_123", "user@example.com")
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Contains(t, events[0].EventID, "sub_a")
	require.Contains(t, events[1].EventID, "sub_b")
}

func TestToTitleWithOnlyUnderscores(t *testing.T) {
	require.Equal(t, "___", toTitle("___"))
}

func TestValidatePaddleCatalogPricingDedupEmptyPriceID(t *testing.T) {
	client := &stubPaddleCommerceClient{
		priceByID: map[string]paddlePriceDetails{
			"pri_pro": {
				ID:           "pri_pro",
				PriceCents:   2700,
				BillingCycle: paddlePriceBillingCycle{Interval: "month", Frequency: 1},
			},
		},
	}
	planDefs := map[string]paddlePlanDefinition{
		"pro": {PriceID: "pri_pro", PriceCents: 2700, Plan: SubscriptionPlan{Code: "pro"}},
	}
	packDefs := map[string]paddlePackDefinition{
		"top_up": {PriceID: " ", PriceCents: 1000, Pack: TopUpPack{Code: "top_up"}},
	}
	err := validatePaddleCatalogPricing(client, context.Background(), planDefs, packDefs)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIPriceNotFound)
}

func TestNewPaddleProviderPackPriceMissing(t *testing.T) {
	settings := testPaddleProviderSettings()
	settings.TopUpPackPriceIDs["extra_pack"] = "pri_extra"
	settings.TopUpPackCredits["extra_pack"] = 500
	// Deliberately don't add to TopUpPackPrices
	_, err := NewPaddleProvider(settings, &stubPaddleVerifier{}, &stubPaddleCommerceClient{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleProviderPackPriceMissing)
}

// TestBuildUserSubscriptionSyncEventsSortDifferentTimestampsSameActivity exercises the
// leftOccurredAt.Before(rightOccurredAt) branch (line 560) in the subscription sort
// comparator. Two inactive subscriptions with different timestamps share the same isActive
// value, so the sort reaches the timestamp comparison and uses Before rather than the
// EventID tiebreaker.
func TestBuildUserSubscriptionSyncEventsSortDifferentTimestampsSameActivity(t *testing.T) {
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, &stubPaddleCommerceClient{
		listSubscriptions: []paddleSubscriptionWebhookData{
			{ID: "sub_later", Status: paddleSubscriptionStatusCanceled, UpdatedAt: "2026-03-15T12:00:00Z"},
			{ID: "sub_earlier", Status: paddleSubscriptionStatusCanceled, UpdatedAt: "2026-03-15T09:00:00Z"},
		},
	})
	require.NoError(t, providerErr)

	events, err := provider.buildUserSubscriptionSyncEvents(context.Background(), "cus_123", "user@example.com")
	require.NoError(t, err)
	require.Len(t, events, 2)
	// Earlier timestamp comes first within same-activity group.
	require.Contains(t, events[0].EventID, "sub_earlier")
	require.Contains(t, events[1].EventID, "sub_later")
}

// stubPaddleClientWithFixedPrices is a minimal paddleCommerceClient that returns a
// caller-supplied price map from ListPrices regardless of what is requested. Used to
// simulate ListPrices succeeding but not containing every requested price ID.
type stubPaddleClientWithFixedPrices struct {
	stubPaddleCommerceClient
	fixedPriceMap map[string]paddlePriceDetails
}

func (c *stubPaddleClientWithFixedPrices) ListPrices(
	_ context.Context,
	priceIDs []string,
) (map[string]paddlePriceDetails, error) {
	c.receivedListPriceIDs = append(c.receivedListPriceIDs, priceIDs...)
	if c.listPricesErr != nil {
		return nil, c.listPricesErr
	}
	return c.fixedPriceMap, nil
}

// TestValidatePaddleCatalogPricingPlanPriceIDMissingFromResult exercises the
// resolvePriceErr != nil branch at line 879 in validatePaddleCatalogPricing. ListPrices
// succeeds but omits the plan's price ID from the returned map, so
// resolvePaddleCatalogPriceDetailsFromMap returns ErrPaddleAPIPriceNotFound.
func TestValidatePaddleCatalogPricingPlanPriceIDMissingFromResult(t *testing.T) {
	client := &stubPaddleClientWithFixedPrices{
		// Return an empty map — ListPrices succeeds but contains no prices.
		fixedPriceMap: map[string]paddlePriceDetails{},
	}
	planDefs := map[string]paddlePlanDefinition{
		"pro": {PriceID: "pri_pro", PriceCents: 2700, Plan: SubscriptionPlan{Code: "pro"}},
	}
	err := validatePaddleCatalogPricing(client, context.Background(), planDefs, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIPriceNotFound)
}
