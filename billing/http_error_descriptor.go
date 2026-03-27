package billing

import (
	"errors"
	"net/http"
	"strings"
)

const (
	billingHTTPMessageUnavailable             = "Billing unavailable"
	billingHTTPMessageInvalidPlan             = "invalid_plan"
	billingHTTPMessageSubscriptionActive      = "subscription_already_active"
	billingHTTPMessageSubscriptionUpgradeOnly = "subscription_upgrade_required"
	billingHTTPMessageSubscriptionRequired    = "subscription_required_for_top_up_packs"
	billingHTTPMessageInvalidPackCode         = "invalid_pack_code"
	billingHTTPMessageInvalidTransactionID    = "invalid_transaction_id"
	billingHTTPMessageTransactionPending      = "transaction_pending"
	billingHTTPMessageTransactionUserMismatch = "transaction_user_mismatch"
	billingHTTPMessageUserSyncFailed          = "billing_user_sync_failed"
	billingHTTPMessageCheckoutNotConfigured   = "Paddle checkout is not configured: set a Default payment link in Paddle Checkout settings."
	billingHTTPMessagePriceNotFound           = "Paddle price configuration mismatch: configured price IDs were not found for the current Paddle environment."
)

type HTTPErrorDescriptor struct {
	StatusCode int
	Message    string
}

var providerHTTPErrorResolvers = map[string]func(error) (HTTPErrorDescriptor, bool){
	ProviderCodePaddle: resolvePaddleHTTPErrorDescriptor,
	ProviderCodeStripe: resolveStripeHTTPErrorDescriptor,
}

func ResolveHTTPErrorDescriptor(providerCode string, err error) HTTPErrorDescriptor {
	switch {
	case errors.Is(err, ErrBillingProviderUnavailable):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusServiceUnavailable,
			Message:    billingHTTPMessageUnavailable,
		}
	case errors.Is(err, ErrBillingUserEmailInvalid):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusBadRequest,
			Message:    http.StatusText(http.StatusBadRequest),
		}
	case errors.Is(err, ErrBillingPlanUnsupported):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusBadRequest,
			Message:    billingHTTPMessageInvalidPlan,
		}
	case errors.Is(err, ErrBillingSubscriptionActive):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusConflict,
			Message:    billingHTTPMessageSubscriptionActive,
		}
	case errors.Is(err, ErrBillingSubscriptionUpgrade):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusConflict,
			Message:    billingHTTPMessageSubscriptionUpgradeOnly,
		}
	case errors.Is(err, ErrBillingSubscriptionRequired):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusForbidden,
			Message:    billingHTTPMessageSubscriptionRequired,
		}
	case errors.Is(err, ErrBillingTopUpPackUnknown):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusBadRequest,
			Message:    billingHTTPMessageInvalidPackCode,
		}
	case errors.Is(err, ErrBillingCheckoutTransactionInvalid):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusBadRequest,
			Message:    billingHTTPMessageInvalidTransactionID,
		}
	case errors.Is(err, ErrBillingCheckoutTransactionPending):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusConflict,
			Message:    billingHTTPMessageTransactionPending,
		}
	case errors.Is(err, ErrBillingCheckoutOwnershipMismatch):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusForbidden,
			Message:    billingHTTPMessageTransactionUserMismatch,
		}
	case errors.Is(err, ErrBillingCheckoutReconciliationUnavailable):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusServiceUnavailable,
			Message:    billingHTTPMessageUnavailable,
		}
	case errors.Is(err, ErrBillingCheckoutReconciliationUnsupported):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusServiceUnavailable,
			Message:    billingHTTPMessageUnavailable,
		}
	case errors.Is(err, ErrBillingUserSyncFailed):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusBadGateway,
			Message:    billingHTTPMessageUserSyncFailed,
		}
	}

	normalizedProviderCode := strings.ToLower(strings.TrimSpace(providerCode))
	providerHTTPErrorResolver, hasProviderHTTPErrorResolver := providerHTTPErrorResolvers[normalizedProviderCode]
	if hasProviderHTTPErrorResolver {
		resolvedDescriptor, resolved := providerHTTPErrorResolver(err)
		if resolved {
			return resolvedDescriptor
		}
	}

	return HTTPErrorDescriptor{
		StatusCode: http.StatusBadGateway,
		Message:    http.StatusText(http.StatusBadGateway),
	}
}

func resolvePaddleHTTPErrorDescriptor(err error) (HTTPErrorDescriptor, bool) {
	switch {
	case errors.Is(err, ErrPaddleAPIDefaultCheckoutURL):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusBadGateway,
			Message:    billingHTTPMessageCheckoutNotConfigured,
		}, true
	case errors.Is(err, ErrPaddleAPIPriceNotFound):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusBadGateway,
			Message:    billingHTTPMessagePriceNotFound,
		}, true
	case errors.Is(err, ErrPaddleAPITransactionNotFound):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusBadRequest,
			Message:    billingHTTPMessageInvalidTransactionID,
		}, true
	default:
		return HTTPErrorDescriptor{}, false
	}
}

func resolveStripeHTTPErrorDescriptor(err error) (HTTPErrorDescriptor, bool) {
	switch {
	case errors.Is(err, ErrStripeAPICheckoutSessionNotFound):
		return HTTPErrorDescriptor{
			StatusCode: http.StatusBadRequest,
			Message:    billingHTTPMessageInvalidTransactionID,
		}, true
	default:
		return HTTPErrorDescriptor{}, false
	}
}
