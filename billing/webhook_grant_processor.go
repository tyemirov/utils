package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	paddleEventTypeTransactionCompleted = "transaction.completed"
	paddleEventTypeTransactionPaid      = "transaction.paid"
	paddleEventTypeTransactionUpdated   = "transaction.updated"
	paddleTransactionStatusPaid         = "paid"
	paddleTransactionStatusCompleted    = "completed"

	billingGrantMetadataProviderKey       = "billing_provider"
	billingGrantMetadataEventIDKey        = "billing_event_id"
	billingGrantMetadataEventTypeKey      = "billing_event_type"
	billingGrantMetadataPurchaseKindKey   = "billing_purchase_kind"
	billingGrantMetadataTransactionIDKey  = "billing_transaction_id"
	billingGrantMetadataSubscriptionIDKey = "billing_subscription_id"
	billingGrantMetadataPlanCodeKey       = "billing_plan_code"
	billingGrantMetadataPackCodeKey       = "billing_pack_code"
	billingGrantMetadataPriceIDKey        = "billing_price_id"

	billingGrantReasonSubscriptionPrefix = "subscription_monthly"
	billingGrantReasonTopUpPackPrefix    = "top_up_pack"

	billingGrantReferenceSubscriptionPrefix = "paddle:subscription"
	billingGrantReferenceTopUpPackPrefix    = "paddle:top_up_pack"

	metadataBillingEventOccurredAtKey = "billing_event_occurred_at"
)

var (
	ErrWebhookGrantResolverUnavailable         = errors.New("billing.webhook.grant_resolver.unavailable")
	ErrWebhookGrantResolverProviderUnavailable = errors.New("billing.webhook.grant_resolver.provider.unavailable")
	ErrWebhookGrantResolverProviderUnsupported = errors.New("billing.webhook.grant_resolver.provider.unsupported")
	ErrWebhookCreditsServiceUnavailable        = errors.New("billing.webhook.credits_service.unavailable")
	ErrWebhookGrantPayloadInvalid              = errors.New("billing.webhook.payload.invalid")
	ErrWebhookGrantMetadataInvalid             = errors.New("billing.webhook.metadata.invalid")
	ErrWebhookGrantPlanUnknown                 = errors.New("billing.webhook.plan.unknown")
	ErrWebhookGrantPackUnknown                 = errors.New("billing.webhook.pack.unknown")
)

type WebhookGrant struct {
	UserEmail string
	Credits   int64
	Reason    string
	Reference string
	Metadata  map[string]string
}

type WebhookGrantResolver interface {
	Resolve(context.Context, WebhookEvent) (WebhookGrant, bool, error)
}

type webhookCreditsProcessor struct {
	granter  CreditGranter
	resolver WebhookGrantResolver
}

func NewCreditsWebhookProcessor(
	granter CreditGranter,
	resolver WebhookGrantResolver,
) (WebhookProcessor, error) {
	if granter == nil {
		return nil, ErrWebhookCreditsServiceUnavailable
	}
	if resolver == nil {
		return nil, ErrWebhookGrantResolverUnavailable
	}
	return &webhookCreditsProcessor{
		granter:  granter,
		resolver: resolver,
	}, nil
}

func (processor *webhookCreditsProcessor) Process(ctx context.Context, event WebhookEvent) error {
	grant, shouldGrant, grantResolveErr := processor.resolver.Resolve(ctx, event)
	if grantResolveErr != nil {
		return fmt.Errorf("billing.webhook.grant.resolve: %w", grantResolveErr)
	}
	if !shouldGrant {
		return nil
	}

	grantMetadata := cloneGrantMetadata(grant.Metadata)
	grantMetadata[billingGrantMetadataProviderKey] = strings.ToLower(strings.TrimSpace(event.ProviderCode))
	grantMetadata[billingGrantMetadataEventIDKey] = strings.TrimSpace(event.EventID)
	grantMetadata[billingGrantMetadataEventTypeKey] = strings.TrimSpace(event.EventType)
	if !event.OccurredAt.IsZero() {
		grantMetadata[metadataBillingEventOccurredAtKey] = event.OccurredAt.UTC().Format(time.RFC3339Nano)
	}

	grantErr := processor.granter.GrantBillingCredits(ctx, CreditGrantInput{
		UserEmail:      grant.UserEmail,
		Credits:        grant.Credits,
		IdempotencyKey: strings.TrimSpace(grant.Reference),
		Reason:         strings.TrimSpace(grant.Reason),
		Reference:      strings.TrimSpace(grant.Reference),
		Metadata:       grantMetadata,
	})
	if grantErr == nil || errors.Is(grantErr, ErrDuplicateGrant) {
		return nil
	}
	return fmt.Errorf("billing.webhook.grant.apply: %w", grantErr)
}

func cloneGrantMetadata(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	target := make(map[string]string, len(source))
	for key, value := range source {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		target[trimmedKey] = strings.TrimSpace(value)
	}
	return target
}

func NewWebhookGrantResolver(provider CommerceProvider) (WebhookGrantResolver, error) {
	if provider == nil {
		return nil, ErrWebhookGrantResolverProviderUnavailable
	}
	webhookGrantResolverProvider, isWebhookGrantResolverProvider := provider.(WebhookGrantResolverProvider)
	if !isWebhookGrantResolverProvider {
		normalizedProviderCode := strings.ToLower(strings.TrimSpace(provider.Code()))
		return nil, fmt.Errorf("%w: %s", ErrWebhookGrantResolverProviderUnsupported, normalizedProviderCode)
	}
	return webhookGrantResolverProvider.NewWebhookGrantResolver()
}

type paddleGrantDefinition struct {
	Code    string
	Credits int64
}

type paddleCustomerEmailResolver interface {
	ResolveCustomerEmail(context.Context, string) (string, error)
}

type paddleWebhookGrantResolver struct {
	planCreditsByCode     map[string]int64
	packCreditsByCode     map[string]int64
	planGrantByPriceID    map[string]paddleGrantDefinition
	packGrantByPriceID    map[string]paddleGrantDefinition
	customerEmailResolver paddleCustomerEmailResolver
	eventStatusProvider   CheckoutEventStatusProvider
}

func newPaddleWebhookGrantResolverFromProvider(provider *PaddleProvider) (*paddleWebhookGrantResolver, error) {
	if provider == nil {
		return nil, ErrWebhookGrantResolverProviderUnavailable
	}
	planGrantByPriceID := make(map[string]paddleGrantDefinition, len(provider.plans))
	for _, definition := range provider.plans {
		normalizedPriceID := strings.TrimSpace(definition.PriceID)
		normalizedPlanCode := strings.ToLower(strings.TrimSpace(definition.Plan.Code))
		if normalizedPriceID == "" || normalizedPlanCode == "" || definition.Plan.MonthlyCredits <= 0 {
			continue
		}
		planGrantByPriceID[normalizedPriceID] = paddleGrantDefinition{
			Code:    normalizedPlanCode,
			Credits: definition.Plan.MonthlyCredits,
		}
	}
	packGrantByPriceID := make(map[string]paddleGrantDefinition, len(provider.packs))
	for _, definition := range provider.packs {
		normalizedPriceID := strings.TrimSpace(definition.PriceID)
		normalizedPackCode := NormalizePackCode(definition.Pack.Code)
		if normalizedPriceID == "" || normalizedPackCode == "" || definition.Pack.Credits <= 0 {
			continue
		}
		packGrantByPriceID[normalizedPriceID] = paddleGrantDefinition{
			Code:    normalizedPackCode,
			Credits: definition.Pack.Credits,
		}
	}
	return newPaddleWebhookGrantResolverWithCatalog(
		provider.SubscriptionPlans(),
		provider.TopUpPacks(),
		planGrantByPriceID,
		packGrantByPriceID,
		provider.client,
		provider,
	)
}

func newPaddleWebhookGrantResolverWithCatalog(
	plans []SubscriptionPlan,
	packs []TopUpPack,
	planGrantByPriceID map[string]paddleGrantDefinition,
	packGrantByPriceID map[string]paddleGrantDefinition,
	customerEmailResolver paddleCustomerEmailResolver,
	eventStatusProvider CheckoutEventStatusProvider,
) (*paddleWebhookGrantResolver, error) {
	planCreditsByCode := make(map[string]int64, len(plans))
	for _, plan := range plans {
		normalizedPlanCode := strings.ToLower(strings.TrimSpace(plan.Code))
		if normalizedPlanCode == "" || plan.MonthlyCredits <= 0 {
			return nil, ErrWebhookGrantMetadataInvalid
		}
		planCreditsByCode[normalizedPlanCode] = plan.MonthlyCredits
	}
	packCreditsByCode := make(map[string]int64, len(packs))
	for _, pack := range packs {
		normalizedPackCode := NormalizePackCode(pack.Code)
		if normalizedPackCode == "" || pack.Credits <= 0 {
			return nil, ErrWebhookGrantMetadataInvalid
		}
		packCreditsByCode[normalizedPackCode] = pack.Credits
	}
	return &paddleWebhookGrantResolver{
		planCreditsByCode:     planCreditsByCode,
		packCreditsByCode:     packCreditsByCode,
		planGrantByPriceID:    clonePaddleGrantDefinitionsByPriceID(planGrantByPriceID),
		packGrantByPriceID:    clonePaddleGrantDefinitionsByPriceID(packGrantByPriceID),
		customerEmailResolver: customerEmailResolver,
		eventStatusProvider:   eventStatusProvider,
	}, nil
}

func clonePaddleGrantDefinitionsByPriceID(
	source map[string]paddleGrantDefinition,
) map[string]paddleGrantDefinition {
	if len(source) == 0 {
		return map[string]paddleGrantDefinition{}
	}
	target := make(map[string]paddleGrantDefinition, len(source))
	for rawPriceID, grantDefinition := range source {
		normalizedPriceID := strings.TrimSpace(rawPriceID)
		normalizedCode := NormalizePackCode(grantDefinition.Code)
		if normalizedPriceID == "" || normalizedCode == "" || grantDefinition.Credits <= 0 {
			continue
		}
		target[normalizedPriceID] = paddleGrantDefinition{
			Code:    normalizedCode,
			Credits: grantDefinition.Credits,
		}
	}
	return target
}

func (resolver *paddleWebhookGrantResolver) Resolve(ctx context.Context, event WebhookEvent) (WebhookGrant, bool, error) {
	normalizedProviderCode := strings.ToLower(strings.TrimSpace(event.ProviderCode))
	if normalizedProviderCode != ProviderCodePaddle {
		return WebhookGrant{}, false, nil
	}
	if resolver.eventStatusProvider == nil {
		return WebhookGrant{}, false, ErrWebhookGrantResolverUnavailable
	}
	checkoutEventStatus := resolver.eventStatusProvider.ResolveCheckoutEventStatus(event.EventType)
	if checkoutEventStatus != CheckoutEventStatusSucceeded && checkoutEventStatus != CheckoutEventStatusPending {
		return WebhookGrant{}, false, nil
	}
	normalizedEventType := strings.TrimSpace(event.EventType)

	payload := paddleTransactionCompletedWebhookPayload{}
	if decodeErr := json.Unmarshal(event.Payload, &payload); decodeErr != nil {
		return WebhookGrant{}, false, ErrWebhookGrantPayloadInvalid
	}
	if checkoutEventStatus == CheckoutEventStatusPending &&
		!isGrantablePaddleTransactionStatus(normalizedEventType, payload.Data.Status) {
		return WebhookGrant{}, false, nil
	}

	transactionID := strings.TrimSpace(payload.Data.ID)
	if transactionID == "" {
		return WebhookGrant{}, false, ErrWebhookGrantPayloadInvalid
	}

	priceID := strings.TrimSpace(resolvePaddleTransactionPriceID(payload.Data))
	userEmail, userEmailErr := resolver.resolveUserEmail(ctx, payload.Data)
	if userEmailErr != nil {
		return WebhookGrant{}, false, userEmailErr
	}

	purchaseKind := NormalizePurchaseKind(
		webhookMetadataValue(payload.Data.CustomData, paddleMetadataPurchaseKindKey),
	)
	switch purchaseKind {
	case paddlePurchaseKindSubscription, paddlePurchaseKindTopUpPack:
	default:
		purchaseKind = resolver.resolvePurchaseKindFromPriceID(priceID)
	}
	if userEmail == "" || purchaseKind == "" {
		return WebhookGrant{}, false, ErrWebhookGrantMetadataInvalid
	}

	if purchaseKind == paddlePurchaseKindSubscription {
		planCode := strings.ToLower(webhookMetadataValue(payload.Data.CustomData, paddleMetadataPlanCodeKey))
		if planCode == "" {
			planGrantDefinition, hasPlanGrantDefinition := resolver.planGrantByPriceID[priceID]
			if hasPlanGrantDefinition {
				planCode = planGrantDefinition.Code
			}
		}
		planCredits, hasPlanCredits := resolver.planCreditsByCode[planCode]
		if !hasPlanCredits {
			planGrantDefinition, hasPlanGrantDefinition := resolver.planGrantByPriceID[priceID]
			if hasPlanGrantDefinition && planGrantDefinition.Code == planCode {
				planCredits = planGrantDefinition.Credits
				hasPlanCredits = true
			}
		}
		if !hasPlanCredits {
			return WebhookGrant{}, false, fmt.Errorf("%w: %s", ErrWebhookGrantPlanUnknown, planCode)
		}
		reason := fmt.Sprintf("%s_%s", billingGrantReasonSubscriptionPrefix, planCode)
		reference := fmt.Sprintf("%s:%s:%s", billingGrantReferenceSubscriptionPrefix, transactionID, planCode)
		metadata := map[string]string{
			billingGrantMetadataPurchaseKindKey:  purchaseKind,
			billingGrantMetadataTransactionIDKey: transactionID,
			billingGrantMetadataPlanCodeKey:      planCode,
		}
		subscriptionID := strings.TrimSpace(payload.Data.SubscriptionID)
		if subscriptionID != "" {
			metadata[billingGrantMetadataSubscriptionIDKey] = subscriptionID
		}
		if priceID != "" {
			metadata[billingGrantMetadataPriceIDKey] = priceID
		}
		return WebhookGrant{
			UserEmail: userEmail,
			Credits:   planCredits,
			Reason:    reason,
			Reference: reference,
			Metadata:  metadata,
		}, true, nil
	}

	// purchaseKind == paddlePurchaseKindTopUpPack
	packCode := NormalizePackCode(webhookMetadataValue(payload.Data.CustomData, paddleMetadataPackCodeKey))
	if packCode == "" {
		packGrantDefinition, hasPackGrantDefinition := resolver.packGrantByPriceID[priceID]
		if hasPackGrantDefinition {
			packCode = packGrantDefinition.Code
		}
	}
	packCreditsFromMetadata, packCreditsMetadataErr := parsePackCreditsFromMetadata(payload.Data.CustomData)
	if packCreditsMetadataErr != nil {
		return WebhookGrant{}, false, packCreditsMetadataErr
	}
	packCredits := packCreditsFromMetadata
	if packCredits == 0 {
		resolvedPackCredits, hasPackCredits := resolver.packCreditsByCode[packCode]
		if !hasPackCredits {
			packGrantDefinition, hasPackGrantDefinition := resolver.packGrantByPriceID[priceID]
			if hasPackGrantDefinition {
				resolvedPackCredits = packGrantDefinition.Credits
				hasPackCredits = true
			}
		}
		if !hasPackCredits {
			return WebhookGrant{}, false, fmt.Errorf("%w: %s", ErrWebhookGrantPackUnknown, packCode)
		}
		packCredits = resolvedPackCredits
	}
	reason := fmt.Sprintf("%s_%s", billingGrantReasonTopUpPackPrefix, packCode)
	reference := fmt.Sprintf(
		"%s:%s:%s",
		billingGrantReferenceTopUpPackPrefix,
		transactionID,
		packReferenceCode(packCode),
	)
	metadata := map[string]string{
		billingGrantMetadataPurchaseKindKey:  purchaseKind,
		billingGrantMetadataTransactionIDKey: transactionID,
		billingGrantMetadataPackCodeKey:      packCode,
	}
	if priceID != "" {
		metadata[billingGrantMetadataPriceIDKey] = priceID
	}
	return WebhookGrant{
		UserEmail: userEmail,
		Credits:   packCredits,
		Reason:    reason,
		Reference: reference,
		Metadata:  metadata,
	}, true, nil
}

func (resolver *paddleWebhookGrantResolver) resolvePurchaseKindFromPriceID(priceID string) string {
	normalizedPriceID := strings.TrimSpace(priceID)
	if normalizedPriceID == "" {
		return ""
	}
	if _, hasPlanGrantDefinition := resolver.planGrantByPriceID[normalizedPriceID]; hasPlanGrantDefinition {
		return paddlePurchaseKindSubscription
	}
	if _, hasPackGrantDefinition := resolver.packGrantByPriceID[normalizedPriceID]; hasPackGrantDefinition {
		return paddlePurchaseKindTopUpPack
	}
	return ""
}

func (resolver *paddleWebhookGrantResolver) resolveUserEmail(
	ctx context.Context,
	payload paddleTransactionCompletedWebhookData,
) (string, error) {
	userEmail := webhookMetadataValue(payload.CustomData, paddleMetadataUserEmailKey)
	if userEmail != "" {
		return userEmail, nil
	}
	userEmail = resolvePaddleCustomerEmail(payload.Customer)
	if userEmail != "" {
		return userEmail, nil
	}
	customerID := strings.TrimSpace(payload.CustomerID)
	if customerID == "" {
		return "", ErrWebhookGrantMetadataInvalid
	}
	if resolver.customerEmailResolver == nil {
		return "", ErrWebhookGrantMetadataInvalid
	}
	resolvedCustomerEmail, customerEmailErr := resolver.customerEmailResolver.ResolveCustomerEmail(ctx, customerID)
	if customerEmailErr != nil {
		return "", ErrWebhookGrantMetadataInvalid
	}
	return strings.TrimSpace(resolvedCustomerEmail), nil
}

func resolvePaddleCustomerEmail(customer paddleTransactionCompletedCustomer) string {
	customerEmail := strings.TrimSpace(customer.Email)
	if customerEmail != "" {
		return customerEmail
	}
	return strings.TrimSpace(customer.EmailAddress)
}

func isGrantablePaddleTransactionStatus(eventType string, rawStatus string) bool {
	normalizedEventType := strings.TrimSpace(eventType)
	if normalizedEventType == paddleEventTypeTransactionCompleted || normalizedEventType == paddleEventTypeTransactionPaid {
		return true
	}
	if normalizedEventType != paddleEventTypeTransactionUpdated {
		return false
	}
	normalizedStatus := strings.ToLower(strings.TrimSpace(rawStatus))
	return normalizedStatus == paddleTransactionStatusPaid || normalizedStatus == paddleTransactionStatusCompleted
}

type paddleTransactionCompletedWebhookPayload struct {
	Data paddleTransactionCompletedWebhookData `json:"data"`
}

type paddleTransactionCompletedWebhookData struct {
	ID             string                                `json:"id"`
	Status         string                                `json:"status"`
	CreatedAt      string                                `json:"created_at"`
	UpdatedAt      string                                `json:"updated_at"`
	BilledAt       string                                `json:"billed_at"`
	CompletedAt    string                                `json:"completed_at"`
	SubscriptionID string                                `json:"subscription_id"`
	CustomerID     string                                `json:"customer_id"`
	Customer       paddleTransactionCompletedCustomer    `json:"customer"`
	CustomData     map[string]interface{}                `json:"custom_data"`
	Items          []paddleTransactionCompletedLineItem  `json:"items"`
	Details        paddleTransactionCompletedLineDetails `json:"details"`
}

type paddleTransactionCompletedCustomer struct {
	Email        string `json:"email"`
	EmailAddress string `json:"email_address"`
}

type paddleTransactionCompletedLineDetails struct {
	LineItems []paddleTransactionCompletedLineItem `json:"line_items"`
}

type paddleTransactionCompletedLineItem struct {
	ID      string                                  `json:"id"`
	PriceID string                                  `json:"price_id"`
	Price   paddleTransactionCompletedLineItemPrice `json:"price"`
}

type paddleTransactionCompletedLineItemPrice struct {
	ID string `json:"id"`
}

func webhookMetadataValue(metadata map[string]interface{}, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	rawValue, hasValue := metadata[key]
	if !hasValue || rawValue == nil {
		return ""
	}
	switch typedValue := rawValue.(type) {
	case string:
		return strings.TrimSpace(typedValue)
	case float64:
		if typedValue == math.Trunc(typedValue) {
			return strconv.FormatInt(int64(typedValue), 10)
		}
		return strconv.FormatFloat(typedValue, 'f', -1, 64)
	case bool:
		if typedValue {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typedValue))
	}
}

func parsePackCreditsFromMetadata(metadata map[string]interface{}) (int64, error) {
	rawCreditsValue := webhookMetadataValue(metadata, paddleMetadataPackCreditsKey)
	if rawCreditsValue == "" {
		return 0, nil
	}
	packCredits, parseErr := strconv.ParseInt(rawCreditsValue, 10, 64)
	if parseErr != nil || packCredits <= 0 {
		return 0, ErrWebhookGrantMetadataInvalid
	}
	return packCredits, nil
}

func resolvePaddleTransactionPriceID(data paddleTransactionCompletedWebhookData) string {
	for _, lineItem := range data.Items {
		priceID := strings.TrimSpace(lineItem.PriceID)
		if priceID != "" {
			return priceID
		}
		priceID = strings.TrimSpace(lineItem.Price.ID)
		if priceID != "" {
			return priceID
		}
	}
	for _, lineItem := range data.Details.LineItems {
		priceID := strings.TrimSpace(lineItem.PriceID)
		if priceID != "" {
			return priceID
		}
		priceID = strings.TrimSpace(lineItem.Price.ID)
		if priceID != "" {
			return priceID
		}
	}
	return ""
}
