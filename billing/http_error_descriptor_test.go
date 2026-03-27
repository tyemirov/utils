package billing

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveHTTPErrorDescriptorResolvesGenericBillingErrors(t *testing.T) {
	testCases := []struct {
		name                 string
		err                  error
		expectedStatusCode   int
		expectedErrorMessage string
	}{
		{
			name:                 "provider unavailable",
			err:                  ErrBillingProviderUnavailable,
			expectedStatusCode:   http.StatusServiceUnavailable,
			expectedErrorMessage: "Billing unavailable",
		},
		{
			name:                 "invalid plan",
			err:                  ErrBillingPlanUnsupported,
			expectedStatusCode:   http.StatusBadRequest,
			expectedErrorMessage: "invalid_plan",
		},
		{
			name:                 "subscription already active",
			err:                  ErrBillingSubscriptionActive,
			expectedStatusCode:   http.StatusConflict,
			expectedErrorMessage: "subscription_already_active",
		},
		{
			name:                 "subscription upgrade required",
			err:                  ErrBillingSubscriptionUpgrade,
			expectedStatusCode:   http.StatusConflict,
			expectedErrorMessage: "subscription_upgrade_required",
		},
		{
			name:                 "subscription required for top-up packs",
			err:                  ErrBillingSubscriptionRequired,
			expectedStatusCode:   http.StatusForbidden,
			expectedErrorMessage: "subscription_required_for_top_up_packs",
		},
		{
			name:                 "invalid pack code",
			err:                  ErrBillingTopUpPackUnknown,
			expectedStatusCode:   http.StatusBadRequest,
			expectedErrorMessage: "invalid_pack_code",
		},
		{
			name:                 "invalid transaction id",
			err:                  ErrBillingCheckoutTransactionInvalid,
			expectedStatusCode:   http.StatusBadRequest,
			expectedErrorMessage: "invalid_transaction_id",
		},
		{
			name:                 "pending transaction",
			err:                  ErrBillingCheckoutTransactionPending,
			expectedStatusCode:   http.StatusConflict,
			expectedErrorMessage: "transaction_pending",
		},
		{
			name:                 "checkout ownership mismatch",
			err:                  ErrBillingCheckoutOwnershipMismatch,
			expectedStatusCode:   http.StatusForbidden,
			expectedErrorMessage: "transaction_user_mismatch",
		},
		{
			name:                 "reconcile unavailable",
			err:                  ErrBillingCheckoutReconciliationUnavailable,
			expectedStatusCode:   http.StatusServiceUnavailable,
			expectedErrorMessage: "Billing unavailable",
		},
		{
			name:                 "user sync failed",
			err:                  ErrBillingUserSyncFailed,
			expectedStatusCode:   http.StatusBadGateway,
			expectedErrorMessage: "billing_user_sync_failed",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			httpErrorDescriptor := ResolveHTTPErrorDescriptor(ProviderCodePaddle, testCase.err)
			require.Equal(t, testCase.expectedStatusCode, httpErrorDescriptor.StatusCode)
			require.Equal(t, testCase.expectedErrorMessage, httpErrorDescriptor.Message)
		})
	}
}

func TestResolveHTTPErrorDescriptorResolvesProviderSpecificErrors(t *testing.T) {
	httpErrorDescriptor := ResolveHTTPErrorDescriptor(ProviderCodePaddle, ErrPaddleAPIDefaultCheckoutURL)
	require.Equal(t, http.StatusBadGateway, httpErrorDescriptor.StatusCode)
	require.Equal(
		t,
		"Paddle checkout is not configured: set a Default payment link in Paddle Checkout settings.",
		httpErrorDescriptor.Message,
	)

	httpErrorDescriptor = ResolveHTTPErrorDescriptor(
		ProviderCodePaddle,
		fmt.Errorf("wrapped: %w", ErrPaddleAPIPriceNotFound),
	)
	require.Equal(t, http.StatusBadGateway, httpErrorDescriptor.StatusCode)
	require.Equal(
		t,
		"Paddle price configuration mismatch: configured price IDs were not found for the current Paddle environment.",
		httpErrorDescriptor.Message,
	)

	httpErrorDescriptor = ResolveHTTPErrorDescriptor(
		ProviderCodePaddle,
		fmt.Errorf("wrapped: %w", ErrPaddleAPITransactionNotFound),
	)
	require.Equal(t, http.StatusBadRequest, httpErrorDescriptor.StatusCode)
	require.Equal(t, "invalid_transaction_id", httpErrorDescriptor.Message)

	httpErrorDescriptor = ResolveHTTPErrorDescriptor(
		ProviderCodeStripe,
		fmt.Errorf("wrapped: %w", ErrStripeAPICheckoutSessionNotFound),
	)
	require.Equal(t, http.StatusBadRequest, httpErrorDescriptor.StatusCode)
	require.Equal(t, "invalid_transaction_id", httpErrorDescriptor.Message)
}

func TestResolveHTTPErrorDescriptorFallsBackToBadGateway(t *testing.T) {
	httpErrorDescriptor := ResolveHTTPErrorDescriptor(ProviderCodePaddle, fmt.Errorf("unexpected"))
	require.Equal(t, http.StatusBadGateway, httpErrorDescriptor.StatusCode)
	require.Equal(t, http.StatusText(http.StatusBadGateway), httpErrorDescriptor.Message)

	httpErrorDescriptor = ResolveHTTPErrorDescriptor("stripe", ErrPaddleAPIDefaultCheckoutURL)
	require.Equal(t, http.StatusBadGateway, httpErrorDescriptor.StatusCode)
	require.Equal(t, http.StatusText(http.StatusBadGateway), httpErrorDescriptor.Message)
}

func TestResolveHTTPErrorDescriptorUserEmailInvalid(t *testing.T) {
	httpErrorDescriptor := ResolveHTTPErrorDescriptor(ProviderCodePaddle, ErrBillingUserEmailInvalid)
	require.Equal(t, http.StatusBadRequest, httpErrorDescriptor.StatusCode)
	require.Equal(t, http.StatusText(http.StatusBadRequest), httpErrorDescriptor.Message)
}

func TestResolveHTTPErrorDescriptorReconcileUnsupported(t *testing.T) {
	httpErrorDescriptor := ResolveHTTPErrorDescriptor(ProviderCodeStripe, ErrBillingCheckoutReconciliationUnsupported)
	require.Equal(t, http.StatusServiceUnavailable, httpErrorDescriptor.StatusCode)
	require.Equal(t, "Billing unavailable", httpErrorDescriptor.Message)
}

func TestResolveStripeHTTPErrorDescriptorDefaultCase(t *testing.T) {
	httpErrorDescriptor := ResolveHTTPErrorDescriptor(ProviderCodeStripe, fmt.Errorf("unexpected stripe error"))
	require.Equal(t, http.StatusBadGateway, httpErrorDescriptor.StatusCode)
	require.Equal(t, http.StatusText(http.StatusBadGateway), httpErrorDescriptor.Message)
}
