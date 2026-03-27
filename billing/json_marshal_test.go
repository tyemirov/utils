package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var errForcedMarshal = errors.New("forced marshal error")

func failingMarshal(_ interface{}) ([]byte, error) {
	return nil, errForcedMarshal
}

// --- Paddle: buildUserTransactionSyncEvents (json marshal in transaction loop) ---

func TestPaddleBuildUserTransactionSyncEventsMarshalError(t *testing.T) {
	original := jsonMarshalFunc
	defer func() { jsonMarshalFunc = original }()

	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_1",
		listTransactions: []paddleTransactionCompletedWebhookData{
			{
				ID:       "txn_1",
				Status:   paddleTransactionStatusCompleted,
				BilledAt: "2026-02-19T09:00:00Z",
			},
		},
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	jsonMarshalFunc = failingMarshal

	_, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, syncErr)
	require.ErrorIs(t, syncErr, errForcedMarshal)
}

// --- Paddle: buildUserSubscriptionSyncEvents empty subscriptions (inactive event) ---

func TestPaddleBuildUserSubscriptionSyncEventsInactiveMarshalError(t *testing.T) {
	original := jsonMarshalFunc
	defer func() { jsonMarshalFunc = original }()

	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_1",
		listSubscriptions:  []paddleSubscriptionWebhookData{},
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	jsonMarshalFunc = failingMarshal

	_, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, syncErr)
	require.ErrorIs(t, syncErr, errForcedMarshal)
}

// --- Paddle: buildUserSubscriptionSyncEvents subscription loop ---

func TestPaddleBuildUserSubscriptionSyncEventsSubscriptionMarshalError(t *testing.T) {
	original := jsonMarshalFunc
	defer func() { jsonMarshalFunc = original }()

	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "ctm_1",
		listSubscriptions: []paddleSubscriptionWebhookData{
			{
				ID:        "sub_1",
				Status:    paddleSubscriptionStatusActive,
				UpdatedAt: "2026-02-19T10:00:00Z",
			},
		},
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	jsonMarshalFunc = failingMarshal

	_, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, syncErr)
	require.ErrorIs(t, syncErr, errForcedMarshal)
}

// --- Paddle: BuildCheckoutReconcileEvent ---

func TestPaddleBuildCheckoutReconcileEventMarshalError(t *testing.T) {
	original := jsonMarshalFunc
	defer func() { jsonMarshalFunc = original }()

	client := &stubPaddleCommerceClient{
		transaction: paddleTransactionCompletedWebhookData{
			ID:         "txn_1",
			Status:     paddleTransactionStatusCompleted,
			CustomerID: "ctm_1",
			BilledAt:   "2026-02-19T09:00:00Z",
			CustomData: map[string]interface{}{
				paddleMetadataUserEmailKey: "user@example.com",
			},
		},
	}
	provider, providerErr := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, providerErr)

	jsonMarshalFunc = failingMarshal

	_, _, reconcileErr := provider.BuildCheckoutReconcileEvent(context.Background(), "txn_1")
	require.Error(t, reconcileErr)
	require.ErrorIs(t, reconcileErr, errForcedMarshal)
}

// --- Stripe: buildStripeSyncSubscriptionEvents subscription loop ---

func TestStripeBuildSyncSubscriptionEventsMarshalError(t *testing.T) {
	original := jsonMarshalFunc
	defer func() { jsonMarshalFunc = original }()

	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "cus_1",
		subscriptions: []stripeSubscriptionWebhookData{
			{
				ID:        "sub_1",
				Status:    stripeSubscriptionStatusActive,
				CreatedAt: time.Date(2026, time.February, 24, 9, 0, 0, 0, time.UTC).Unix(),
			},
		},
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	jsonMarshalFunc = failingMarshal

	_, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, syncErr)
	require.ErrorIs(t, syncErr, errForcedMarshal)
}

// --- Stripe: buildStripeInactiveSyncEvent via empty customerID ---

func TestStripeBuildInactiveSyncEventMarshalErrorNoCustomer(t *testing.T) {
	original := jsonMarshalFunc
	defer func() { jsonMarshalFunc = original }()

	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "",
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	jsonMarshalFunc = failingMarshal

	_, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, syncErr)
	require.ErrorIs(t, syncErr, errForcedMarshal)
}

// --- Stripe: buildStripeInactiveSyncEvent via customer not found on ListSubscriptions ---

func TestStripeBuildInactiveSyncEventMarshalErrorCustomerNotFound(t *testing.T) {
	original := jsonMarshalFunc
	defer func() { jsonMarshalFunc = original }()

	commerceClient := &stubStripeCommerceClient{
		foundCustomerID:  "cus_1",
		listSubscriptionsErr: ErrStripeAPICustomerNotFound,
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	jsonMarshalFunc = failingMarshal

	_, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, syncErr)
	require.ErrorIs(t, syncErr, errForcedMarshal)
}

// --- Stripe: buildStripeInactiveSyncEvent via empty subscriptions list ---

func TestStripeBuildInactiveSyncEventMarshalErrorEmptySubscriptions(t *testing.T) {
	original := jsonMarshalFunc
	defer func() { jsonMarshalFunc = original }()

	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "cus_1",
		subscriptions:   []stripeSubscriptionWebhookData{},
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	jsonMarshalFunc = failingMarshal

	_, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, syncErr)
	require.ErrorIs(t, syncErr, errForcedMarshal)
}

// --- Stripe: buildStripeInactiveSyncEvent via all subscriptions filtered (empty ID) ---

func TestStripeBuildInactiveSyncEventMarshalErrorAllSubscriptionsFiltered(t *testing.T) {
	original := jsonMarshalFunc
	defer func() { jsonMarshalFunc = original }()

	commerceClient := &stubStripeCommerceClient{
		foundCustomerID: "cus_1",
		subscriptions: []stripeSubscriptionWebhookData{
			{
				ID:        "",
				Status:    stripeSubscriptionStatusActive,
				CreatedAt: time.Date(2026, time.February, 24, 9, 0, 0, 0, time.UTC).Unix(),
			},
		},
	}
	provider, providerErr := NewStripeProvider(
		testStripeProviderSettings(),
		&stubStripeVerifier{},
		commerceClient,
	)
	require.NoError(t, providerErr)

	jsonMarshalFunc = failingMarshal

	_, syncErr := provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.Error(t, syncErr)
	require.ErrorIs(t, syncErr, errForcedMarshal)
}

// --- Stripe: BuildCheckoutReconcileEvent ---

func TestStripeBuildCheckoutReconcileEventMarshalError(t *testing.T) {
	original := jsonMarshalFunc
	defer func() { jsonMarshalFunc = original }()

	commerceClient := &stubStripeCommerceClient{
		reconcileSession: stripeCheckoutSessionWebhookData{
			ID:            "cs_test_1",
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

	jsonMarshalFunc = failingMarshal

	_, _, reconcileErr := provider.BuildCheckoutReconcileEvent(context.Background(), "cs_test_1")
	require.Error(t, reconcileErr)
	require.ErrorIs(t, reconcileErr, errForcedMarshal)
}
