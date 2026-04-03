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

func NormalizePackCode(rawPackCode string) string {
	return strings.ToLower(strings.TrimSpace(rawPackCode))
}

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

func NormalizePurchaseKind(rawPurchaseKind string) string {
	return strings.ToLower(strings.TrimSpace(rawPurchaseKind))
}

func packReferenceCode(rawPackCode string) string {
	return NormalizePackCode(rawPackCode)
}

type WebhookEventMetadata struct {
	EventID    string
	EventType  string
	OccurredAt time.Time
}

type WebhookEvent struct {
	ProviderCode string
	EventID      string
	EventType    string
	OccurredAt   time.Time
	Payload      []byte
}

type WebhookProvider interface {
	Code() string
	SignatureHeaderName() string
	VerifySignature(signatureHeader string, payload []byte) error
	ParseWebhookEvent(payload []byte) (WebhookEventMetadata, error)
}

type SubscriptionPlan struct {
	Code           string `json:"code"`
	Label          string `json:"label"`
	MonthlyCredits int64  `json:"monthly_credits"`
	PriceDisplay   string `json:"price_display,omitempty"`
	BillingPeriod  string `json:"billing_period,omitempty"`
}

type TopUpPack struct {
	Code          string `json:"code"`
	Label         string `json:"label"`
	Credits       int64  `json:"credits"`
	PriceDisplay  string `json:"price_display,omitempty"`
	BillingPeriod string `json:"billing_period,omitempty"`
}

type PublicConfig struct {
	ProviderCode string
	Environment  string
	ClientToken  string
}

type PlanCatalogItem struct {
	Code           string
	Label          string
	PriceID        string
	MonthlyCredits int64
	PriceCents     int64
}

type PackCatalogItem struct {
	Code       string
	Label      string
	PriceID    string
	Credits    int64
	PriceCents int64
}

type CustomerContext struct {
	Email     string
	SubjectID string
}

func NormalizeCustomerContext(customer CustomerContext) CustomerContext {
	return CustomerContext{
		Email:     strings.ToLower(strings.TrimSpace(customer.Email)),
		SubjectID: strings.TrimSpace(customer.SubjectID),
	}
}

type TopUpEligibilityPolicy string

const (
	TopUpEligibilityPolicyRequiresActiveSubscription TopUpEligibilityPolicy = "requires_active_subscription"
	TopUpEligibilityPolicyUnrestricted               TopUpEligibilityPolicy = "unrestricted"
)

func NormalizeTopUpEligibilityPolicy(rawPolicy TopUpEligibilityPolicy) TopUpEligibilityPolicy {
	switch TopUpEligibilityPolicy(strings.ToLower(strings.TrimSpace(string(rawPolicy)))) {
	case TopUpEligibilityPolicyUnrestricted:
		return TopUpEligibilityPolicyUnrestricted
	default:
		return TopUpEligibilityPolicyRequiresActiveSubscription
	}
}

type CheckoutSession struct {
	ProviderCode  string
	TransactionID string
	CheckoutMode  string
}

type PortalSession struct {
	ProviderCode string
	URL          string
}

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

type ProviderSubscription struct {
	SubscriptionID string
	PlanCode       string
	Status         string
	ProviderStatus string
	NextBillingAt  time.Time
	OccurredAt     time.Time
}

type SubscriptionInspector interface {
	InspectSubscriptions(context.Context, string) ([]ProviderSubscription, error)
}

type WebhookGrantResolverProvider interface {
	NewWebhookGrantResolver() (WebhookGrantResolver, error)
}

type SubscriptionStatusWebhookProcessorProvider interface {
	NewSubscriptionStatusWebhookProcessor(SubscriptionStateRepository) (WebhookProcessor, error)
}

type CheckoutReconcileProvider interface {
	BuildCheckoutReconcileEvent(context.Context, string) (WebhookEvent, string, error)
}

type CheckoutEventStatus string

const (
	CheckoutEventStatusUnknown   CheckoutEventStatus = "unknown"
	CheckoutEventStatusPending   CheckoutEventStatus = "pending"
	CheckoutEventStatusSucceeded CheckoutEventStatus = "succeeded"
	CheckoutEventStatusFailed    CheckoutEventStatus = "failed"
	CheckoutEventStatusExpired   CheckoutEventStatus = "expired"
)

type CheckoutEventStatusProvider interface {
	ResolveCheckoutEventStatus(string) CheckoutEventStatus
}

type CatalogValidationProvider interface {
	ValidateCatalog(context.Context) error
}

type Provider interface {
	WebhookProvider
	CommerceProvider
}

type WebhookProcessor interface {
	Process(context.Context, WebhookEvent) error
}

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
