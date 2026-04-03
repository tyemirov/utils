package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	paddleWebhookSignatureHeaderName = "Paddle-Signature"

	paddlePurchaseKindSubscription = PurchaseKindSubscription
	paddlePurchaseKindTopUpPack    = PurchaseKindTopUpPack

	paddlePlanProLabel         = "Subscription Pro"
	paddlePlanPlusLabel        = "Subscription Plus"
	paddleBillingPeriodMonthly = "monthly"
	paddleBillingPeriodOneTime = "one-time"
)

var (
	ErrPaddleProviderVerifierUnavailable   = errors.New("billing.paddle.provider.verifier.unavailable")
	ErrPaddleProviderAPIKeyEmpty           = errors.New("billing.paddle.provider.api_key.empty")
	ErrPaddleProviderClientTokenEmpty      = errors.New("billing.paddle.provider.client_token.empty")
	ErrPaddleProviderPriceIDEmpty          = errors.New("billing.paddle.provider.price_id.empty")
	ErrPaddleProviderPriceRecurringInvalid = errors.New("billing.paddle.provider.price.recurring.invalid")
	ErrPaddleProviderPriceOneOffInvalid    = errors.New("billing.paddle.provider.price.one_off.invalid")
	ErrPaddleProviderPriceAmountMismatch   = errors.New("billing.paddle.provider.price.amount.mismatch")
	ErrPaddleProviderPlanCreditsMissing    = errors.New("billing.paddle.provider.plan_credits.missing")
	ErrPaddleProviderPlanCreditsInvalid    = errors.New("billing.paddle.provider.plan_credits.invalid")
	ErrPaddleProviderPlanPriceMissing      = errors.New("billing.paddle.provider.plan_price.missing")
	ErrPaddleProviderPlanPriceInvalid      = errors.New("billing.paddle.provider.plan_price.invalid")
	ErrPaddleProviderPackCreditsMissing    = errors.New("billing.paddle.provider.pack_credits.missing")
	ErrPaddleProviderPackCreditsInvalid    = errors.New("billing.paddle.provider.pack_credits.invalid")
	ErrPaddleProviderPackPriceMissing      = errors.New("billing.paddle.provider.pack_price.missing")
	ErrPaddleProviderPackPriceInvalid      = errors.New("billing.paddle.provider.pack_price.invalid")
	ErrPaddleProviderPackPriceIDMissing    = errors.New("billing.paddle.provider.pack_price_id.missing")
	ErrPaddleWebhookPayloadInvalid         = errors.New("billing.paddle.webhook.payload.invalid")
	ErrPaddleProviderClientUnavailable     = errors.New("billing.paddle.provider.client.unavailable")
)

type PaddleProviderSettings struct {
	Environment                string
	APIBaseURL                 string
	APIKey                     string
	ClientToken                string
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

type paddleTransactionInput struct {
	CustomerID string
	PriceID    string
	Metadata   map[string]string
}

type paddlePriceDetails struct {
	ID           string
	ProductID    string
	ProductName  string
	PriceName    string
	PriceCents   int64
	BillingCycle paddlePriceBillingCycle
}

type paddlePriceBillingCycle struct {
	Interval  string
	Frequency int64
}

type paddleCommerceClient interface {
	ResolveCustomerID(context.Context, string) (string, error)
	FindCustomerIDByEmail(context.Context, string) (string, error)
	ResolveCustomerEmail(context.Context, string) (string, error)
	CreateTransaction(context.Context, paddleTransactionInput) (string, error)
	ListCustomerTransactions(context.Context, string) ([]paddleTransactionCompletedWebhookData, error)
	ListCustomerSubscriptions(context.Context, string) ([]paddleSubscriptionWebhookData, error)
	GetTransaction(context.Context, string) (paddleTransactionCompletedWebhookData, error)
	GetSubscription(context.Context, string) (paddleSubscriptionWebhookData, error)
	GetPrice(context.Context, string) (paddlePriceDetails, error)
	ListPrices(context.Context, []string) (map[string]paddlePriceDetails, error)
	CreateCustomerPortalURL(context.Context, string) (string, error)
}

type paddlePlanDefinition struct {
	Plan       SubscriptionPlan
	PriceID    string
	PriceCents int64
}

type paddlePackDefinition struct {
	Pack       TopUpPack
	PriceID    string
	PriceCents int64
}

type paddleInspectedSubscription struct {
	payload    paddleSubscriptionWebhookData
	normalized ProviderSubscription
}

type PaddleSignatureVerifier interface {
	Verify(signatureHeader string, payload []byte) error
}

type PaddleProvider struct {
	environment string
	clientToken string
	verifier    PaddleSignatureVerifier
	client      paddleCommerceClient
	plans       map[string]paddlePlanDefinition
	packs       map[string]paddlePackDefinition
}

func NewPaddleProvider(
	settings PaddleProviderSettings,
	verifier PaddleSignatureVerifier,
	client PaddleCommerceClient,
) (*PaddleProvider, error) {
	if verifier == nil {
		return nil, ErrPaddleProviderVerifierUnavailable
	}

	normalizedAPIKey := strings.TrimSpace(settings.APIKey)
	if normalizedAPIKey == "" {
		return nil, ErrPaddleProviderAPIKeyEmpty
	}
	normalizedClientToken := strings.TrimSpace(settings.ClientToken)
	if normalizedClientToken == "" {
		return nil, ErrPaddleProviderClientTokenEmpty
	}
	planDefinitions := make(map[string]paddlePlanDefinition)
	for _, item := range buildPaddlePlanCatalogItems(settings) {
		normalizedPlanCode := strings.ToLower(strings.TrimSpace(item.Code))
		if normalizedPlanCode == "" {
			continue
		}
		if strings.TrimSpace(item.PriceID) == "" {
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPriceIDEmpty, normalizedPlanCode)
		}
		if item.MonthlyCredits <= 0 {
			if len(settings.Plans) == 0 {
				if _, hasConfiguredCredits := settings.SubscriptionMonthlyCredits[normalizedPlanCode]; !hasConfiguredCredits {
					return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPlanCreditsMissing, normalizedPlanCode)
				}
			}
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPlanCreditsInvalid, normalizedPlanCode)
		}
		if item.PriceCents <= 0 {
			if len(settings.Plans) == 0 {
				if _, hasConfiguredPrice := settings.SubscriptionMonthlyPrices[normalizedPlanCode]; !hasConfiguredPrice {
					return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPlanPriceMissing, normalizedPlanCode)
				}
			}
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPlanPriceInvalid, normalizedPlanCode)
		}
		planDefinitions[normalizedPlanCode] = paddlePlanDefinition{
			Plan: SubscriptionPlan{
				Code:           normalizedPlanCode,
				Label:          firstNonEmptyString(item.Label, defaultPlanLabel(normalizedPlanCode)),
				MonthlyCredits: item.MonthlyCredits,
				PriceDisplay:   formatUSDPriceCents(item.PriceCents),
				BillingPeriod:  paddleBillingPeriodMonthly,
			},
			PriceID:    strings.TrimSpace(item.PriceID),
			PriceCents: item.PriceCents,
		}
	}

	packDefinitions := make(map[string]paddlePackDefinition)
	for _, item := range buildPaddlePackCatalogItems(settings) {
		normalizedPackCode := NormalizePackCode(item.Code)
		if normalizedPackCode == "" {
			continue
		}
		if strings.TrimSpace(item.PriceID) == "" {
			if len(settings.Packs) == 0 {
				if _, hasConfiguredPriceID := settings.TopUpPackPriceIDs[normalizedPackCode]; !hasConfiguredPriceID {
					return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPackPriceIDMissing, normalizedPackCode)
				}
			}
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPriceIDEmpty, normalizedPackCode)
		}
		if item.Credits <= 0 {
			if len(settings.Packs) == 0 {
				if _, hasConfiguredCredits := settings.TopUpPackCredits[normalizedPackCode]; !hasConfiguredCredits {
					return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPackCreditsMissing, normalizedPackCode)
				}
			}
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPackCreditsInvalid, normalizedPackCode)
		}
		if item.PriceCents <= 0 {
			if len(settings.Packs) == 0 {
				if _, hasConfiguredPrice := settings.TopUpPackPrices[normalizedPackCode]; !hasConfiguredPrice {
					return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPackPriceMissing, normalizedPackCode)
				}
			}
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPackPriceInvalid, normalizedPackCode)
		}
		packDefinitions[normalizedPackCode] = paddlePackDefinition{
			Pack: TopUpPack{
				Code:          normalizedPackCode,
				Label:         firstNonEmptyString(item.Label, PackLabelForCode(normalizedPackCode), toTitle(normalizedPackCode)),
				Credits:       item.Credits,
				PriceDisplay:  formatUSDPriceCents(item.PriceCents),
				BillingPeriod: paddleBillingPeriodOneTime,
			},
			PriceID:    strings.TrimSpace(item.PriceID),
			PriceCents: item.PriceCents,
		}
	}
	resolvedClient := client
	if resolvedClient == nil {
		clientInstance, clientErr := newPaddleAPIClient(
			settings.Environment,
			normalizedAPIKey,
			settings.APIBaseURL,
			nil,
		)
		if clientErr != nil {
			return nil, clientErr
		}
		resolvedClient = clientInstance
	}

	return &PaddleProvider{
		environment: strings.ToLower(strings.TrimSpace(settings.Environment)),
		clientToken: normalizedClientToken,
		verifier:    verifier,
		client:      resolvedClient,
		plans:       planDefinitions,
		packs:       packDefinitions,
	}, nil
}

func buildPaddlePlanCatalogItems(settings PaddleProviderSettings) []PlanCatalogItem {
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
	appendLegacyPlan(PlanCodePro, paddlePlanProLabel, settings.ProMonthlyPriceID)
	appendLegacyPlan(PlanCodePlus, paddlePlanPlusLabel, settings.PlusMonthlyPriceID)
	return items
}

func buildPaddlePackCatalogItems(settings PaddleProviderSettings) []PackCatalogItem {
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

func defaultPlanLabel(planCode string) string {
	switch strings.ToLower(strings.TrimSpace(planCode)) {
	case PlanCodePro:
		return paddlePlanProLabel
	case PlanCodePlus:
		return paddlePlanPlusLabel
	default:
		return toTitle(planCode)
	}
}

func (provider *PaddleProvider) Code() string {
	return ProviderCodePaddle
}

func (provider *PaddleProvider) ResolveCheckoutEventStatus(eventType string) CheckoutEventStatus {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case paddleEventTypeTransactionCompleted, paddleEventTypeTransactionPaid:
		return CheckoutEventStatusSucceeded
	case paddleEventTypeTransactionUpdated:
		return CheckoutEventStatusPending
	default:
		return CheckoutEventStatusUnknown
	}
}

func (provider *PaddleProvider) SignatureHeaderName() string {
	return paddleWebhookSignatureHeaderName
}

func (provider *PaddleProvider) VerifySignature(signatureHeader string, payload []byte) error {
	if provider == nil || provider.verifier == nil {
		return ErrPaddleProviderVerifierUnavailable
	}
	return provider.verifier.Verify(signatureHeader, payload)
}

func (provider *PaddleProvider) ParseWebhookEvent(payload []byte) (WebhookEventMetadata, error) {
	var envelope PaddleWebhookEnvelope
	if decodeErr := json.Unmarshal(payload, &envelope); decodeErr != nil {
		return WebhookEventMetadata{}, ErrPaddleWebhookPayloadInvalid
	}
	eventID := strings.TrimSpace(envelope.EventID)
	eventType := strings.TrimSpace(envelope.EventType)
	if eventID == "" || eventType == "" {
		return WebhookEventMetadata{}, ErrPaddleWebhookPayloadInvalid
	}
	occurredAt, occurredAtErr := parseRequiredPaddleTimestamp(envelope.OccurredAt)
	if occurredAtErr != nil {
		return WebhookEventMetadata{}, ErrPaddleWebhookPayloadInvalid
	}
	return WebhookEventMetadata{
		EventID:    eventID,
		EventType:  eventType,
		OccurredAt: occurredAt,
	}, nil
}

func (provider *PaddleProvider) SubscriptionPlans() []SubscriptionPlan {
	if provider == nil || len(provider.plans) == 0 {
		return []SubscriptionPlan{}
	}
	codes := make([]string, 0, len(provider.plans))
	for planCode := range provider.plans {
		codes = append(codes, planCode)
	}
	sort.Strings(codes)
	plans := make([]SubscriptionPlan, 0, len(codes))
	for _, planCode := range codes {
		plans = append(plans, provider.plans[planCode].Plan)
	}
	return plans
}

func (provider *PaddleProvider) TopUpPacks() []TopUpPack {
	if provider == nil || len(provider.packs) == 0 {
		return []TopUpPack{}
	}
	codes := make([]string, 0, len(provider.packs))
	for packCode := range provider.packs {
		codes = append(codes, packCode)
	}
	sort.Strings(codes)
	packs := make([]TopUpPack, 0, len(codes))
	for _, packCode := range codes {
		packs = append(packs, provider.packs[packCode].Pack)
	}
	return packs
}

func (provider *PaddleProvider) PublicConfig() PublicConfig {
	if provider == nil {
		return PublicConfig{}
	}
	return PublicConfig{
		ProviderCode: provider.Code(),
		Environment:  provider.environment,
		ClientToken:  provider.clientToken,
	}
}

func (provider *PaddleProvider) NewWebhookGrantResolver() (WebhookGrantResolver, error) {
	return newPaddleWebhookGrantResolverFromProvider(provider)
}

func (provider *PaddleProvider) NewSubscriptionStatusWebhookProcessor(
	stateRepository SubscriptionStateRepository,
) (WebhookProcessor, error) {
	return newPaddleSubscriptionStatusWebhookProcessor(provider, stateRepository)
}

func (provider *PaddleProvider) BuildUserSyncEvents(
	ctx context.Context,
	userEmail string,
) ([]WebhookEvent, error) {
	if provider == nil || provider.client == nil {
		return nil, ErrPaddleProviderClientUnavailable
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return nil, ErrBillingUserEmailInvalid
	}
	customerID, customerIDErr := provider.client.FindCustomerIDByEmail(ctx, normalizedUserEmail)
	if customerIDErr != nil {
		return nil, fmt.Errorf("billing.paddle.customer.find: %w", customerIDErr)
	}
	if strings.TrimSpace(customerID) == "" {
		return []WebhookEvent{}, nil
	}

	transactionEvents, transactionEventsErr := provider.buildUserTransactionSyncEvents(ctx, customerID)
	if transactionEventsErr != nil {
		return nil, transactionEventsErr
	}
	subscriptionEvents, subscriptionEventsErr := provider.buildUserSubscriptionSyncEvents(
		ctx,
		customerID,
		normalizedUserEmail,
	)
	if subscriptionEventsErr != nil {
		return nil, subscriptionEventsErr
	}

	syncEvents := make([]WebhookEvent, 0, len(transactionEvents)+len(subscriptionEvents))
	syncEvents = append(syncEvents, transactionEvents...)
	syncEvents = append(syncEvents, subscriptionEvents...)
	return syncEvents, nil
}

func (provider *PaddleProvider) InspectSubscriptions(
	ctx context.Context,
	userEmail string,
) ([]ProviderSubscription, error) {
	if provider == nil || provider.client == nil {
		return nil, ErrPaddleProviderClientUnavailable
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return nil, ErrBillingUserEmailInvalid
	}
	customerID, customerIDErr := provider.client.FindCustomerIDByEmail(ctx, normalizedUserEmail)
	if customerIDErr != nil {
		return nil, fmt.Errorf("billing.paddle.customer.find: %w", customerIDErr)
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

func (provider *PaddleProvider) buildUserTransactionSyncEvents(
	ctx context.Context,
	customerID string,
) ([]WebhookEvent, error) {
	transactions, transactionsErr := provider.client.ListCustomerTransactions(ctx, customerID)
	if transactionsErr != nil {
		return nil, fmt.Errorf("billing.paddle.transactions.list: %w", transactionsErr)
	}
	syncEvents := make([]WebhookEvent, 0, len(transactions))
	for _, transaction := range transactions {
		transactionID := strings.TrimSpace(transaction.ID)
		if transactionID == "" {
			continue
		}
		eventType := resolvePaddleCheckoutReconcileEventType(transaction.Status)
		if !isGrantablePaddleTransactionStatus(eventType, transaction.Status) {
			continue
		}
		payloadBytes, payloadErr := jsonMarshalFunc(paddleTransactionCompletedWebhookPayload{
			Data: transaction,
		})
		if payloadErr != nil {
			return nil, fmt.Errorf("billing.paddle.sync.transaction.payload: %w", payloadErr)
		}
		occurredAt := resolvePaddleTransactionOccurredAt(transaction)
		if occurredAt.IsZero() {
			return nil, ErrPaddleWebhookPayloadInvalid
		}
		syncEvents = append(syncEvents, WebhookEvent{
			ProviderCode: provider.Code(),
			EventID:      fmt.Sprintf("sync:transaction:%s", transactionID),
			EventType:    eventType,
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

func (provider *PaddleProvider) buildUserSubscriptionSyncEvents(
	ctx context.Context,
	customerID string,
	userEmail string,
) ([]WebhookEvent, error) {
	subscriptions, subscriptionsErr := provider.client.ListCustomerSubscriptions(ctx, customerID)
	if subscriptionsErr != nil {
		return nil, fmt.Errorf("billing.paddle.subscriptions.list: %w", subscriptionsErr)
	}
	if len(subscriptions) == 0 {
		occurredAt := time.Now().UTC()
		payloadBytes, payloadErr := jsonMarshalFunc(paddleSubscriptionWebhookPayload{
			Data: paddleSubscriptionWebhookData{
				ID:         "",
				Status:     paddleSubscriptionStatusInactive,
				CustomerID: customerID,
				CustomData: map[string]interface{}{
					billingMetadataUserEmailKey:      userEmail,
					paddleLegacyMetadataUserEmailKey: userEmail,
				},
				UpdatedAt: occurredAt.Format(time.RFC3339Nano),
			},
		})
		if payloadErr != nil {
			return nil, fmt.Errorf("billing.paddle.sync.subscription.payload: %w", payloadErr)
		}
		return []WebhookEvent{
			{
				ProviderCode: provider.Code(),
				EventID:      fmt.Sprintf("sync:subscription:none:%d", occurredAt.UnixNano()),
				EventType:    paddleEventTypeSubscriptionUpdated,
				OccurredAt:   occurredAt,
				Payload:      payloadBytes,
			},
		}, nil
	}
	subscriptionEvents := make([]WebhookEvent, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		subscriptionID := strings.TrimSpace(subscription.ID)
		if subscriptionID == "" {
			continue
		}
		payloadBytes, payloadErr := jsonMarshalFunc(paddleSubscriptionWebhookPayload{
			Data: subscription,
		})
		if payloadErr != nil {
			return nil, fmt.Errorf("billing.paddle.sync.subscription.payload: %w", payloadErr)
		}
		occurredAt := resolvePaddleSubscriptionOccurredAt(subscription)
		if occurredAt.IsZero() {
			return nil, ErrPaddleWebhookPayloadInvalid
		}
		subscriptionEvents = append(subscriptionEvents, WebhookEvent{
			ProviderCode: provider.Code(),
			EventID: fmt.Sprintf(
				"sync:subscription:%s:%s",
				subscriptionID,
				strings.ToLower(strings.TrimSpace(subscription.Status)),
			),
			EventType:  paddleEventTypeSubscriptionUpdated,
			OccurredAt: occurredAt,
			Payload:    payloadBytes,
		})
	}
	sort.SliceStable(subscriptionEvents, func(leftIndex int, rightIndex int) bool {
		leftStatus := resolvePaddleSubscriptionState(
			paddleEventTypeSubscriptionUpdated,
			extractPaddleSyncSubscriptionStatus(subscriptionEvents[leftIndex]),
		)
		rightStatus := resolvePaddleSubscriptionState(
			paddleEventTypeSubscriptionUpdated,
			extractPaddleSyncSubscriptionStatus(subscriptionEvents[rightIndex]),
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

func (provider *PaddleProvider) CreateSubscriptionCheckout(
	ctx context.Context,
	customer CustomerContext,
	planCode string,
) (CheckoutSession, error) {
	if provider == nil || provider.client == nil {
		return CheckoutSession{}, ErrPaddleProviderClientUnavailable
	}
	normalizedCustomer := NormalizeCustomerContext(customer)
	normalizedUserEmail := normalizedCustomer.Email
	if normalizedUserEmail == "" {
		return CheckoutSession{}, ErrBillingUserEmailInvalid
	}
	normalizedPlanCode := strings.ToLower(strings.TrimSpace(planCode))
	planDefinition, hasPlan := provider.plans[normalizedPlanCode]
	if !hasPlan {
		return CheckoutSession{}, ErrBillingPlanUnsupported
	}

	customerID, err := provider.client.ResolveCustomerID(ctx, normalizedUserEmail)
	if err != nil {
		return CheckoutSession{}, fmt.Errorf("billing.paddle.customer.resolve: %w", err)
	}

	transactionID, err := provider.client.CreateTransaction(ctx, paddleTransactionInput{
		CustomerID: customerID,
		PriceID:    planDefinition.PriceID,
		Metadata:   buildCheckoutMetadata(normalizedCustomer, paddlePurchaseKindSubscription, planDefinition.Plan.Code, 0, planDefinition.PriceID),
	})
	if err != nil {
		return CheckoutSession{}, fmt.Errorf("billing.paddle.transaction.create: %w", err)
	}

	return CheckoutSession{
		ProviderCode:  provider.Code(),
		TransactionID: transactionID,
		CheckoutMode:  CheckoutModeOverlay,
	}, nil
}

func (provider *PaddleProvider) CreateTopUpCheckout(
	ctx context.Context,
	customer CustomerContext,
	packCode string,
) (CheckoutSession, error) {
	if provider == nil || provider.client == nil {
		return CheckoutSession{}, ErrPaddleProviderClientUnavailable
	}
	normalizedCustomer := NormalizeCustomerContext(customer)
	normalizedUserEmail := normalizedCustomer.Email
	if normalizedUserEmail == "" {
		return CheckoutSession{}, ErrBillingUserEmailInvalid
	}
	normalizedPackCode := NormalizePackCode(packCode)
	packDefinition, hasPack := provider.packs[normalizedPackCode]
	if !hasPack {
		return CheckoutSession{}, ErrBillingTopUpPackUnknown
	}

	customerID, err := provider.client.ResolveCustomerID(ctx, normalizedUserEmail)
	if err != nil {
		return CheckoutSession{}, fmt.Errorf("billing.paddle.customer.resolve: %w", err)
	}

	transactionID, err := provider.client.CreateTransaction(ctx, paddleTransactionInput{
		CustomerID: customerID,
		PriceID:    packDefinition.PriceID,
		Metadata:   buildCheckoutMetadata(normalizedCustomer, paddlePurchaseKindTopUpPack, packDefinition.Pack.Code, packDefinition.Pack.Credits, packDefinition.PriceID),
	})
	if err != nil {
		return CheckoutSession{}, fmt.Errorf("billing.paddle.transaction.create: %w", err)
	}

	return CheckoutSession{
		ProviderCode:  provider.Code(),
		TransactionID: transactionID,
		CheckoutMode:  CheckoutModeOverlay,
	}, nil
}

func (provider *PaddleProvider) CreateCustomerPortalSession(
	ctx context.Context,
	userEmail string,
) (PortalSession, error) {
	if provider == nil || provider.client == nil {
		return PortalSession{}, ErrPaddleProviderClientUnavailable
	}
	normalizedUserEmail := strings.TrimSpace(userEmail)
	if normalizedUserEmail == "" {
		return PortalSession{}, ErrBillingUserEmailInvalid
	}
	customerID, err := provider.client.ResolveCustomerID(ctx, normalizedUserEmail)
	if err != nil {
		return PortalSession{}, fmt.Errorf("billing.paddle.customer.resolve: %w", err)
	}
	portalURL, err := provider.client.CreateCustomerPortalURL(ctx, customerID)
	if err != nil {
		return PortalSession{}, fmt.Errorf("billing.paddle.portal.create: %w", err)
	}
	return PortalSession{
		ProviderCode: provider.Code(),
		URL:          portalURL,
	}, nil
}

func (provider *PaddleProvider) BuildCheckoutReconcileEvent(
	ctx context.Context,
	transactionID string,
) (WebhookEvent, string, error) {
	if provider == nil || provider.client == nil {
		return WebhookEvent{}, "", ErrPaddleProviderClientUnavailable
	}
	normalizedTransactionID := strings.TrimSpace(transactionID)
	if normalizedTransactionID == "" {
		return WebhookEvent{}, "", ErrPaddleAPITransactionNotFound
	}

	transactionData, transactionErr := provider.client.GetTransaction(ctx, normalizedTransactionID)
	if transactionErr != nil {
		return WebhookEvent{}, "", fmt.Errorf("billing.paddle.transaction.get: %w", transactionErr)
	}
	checkoutUserEmail, checkoutUserEmailErr := provider.resolveCheckoutUserEmail(ctx, transactionData)
	if checkoutUserEmailErr != nil {
		return WebhookEvent{}, "", fmt.Errorf("billing.paddle.checkout.user_email: %w", checkoutUserEmailErr)
	}

	payloadBytes, payloadErr := jsonMarshalFunc(paddleTransactionCompletedWebhookPayload{
		Data: transactionData,
	})
	if payloadErr != nil {
		return WebhookEvent{}, "", fmt.Errorf("billing.paddle.checkout.payload: %w", payloadErr)
	}
	transactionStatus := strings.ToLower(strings.TrimSpace(transactionData.Status))
	eventID := fmt.Sprintf("reconcile:%s", normalizedTransactionID)
	if transactionStatus != "" {
		eventID = fmt.Sprintf("%s:%s", eventID, transactionStatus)
	}
	return WebhookEvent{
		ProviderCode: provider.Code(),
		EventID:      eventID,
		EventType:    resolvePaddleCheckoutReconcileEventType(transactionStatus),
		OccurredAt:   resolvePaddleTransactionOccurredAt(transactionData),
		Payload:      payloadBytes,
	}, checkoutUserEmail, nil
}

func (provider *PaddleProvider) resolveCheckoutUserEmail(
	ctx context.Context,
	transactionData paddleTransactionCompletedWebhookData,
) (string, error) {
	checkoutUserEmail := strings.TrimSpace(
		webhookMetadataValueAny(
			transactionData.CustomData,
			billingMetadataUserEmailKey,
			paddleLegacyMetadataUserEmailKey,
			crosswordLegacyMetadataUserEmailKey,
		),
	)
	if checkoutUserEmail == "" {
		checkoutUserEmail = strings.TrimSpace(resolvePaddleCustomerEmail(transactionData.Customer))
	}
	if checkoutUserEmail == "" {
		normalizedCustomerID := strings.TrimSpace(transactionData.CustomerID)
		if normalizedCustomerID == "" {
			return "", ErrWebhookGrantMetadataInvalid
		}
		resolvedCustomerEmail, customerEmailErr := provider.client.ResolveCustomerEmail(ctx, normalizedCustomerID)
		if customerEmailErr != nil {
			return "", fmt.Errorf("billing.paddle.customer_email.resolve: %w", customerEmailErr)
		}
		checkoutUserEmail = strings.TrimSpace(resolvedCustomerEmail)
	}
	normalizedCheckoutUserEmail := strings.ToLower(strings.TrimSpace(checkoutUserEmail))
	if normalizedCheckoutUserEmail == "" {
		return "", ErrWebhookGrantMetadataInvalid
	}
	return normalizedCheckoutUserEmail, nil
}

func resolvePaddleCheckoutReconcileEventType(transactionStatus string) string {
	switch strings.ToLower(strings.TrimSpace(transactionStatus)) {
	case paddleTransactionStatusPaid, paddleTransactionStatusCompleted:
		return paddleEventTypeTransactionCompleted
	default:
		return paddleEventTypeTransactionUpdated
	}
}

func resolvePaddleTransactionOccurredAt(transactionData paddleTransactionCompletedWebhookData) time.Time {
	occurredAtFields := []string{
		transactionData.BilledAt,
		transactionData.CompletedAt,
		transactionData.UpdatedAt,
		transactionData.CreatedAt,
	}
	for _, occurredAtField := range occurredAtFields {
		parsedOccurredAt, parseErr := parsePaddleTimestamp(occurredAtField)
		if parseErr == nil {
			return parsedOccurredAt
		}
	}
	return time.Time{}
}

func resolvePaddleSubscriptionOccurredAt(subscriptionData paddleSubscriptionWebhookData) time.Time {
	occurredAtFields := []string{
		subscriptionData.UpdatedAt,
		subscriptionData.CurrentBillingPeriod.StartsAt,
		subscriptionData.CurrentBillingPeriod.EndsAt,
		subscriptionData.NextBilledAt,
	}
	for _, occurredAtCandidate := range occurredAtFields {
		occurredAt, parseErr := parsePaddleTimestamp(occurredAtCandidate)
		if parseErr == nil && !occurredAt.IsZero() {
			return occurredAt
		}
	}
	return time.Time{}
}

func extractPaddleSyncSubscriptionStatus(event WebhookEvent) string {
	payload := paddleSubscriptionWebhookPayload{}
	if decodeErr := json.Unmarshal(event.Payload, &payload); decodeErr != nil {
		return ""
	}
	return payload.Data.Status
}

func (provider *PaddleProvider) inspectCustomerSubscriptions(
	ctx context.Context,
	customerID string,
) ([]paddleInspectedSubscription, error) {
	subscriptions, subscriptionsErr := provider.client.ListCustomerSubscriptions(ctx, customerID)
	if subscriptionsErr != nil {
		return nil, fmt.Errorf("billing.paddle.subscriptions.list: %w", subscriptionsErr)
	}
	planCodeByPriceID := buildPaddlePlanCodeByPriceID(provider.plans)
	inspectedSubscriptions := make([]paddleInspectedSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		subscriptionID := strings.TrimSpace(subscription.ID)
		if subscriptionID == "" {
			continue
		}
		planCode := strings.ToLower(
			webhookMetadataValueAny(
				subscription.CustomData,
				billingMetadataPlanCodeKey,
				paddleLegacyMetadataPlanCodeKey,
			),
		)
		if planCode == "" {
			priceID := resolvePaddleSubscriptionPriceID(subscription)
			planCode = strings.ToLower(strings.TrimSpace(planCodeByPriceID[priceID]))
		}
		inspectedSubscriptions = append(inspectedSubscriptions, paddleInspectedSubscription{
			payload: subscription,
			normalized: normalizeProviderSubscription(ProviderSubscription{
				SubscriptionID: subscriptionID,
				PlanCode:       planCode,
				Status:         resolvePaddleSubscriptionState(paddleEventTypeSubscriptionUpdated, subscription.Status),
				ProviderStatus: subscription.Status,
				NextBillingAt:  resolvePaddleSubscriptionNextBillingAt(subscription),
				OccurredAt:     resolvePaddleSubscriptionOccurredAt(subscription),
			}),
		})
	}
	return inspectedSubscriptions, nil
}

func parsePaddleTimestamp(rawTimestamp string) (time.Time, error) {
	normalizedTimestamp := strings.TrimSpace(rawTimestamp)
	if normalizedTimestamp == "" {
		return time.Time{}, nil
	}
	parsedTimestamp, parseErr := time.Parse(time.RFC3339, normalizedTimestamp)
	if parseErr != nil {
		return time.Time{}, parseErr
	}
	return parsedTimestamp.UTC(), nil
}

func parseRequiredPaddleTimestamp(rawTimestamp string) (time.Time, error) {
	parsedTimestamp, parseErr := parsePaddleTimestamp(rawTimestamp)
	if parseErr != nil {
		return time.Time{}, parseErr
	}
	if parsedTimestamp.IsZero() {
		return time.Time{}, ErrPaddleWebhookPayloadInvalid
	}
	return parsedTimestamp, nil
}

func (provider *PaddleProvider) Environment() string {
	if provider == nil {
		return ""
	}
	return provider.environment
}

func (provider *PaddleProvider) ClientToken() string {
	if provider == nil {
		return ""
	}
	return provider.clientToken
}

func (provider *PaddleProvider) ValidateCatalog(ctx context.Context) error {
	if provider == nil || provider.client == nil {
		return ErrPaddleProviderClientUnavailable
	}
	planDefinitions := provider.plans
	packDefinitions := provider.packs
	return validatePaddleCatalogPricing(provider.client, ctx, planDefinitions, packDefinitions)
}

func validatePaddleCatalogPricing(
	client paddleCommerceClient,
	ctx context.Context,
	planDefinitions map[string]paddlePlanDefinition,
	packDefinitions map[string]paddlePackDefinition,
) error {
	if client == nil {
		return ErrPaddleProviderClientUnavailable
	}
	priceIDs := make([]string, 0, len(planDefinitions)+len(packDefinitions))
	seenPriceIDs := make(map[string]struct{}, len(planDefinitions)+len(packDefinitions))
	appendPriceID := func(priceID string) {
		normalizedPriceID := strings.TrimSpace(priceID)
		if normalizedPriceID == "" {
			return
		}
		if _, alreadySeen := seenPriceIDs[normalizedPriceID]; alreadySeen {
			return
		}
		seenPriceIDs[normalizedPriceID] = struct{}{}
		priceIDs = append(priceIDs, normalizedPriceID)
	}
	for _, planDefinition := range planDefinitions {
		appendPriceID(planDefinition.PriceID)
	}
	for _, packDefinition := range packDefinitions {
		appendPriceID(packDefinition.PriceID)
	}
	priceDetailsByID, listPricesErr := client.ListPrices(ctx, priceIDs)
	if listPricesErr != nil {
		return fmt.Errorf("billing.paddle.catalog.prices.list: %w", listPricesErr)
	}

	for planCode, planDefinition := range planDefinitions {
		priceDetails, resolvePriceErr := resolvePaddleCatalogPriceDetailsFromMap(
			priceDetailsByID,
			planDefinition.PriceID,
		)
		if resolvePriceErr != nil {
			return fmt.Errorf("billing.paddle.catalog.plan.%s: %w", planCode, resolvePriceErr)
		}
		if !isPaddlePriceRecurringMonthly(priceDetails) {
			return fmt.Errorf(
				"%w: plan=%s price_id=%s",
				ErrPaddleProviderPriceRecurringInvalid,
				planCode,
				planDefinition.PriceID,
			)
		}
		if priceDetails.PriceCents != planDefinition.PriceCents {
			return fmt.Errorf(
				"%w: plan=%s price_id=%s expected=%d actual=%d",
				ErrPaddleProviderPriceAmountMismatch,
				planCode,
				planDefinition.PriceID,
				planDefinition.PriceCents,
				priceDetails.PriceCents,
			)
		}
	}
	for packCode, packDefinition := range packDefinitions {
		priceDetails, resolvePriceErr := resolvePaddleCatalogPriceDetailsFromMap(
			priceDetailsByID,
			packDefinition.PriceID,
		)
		if resolvePriceErr != nil {
			return fmt.Errorf("billing.paddle.catalog.pack.%s: %w", packCode, resolvePriceErr)
		}
		if !isPaddlePriceOneOff(priceDetails) {
			return fmt.Errorf(
				"%w: pack=%s price_id=%s",
				ErrPaddleProviderPriceOneOffInvalid,
				packCode,
				packDefinition.PriceID,
			)
		}
		if priceDetails.PriceCents != packDefinition.PriceCents {
			return fmt.Errorf(
				"%w: pack=%s price_id=%s expected=%d actual=%d",
				ErrPaddleProviderPriceAmountMismatch,
				packCode,
				packDefinition.PriceID,
				packDefinition.PriceCents,
				priceDetails.PriceCents,
			)
		}
	}
	return nil
}

func resolvePaddleCatalogPriceDetailsFromMap(
	priceDetailsByID map[string]paddlePriceDetails,
	priceID string,
) (paddlePriceDetails, error) {
	resolvedPriceID := strings.TrimSpace(priceID)
	if resolvedPriceID == "" {
		return paddlePriceDetails{}, ErrPaddleAPIPriceNotFound
	}
	resolvedPriceDetails, hasResolvedPriceDetails := priceDetailsByID[resolvedPriceID]
	if !hasResolvedPriceDetails {
		return paddlePriceDetails{}, fmt.Errorf(
			"billing.paddle.catalog.price.get: %w: price_id=%s",
			ErrPaddleAPIPriceNotFound,
			resolvedPriceID,
		)
	}
	return resolvedPriceDetails, nil
}

func isPaddlePriceRecurringMonthly(priceDetails paddlePriceDetails) bool {
	interval := strings.ToLower(strings.TrimSpace(priceDetails.BillingCycle.Interval))
	if interval != "month" {
		return false
	}
	return priceDetails.BillingCycle.Frequency > 0
}

func isPaddlePriceOneOff(priceDetails paddlePriceDetails) bool {
	interval := strings.ToLower(strings.TrimSpace(priceDetails.BillingCycle.Interval))
	return interval == ""
}

func toTitle(value string) string {
	parts := strings.Fields(strings.ReplaceAll(value, "_", " "))
	if len(parts) == 0 {
		return value
	}
	for index, part := range parts {
		parts[index] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}
