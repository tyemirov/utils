package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	paddleEventTypeSubscriptionCreated   = "subscription.created"
	paddleEventTypeSubscriptionUpdated   = "subscription.updated"
	paddleEventTypeSubscriptionCanceled  = "subscription.canceled"
	paddleEventTypeSubscriptionResumed   = "subscription.resumed"
	paddleEventTypeSubscriptionActivated = "subscription.activated"
	paddleEventTypeSubscriptionPaused    = "subscription.paused"

	paddleSubscriptionStatusActive   = "active"
	paddleSubscriptionStatusTrialing = "trialing"
	paddleSubscriptionStatusPaused   = "paused"
	paddleSubscriptionStatusCanceled = "canceled"
	paddleSubscriptionStatusInactive = "inactive"
	paddleSubscriptionStatusPastDue  = "past_due"
)

var (
	ErrWebhookSubscriptionStateRepositoryUnavailable = errors.New("billing.webhook.subscription_state.repository.unavailable")
	ErrWebhookSubscriptionStateProviderUnsupported   = errors.New("billing.webhook.subscription_state.provider.unsupported")
)

type subscriptionStatusWebhookProcessor struct {
	providerCode          string
	stateRepository       SubscriptionStateRepository
	grantResolver         WebhookGrantResolver
	customerEmailResolver paddleCustomerEmailResolver
	subscriptionResolver  paddleSubscriptionResolver
	planCodeByPriceID     map[string]string
	eventStatusProvider   CheckoutEventStatusProvider
}

type paddleSubscriptionResolver interface {
	GetSubscription(context.Context, string) (paddleSubscriptionWebhookData, error)
}

func NewSubscriptionStatusWebhookProcessor(
	provider CommerceProvider,
	stateRepository SubscriptionStateRepository,
) (WebhookProcessor, error) {
	if stateRepository == nil {
		return nil, ErrWebhookSubscriptionStateRepositoryUnavailable
	}
	if provider == nil {
		return nil, ErrWebhookGrantResolverProviderUnavailable
	}
	subscriptionStatusWebhookProcessorProvider, isSubscriptionStatusWebhookProcessorProvider := provider.(SubscriptionStatusWebhookProcessorProvider)
	if !isSubscriptionStatusWebhookProcessorProvider {
		normalizedProviderCode := strings.ToLower(strings.TrimSpace(provider.Code()))
		return nil, fmt.Errorf("%w: %s", ErrWebhookSubscriptionStateProviderUnsupported, normalizedProviderCode)
	}
	return subscriptionStatusWebhookProcessorProvider.NewSubscriptionStatusWebhookProcessor(stateRepository)
}

func newPaddleSubscriptionStatusWebhookProcessor(
	provider *PaddleProvider,
	stateRepository SubscriptionStateRepository,
) (WebhookProcessor, error) {
	if provider == nil {
		return nil, ErrWebhookGrantResolverProviderUnavailable
	}
	if stateRepository == nil {
		return nil, ErrWebhookSubscriptionStateRepositoryUnavailable
	}
	grantResolver, grantResolverErr := provider.NewWebhookGrantResolver()
	if grantResolverErr != nil {
		return nil, grantResolverErr
	}
	processor := &subscriptionStatusWebhookProcessor{
		providerCode:        ProviderCodePaddle,
		stateRepository:     stateRepository,
		grantResolver:       grantResolver,
		planCodeByPriceID:   map[string]string{},
		eventStatusProvider: provider,
	}
	processor.customerEmailResolver = provider.client
	processor.subscriptionResolver = provider.client
	processor.planCodeByPriceID = buildPaddlePlanCodeByPriceID(provider.plans)
	return processor, nil
}

func buildPaddlePlanCodeByPriceID(planDefinitions map[string]paddlePlanDefinition) map[string]string {
	if len(planDefinitions) == 0 {
		return map[string]string{}
	}
	planCodeByPriceID := make(map[string]string, len(planDefinitions))
	for _, planDefinition := range planDefinitions {
		priceID := strings.TrimSpace(planDefinition.PriceID)
		planCode := strings.ToLower(strings.TrimSpace(planDefinition.Plan.Code))
		if priceID == "" || planCode == "" {
			continue
		}
		planCodeByPriceID[priceID] = planCode
	}
	return planCodeByPriceID
}

func (processor *subscriptionStatusWebhookProcessor) Process(ctx context.Context, event WebhookEvent) error {
	if strings.ToLower(strings.TrimSpace(event.ProviderCode)) != processor.providerCode {
		return nil
	}
	normalizedEventType := strings.ToLower(strings.TrimSpace(event.EventType))
	if processor.eventStatusProvider != nil {
		checkoutEventStatus := processor.eventStatusProvider.ResolveCheckoutEventStatus(normalizedEventType)
		if checkoutEventStatus == CheckoutEventStatusSucceeded || checkoutEventStatus == CheckoutEventStatusPending {
			return processor.processTransactionEvent(ctx, event)
		}
	}
	if strings.HasPrefix(normalizedEventType, "subscription.") {
		return processor.processSubscriptionLifecycleEvent(ctx, event)
	}
	return nil
}

func (processor *subscriptionStatusWebhookProcessor) processTransactionEvent(
	ctx context.Context,
	event WebhookEvent,
) error {
	grant, shouldGrant, grantResolveErr := processor.grantResolver.Resolve(ctx, event)
	if grantResolveErr != nil || !shouldGrant {
		return grantResolveErr
	}
	purchaseKind := strings.ToLower(strings.TrimSpace(grant.Metadata[billingGrantMetadataPurchaseKindKey]))
	if purchaseKind != paddlePurchaseKindSubscription {
		return nil
	}
	subscriptionID := strings.TrimSpace(grant.Metadata[billingGrantMetadataSubscriptionIDKey])
	existingState, hasExistingState, stateErr := processor.stateRepository.Get(
		ctx,
		processor.providerCode,
		grant.UserEmail,
	)
	if stateErr != nil {
		return fmt.Errorf("billing.webhook.subscription_state.get: %w", stateErr)
	}
	if hasExistingState &&
		!isSyntheticSyncEvent(event.EventID) &&
		isStaleSubscriptionEvent(existingState.LastEventOccurredAt, event.OccurredAt) {
		return nil
	}

	planCode := strings.ToLower(strings.TrimSpace(grant.Metadata[billingGrantMetadataPlanCodeKey]))
	if planCode == "" {
		planCode = resolveSubscriptionPlanCodeFromGrantReason(grant.Reason)
	}
	if planCode == "" && hasExistingState {
		planCode = strings.ToLower(strings.TrimSpace(existingState.ActivePlan))
	}

	subscriptionStatus := subscriptionStatusActive
	providerStatus := paddleSubscriptionStatusActive
	nextBillingAt := time.Time{}
	if subscriptionID != "" && processor.subscriptionResolver != nil {
		subscriptionData, subscriptionErr := processor.subscriptionResolver.GetSubscription(ctx, subscriptionID)
		if subscriptionErr == nil {
			resolvedSubscriptionID := strings.TrimSpace(subscriptionData.ID)
			resolvedSubscriptionStatus := strings.TrimSpace(subscriptionData.Status)
			if resolvedSubscriptionID != "" && resolvedSubscriptionStatus != "" {
				subscriptionStatus = resolvePaddleSubscriptionState(
					paddleEventTypeSubscriptionUpdated,
					subscriptionData.Status,
				)
				providerStatus = strings.ToLower(resolvedSubscriptionStatus)
				if planCode == "" {
					planCode = processor.resolvePlanCode(subscriptionData)
				}
				nextBillingAt = resolvePaddleSubscriptionNextBillingAt(subscriptionData)
			}
		} else if !errors.Is(subscriptionErr, ErrPaddleAPISubscriptionNotFound) {
			nextBillingAt = time.Time{}
		}
	}
	if subscriptionStatus != subscriptionStatusActive {
		planCode = ""
	}

	upsertErr := processor.stateRepository.Upsert(ctx, SubscriptionStateUpsertInput{
		ProviderCode:      processor.providerCode,
		UserEmail:         grant.UserEmail,
		Status:            subscriptionStatus,
		ProviderStatus:    providerStatus,
		ActivePlan:        planCode,
		SubscriptionID:    subscriptionID,
		NextBillingAt:     nextBillingAt,
		LastEventID:       strings.TrimSpace(event.EventID),
		LastEventType:     strings.TrimSpace(event.EventType),
		EventOccurredAt:   event.OccurredAt,
		LastTransactionID: strings.TrimSpace(grant.Metadata[billingGrantMetadataTransactionIDKey]),
	})
	if upsertErr != nil {
		return fmt.Errorf("billing.webhook.subscription_state.upsert: %w", upsertErr)
	}
	return nil
}

func resolveSubscriptionPlanCodeFromGrantReason(reason string) string {
	normalizedReason := strings.ToLower(strings.TrimSpace(reason))
	prefix := billingGrantReasonSubscriptionPrefix + "_"
	if !strings.HasPrefix(normalizedReason, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(normalizedReason, prefix))
}

func (processor *subscriptionStatusWebhookProcessor) processSubscriptionLifecycleEvent(
	ctx context.Context,
	event WebhookEvent,
) error {
	payload := paddleSubscriptionWebhookPayload{}
	if decodeErr := json.Unmarshal(event.Payload, &payload); decodeErr != nil {
		return ErrWebhookGrantPayloadInvalid
	}

	userEmail, userEmailErr := processor.resolveLifecycleUserEmail(ctx, payload.Data)
	if userEmailErr != nil {
		return userEmailErr
	}
	existingState, hasExistingState, stateErr := processor.stateRepository.Get(
		ctx,
		processor.providerCode,
		userEmail,
	)
	if stateErr != nil {
		return fmt.Errorf("billing.webhook.subscription_state.get: %w", stateErr)
	}
	if hasExistingState &&
		!isSyntheticSyncEvent(event.EventID) &&
		isStaleSubscriptionEvent(existingState.LastEventOccurredAt, event.OccurredAt) {
		return nil
	}
	subscriptionStatus := resolvePaddleSubscriptionState(event.EventType, payload.Data.Status)
	planCode := processor.resolvePlanCode(payload.Data)
	if subscriptionStatus != subscriptionStatusActive {
		planCode = ""
	}
	upsertErr := processor.stateRepository.Upsert(ctx, SubscriptionStateUpsertInput{
		ProviderCode:      processor.providerCode,
		UserEmail:         userEmail,
		Status:            subscriptionStatus,
		ProviderStatus:    strings.ToLower(strings.TrimSpace(payload.Data.Status)),
		ActivePlan:        planCode,
		SubscriptionID:    strings.TrimSpace(payload.Data.ID),
		NextBillingAt:     resolvePaddleSubscriptionNextBillingAt(payload.Data),
		LastEventID:       strings.TrimSpace(event.EventID),
		LastEventType:     strings.TrimSpace(event.EventType),
		EventOccurredAt:   event.OccurredAt,
		LastTransactionID: "",
	})
	if upsertErr != nil {
		return fmt.Errorf("billing.webhook.subscription_state.upsert: %w", upsertErr)
	}
	return nil
}

func resolvePaddleSubscriptionState(eventType string, rawStatus string) string {
	normalizedEventType := strings.ToLower(strings.TrimSpace(eventType))
	if normalizedEventType == paddleEventTypeSubscriptionCanceled {
		return subscriptionStatusInactive
	}
	normalizedStatus := strings.ToLower(strings.TrimSpace(rawStatus))
	switch normalizedStatus {
	case paddleSubscriptionStatusActive, paddleSubscriptionStatusTrialing:
		return subscriptionStatusActive
	case paddleSubscriptionStatusPaused,
		paddleSubscriptionStatusCanceled,
		paddleSubscriptionStatusInactive,
		paddleSubscriptionStatusPastDue:
		return subscriptionStatusInactive
	}
	switch normalizedEventType {
	case paddleEventTypeSubscriptionCreated,
		paddleEventTypeSubscriptionResumed,
		paddleEventTypeSubscriptionActivated:
		return subscriptionStatusActive
	default:
		return subscriptionStatusInactive
	}
}

func (processor *subscriptionStatusWebhookProcessor) resolveLifecycleUserEmail(
	ctx context.Context,
	payloadData paddleSubscriptionWebhookData,
) (string, error) {
	userEmail, userEmailErr := processor.resolvePayloadUserEmail(ctx, payloadData)
	if userEmailErr == nil {
		return userEmail, nil
	}
	subscriptionID := strings.TrimSpace(payloadData.ID)
	if subscriptionID == "" {
		return "", userEmailErr
	}
	state, found, stateErr := processor.stateRepository.GetBySubscriptionID(
		ctx,
		processor.providerCode,
		subscriptionID,
	)
	if stateErr != nil {
		return "", fmt.Errorf("billing.webhook.subscription_state.get_by_subscription_id: %w", stateErr)
	}
	if !found {
		return "", userEmailErr
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(state.UserEmail))
	if normalizedUserEmail == "" {
		return "", ErrWebhookGrantMetadataInvalid
	}
	return normalizedUserEmail, nil
}

func (processor *subscriptionStatusWebhookProcessor) resolvePayloadUserEmail(
	ctx context.Context,
	payloadData paddleSubscriptionWebhookData,
) (string, error) {
	userEmail := webhookMetadataValue(payloadData.CustomData, paddleMetadataUserEmailKey)
	if userEmail == "" {
		userEmail = resolvePaddleCustomerEmail(payloadData.Customer)
	}
	if userEmail == "" {
		customerID := strings.TrimSpace(payloadData.CustomerID)
		if customerID == "" || processor.customerEmailResolver == nil {
			return "", ErrWebhookGrantMetadataInvalid
		}
		resolvedUserEmail, resolveErr := processor.customerEmailResolver.ResolveCustomerEmail(ctx, customerID)
		if resolveErr != nil {
			return "", ErrWebhookGrantMetadataInvalid
		}
		userEmail = strings.TrimSpace(resolvedUserEmail)
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return "", ErrWebhookGrantMetadataInvalid
	}
	return normalizedUserEmail, nil
}

func (processor *subscriptionStatusWebhookProcessor) resolvePlanCode(
	payloadData paddleSubscriptionWebhookData,
) string {
	planCode := strings.ToLower(webhookMetadataValue(payloadData.CustomData, paddleMetadataPlanCodeKey))
	if planCode != "" {
		return planCode
	}
	priceID := resolvePaddleSubscriptionPriceID(payloadData)
	if priceID == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(processor.planCodeByPriceID[priceID]))
}

func resolvePaddleSubscriptionPriceID(payloadData paddleSubscriptionWebhookData) string {
	for _, item := range payloadData.Items {
		priceID := strings.TrimSpace(item.PriceID)
		if priceID != "" {
			return priceID
		}
		priceID = strings.TrimSpace(item.Price.ID)
		if priceID != "" {
			return priceID
		}
	}
	return ""
}

func resolvePaddleSubscriptionNextBillingAt(payloadData paddleSubscriptionWebhookData) time.Time {
	nextBillingAt, nextBillingAtErr := parsePaddleTimestamp(payloadData.NextBilledAt)
	if nextBillingAtErr == nil && !nextBillingAt.IsZero() {
		return nextBillingAt
	}
	currentPeriodEndAt, currentPeriodEndAtErr := parsePaddleTimestamp(payloadData.CurrentBillingPeriod.EndsAt)
	if currentPeriodEndAtErr == nil && !currentPeriodEndAt.IsZero() {
		return currentPeriodEndAt
	}
	return time.Time{}
}

func isStaleSubscriptionEvent(existingEventOccurredAt time.Time, incomingEventOccurredAt time.Time) bool {
	if existingEventOccurredAt.IsZero() || incomingEventOccurredAt.IsZero() {
		return false
	}
	return incomingEventOccurredAt.Before(existingEventOccurredAt)
}

func isSyntheticSyncEvent(eventID string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(eventID)), "sync:")
}

type paddleSubscriptionWebhookPayload struct {
	Data paddleSubscriptionWebhookData `json:"data"`
}

type paddleSubscriptionWebhookData struct {
	ID                   string                             `json:"id"`
	Status               string                             `json:"status"`
	UpdatedAt            string                             `json:"updated_at"`
	CustomerID           string                             `json:"customer_id"`
	Customer             paddleTransactionCompletedCustomer `json:"customer"`
	CustomData           map[string]interface{}             `json:"custom_data"`
	Items                []paddleSubscriptionWebhookItem    `json:"items"`
	NextBilledAt         string                             `json:"next_billed_at"`
	CurrentBillingPeriod paddleSubscriptionBillingPeriod    `json:"current_billing_period"`
}

type paddleSubscriptionBillingPeriod struct {
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
}

type paddleSubscriptionWebhookItem struct {
	PriceID string                             `json:"price_id"`
	Price   paddleSubscriptionWebhookItemPrice `json:"price"`
}

type paddleSubscriptionWebhookItemPrice struct {
	ID string `json:"id"`
}
