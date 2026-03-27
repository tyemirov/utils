package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	stripeWebhookSignatureHeaderName = "Stripe-Signature"

	stripeMetadataUserEmailKey    = "poodle_scanner_user_email"
	stripeMetadataPurchaseKindKey = "poodle_scanner_purchase_kind"
	stripeMetadataPlanCodeKey     = "poodle_scanner_plan_code"
	stripeMetadataPackCodeKey     = "poodle_scanner_pack_code"
	stripeMetadataPackCreditsKey  = "poodle_scanner_pack_credits"
	stripeMetadataPriceIDKey      = "poodle_scanner_price_id"

	stripePurchaseKindSubscription = PurchaseKindSubscription
	stripePurchaseKindTopUpPack    = PurchaseKindTopUpPack

	stripePlanProLabel         = "Subscription Pro"
	stripePlanPlusLabel        = "Subscription Plus"
	stripeBillingPeriodMonthly = "monthly"
	stripeBillingPeriodOneTime = "one-time"
)

var (
	ErrStripeProviderVerifierUnavailable   = errors.New("billing.stripe.provider.verifier.unavailable")
	ErrStripeProviderAPIKeyEmpty           = errors.New("billing.stripe.provider.api_key.empty")
	ErrStripeProviderClientTokenEmpty      = errors.New("billing.stripe.provider.client_token.empty")
	ErrStripeProviderURLInvalid            = errors.New("billing.stripe.provider.url.invalid")
	ErrStripeProviderPriceIDEmpty          = errors.New("billing.stripe.provider.price_id.empty")
	ErrStripeProviderPlanCreditsMissing    = errors.New("billing.stripe.provider.plan_credits.missing")
	ErrStripeProviderPlanCreditsInvalid    = errors.New("billing.stripe.provider.plan_credits.invalid")
	ErrStripeProviderPlanPriceMissing      = errors.New("billing.stripe.provider.plan_price.missing")
	ErrStripeProviderPlanPriceInvalid      = errors.New("billing.stripe.provider.plan_price.invalid")
	ErrStripeProviderPackCreditsMissing    = errors.New("billing.stripe.provider.pack_credits.missing")
	ErrStripeProviderPackCreditsInvalid    = errors.New("billing.stripe.provider.pack_credits.invalid")
	ErrStripeProviderPackPriceMissing      = errors.New("billing.stripe.provider.pack_price.missing")
	ErrStripeProviderPackPriceInvalid      = errors.New("billing.stripe.provider.pack_price.invalid")
	ErrStripeProviderPackPriceIDMissing    = errors.New("billing.stripe.provider.pack_price_id.missing")
	ErrStripeProviderPriceRecurringInvalid = errors.New("billing.stripe.provider.price.recurring.invalid")
	ErrStripeProviderPriceOneOffInvalid    = errors.New("billing.stripe.provider.price.one_off.invalid")
	ErrStripeProviderPriceAmountMismatch   = errors.New("billing.stripe.provider.price.amount.mismatch")
	ErrStripeWebhookPayloadInvalid         = errors.New("billing.stripe.webhook.payload.invalid")
	ErrStripeProviderClientUnavailable     = errors.New("billing.stripe.provider.client.unavailable")
)

const (
	stripeEventTypeCheckoutSessionCompleted             = "checkout.session.completed"
	stripeEventTypeCheckoutSessionAsyncPaymentSucceeded = "checkout.session.async_payment_succeeded"
	stripeEventTypeCheckoutSessionAsyncPaymentFailed    = "checkout.session.async_payment_failed"
	stripeEventTypeCheckoutSessionExpired               = "checkout.session.expired"
	stripeEventTypeCheckoutSessionPending               = "checkout.session.pending"
	stripeEventTypeSubscriptionCreated                  = "customer.subscription.created"
	stripeEventTypeSubscriptionUpdated                  = "customer.subscription.updated"
	stripeEventTypeSubscriptionDeleted                  = "customer.subscription.deleted"

	stripeCheckoutStatusComplete      = "complete"
	stripeCheckoutPaymentStatusPaid   = "paid"
	stripeCheckoutModeSubscriptionRaw = "subscription"
	stripeCheckoutModePaymentRaw      = "payment"

	stripeSubscriptionStatusActive            = "active"
	stripeSubscriptionStatusTrialing          = "trialing"
	stripeSubscriptionStatusPaused            = "paused"
	stripeSubscriptionStatusCanceled          = "canceled"
	stripeSubscriptionStatusIncomplete        = "incomplete"
	stripeSubscriptionStatusIncompleteExpired = "incomplete_expired"
	stripeSubscriptionStatusPastDue           = "past_due"
	stripeSubscriptionStatusUnpaid            = "unpaid"
)

type StripeProviderSettings struct {
	Environment                string
	APIKey                     string
	ClientToken                string
	CheckoutSuccessURL         string
	CheckoutCancelURL          string
	PortalReturnURL            string
	ProMonthlyPriceID          string
	PlusMonthlyPriceID         string
	SubscriptionMonthlyCredits map[string]int64
	SubscriptionMonthlyPrices  map[string]int64
	TopUpPackPriceIDs          map[string]string
	TopUpPackCredits           map[string]int64
	TopUpPackPrices            map[string]int64
}

type stripeWebhookEnvelope struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Created int64  `json:"created"`
}

type stripePlanDefinition struct {
	Plan       SubscriptionPlan
	PriceID    string
	PriceCents int64
}

type stripePackDefinition struct {
	Pack       TopUpPack
	PriceID    string
	PriceCents int64
}

type stripeSubscriptionSyncEvent struct {
	WebhookEvent
	isActive bool
}

type StripeSignatureVerifier interface {
	Verify(signatureHeader string, payload []byte) error
}

type stripeCommerceClient interface {
	FindCustomerID(context.Context, string) (string, error)
	ResolveCustomerID(context.Context, string) (string, error)
	ResolveCustomerEmail(context.Context, string) (string, error)
	CreateCheckoutSession(context.Context, stripeCheckoutSessionInput) (string, error)
	GetCheckoutSession(context.Context, string) (stripeCheckoutSessionWebhookData, error)
	CreateCustomerPortalURL(context.Context, stripePortalSessionInput) (string, error)
	GetPrice(context.Context, string) (stripePriceResponse, error)
	ListSubscriptions(context.Context, string) ([]stripeSubscriptionWebhookData, error)
}

type StripeProvider struct {
	environment        string
	clientToken        string
	checkoutSuccessURL string
	checkoutCancelURL  string
	portalReturnURL    string
	verifier           StripeSignatureVerifier
	client             stripeCommerceClient
	plans              map[string]stripePlanDefinition
	packs              map[string]stripePackDefinition
}

func NewStripeProvider(
	settings StripeProviderSettings,
	verifier StripeSignatureVerifier,
	client stripeCommerceClient,
) (*StripeProvider, error) {
	if verifier == nil {
		return nil, ErrStripeProviderVerifierUnavailable
	}

	normalizedAPIKey := strings.TrimSpace(settings.APIKey)
	if normalizedAPIKey == "" {
		return nil, ErrStripeProviderAPIKeyEmpty
	}
	normalizedClientToken := strings.TrimSpace(settings.ClientToken)
	if normalizedClientToken == "" {
		return nil, ErrStripeProviderClientTokenEmpty
	}
	normalizedCheckoutSuccessURL, checkoutSuccessURLErr := normalizeStripeProviderURL(
		"checkout_success_url",
		settings.CheckoutSuccessURL,
	)
	if checkoutSuccessURLErr != nil {
		return nil, checkoutSuccessURLErr
	}
	normalizedCheckoutCancelURL, checkoutCancelURLErr := normalizeStripeProviderURL(
		"checkout_cancel_url",
		settings.CheckoutCancelURL,
	)
	if checkoutCancelURLErr != nil {
		return nil, checkoutCancelURLErr
	}
	normalizedPortalReturnURL, portalReturnURLErr := normalizeStripeProviderURL(
		"portal_return_url",
		settings.PortalReturnURL,
	)
	if portalReturnURLErr != nil {
		return nil, portalReturnURLErr
	}

	planCreditsByCode := make(map[string]int64, len(settings.SubscriptionMonthlyCredits))
	for rawPlanCode, rawPlanCredits := range settings.SubscriptionMonthlyCredits {
		normalizedPlanCode := strings.ToLower(strings.TrimSpace(rawPlanCode))
		if normalizedPlanCode == "" {
			continue
		}
		if rawPlanCredits <= 0 {
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPlanCreditsInvalid, normalizedPlanCode)
		}
		planCreditsByCode[normalizedPlanCode] = rawPlanCredits
	}
	planPricesByCode := make(map[string]int64, len(settings.SubscriptionMonthlyPrices))
	for rawPlanCode, rawPlanPrice := range settings.SubscriptionMonthlyPrices {
		normalizedPlanCode := strings.ToLower(strings.TrimSpace(rawPlanCode))
		if normalizedPlanCode == "" {
			continue
		}
		if rawPlanPrice <= 0 {
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPlanPriceInvalid, normalizedPlanCode)
		}
		planPricesByCode[normalizedPlanCode] = rawPlanPrice
	}
	planDefinitions := map[string]stripePlanDefinition{
		PlanCodePro: {
			Plan: SubscriptionPlan{
				Code:          PlanCodePro,
				Label:         stripePlanProLabel,
				BillingPeriod: stripeBillingPeriodMonthly,
			},
			PriceID: strings.TrimSpace(settings.ProMonthlyPriceID),
		},
		PlanCodePlus: {
			Plan: SubscriptionPlan{
				Code:          PlanCodePlus,
				Label:         stripePlanPlusLabel,
				BillingPeriod: stripeBillingPeriodMonthly,
			},
			PriceID: strings.TrimSpace(settings.PlusMonthlyPriceID),
		},
	}
	for planCode, definition := range planDefinitions {
		if definition.PriceID == "" {
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPriceIDEmpty, planCode)
		}
		planCredits, hasPlanCredits := planCreditsByCode[planCode]
		if !hasPlanCredits {
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPlanCreditsMissing, planCode)
		}
		definition.Plan.MonthlyCredits = planCredits
		planPrice, hasPlanPrice := planPricesByCode[planCode]
		if !hasPlanPrice {
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPlanPriceMissing, planCode)
		}
		definition.Plan.PriceDisplay = formatUSDPriceCents(planPrice)
		definition.PriceCents = planPrice
		planDefinitions[planCode] = definition
	}

	packCreditsByCode := make(map[string]int64, len(settings.TopUpPackCredits))
	for rawPackCode, rawPackCredits := range settings.TopUpPackCredits {
		normalizedPackCode := NormalizePackCode(rawPackCode)
		if normalizedPackCode == "" {
			continue
		}
		if rawPackCredits <= 0 {
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPackCreditsInvalid, normalizedPackCode)
		}
		packCreditsByCode[normalizedPackCode] = rawPackCredits
	}
	packPricesByCode := make(map[string]int64, len(settings.TopUpPackPrices))
	for rawPackCode, rawPackPrice := range settings.TopUpPackPrices {
		normalizedPackCode := NormalizePackCode(rawPackCode)
		if normalizedPackCode == "" {
			continue
		}
		if rawPackPrice <= 0 {
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPackPriceInvalid, normalizedPackCode)
		}
		packPricesByCode[normalizedPackCode] = rawPackPrice
	}

	packDefinitions := make(map[string]stripePackDefinition, len(settings.TopUpPackPriceIDs))
	for rawPackCode, rawPriceID := range settings.TopUpPackPriceIDs {
		normalizedPackCode := NormalizePackCode(rawPackCode)
		if normalizedPackCode == "" {
			continue
		}
		normalizedPriceID := strings.TrimSpace(rawPriceID)
		if normalizedPriceID == "" {
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPriceIDEmpty, normalizedPackCode)
		}
		packCredits, hasPackCredits := packCreditsByCode[normalizedPackCode]
		if !hasPackCredits {
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPackCreditsMissing, normalizedPackCode)
		}
		packPrice, hasPackPrice := packPricesByCode[normalizedPackCode]
		if !hasPackPrice {
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPackPriceMissing, normalizedPackCode)
		}
		packLabel := PackLabelForCode(normalizedPackCode)
		if packLabel == "" {
			packLabel = toTitle(normalizedPackCode)
		}
		packDefinitions[normalizedPackCode] = stripePackDefinition{
			Pack: TopUpPack{
				Code:          normalizedPackCode,
				Label:         packLabel,
				Credits:       packCredits,
				PriceDisplay:  formatUSDPriceCents(packPrice),
				BillingPeriod: stripeBillingPeriodOneTime,
			},
			PriceID:    normalizedPriceID,
			PriceCents: packPrice,
		}
	}
	for normalizedPackCode := range packCreditsByCode {
		if _, hasPackDefinition := packDefinitions[normalizedPackCode]; !hasPackDefinition {
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPackPriceIDMissing, normalizedPackCode)
		}
	}

	resolvedClient := client
	if resolvedClient == nil {
		clientInstance, _ := newStripeAPIClient(normalizedAPIKey, nil)
		resolvedClient = clientInstance
	}

	return &StripeProvider{
		environment:        strings.ToLower(strings.TrimSpace(settings.Environment)),
		clientToken:        normalizedClientToken,
		checkoutSuccessURL: normalizedCheckoutSuccessURL,
		checkoutCancelURL:  normalizedCheckoutCancelURL,
		portalReturnURL:    normalizedPortalReturnURL,
		verifier:           verifier,
		client:             resolvedClient,
		plans:              planDefinitions,
		packs:              packDefinitions,
	}, nil
}

func normalizeStripeProviderURL(field string, rawURL string) (string, error) {
	normalizedURL := strings.TrimSpace(rawURL)
	if normalizedURL == "" {
		return "", fmt.Errorf("%w: %s", ErrStripeProviderURLInvalid, field)
	}
	parsedURL, parseErr := url.Parse(normalizedURL)
	if parseErr != nil || strings.TrimSpace(parsedURL.Host) == "" {
		return "", fmt.Errorf("%w: %s", ErrStripeProviderURLInvalid, field)
	}
	if parsedURL.Scheme != "https" && parsedURL.Scheme != "http" {
		return "", fmt.Errorf("%w: %s", ErrStripeProviderURLInvalid, field)
	}
	return parsedURL.String(), nil
}

func (provider *StripeProvider) Code() string {
	return ProviderCodeStripe
}

func (provider *StripeProvider) ResolveCheckoutEventStatus(eventType string) CheckoutEventStatus {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case stripeEventTypeCheckoutSessionCompleted, stripeEventTypeCheckoutSessionAsyncPaymentSucceeded:
		return CheckoutEventStatusSucceeded
	case stripeEventTypeCheckoutSessionPending:
		return CheckoutEventStatusPending
	case stripeEventTypeCheckoutSessionAsyncPaymentFailed:
		return CheckoutEventStatusFailed
	case stripeEventTypeCheckoutSessionExpired:
		return CheckoutEventStatusExpired
	default:
		return CheckoutEventStatusUnknown
	}
}

func (provider *StripeProvider) SignatureHeaderName() string {
	return stripeWebhookSignatureHeaderName
}

func (provider *StripeProvider) VerifySignature(signatureHeader string, payload []byte) error {
	if provider == nil || provider.verifier == nil {
		return ErrStripeProviderVerifierUnavailable
	}
	return provider.verifier.Verify(signatureHeader, payload)
}

func (provider *StripeProvider) ParseWebhookEvent(payload []byte) (WebhookEventMetadata, error) {
	envelope := stripeWebhookEnvelope{}
	if decodeErr := json.Unmarshal(payload, &envelope); decodeErr != nil {
		return WebhookEventMetadata{}, ErrStripeWebhookPayloadInvalid
	}
	eventID := strings.TrimSpace(envelope.ID)
	eventType := strings.TrimSpace(envelope.Type)
	if eventID == "" || eventType == "" {
		return WebhookEventMetadata{}, ErrStripeWebhookPayloadInvalid
	}
	occurredAt := parseStripeUnixTimestamp(envelope.Created)
	if occurredAt.IsZero() {
		return WebhookEventMetadata{}, ErrStripeWebhookPayloadInvalid
	}
	return WebhookEventMetadata{
		EventID:    eventID,
		EventType:  eventType,
		OccurredAt: occurredAt,
	}, nil
}

func (provider *StripeProvider) SubscriptionPlans() []SubscriptionPlan {
	if provider == nil || len(provider.plans) == 0 {
		return []SubscriptionPlan{}
	}
	planCodes := make([]string, 0, len(provider.plans))
	for planCode := range provider.plans {
		planCodes = append(planCodes, planCode)
	}
	sort.Strings(planCodes)
	plans := make([]SubscriptionPlan, 0, len(planCodes))
	for _, planCode := range planCodes {
		plans = append(plans, provider.plans[planCode].Plan)
	}
	return plans
}

func (provider *StripeProvider) TopUpPacks() []TopUpPack {
	if provider == nil || len(provider.packs) == 0 {
		return []TopUpPack{}
	}
	packCodes := make([]string, 0, len(provider.packs))
	for packCode := range provider.packs {
		packCodes = append(packCodes, packCode)
	}
	sort.Strings(packCodes)
	packs := make([]TopUpPack, 0, len(packCodes))
	for _, packCode := range packCodes {
		packs = append(packs, provider.packs[packCode].Pack)
	}
	return packs
}

func (provider *StripeProvider) PublicConfig() PublicConfig {
	if provider == nil {
		return PublicConfig{}
	}
	return PublicConfig{
		ProviderCode: provider.Code(),
		Environment:  provider.environment,
		ClientToken:  provider.clientToken,
	}
}

func (provider *StripeProvider) BuildUserSyncEvents(ctx context.Context, userEmail string) ([]WebhookEvent, error) {
	if provider == nil || provider.client == nil {
		return nil, ErrStripeProviderClientUnavailable
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return nil, ErrBillingUserEmailInvalid
	}
	customerID, customerIDErr := provider.client.FindCustomerID(ctx, normalizedUserEmail)
	if customerIDErr != nil {
		return nil, fmt.Errorf("billing.stripe.sync.customer.find: %w", customerIDErr)
	}
	now := time.Now().UTC()
	if strings.TrimSpace(customerID) == "" {
		event, eventErr := buildStripeInactiveSyncEvent(normalizedUserEmail, now)
		if eventErr != nil {
			return nil, eventErr
		}
		return []WebhookEvent{event}, nil
	}
	subscriptions, subscriptionsErr := provider.client.ListSubscriptions(ctx, customerID)
	if subscriptionsErr != nil {
		if errors.Is(subscriptionsErr, ErrStripeAPICustomerNotFound) {
			event, eventErr := buildStripeInactiveSyncEvent(normalizedUserEmail, now)
			if eventErr != nil {
				return nil, eventErr
			}
			return []WebhookEvent{event}, nil
		}
		return nil, fmt.Errorf("billing.stripe.sync.subscription.list: %w", subscriptionsErr)
	}
	return buildStripeSyncSubscriptionEvents(normalizedUserEmail, subscriptions, now)
}

func buildStripeSyncSubscriptionEvents(
	normalizedUserEmail string,
	subscriptions []stripeSubscriptionWebhookData,
	now time.Time,
) ([]WebhookEvent, error) {
	if len(subscriptions) == 0 {
		event, eventErr := buildStripeInactiveSyncEvent(normalizedUserEmail, now)
		if eventErr != nil {
			return nil, eventErr
		}
		return []WebhookEvent{event}, nil
	}
	subscriptionEvents := make([]stripeSubscriptionSyncEvent, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		subscriptionID := strings.TrimSpace(subscription.ID)
		if subscriptionID == "" {
			continue
		}
		payloadBytes, payloadErr := jsonMarshalFunc(stripeSubscriptionWebhookPayload{
			Data: stripeSubscriptionWebhookPayloadData{
				Object: subscription,
			},
		})
		if payloadErr != nil {
			return nil, fmt.Errorf("billing.stripe.sync.subscription.payload: %w", payloadErr)
		}
		occurredAt := parseStripeUnixTimestamp(subscription.CreatedAt)
		if occurredAt.IsZero() {
			return nil, ErrStripeWebhookPayloadInvalid
		}
		resolvedStatus := resolveStripeSubscriptionState(stripeEventTypeSubscriptionUpdated, subscription.Status)
		subscriptionEvents = append(subscriptionEvents, stripeSubscriptionSyncEvent{
			WebhookEvent: WebhookEvent{
				ProviderCode: ProviderCodeStripe,
				EventID: fmt.Sprintf(
					"sync:subscription:%s:%s",
					subscriptionID,
					strings.ToLower(strings.TrimSpace(subscription.Status)),
				),
				EventType:  stripeEventTypeSubscriptionUpdated,
				OccurredAt: occurredAt,
				Payload:    payloadBytes,
			},
			isActive: resolvedStatus == subscriptionStatusActive,
		})
	}
	if len(subscriptionEvents) == 0 {
		event, eventErr := buildStripeInactiveSyncEvent(normalizedUserEmail, now)
		if eventErr != nil {
			return nil, eventErr
		}
		return []WebhookEvent{event}, nil
	}
	sort.SliceStable(subscriptionEvents, func(leftIndex int, rightIndex int) bool {
		if subscriptionEvents[leftIndex].isActive != subscriptionEvents[rightIndex].isActive {
			return !subscriptionEvents[leftIndex].isActive
		}
		leftOccurredAt := subscriptionEvents[leftIndex].OccurredAt
		rightOccurredAt := subscriptionEvents[rightIndex].OccurredAt
		if leftOccurredAt.Equal(rightOccurredAt) {
			return subscriptionEvents[leftIndex].EventID < subscriptionEvents[rightIndex].EventID
		}
		return leftOccurredAt.Before(rightOccurredAt)
	})
	resolvedEvents := make([]WebhookEvent, 0, len(subscriptionEvents))
	for _, subscriptionEvent := range subscriptionEvents {
		resolvedEvents = append(resolvedEvents, subscriptionEvent.WebhookEvent)
	}
	return resolvedEvents, nil
}

func buildStripeInactiveSyncEvent(normalizedUserEmail string, occurredAt time.Time) (WebhookEvent, error) {
	payloadBytes, payloadErr := jsonMarshalFunc(stripeSubscriptionWebhookPayload{
		Data: stripeSubscriptionWebhookPayloadData{
			Object: stripeSubscriptionWebhookData{
				Status: stripeSubscriptionStatusCanceled,
				Metadata: map[string]string{
					stripeMetadataUserEmailKey: normalizedUserEmail,
				},
				CreatedAt: occurredAt.Unix(),
			},
		},
	})
	if payloadErr != nil {
		return WebhookEvent{}, fmt.Errorf("billing.stripe.sync.subscription.payload: %w", payloadErr)
	}
	return WebhookEvent{
		ProviderCode: ProviderCodeStripe,
		EventID:      fmt.Sprintf("sync:subscription:none:%d", occurredAt.UnixNano()),
		EventType:    stripeEventTypeSubscriptionUpdated,
		OccurredAt:   occurredAt,
		Payload:      payloadBytes,
	}, nil
}

func (provider *StripeProvider) ValidateCatalog(ctx context.Context) error {
	if provider == nil || provider.client == nil {
		return ErrStripeProviderClientUnavailable
	}
	for planCode, planDefinition := range provider.plans {
		priceResponse, priceErr := provider.client.GetPrice(ctx, planDefinition.PriceID)
		if priceErr != nil {
			return fmt.Errorf("billing.stripe.catalog.price.%s: %w", planCode, priceErr)
		}
		if !isStripeRecurringMonthlyPrice(priceResponse) {
			return fmt.Errorf("%w: %s", ErrStripeProviderPriceRecurringInvalid, planCode)
		}
		if priceResponse.UnitAmount != planDefinition.PriceCents {
			return fmt.Errorf(
				"%w: plan=%s price_id=%s expected=%d actual=%d",
				ErrStripeProviderPriceAmountMismatch,
				planCode,
				planDefinition.PriceID,
				planDefinition.PriceCents,
				priceResponse.UnitAmount,
			)
		}
	}
	for packCode, packDefinition := range provider.packs {
		priceResponse, priceErr := provider.client.GetPrice(ctx, packDefinition.PriceID)
		if priceErr != nil {
			return fmt.Errorf("billing.stripe.catalog.price.%s: %w", packCode, priceErr)
		}
		if !isStripeOneOffPrice(priceResponse) {
			return fmt.Errorf("%w: %s", ErrStripeProviderPriceOneOffInvalid, packCode)
		}
		if priceResponse.UnitAmount != packDefinition.PriceCents {
			return fmt.Errorf(
				"%w: pack=%s price_id=%s expected=%d actual=%d",
				ErrStripeProviderPriceAmountMismatch,
				packCode,
				packDefinition.PriceID,
				packDefinition.PriceCents,
				priceResponse.UnitAmount,
			)
		}
	}
	return nil
}

func (provider *StripeProvider) CreateSubscriptionCheckout(
	ctx context.Context,
	userEmail string,
	planCode string,
) (CheckoutSession, error) {
	if provider == nil || provider.client == nil {
		return CheckoutSession{}, ErrStripeProviderClientUnavailable
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return CheckoutSession{}, ErrBillingUserEmailInvalid
	}
	normalizedPlanCode := strings.ToLower(strings.TrimSpace(planCode))
	planDefinition, hasPlanDefinition := provider.plans[normalizedPlanCode]
	if !hasPlanDefinition {
		return CheckoutSession{}, ErrBillingPlanUnsupported
	}

	customerID, customerIDErr := provider.client.ResolveCustomerID(ctx, normalizedUserEmail)
	if customerIDErr != nil {
		return CheckoutSession{}, fmt.Errorf("billing.stripe.customer.resolve: %w", customerIDErr)
	}
	checkoutSessionID, checkoutSessionErr := provider.client.CreateCheckoutSession(ctx, stripeCheckoutSessionInput{
		CustomerID: customerID,
		PriceID:    planDefinition.PriceID,
		Mode:       stripeCheckoutModeSubscription,
		SuccessURL: provider.checkoutSuccessURL,
		CancelURL:  provider.checkoutCancelURL,
		Metadata: formatStripeMetadata(map[string]string{
			stripeMetadataUserEmailKey:    normalizedUserEmail,
			stripeMetadataPurchaseKindKey: stripePurchaseKindSubscription,
			stripeMetadataPlanCodeKey:     planDefinition.Plan.Code,
			stripeMetadataPriceIDKey:      planDefinition.PriceID,
		}),
	})
	if checkoutSessionErr != nil {
		return CheckoutSession{}, fmt.Errorf("billing.stripe.checkout.subscription: %w", checkoutSessionErr)
	}
	return CheckoutSession{
		ProviderCode:  provider.Code(),
		TransactionID: checkoutSessionID,
		CheckoutMode:  CheckoutModeOverlay,
	}, nil
}

func (provider *StripeProvider) CreateTopUpCheckout(
	ctx context.Context,
	userEmail string,
	packCode string,
) (CheckoutSession, error) {
	if provider == nil || provider.client == nil {
		return CheckoutSession{}, ErrStripeProviderClientUnavailable
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return CheckoutSession{}, ErrBillingUserEmailInvalid
	}
	normalizedPackCode := NormalizePackCode(packCode)
	packDefinition, hasPackDefinition := provider.packs[normalizedPackCode]
	if !hasPackDefinition {
		return CheckoutSession{}, ErrBillingTopUpPackUnknown
	}

	customerID, customerIDErr := provider.client.ResolveCustomerID(ctx, normalizedUserEmail)
	if customerIDErr != nil {
		return CheckoutSession{}, fmt.Errorf("billing.stripe.customer.resolve: %w", customerIDErr)
	}
	checkoutSessionID, checkoutSessionErr := provider.client.CreateCheckoutSession(ctx, stripeCheckoutSessionInput{
		CustomerID: customerID,
		PriceID:    packDefinition.PriceID,
		Mode:       stripeCheckoutModePayment,
		SuccessURL: provider.checkoutSuccessURL,
		CancelURL:  provider.checkoutCancelURL,
		Metadata: formatStripeMetadata(map[string]string{
			stripeMetadataUserEmailKey:    normalizedUserEmail,
			stripeMetadataPurchaseKindKey: stripePurchaseKindTopUpPack,
			stripeMetadataPackCodeKey:     packDefinition.Pack.Code,
			stripeMetadataPackCreditsKey:  strconv.FormatInt(packDefinition.Pack.Credits, 10),
			stripeMetadataPriceIDKey:      packDefinition.PriceID,
		}),
	})
	if checkoutSessionErr != nil {
		return CheckoutSession{}, fmt.Errorf("billing.stripe.checkout.credits: %w", checkoutSessionErr)
	}
	return CheckoutSession{
		ProviderCode:  provider.Code(),
		TransactionID: checkoutSessionID,
		CheckoutMode:  CheckoutModeOverlay,
	}, nil
}

func (provider *StripeProvider) CreateCustomerPortalSession(
	ctx context.Context,
	userEmail string,
) (PortalSession, error) {
	if provider == nil || provider.client == nil {
		return PortalSession{}, ErrStripeProviderClientUnavailable
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return PortalSession{}, ErrBillingUserEmailInvalid
	}
	customerID, customerIDErr := provider.client.ResolveCustomerID(ctx, normalizedUserEmail)
	if customerIDErr != nil {
		return PortalSession{}, fmt.Errorf("billing.stripe.customer.resolve: %w", customerIDErr)
	}
	portalURL, portalURLErr := provider.client.CreateCustomerPortalURL(ctx, stripePortalSessionInput{
		CustomerID: customerID,
		ReturnURL:  provider.portalReturnURL,
	})
	if portalURLErr != nil {
		return PortalSession{}, fmt.Errorf("billing.stripe.portal.create: %w", portalURLErr)
	}
	return PortalSession{
		ProviderCode: provider.Code(),
		URL:          portalURL,
	}, nil
}

func (provider *StripeProvider) BuildCheckoutReconcileEvent(
	ctx context.Context,
	transactionID string,
) (WebhookEvent, string, error) {
	if provider == nil || provider.client == nil {
		return WebhookEvent{}, "", ErrStripeProviderClientUnavailable
	}
	normalizedTransactionID := strings.TrimSpace(transactionID)
	if normalizedTransactionID == "" {
		return WebhookEvent{}, "", ErrStripeAPICheckoutSessionNotFound
	}
	checkoutSession, checkoutSessionErr := provider.client.GetCheckoutSession(ctx, normalizedTransactionID)
	if checkoutSessionErr != nil {
		return WebhookEvent{}, "", fmt.Errorf("billing.stripe.checkout.session.get: %w", checkoutSessionErr)
	}
	checkoutUserEmail, checkoutUserEmailErr := provider.resolveCheckoutUserEmail(ctx, checkoutSession)
	if checkoutUserEmailErr != nil {
		return WebhookEvent{}, "", fmt.Errorf("billing.stripe.checkout.user_email: %w", checkoutUserEmailErr)
	}
	payloadBytes, payloadErr := jsonMarshalFunc(stripeCheckoutSessionWebhookPayload{
		Data: stripeCheckoutSessionWebhookPayloadData{
			Object: checkoutSession,
		},
	})
	if payloadErr != nil {
		return WebhookEvent{}, "", fmt.Errorf("billing.stripe.checkout.payload: %w", payloadErr)
	}
	eventType := resolveStripeCheckoutReconcileEventType(checkoutSession)
	eventID := fmt.Sprintf("reconcile:%s:%s", normalizedTransactionID, strings.ReplaceAll(eventType, ".", "_"))
	return WebhookEvent{
		ProviderCode: provider.Code(),
		EventID:      eventID,
		EventType:    eventType,
		OccurredAt:   resolveStripeCheckoutOccurredAt(checkoutSession),
		Payload:      payloadBytes,
	}, checkoutUserEmail, nil
}

func (provider *StripeProvider) resolveCheckoutUserEmail(
	ctx context.Context,
	checkoutSession stripeCheckoutSessionWebhookData,
) (string, error) {
	checkoutUserEmail := strings.ToLower(strings.TrimSpace(checkoutSession.Metadata[stripeMetadataUserEmailKey]))
	if checkoutUserEmail == "" {
		checkoutUserEmail = strings.ToLower(strings.TrimSpace(checkoutSession.CustomerEmail))
	}
	if checkoutUserEmail == "" {
		checkoutUserEmail = strings.ToLower(strings.TrimSpace(checkoutSession.CustomerDetails.Email))
	}
	if checkoutUserEmail == "" {
		customerID := strings.TrimSpace(checkoutSession.CustomerID)
		if customerID == "" {
			return "", ErrWebhookGrantMetadataInvalid
		}
		resolvedEmail, resolveErr := provider.client.ResolveCustomerEmail(ctx, customerID)
		if resolveErr != nil {
			return "", fmt.Errorf("billing.stripe.customer.email.resolve: %w", resolveErr)
		}
		checkoutUserEmail = strings.ToLower(strings.TrimSpace(resolvedEmail))
	}
	if checkoutUserEmail == "" {
		return "", ErrWebhookGrantMetadataInvalid
	}
	return checkoutUserEmail, nil
}

func resolveStripeCheckoutReconcileEventType(checkoutSession stripeCheckoutSessionWebhookData) string {
	if isStripeCheckoutSessionPaid(checkoutSession) {
		return stripeEventTypeCheckoutSessionCompleted
	}
	return stripeEventTypeCheckoutSessionPending
}

func resolveStripeCheckoutOccurredAt(checkoutSession stripeCheckoutSessionWebhookData) time.Time {
	occurredAt := parseStripeUnixTimestamp(checkoutSession.CreatedAt)
	if occurredAt.IsZero() {
		return time.Now().UTC()
	}
	return occurredAt
}

func isStripeRecurringMonthlyPrice(priceResponse stripePriceResponse) bool {
	normalizedType := strings.ToLower(strings.TrimSpace(priceResponse.Type))
	if normalizedType != "recurring" {
		return false
	}
	if priceResponse.Recurring == nil {
		return false
	}
	normalizedInterval := strings.ToLower(strings.TrimSpace(priceResponse.Recurring.Interval))
	return normalizedInterval == "month"
}

func isStripeOneOffPrice(priceResponse stripePriceResponse) bool {
	return strings.ToLower(strings.TrimSpace(priceResponse.Type)) == "one_time"
}

func (provider *StripeProvider) NewWebhookGrantResolver() (WebhookGrantResolver, error) {
	return newStripeWebhookGrantResolverFromProvider(provider)
}

func (provider *StripeProvider) NewSubscriptionStatusWebhookProcessor(
	stateRepository SubscriptionStateRepository,
) (WebhookProcessor, error) {
	return newStripeSubscriptionStatusWebhookProcessor(provider, stateRepository)
}
