package billing

const (
	PaddleWebhookSignatureHeaderName = paddleWebhookSignatureHeaderName
	StripeWebhookSignatureHeaderName = stripeWebhookSignatureHeaderName

	PaddleMetadataUserEmailKey    = paddleLegacyMetadataUserEmailKey
	PaddleMetadataPurchaseKindKey = paddleLegacyMetadataPurchaseKindKey
	PaddleMetadataPlanCodeKey     = paddleLegacyMetadataPlanCodeKey
	PaddleMetadataPackCodeKey     = paddleLegacyMetadataPackCodeKey
	PaddleMetadataPackCreditsKey  = paddleLegacyMetadataPackCreditsKey

	StripeMetadataUserEmailKey    = stripeLegacyMetadataUserEmailKey
	StripeMetadataPurchaseKindKey = stripeLegacyMetadataPurchaseKindKey
	StripeMetadataPlanCodeKey     = stripeLegacyMetadataPlanCodeKey
	StripeMetadataPackCodeKey     = stripeLegacyMetadataPackCodeKey
	StripeMetadataPackCreditsKey  = stripeLegacyMetadataPackCreditsKey
	StripeMetadataPriceIDKey      = stripeLegacyMetadataPriceIDKey

	PaddlePurchaseKindSubscription = paddlePurchaseKindSubscription
	PaddlePurchaseKindTopUpPack    = paddlePurchaseKindTopUpPack
	StripePurchaseKindSubscription = stripePurchaseKindSubscription
	StripePurchaseKindTopUpPack    = stripePurchaseKindTopUpPack

	PaddleEventTypeTransactionCompleted  = paddleEventTypeTransactionCompleted
	PaddleEventTypeTransactionPaid       = paddleEventTypeTransactionPaid
	PaddleEventTypeTransactionUpdated    = paddleEventTypeTransactionUpdated
	PaddleEventTypeSubscriptionCreated   = paddleEventTypeSubscriptionCreated
	PaddleEventTypeSubscriptionUpdated   = paddleEventTypeSubscriptionUpdated
	PaddleEventTypeSubscriptionCanceled  = paddleEventTypeSubscriptionCanceled
	PaddleEventTypeSubscriptionResumed   = paddleEventTypeSubscriptionResumed
	PaddleEventTypeSubscriptionPaused    = paddleEventTypeSubscriptionPaused
	PaddleEventTypeSubscriptionActivated = paddleEventTypeSubscriptionActivated

	PaddleTransactionStatusPaid      = paddleTransactionStatusPaid
	PaddleTransactionStatusCompleted = paddleTransactionStatusCompleted
	PaddleSubscriptionStatusActive   = paddleSubscriptionStatusActive
	PaddleSubscriptionStatusTrialing = paddleSubscriptionStatusTrialing
	PaddleSubscriptionStatusPaused   = paddleSubscriptionStatusPaused
	PaddleSubscriptionStatusCanceled = paddleSubscriptionStatusCanceled
	PaddleSubscriptionStatusInactive = paddleSubscriptionStatusInactive
	PaddleSubscriptionStatusPastDue  = paddleSubscriptionStatusPastDue

	StripeEventTypeCheckoutSessionCompleted             = stripeEventTypeCheckoutSessionCompleted
	StripeEventTypeCheckoutSessionAsyncPaymentSucceeded = stripeEventTypeCheckoutSessionAsyncPaymentSucceeded
	StripeEventTypeCheckoutSessionAsyncPaymentFailed    = stripeEventTypeCheckoutSessionAsyncPaymentFailed
	StripeEventTypeCheckoutSessionExpired               = stripeEventTypeCheckoutSessionExpired
	StripeEventTypeCheckoutSessionPending               = stripeEventTypeCheckoutSessionPending
	StripeEventTypeSubscriptionCreated                  = stripeEventTypeSubscriptionCreated
	StripeEventTypeSubscriptionUpdated                  = stripeEventTypeSubscriptionUpdated
	StripeEventTypeSubscriptionDeleted                  = stripeEventTypeSubscriptionDeleted

	StripeCheckoutStatusComplete      = stripeCheckoutStatusComplete
	StripeCheckoutPaymentStatusPaid   = stripeCheckoutPaymentStatusPaid
	StripeCheckoutModeSubscription    = stripeCheckoutModeSubscription
	StripeCheckoutModePayment         = stripeCheckoutModePayment
	StripeCheckoutModeSubscriptionRaw = stripeCheckoutModeSubscriptionRaw
	StripeCheckoutModePaymentRaw      = stripeCheckoutModePaymentRaw

	StripeSubscriptionStatusActive            = stripeSubscriptionStatusActive
	StripeSubscriptionStatusTrialing          = stripeSubscriptionStatusTrialing
	StripeSubscriptionStatusPaused            = stripeSubscriptionStatusPaused
	StripeSubscriptionStatusCanceled          = stripeSubscriptionStatusCanceled
	StripeSubscriptionStatusIncomplete        = stripeSubscriptionStatusIncomplete
	StripeSubscriptionStatusIncompleteExpired = stripeSubscriptionStatusIncompleteExpired
	StripeSubscriptionStatusPastDue           = stripeSubscriptionStatusPastDue
	StripeSubscriptionStatusUnpaid            = stripeSubscriptionStatusUnpaid
)

type PaddleCommerceClient = paddleCommerceClient
type PaddleTransactionInput = paddleTransactionInput
type PaddlePriceDetails = paddlePriceDetails
type PaddlePriceBillingCycle = paddlePriceBillingCycle
type PaddleTransactionCompletedWebhookPayload = paddleTransactionCompletedWebhookPayload
type PaddleTransactionCompletedWebhookData = paddleTransactionCompletedWebhookData
type PaddleTransactionCompletedLineDetails = paddleTransactionCompletedLineDetails
type PaddleTransactionCompletedLineItem = paddleTransactionCompletedLineItem
type PaddleTransactionCompletedLineItemPrice = paddleTransactionCompletedLineItemPrice
type PaddleSubscriptionWebhookPayload = paddleSubscriptionWebhookPayload
type PaddleSubscriptionWebhookData = paddleSubscriptionWebhookData
type PaddleSubscriptionWebhookItem = paddleSubscriptionWebhookItem
type PaddleSubscriptionWebhookItemPrice = paddleSubscriptionWebhookItemPrice

type StripeCommerceClient = stripeCommerceClient
type StripeCheckoutSessionInput = stripeCheckoutSessionInput
type StripePortalSessionInput = stripePortalSessionInput
type StripePriceResponse = stripePriceResponse
type StripePriceRecurring = stripePriceRecurring
type StripeCheckoutSessionWebhookPayload = stripeCheckoutSessionWebhookPayload
type StripeCheckoutSessionWebhookPayloadData = stripeCheckoutSessionWebhookPayloadData
type StripeCheckoutSessionWebhookData = stripeCheckoutSessionWebhookData
type StripeSubscriptionWebhookPayload = stripeSubscriptionWebhookPayload
type StripeSubscriptionWebhookPayloadData = stripeSubscriptionWebhookPayloadData
type StripeSubscriptionWebhookData = stripeSubscriptionWebhookData
type StripeSubscriptionItems = stripeSubscriptionItems
type StripeSubscriptionItem = stripeSubscriptionItem
type StripeSubscriptionItemPrice = stripeSubscriptionItemPrice
