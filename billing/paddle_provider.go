package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	paddleWebhookSignatureHeaderName = "Paddle-Signature"

	paddleMetadataUserEmailKey    = "product_scanner_user_email"
	paddleMetadataPurchaseKindKey = "product_scanner_purchase_kind"
	paddleMetadataPlanCodeKey     = "product_scanner_plan_code"
	paddleMetadataPackCodeKey     = "product_scanner_pack_code"
	paddleMetadataPackCreditsKey  = "product_scanner_pack_credits"

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
	client paddleCommerceClient,
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
	planCreditsByCode := make(map[string]int64, len(settings.SubscriptionMonthlyCredits))
	for rawPlanCode, rawPlanCredits := range settings.SubscriptionMonthlyCredits {
		normalizedPlanCode := strings.ToLower(strings.TrimSpace(rawPlanCode))
		if normalizedPlanCode == "" {
			continue
		}
		if rawPlanCredits <= 0 {
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPlanCreditsInvalid, normalizedPlanCode)
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
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPlanPriceInvalid, normalizedPlanCode)
		}
		planPricesByCode[normalizedPlanCode] = rawPlanPrice
	}

	planDefinitions := map[string]paddlePlanDefinition{
		PlanCodePro: {
			Plan: SubscriptionPlan{
				Code:          PlanCodePro,
				Label:         paddlePlanProLabel,
				BillingPeriod: paddleBillingPeriodMonthly,
			},
			PriceID: strings.TrimSpace(settings.ProMonthlyPriceID),
		},
		PlanCodePlus: {
			Plan: SubscriptionPlan{
				Code:          PlanCodePlus,
				Label:         paddlePlanPlusLabel,
				BillingPeriod: paddleBillingPeriodMonthly,
			},
			PriceID: strings.TrimSpace(settings.PlusMonthlyPriceID),
		},
	}
	for planCode, definition := range planDefinitions {
		if definition.PriceID == "" {
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPriceIDEmpty, planCode)
		}
		planCredits, hasPlanCredits := planCreditsByCode[planCode]
		if !hasPlanCredits {
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPlanCreditsMissing, planCode)
		}
		definition.Plan.MonthlyCredits = planCredits
		planPrice, hasPlanPrice := planPricesByCode[planCode]
		if !hasPlanPrice {
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPlanPriceMissing, planCode)
		}
		definition.Plan.PriceDisplay = formatUSDPriceCents(planPrice)
		definition.PriceCents = planPrice
		planDefinitions[planCode] = definition
	}

	packDefinitions := make(map[string]paddlePackDefinition, len(settings.TopUpPackPriceIDs))
	packCreditsByCode := make(map[string]int64, len(settings.TopUpPackCredits))
	for rawPackCode, rawPackCredits := range settings.TopUpPackCredits {
		normalizedPackCode := NormalizePackCode(rawPackCode)
		if normalizedPackCode == "" {
			continue
		}
		if rawPackCredits <= 0 {
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPackCreditsInvalid, normalizedPackCode)
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
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPackPriceInvalid, normalizedPackCode)
		}
		packPricesByCode[normalizedPackCode] = rawPackPrice
	}
	for rawPackCode, rawPriceID := range settings.TopUpPackPriceIDs {
		normalizedPackCode := NormalizePackCode(rawPackCode)
		if normalizedPackCode == "" {
			continue
		}
		normalizedPriceID := strings.TrimSpace(rawPriceID)
		if normalizedPriceID == "" {
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPriceIDEmpty, normalizedPackCode)
		}
		packCredits, hasPackCredits := packCreditsByCode[normalizedPackCode]
		if !hasPackCredits {
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPackCreditsMissing, normalizedPackCode)
		}
		packPrice, hasPackPrice := packPricesByCode[normalizedPackCode]
		if !hasPackPrice {
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPackPriceMissing, normalizedPackCode)
		}
		packLabel := PackLabelForCode(normalizedPackCode)
		if packLabel == "" {
			packLabel = toTitle(normalizedPackCode)
		}
		packDefinitions[normalizedPackCode] = paddlePackDefinition{
			Pack: TopUpPack{
				Code:          normalizedPackCode,
				Label:         packLabel,
				Credits:       packCredits,
				PriceDisplay:  formatUSDPriceCents(packPrice),
				BillingPeriod: paddleBillingPeriodOneTime,
			},
			PriceID:    normalizedPriceID,
			PriceCents: packPrice,
		}
	}
	for normalizedPackCode := range packCreditsByCode {
		if _, hasPackDefinition := packDefinitions[normalizedPackCode]; !hasPackDefinition {
			return nil, fmt.Errorf("%w: %s", ErrPaddleProviderPackPriceIDMissing, normalizedPackCode)
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

type paddleSubscriptionSyncEvent struct {
	WebhookEvent
	isActive bool
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
					paddleMetadataUserEmailKey: userEmail,
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

	subscriptionEvents := make([]paddleSubscriptionSyncEvent, 0, len(subscriptions))
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
		resolvedStatus := resolvePaddleSubscriptionState(paddleEventTypeSubscriptionUpdated, subscription.Status)
		subscriptionEvents = append(subscriptionEvents, paddleSubscriptionSyncEvent{
			WebhookEvent: WebhookEvent{
				ProviderCode: provider.Code(),
				EventID: fmt.Sprintf(
					"sync:subscription:%s:%s",
					subscriptionID,
					strings.ToLower(strings.TrimSpace(subscription.Status)),
				),
				EventType:  paddleEventTypeSubscriptionUpdated,
				OccurredAt: occurredAt,
				Payload:    payloadBytes,
			},
			isActive: resolvedStatus == subscriptionStatusActive,
		})
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

func (provider *PaddleProvider) CreateSubscriptionCheckout(
	ctx context.Context,
	userEmail string,
	planCode string,
) (CheckoutSession, error) {
	if provider == nil || provider.client == nil {
		return CheckoutSession{}, ErrPaddleProviderClientUnavailable
	}
	normalizedUserEmail := strings.TrimSpace(userEmail)
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
		Metadata: map[string]string{
			paddleMetadataUserEmailKey:    normalizedUserEmail,
			paddleMetadataPurchaseKindKey: paddlePurchaseKindSubscription,
			paddleMetadataPlanCodeKey:     planDefinition.Plan.Code,
		},
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
	userEmail string,
	packCode string,
) (CheckoutSession, error) {
	if provider == nil || provider.client == nil {
		return CheckoutSession{}, ErrPaddleProviderClientUnavailable
	}
	normalizedUserEmail := strings.TrimSpace(userEmail)
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
		Metadata: map[string]string{
			paddleMetadataUserEmailKey:    normalizedUserEmail,
			paddleMetadataPurchaseKindKey: paddlePurchaseKindTopUpPack,
			paddleMetadataPackCodeKey:     packDefinition.Pack.Code,
			paddleMetadataPackCreditsKey:  strconv.FormatInt(packDefinition.Pack.Credits, 10),
		},
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
		webhookMetadataValue(transactionData.CustomData, paddleMetadataUserEmailKey),
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
