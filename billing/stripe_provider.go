package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	stripeWebhookSignatureHeaderName = "Stripe-Signature"

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

// StripeProviderSettings configures a Stripe-backed provider and its plan/pack
// catalog.
type StripeProviderSettings struct {
	Environment                string
	APIKey                     string
	ClientToken                string
	CheckoutSuccessURL         string
	CheckoutCancelURL          string
	PortalReturnURL            string
	Plans                      []PlanCatalogItem
	Packs                      []PackCatalogItem
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

type stripeInspectedSubscription struct {
	payload    stripeSubscriptionWebhookData
	normalized ProviderSubscription
}

// StripeSignatureVerifier verifies Stripe webhook signatures.
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
	ListCheckoutSessions(context.Context, string) ([]stripeCheckoutSessionWebhookData, error)
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

var newStripeAPIClientFunc = newStripeAPIClient

// NewStripeProvider constructs a Stripe-backed Provider from application
// settings and a webhook signature verifier.
func NewStripeProvider(
	settings StripeProviderSettings,
	verifier StripeSignatureVerifier,
	client StripeCommerceClient,
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
	planDefinitions := make(map[string]stripePlanDefinition)
	for _, item := range buildStripePlanCatalogItems(settings) {
		normalizedPlanCode := strings.ToLower(strings.TrimSpace(item.Code))
		if normalizedPlanCode == "" {
			continue
		}
		if strings.TrimSpace(item.PriceID) == "" {
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPriceIDEmpty, normalizedPlanCode)
		}
		if item.MonthlyCredits <= 0 {
			if len(settings.Plans) == 0 {
				if _, hasConfiguredCredits := settings.SubscriptionMonthlyCredits[normalizedPlanCode]; !hasConfiguredCredits {
					return nil, fmt.Errorf("%w: %s", ErrStripeProviderPlanCreditsMissing, normalizedPlanCode)
				}
			}
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPlanCreditsInvalid, normalizedPlanCode)
		}
		if item.PriceCents <= 0 {
			if len(settings.Plans) == 0 {
				if _, hasConfiguredPrice := settings.SubscriptionMonthlyPrices[normalizedPlanCode]; !hasConfiguredPrice {
					return nil, fmt.Errorf("%w: %s", ErrStripeProviderPlanPriceMissing, normalizedPlanCode)
				}
			}
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPlanPriceInvalid, normalizedPlanCode)
		}
		planDefinitions[normalizedPlanCode] = stripePlanDefinition{
			Plan: SubscriptionPlan{
				Code:           normalizedPlanCode,
				Label:          firstNonEmptyString(item.Label, defaultStripePlanLabel(normalizedPlanCode)),
				MonthlyCredits: item.MonthlyCredits,
				PriceDisplay:   formatUSDPriceCents(item.PriceCents),
				BillingPeriod:  stripeBillingPeriodMonthly,
			},
			PriceID:    strings.TrimSpace(item.PriceID),
			PriceCents: item.PriceCents,
		}
	}

	packDefinitions := make(map[string]stripePackDefinition)
	for _, item := range buildStripePackCatalogItems(settings) {
		normalizedPackCode := NormalizePackCode(item.Code)
		if normalizedPackCode == "" {
			continue
		}
		if strings.TrimSpace(item.PriceID) == "" {
			if len(settings.Packs) == 0 {
				if _, hasConfiguredPriceID := settings.TopUpPackPriceIDs[normalizedPackCode]; !hasConfiguredPriceID {
					return nil, fmt.Errorf("%w: %s", ErrStripeProviderPackPriceIDMissing, normalizedPackCode)
				}
			}
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPriceIDEmpty, normalizedPackCode)
		}
		if item.Credits <= 0 {
			if len(settings.Packs) == 0 {
				if _, hasConfiguredCredits := settings.TopUpPackCredits[normalizedPackCode]; !hasConfiguredCredits {
					return nil, fmt.Errorf("%w: %s", ErrStripeProviderPackCreditsMissing, normalizedPackCode)
				}
			}
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPackCreditsInvalid, normalizedPackCode)
		}
		if item.PriceCents <= 0 {
			if len(settings.Packs) == 0 {
				if _, hasConfiguredPrice := settings.TopUpPackPrices[normalizedPackCode]; !hasConfiguredPrice {
					return nil, fmt.Errorf("%w: %s", ErrStripeProviderPackPriceMissing, normalizedPackCode)
				}
			}
			return nil, fmt.Errorf("%w: %s", ErrStripeProviderPackPriceInvalid, normalizedPackCode)
		}
		packDefinitions[normalizedPackCode] = stripePackDefinition{
			Pack: TopUpPack{
				Code:          normalizedPackCode,
				Label:         firstNonEmptyString(item.Label, PackLabelForCode(normalizedPackCode), toTitle(normalizedPackCode)),
				Credits:       item.Credits,
				PriceDisplay:  formatUSDPriceCents(item.PriceCents),
				BillingPeriod: stripeBillingPeriodOneTime,
			},
			PriceID:    strings.TrimSpace(item.PriceID),
			PriceCents: item.PriceCents,
		}
	}

	resolvedClient := client
	if resolvedClient == nil {
		clientInstance, clientErr := newStripeAPIClientFunc(normalizedAPIKey, nil)
		if clientErr != nil {
			return nil, clientErr
		}
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

func buildStripePlanCatalogItems(settings StripeProviderSettings) []PlanCatalogItem {
	if len(settings.Plans) > 0 {
		return append([]PlanCatalogItem(nil), settings.Plans...)
	}
	items := make([]PlanCatalogItem, 0, 2)
	appendLegacyPlan := func(code string, label string, priceID string) {
		trimmedPriceID := strings.TrimSpace(priceID)
		credits := settings.SubscriptionMonthlyCredits[code]
		priceCents := settings.SubscriptionMonthlyPrices[code]
		if trimmedPriceID == "" && credits == 0 && priceCents == 0 {
			return
		}
		items = append(items, PlanCatalogItem{
			Code:           code,
			Label:          label,
			PriceID:        trimmedPriceID,
			MonthlyCredits: credits,
			PriceCents:     priceCents,
		})
	}
	appendLegacyPlan(PlanCodePro, stripePlanProLabel, settings.ProMonthlyPriceID)
	appendLegacyPlan(PlanCodePlus, stripePlanPlusLabel, settings.PlusMonthlyPriceID)
	return items
}

func buildStripePackCatalogItems(settings StripeProviderSettings) []PackCatalogItem {
	if len(settings.Packs) > 0 {
		return append([]PackCatalogItem(nil), settings.Packs...)
	}
	packCodes := make(map[string]struct{})
	for rawPackCode := range settings.TopUpPackPriceIDs {
		packCodes[NormalizePackCode(rawPackCode)] = struct{}{}
	}
	for rawPackCode := range settings.TopUpPackCredits {
		packCodes[NormalizePackCode(rawPackCode)] = struct{}{}
	}
	for rawPackCode := range settings.TopUpPackPrices {
		packCodes[NormalizePackCode(rawPackCode)] = struct{}{}
	}
	items := make([]PackCatalogItem, 0, len(packCodes))
	for packCode := range packCodes {
		if packCode == "" {
			continue
		}
		items = append(items, PackCatalogItem{
			Code:       packCode,
			Label:      firstNonEmptyString(PackLabelForCode(packCode), toTitle(packCode)),
			PriceID:    strings.TrimSpace(settings.TopUpPackPriceIDs[packCode]),
			Credits:    settings.TopUpPackCredits[packCode],
			PriceCents: settings.TopUpPackPrices[packCode],
		})
	}
	sort.SliceStable(items, func(leftIndex int, rightIndex int) bool {
		return items[leftIndex].Code < items[rightIndex].Code
	})
	return items
}

func defaultStripePlanLabel(planCode string) string {
	switch strings.ToLower(strings.TrimSpace(planCode)) {
	case PlanCodePro:
		return stripePlanProLabel
	case PlanCodePlus:
		return stripePlanPlusLabel
	default:
		return toTitle(planCode)
	}
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
	checkoutEvents, checkoutEventsErr := provider.buildUserCheckoutSyncEvents(ctx, customerID)
	if checkoutEventsErr != nil {
		return nil, checkoutEventsErr
	}
	inspectedSubscriptions, subscriptionsErr := provider.inspectCustomerSubscriptions(ctx, customerID)
	if subscriptionsErr != nil {
		return nil, subscriptionsErr
	}
	subscriptionEvents, subscriptionEventsErr := buildStripeSyncSubscriptionEventsFromInspected(
		normalizedUserEmail,
		inspectedSubscriptions,
		now,
	)
	if subscriptionEventsErr != nil {
		return nil, subscriptionEventsErr
	}
	syncEvents := make([]WebhookEvent, 0, len(checkoutEvents)+len(subscriptionEvents))
	syncEvents = append(syncEvents, checkoutEvents...)
	syncEvents = append(syncEvents, subscriptionEvents...)
	return syncEvents, nil
}

func buildStripeSyncSubscriptionEvents(
	normalizedUserEmail string,
	subscriptions []stripeSubscriptionWebhookData,
	now time.Time,
) ([]WebhookEvent, error) {
	inspectedSubscriptions := make([]stripeInspectedSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		subscriptionID := strings.TrimSpace(subscription.ID)
		if subscriptionID == "" {
			continue
		}
		inspectedSubscriptions = append(inspectedSubscriptions, stripeInspectedSubscription{
			payload: subscription,
			normalized: normalizeProviderSubscription(ProviderSubscription{
				SubscriptionID: subscriptionID,
				Status:         resolveStripeSubscriptionState(stripeEventTypeSubscriptionUpdated, subscription.Status),
				ProviderStatus: subscription.Status,
				NextBillingAt:  resolveStripeSubscriptionNextBillingAt(subscription),
				OccurredAt:     parseStripeUnixTimestamp(subscription.CreatedAt),
			}),
		})
	}
	return buildStripeSyncSubscriptionEventsFromInspected(normalizedUserEmail, inspectedSubscriptions, now)
}

func buildStripeSyncSubscriptionEventsFromInspected(
	normalizedUserEmail string,
	inspectedSubscriptions []stripeInspectedSubscription,
	now time.Time,
) ([]WebhookEvent, error) {
	if len(inspectedSubscriptions) == 0 {
		event, eventErr := buildStripeInactiveSyncEvent(normalizedUserEmail, now)
		if eventErr != nil {
			return nil, eventErr
		}
		return []WebhookEvent{event}, nil
	}
	subscriptionEvents := make([]WebhookEvent, 0, len(inspectedSubscriptions))
	for _, subscription := range inspectedSubscriptions {
		payloadBytes, payloadErr := jsonMarshalFunc(stripeSubscriptionWebhookPayload{
			Data: stripeSubscriptionWebhookPayloadData{
				Object: subscription.payload,
			},
		})
		if payloadErr != nil {
			return nil, fmt.Errorf("billing.stripe.sync.subscription.payload: %w", payloadErr)
		}
		if subscription.normalized.SubscriptionID == "" {
			continue
		}
		if subscription.normalized.OccurredAt.IsZero() {
			return nil, ErrStripeWebhookPayloadInvalid
		}
		subscriptionEvents = append(subscriptionEvents, WebhookEvent{
			ProviderCode: ProviderCodeStripe,
			EventID: fmt.Sprintf(
				"sync:subscription:%s:%s",
				subscription.normalized.SubscriptionID,
				subscription.normalized.ProviderStatus,
			),
			EventType:  stripeEventTypeSubscriptionUpdated,
			OccurredAt: subscription.normalized.OccurredAt,
			Payload:    payloadBytes,
		})
	}
	sort.SliceStable(subscriptionEvents, func(leftIndex int, rightIndex int) bool {
		leftPayload := stripeSubscriptionWebhookPayload{}
		rightPayload := stripeSubscriptionWebhookPayload{}
		_ = json.Unmarshal(subscriptionEvents[leftIndex].Payload, &leftPayload)
		_ = json.Unmarshal(subscriptionEvents[rightIndex].Payload, &rightPayload)
		leftStatus := resolveStripeSubscriptionState(
			stripeEventTypeSubscriptionUpdated,
			leftPayload.Data.Object.Status,
		)
		rightStatus := resolveStripeSubscriptionState(
			stripeEventTypeSubscriptionUpdated,
			rightPayload.Data.Object.Status,
		)
		if leftStatus != rightStatus {
			return leftStatus != subscriptionStatusActive
		}
		leftOccurredAt := subscriptionEvents[leftIndex].OccurredAt
		rightOccurredAt := subscriptionEvents[rightIndex].OccurredAt
		if leftOccurredAt.Equal(rightOccurredAt) {
			return subscriptionEvents[leftIndex].EventID < subscriptionEvents[rightIndex].EventID
		}
		return leftOccurredAt.Before(rightOccurredAt)
	})
	return subscriptionEvents, nil
}

func (provider *StripeProvider) InspectSubscriptions(
	ctx context.Context,
	userEmail string,
) ([]ProviderSubscription, error) {
	if provider == nil || provider.client == nil {
		return nil, ErrStripeProviderClientUnavailable
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return nil, ErrBillingUserEmailInvalid
	}
	customerID, customerIDErr := provider.client.FindCustomerID(ctx, normalizedUserEmail)
	if customerIDErr != nil {
		return nil, fmt.Errorf("billing.stripe.customer.find: %w", customerIDErr)
	}
	if strings.TrimSpace(customerID) == "" {
		return []ProviderSubscription{}, nil
	}
	inspectedSubscriptions, inspectErr := provider.inspectCustomerSubscriptions(ctx, customerID)
	if inspectErr != nil {
		return nil, inspectErr
	}
	resolvedSubscriptions := make([]ProviderSubscription, 0, len(inspectedSubscriptions))
	for _, subscription := range inspectedSubscriptions {
		resolvedSubscriptions = append(resolvedSubscriptions, subscription.normalized)
	}
	return resolvedSubscriptions, nil
}

func (provider *StripeProvider) buildUserCheckoutSyncEvents(
	ctx context.Context,
	customerID string,
) ([]WebhookEvent, error) {
	checkoutSessions, checkoutSessionsErr := provider.client.ListCheckoutSessions(ctx, customerID)
	if checkoutSessionsErr != nil {
		return nil, fmt.Errorf("billing.stripe.sync.checkout.list: %w", checkoutSessionsErr)
	}
	syncEvents := make([]WebhookEvent, 0, len(checkoutSessions))
	for _, checkoutSession := range checkoutSessions {
		checkoutSessionID := strings.TrimSpace(checkoutSession.ID)
		if checkoutSessionID == "" || !isStripeCheckoutSessionPaid(checkoutSession) {
			continue
		}
		payloadBytes, payloadErr := jsonMarshalFunc(stripeCheckoutSessionWebhookPayload{
			Data: stripeCheckoutSessionWebhookPayloadData{
				Object: checkoutSession,
			},
		})
		if payloadErr != nil {
			return nil, fmt.Errorf("billing.stripe.sync.checkout.payload: %w", payloadErr)
		}
		occurredAt := parseStripeUnixTimestamp(checkoutSession.CreatedAt)
		if occurredAt.IsZero() {
			return nil, ErrStripeWebhookPayloadInvalid
		}
		syncEvents = append(syncEvents, WebhookEvent{
			ProviderCode: ProviderCodeStripe,
			EventID:      fmt.Sprintf("sync:checkout:%s:completed", checkoutSessionID),
			EventType:    stripeEventTypeCheckoutSessionCompleted,
			OccurredAt:   occurredAt,
			Payload:      payloadBytes,
		})
	}
	sort.SliceStable(syncEvents, func(leftIndex int, rightIndex int) bool {
		leftOccurredAt := syncEvents[leftIndex].OccurredAt
		rightOccurredAt := syncEvents[rightIndex].OccurredAt
		if leftOccurredAt.Equal(rightOccurredAt) {
			return syncEvents[leftIndex].EventID < syncEvents[rightIndex].EventID
		}
		return leftOccurredAt.Before(rightOccurredAt)
	})
	return syncEvents, nil
}

func buildStripeInactiveSyncEvent(normalizedUserEmail string, occurredAt time.Time) (WebhookEvent, error) {
	payloadBytes, payloadErr := jsonMarshalFunc(stripeSubscriptionWebhookPayload{
		Data: stripeSubscriptionWebhookPayloadData{
			Object: stripeSubscriptionWebhookData{
				Status: stripeSubscriptionStatusCanceled,
				Metadata: map[string]string{
					billingMetadataUserEmailKey:      normalizedUserEmail,
					stripeLegacyMetadataUserEmailKey: normalizedUserEmail,
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

func (provider *StripeProvider) inspectCustomerSubscriptions(
	ctx context.Context,
	customerID string,
) ([]stripeInspectedSubscription, error) {
	subscriptions, subscriptionsErr := provider.client.ListSubscriptions(ctx, customerID)
	if subscriptionsErr != nil {
		if errors.Is(subscriptionsErr, ErrStripeAPICustomerNotFound) {
			return []stripeInspectedSubscription{}, nil
		}
		return nil, fmt.Errorf("billing.stripe.sync.subscription.list: %w", subscriptionsErr)
	}
	planCodeByPriceID := buildStripePlanCodeByPriceID(provider.plans)
	inspectedSubscriptions := make([]stripeInspectedSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		subscriptionID := strings.TrimSpace(subscription.ID)
		if subscriptionID == "" {
			continue
		}
		planCode := strings.ToLower(
			metadataValue(
				subscription.Metadata,
				billingMetadataPlanCodeKey,
				stripeLegacyMetadataPlanCodeKey,
			),
		)
		if planCode == "" {
			priceID := resolveStripeSubscriptionPriceID(subscription)
			planCode = strings.ToLower(strings.TrimSpace(planCodeByPriceID[priceID]))
		}
		inspectedSubscriptions = append(inspectedSubscriptions, stripeInspectedSubscription{
			payload: subscription,
			normalized: normalizeProviderSubscription(ProviderSubscription{
				SubscriptionID: subscriptionID,
				PlanCode:       planCode,
				Status:         resolveStripeSubscriptionState(stripeEventTypeSubscriptionUpdated, subscription.Status),
				ProviderStatus: subscription.Status,
				NextBillingAt:  resolveStripeSubscriptionNextBillingAt(subscription),
				OccurredAt:     parseStripeUnixTimestamp(subscription.CreatedAt),
			}),
		})
	}
	return inspectedSubscriptions, nil
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
	customer CustomerContext,
	planCode string,
) (CheckoutSession, error) {
	if provider == nil || provider.client == nil {
		return CheckoutSession{}, ErrStripeProviderClientUnavailable
	}
	normalizedCustomer := NormalizeCustomerContext(customer)
	normalizedUserEmail := normalizedCustomer.Email
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
		Metadata:   formatStripeMetadata(buildCheckoutMetadata(normalizedCustomer, stripePurchaseKindSubscription, planDefinition.Plan.Code, 0, planDefinition.PriceID)),
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
	customer CustomerContext,
	packCode string,
) (CheckoutSession, error) {
	if provider == nil || provider.client == nil {
		return CheckoutSession{}, ErrStripeProviderClientUnavailable
	}
	normalizedCustomer := NormalizeCustomerContext(customer)
	normalizedUserEmail := normalizedCustomer.Email
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
		Metadata:   formatStripeMetadata(buildCheckoutMetadata(normalizedCustomer, stripePurchaseKindTopUpPack, packDefinition.Pack.Code, packDefinition.Pack.Credits, packDefinition.PriceID)),
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
	checkoutUserEmail := strings.ToLower(
		metadataValue(
			checkoutSession.Metadata,
			billingMetadataUserEmailKey,
			stripeLegacyMetadataUserEmailKey,
			crosswordLegacyMetadataUserEmailKey,
		),
	)
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
