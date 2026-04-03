package billing

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	ProviderCodePaddle  = "paddle"
	ProviderCodeStripe  = "stripe"
	CheckoutModeOverlay = "overlay"

	PlanCodePro  = "pro"
	PlanCodePlus = "plus"

	PackCodeTopUp     = "top_up"
	PackCodeBulkTopUp = "bulk_top_up"

	PurchaseKindSubscription = "subscription"
	PurchaseKindTopUpPack    = "top_up_pack"
)

const (
	billingMetadataUserEmailKey    = "billing_user_email"
	billingMetadataSubjectIDKey    = "billing_subject_id"
	billingMetadataPurchaseKindKey = "billing_purchase_kind"
	billingMetadataPlanCodeKey     = "billing_plan_code"
	billingMetadataPackCodeKey     = "billing_pack_code"
	billingMetadataPackCreditsKey  = "billing_pack_credits"
	billingMetadataPriceIDKey      = "billing_price_id"
)

const (
	packLabelTopUp     = "Top-Up Pack"
	packLabelBulkTopUp = "Bulk Top-Up Pack"
)

// NormalizePackCode lowercases and trims application pack identifiers before
// they are compared or stored in metadata.
func NormalizePackCode(rawPackCode string) string {
	return strings.ToLower(strings.TrimSpace(rawPackCode))
}

// PackLabelForCode returns the built-in display label for package-defined pack
// codes. Applications with custom catalogs can supply their own labels instead.
func PackLabelForCode(rawPackCode string) string {
	switch NormalizePackCode(rawPackCode) {
	case PackCodeTopUp:
		return packLabelTopUp
	case PackCodeBulkTopUp:
		return packLabelBulkTopUp
	default:
		return ""
	}
}

// NormalizePurchaseKind lowercases and trims a purchase-kind marker from
// provider metadata.
func NormalizePurchaseKind(rawPurchaseKind string) string {
	return strings.ToLower(strings.TrimSpace(rawPurchaseKind))
}

func packReferenceCode(rawPackCode string) string {
	return NormalizePackCode(rawPackCode)
}

// WebhookEventMetadata is the minimal event envelope extracted during webhook
// parsing before the full payload is handed to processors.
type WebhookEventMetadata struct {
	EventID    string
	EventType  string
	OccurredAt time.Time
}

// WebhookEvent is the normalized event passed through the package's webhook
// processing pipeline.
type WebhookEvent struct {
	ProviderCode string
	EventID      string
	EventType    string
	OccurredAt   time.Time
	Payload      []byte
}

// WebhookProvider verifies webhook signatures and parses provider-native
// payloads into package-level event metadata.
type WebhookProvider interface {
	Code() string
	SignatureHeaderName() string
	VerifySignature(signatureHeader string, payload []byte) error
	ParseWebhookEvent(payload []byte) (WebhookEventMetadata, error)
}

// SubscriptionPlan is the public catalog entry returned to applications for a
// recurring subscription product.
type SubscriptionPlan struct {
	Code           string `json:"code"`
	Label          string `json:"label"`
	MonthlyCredits int64  `json:"monthly_credits"`
	PriceDisplay   string `json:"price_display,omitempty"`
	BillingPeriod  string `json:"billing_period,omitempty"`
}

// TopUpPack is the public catalog entry returned to applications for a one-off
// purchasable pack.
type TopUpPack struct {
	Code          string `json:"code"`
	Label         string `json:"label"`
	Credits       int64  `json:"credits"`
	PriceDisplay  string `json:"price_display,omitempty"`
	BillingPeriod string `json:"billing_period,omitempty"`
}

// PublicConfig exposes the provider settings a frontend needs to launch the
// provider's hosted checkout experience.
type PublicConfig struct {
	ProviderCode string
	Environment  string
	ClientToken  string
}

// PlanCatalogItem configures a recurring plan when constructing a provider.
type PlanCatalogItem struct {
	Code           string
	Label          string
	PriceID        string
	MonthlyCredits int64
	PriceCents     int64
}

// PackCatalogItem configures a one-off pack when constructing a provider.
type PackCatalogItem struct {
	Code       string
	Label      string
	PriceID    string
	Credits    int64
	PriceCents int64
}

// CustomerContext identifies the checkout owner. Email is currently the
// canonical key for summary, portal, sync, and reconcile flows; SubjectID is
// carried through checkout metadata for app-level identity mapping.
type CustomerContext struct {
	Email     string
	SubjectID string
}

// NormalizeCustomerContext trims SubjectID and normalizes Email for provider
// calls and metadata generation.
func NormalizeCustomerContext(customer CustomerContext) CustomerContext {
	return CustomerContext{
		Email:     strings.ToLower(strings.TrimSpace(customer.Email)),
		SubjectID: strings.TrimSpace(customer.SubjectID),
	}
}

// TopUpEligibilityPolicy controls whether top-up purchases require an active
// subscription according to the shared Service rules.
type TopUpEligibilityPolicy string

const (
	TopUpEligibilityPolicyRequiresActiveSubscription TopUpEligibilityPolicy = "requires_active_subscription"
	TopUpEligibilityPolicyUnrestricted               TopUpEligibilityPolicy = "unrestricted"
)

// NormalizeTopUpEligibilityPolicy converts an arbitrary value into one of the
// supported policy constants, defaulting to subscription-gated behavior.
func NormalizeTopUpEligibilityPolicy(rawPolicy TopUpEligibilityPolicy) TopUpEligibilityPolicy {
	switch TopUpEligibilityPolicy(strings.ToLower(strings.TrimSpace(string(rawPolicy)))) {
	case TopUpEligibilityPolicyUnrestricted:
		return TopUpEligibilityPolicyUnrestricted
	default:
		return TopUpEligibilityPolicyRequiresActiveSubscription
	}
}

// CheckoutSession is the provider-neutral result of a checkout creation call.
// TransactionID is later used by provider-specific frontends or reconcile
// flows to finish the purchase UX.
type CheckoutSession struct {
	ProviderCode  string
	TransactionID string
	CheckoutMode  string
}

// PortalSession contains the provider-hosted customer-portal URL for the
// authenticated billing owner.
type PortalSession struct {
	ProviderCode string
	URL          string
}

// CommerceProvider is the shared provider contract consumed by Service. Custom
// applications usually use a package-supplied PaddleProvider or StripeProvider
// rather than implementing this interface themselves.
type CommerceProvider interface {
	Code() string
	SubscriptionPlans() []SubscriptionPlan
	TopUpPacks() []TopUpPack
	PublicConfig() PublicConfig
	BuildUserSyncEvents(context.Context, string) ([]WebhookEvent, error)
	CreateSubscriptionCheckout(context.Context, CustomerContext, string) (CheckoutSession, error)
	CreateTopUpCheckout(context.Context, CustomerContext, string) (CheckoutSession, error)
	CreateCustomerPortalSession(context.Context, string) (PortalSession, error)
}

// ProviderSubscription is the normalized live subscription shape returned by
// provider inspection APIs and sync flows.
type ProviderSubscription struct {
	SubscriptionID string
	PlanCode       string
	Status         string
	ProviderStatus string
	NextBillingAt  time.Time
	OccurredAt     time.Time
}

// SubscriptionInspector exposes a provider's live subscription inspection API
// to the shared Service.
type SubscriptionInspector interface {
	InspectSubscriptions(context.Context, string) ([]ProviderSubscription, error)
}

// WebhookGrantResolverProvider exposes provider-native webhook grant resolution
// for use in the shared grant processor.
type WebhookGrantResolverProvider interface {
	NewWebhookGrantResolver() (WebhookGrantResolver, error)
}

// SubscriptionStatusWebhookProcessorProvider exposes a provider-native
// subscription-state reducer for webhook events and synthetic sync events.
type SubscriptionStatusWebhookProcessorProvider interface {
	NewSubscriptionStatusWebhookProcessor(SubscriptionStateRepository) (WebhookProcessor, error)
}

// CheckoutReconcileProvider can turn a completed checkout identifier into a
// synthetic webhook event that the shared pipeline can process.
type CheckoutReconcileProvider interface {
	BuildCheckoutReconcileEvent(context.Context, string) (WebhookEvent, string, error)
}

// CheckoutEventStatus classifies a checkout event for shared webhook and
// reconcile logic.
type CheckoutEventStatus string

const (
	CheckoutEventStatusUnknown   CheckoutEventStatus = "unknown"
	CheckoutEventStatusPending   CheckoutEventStatus = "pending"
	CheckoutEventStatusSucceeded CheckoutEventStatus = "succeeded"
	CheckoutEventStatusFailed    CheckoutEventStatus = "failed"
	CheckoutEventStatusExpired   CheckoutEventStatus = "expired"
)

// CheckoutEventStatusProvider maps provider-native event types to
// CheckoutEventStatus values.
type CheckoutEventStatusProvider interface {
	ResolveCheckoutEventStatus(string) CheckoutEventStatus
}

// CatalogValidationProvider validates configured price IDs and amounts against
// the remote provider catalog.
type CatalogValidationProvider interface {
	ValidateCatalog(context.Context) error
}

// Provider is the full provider contract used by webhook handlers. It combines
// the CommerceProvider and WebhookProvider capabilities.
type Provider interface {
	WebhookProvider
	CommerceProvider
}

// WebhookProcessor handles a normalized webhook event.
type WebhookProcessor interface {
	Process(context.Context, WebhookEvent) error
}

// WebhookProcessorFunc adapts a function into a WebhookProcessor.
type WebhookProcessorFunc func(context.Context, WebhookEvent) error

func (function WebhookProcessorFunc) Process(ctx context.Context, event WebhookEvent) error {
	return function(ctx, event)
}

type noopWebhookProcessor struct{}

func (noopWebhookProcessor) Process(context.Context, WebhookEvent) error {
	return nil
}

func resolveWebhookProcessor(processor WebhookProcessor) WebhookProcessor {
	if processor == nil {
		return noopWebhookProcessor{}
	}
	return processor
}

var (
	ErrBillingProviderUnavailable        = errors.New("billing.provider.unavailable")
	ErrBillingUserEmailInvalid           = errors.New("billing.user_email.invalid")
	ErrBillingPlanUnsupported            = errors.New("billing.plan.unsupported")
	ErrBillingSubscriptionManageInPortal = errors.New("billing.subscription.manage_in_portal")
	ErrBillingSubscriptionRequired       = errors.New("billing.subscription.required")
	ErrBillingTopUpPackUnknown           = errors.New("billing.top_up_pack.unknown")
	ErrBillingUserSyncFailed             = errors.New("billing.user_sync.failed")

	// Deprecated aliases kept for compatibility with older callers and tests.
	ErrBillingSubscriptionActive  = ErrBillingSubscriptionManageInPortal
	ErrBillingSubscriptionUpgrade = ErrBillingSubscriptionManageInPortal
)
