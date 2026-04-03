package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	subscriptionStatusInactive = "inactive"
	subscriptionStatusActive   = "active"
)

type SubscriptionSummary struct {
	ProviderCode        string
	Enabled             bool
	Status              string
	ProviderStatus      string
	IsDelinquent        bool
	DelinquentReason    string
	ActivePlan          string
	SubscriptionID      string
	NextBillingAt       time.Time
	LastEventType       string
	LastEventOccurredAt time.Time
	Environment         string
	ClientToken         string
	Plans               []SubscriptionPlan
	TopUpPacks          []TopUpPack
}

type Service struct {
	provider                    CommerceProvider
	subscriptionStateRepository SubscriptionStateRepository
	webhookProcessor            WebhookProcessor
	topUpEligibilityPolicy      TopUpEligibilityPolicy
}

func NewService(provider CommerceProvider, subscriptionStateReaders ...SubscriptionStateRepository) *Service {
	var subscriptionStateRepository SubscriptionStateRepository
	if len(subscriptionStateReaders) > 0 {
		subscriptionStateRepository = subscriptionStateReaders[0]
	}
	return &Service{
		provider:                    provider,
		subscriptionStateRepository: subscriptionStateRepository,
		webhookProcessor:            nil,
		topUpEligibilityPolicy:      TopUpEligibilityPolicyRequiresActiveSubscription,
	}
}

func NewServiceWithWebhookProcessor(
	provider CommerceProvider,
	webhookProcessor WebhookProcessor,
	subscriptionStateReaders ...SubscriptionStateRepository,
) *Service {
	service := NewService(provider, subscriptionStateReaders...)
	service.webhookProcessor = resolveWebhookProcessor(webhookProcessor)
	return service
}

func (service *Service) WithTopUpEligibilityPolicy(policy TopUpEligibilityPolicy) *Service {
	if service == nil {
		return nil
	}
	service.topUpEligibilityPolicy = NormalizeTopUpEligibilityPolicy(policy)
	return service
}

func (service *Service) ProviderCode() string {
	if service == nil || service.provider == nil {
		return ""
	}
	return service.provider.Code()
}

var (
	ErrBillingCheckoutReconciliationUnavailable = errors.New("billing.checkout.reconcile.unavailable")
	ErrBillingCheckoutReconciliationUnsupported = errors.New("billing.checkout.reconcile.provider.unsupported")
	ErrBillingCheckoutTransactionInvalid        = errors.New("billing.checkout.transaction.invalid")
	ErrBillingCheckoutTransactionPending        = errors.New("billing.checkout.transaction.pending")
	ErrBillingCheckoutOwnershipMismatch         = errors.New("billing.checkout.ownership.mismatch")
)

func (service *Service) GetSubscriptionSummary(ctx context.Context, userEmail string) (SubscriptionSummary, error) {
	if service == nil || service.provider == nil {
		return SubscriptionSummary{}, ErrBillingProviderUnavailable
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return SubscriptionSummary{}, ErrBillingUserEmailInvalid
	}
	publicConfig := service.provider.PublicConfig()
	summary := SubscriptionSummary{
		ProviderCode:        service.provider.Code(),
		Enabled:             true,
		Status:              subscriptionStatusInactive,
		ProviderStatus:      "",
		IsDelinquent:        false,
		DelinquentReason:    "",
		ActivePlan:          "",
		SubscriptionID:      "",
		NextBillingAt:       time.Time{},
		LastEventType:       "",
		LastEventOccurredAt: time.Time{},
		Environment:         publicConfig.Environment,
		ClientToken:         publicConfig.ClientToken,
		Plans:               service.provider.SubscriptionPlans(),
		TopUpPacks:          service.provider.TopUpPacks(),
	}
	if service.subscriptionStateRepository == nil {
		return summary, nil
	}
	state, found, stateErr := service.subscriptionStateRepository.Get(
		ctx,
		service.provider.Code(),
		normalizedUserEmail,
	)
	if stateErr != nil {
		return SubscriptionSummary{}, fmt.Errorf("billing.subscription.summary.state: %w", stateErr)
	}
	if !found {
		return summary, nil
	}
	summary.Status = strings.ToLower(strings.TrimSpace(state.Status))
	summary.ProviderStatus = strings.ToLower(strings.TrimSpace(state.ProviderStatus))
	summary.ActivePlan = strings.ToLower(strings.TrimSpace(state.ActivePlan))
	summary.SubscriptionID = strings.TrimSpace(state.SubscriptionID)
	summary.NextBillingAt = state.NextBillingAt
	summary.LastEventType = strings.TrimSpace(state.LastEventType)
	summary.LastEventOccurredAt = state.LastEventOccurredAt
	summary.IsDelinquent, summary.DelinquentReason = resolveSubscriptionDelinquency(
		summary.ProviderStatus,
		summary.LastEventType,
	)
	return summary, nil
}

func resolveSubscriptionDelinquency(providerStatus string, lastEventType string) (bool, string) {
	normalizedProviderStatus := strings.ToLower(strings.TrimSpace(providerStatus))
	switch normalizedProviderStatus {
	case paddleSubscriptionStatusPastDue,
		stripeSubscriptionStatusUnpaid,
		stripeSubscriptionStatusIncomplete,
		stripeSubscriptionStatusIncompleteExpired,
		"payment_failed",
		"delinquent":
		return true, normalizedProviderStatus
	}

	normalizedEventType := strings.ToLower(strings.TrimSpace(lastEventType))
	switch normalizedEventType {
	case stripeEventTypeCheckoutSessionAsyncPaymentFailed,
		"invoice.payment_failed",
		"invoice.payment_action_required":
		return true, "payment_failed"
	}
	if strings.Contains(normalizedEventType, "payment_failed") {
		return true, "payment_failed"
	}
	return false, ""
}

func (service *Service) SyncUserBillingEvents(ctx context.Context, userEmail string) error {
	if syncErr := service.syncUserBillingEvents(ctx, userEmail); syncErr != nil {
		return fmt.Errorf("billing.user.sync: %w", syncErr)
	}
	return nil
}

func (service *Service) syncUserBillingEvents(ctx context.Context, userEmail string) error {
	if service == nil || service.provider == nil {
		return ErrBillingProviderUnavailable
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return ErrBillingUserEmailInvalid
	}
	syncEvents, syncEventsErr := service.provider.BuildUserSyncEvents(ctx, normalizedUserEmail)
	if syncEventsErr != nil {
		return fmt.Errorf("%w: %w", ErrBillingUserSyncFailed, syncEventsErr)
	}
	if len(syncEvents) == 0 {
		return nil
	}
	if service.webhookProcessor == nil {
		return fmt.Errorf("%w: webhook processor unavailable", ErrBillingUserSyncFailed)
	}
	processor := resolveWebhookProcessor(service.webhookProcessor)
	for _, syncEvent := range syncEvents {
		if processErr := processor.Process(ctx, syncEvent); processErr != nil {
			if isWebhookProcessErrorNonRetryable(processErr) {
				continue
			}
			return fmt.Errorf("%w: %w", ErrBillingUserSyncFailed, processErr)
		}
	}
	return nil
}

func (service *Service) CreateSubscriptionCheckout(
	ctx context.Context,
	customer CustomerContext,
	planCode string,
) (CheckoutSession, error) {
	if service == nil || service.provider == nil {
		return CheckoutSession{}, ErrBillingProviderUnavailable
	}
	normalizedCustomer := NormalizeCustomerContext(customer)
	normalizedUserEmail := normalizedCustomer.Email
	if normalizedUserEmail == "" {
		return CheckoutSession{}, ErrBillingUserEmailInvalid
	}
	normalizedPlanCode := strings.ToLower(strings.TrimSpace(planCode))
	if normalizedPlanCode == "" {
		return CheckoutSession{}, ErrBillingPlanUnsupported
	}
	if validationErr := service.validateSubscriptionPlan(normalizedPlanCode); validationErr != nil {
		return CheckoutSession{}, validationErr
	}
	liveSubscriptions, subscriptionsInspected, inspectErr := service.inspectLiveSubscriptions(
		ctx,
		normalizedUserEmail,
	)
	if inspectErr != nil {
		return CheckoutSession{}, fmt.Errorf("billing.checkout.subscription.inspect: %w", inspectErr)
	}
	if subscriptionsInspected && len(liveSubscriptions) > 0 {
		if refreshErr := service.refreshSubscriptionStateFromInspection(
			ctx,
			normalizedUserEmail,
			liveSubscriptions,
		); refreshErr != nil {
			return CheckoutSession{}, fmt.Errorf("billing.checkout.subscription.state: %w", refreshErr)
		}
		if activeProviderSubscriptionExists(liveSubscriptions) {
			return CheckoutSession{}, ErrBillingSubscriptionManageInPortal
		}
	} else if validationErr := service.validateSubscriptionCheckoutTransition(ctx, normalizedUserEmail, normalizedPlanCode); validationErr != nil {
		return CheckoutSession{}, validationErr
	}
	session, err := service.provider.CreateSubscriptionCheckout(ctx, normalizedCustomer, normalizedPlanCode)
	if err != nil {
		return CheckoutSession{}, fmt.Errorf("billing.checkout.subscription: %w", err)
	}
	return session, nil
}

func (service *Service) inspectLiveSubscriptions(
	ctx context.Context,
	userEmail string,
) ([]ProviderSubscription, bool, error) {
	if service == nil || service.provider == nil {
		return nil, false, ErrBillingProviderUnavailable
	}
	subscriptionInspector, hasSubscriptionInspector := service.provider.(SubscriptionInspector)
	if !hasSubscriptionInspector {
		return nil, false, nil
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return nil, false, ErrBillingUserEmailInvalid
	}
	subscriptions, inspectErr := subscriptionInspector.InspectSubscriptions(ctx, normalizedUserEmail)
	if inspectErr != nil {
		return nil, true, inspectErr
	}
	return subscriptions, true, nil
}

func (service *Service) refreshSubscriptionStateFromInspection(
	ctx context.Context,
	userEmail string,
	subscriptions []ProviderSubscription,
) error {
	if service == nil || service.provider == nil {
		return ErrBillingProviderUnavailable
	}
	if service.subscriptionStateRepository == nil {
		return nil
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return ErrBillingUserEmailInvalid
	}
	canonicalSubscription, foundCanonicalSubscription := canonicalProviderSubscription(subscriptions)
	upsertInput := SubscriptionStateUpsertInput{
		ProviderCode: service.provider.Code(),
		UserEmail:    normalizedUserEmail,
		Status:       subscriptionStatusInactive,
	}
	if foundCanonicalSubscription {
		upsertInput.Status = canonicalSubscription.Status
		upsertInput.ProviderStatus = canonicalSubscription.ProviderStatus
		upsertInput.ActivePlan = canonicalSubscription.PlanCode
		upsertInput.SubscriptionID = canonicalSubscription.SubscriptionID
		upsertInput.NextBillingAt = canonicalSubscription.NextBillingAt
		upsertInput.EventOccurredAt = canonicalSubscription.OccurredAt
	}
	if upsertErr := service.subscriptionStateRepository.Upsert(ctx, upsertInput); upsertErr != nil {
		return upsertErr
	}
	return nil
}

func (service *Service) validateSubscriptionCheckoutTransition(
	ctx context.Context,
	userEmail string,
	planCode string,
) error {
	if service == nil || service.provider == nil {
		return ErrBillingProviderUnavailable
	}
	if service.subscriptionStateRepository == nil {
		return nil
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return ErrBillingUserEmailInvalid
	}
	if strings.TrimSpace(planCode) == "" {
		return ErrBillingPlanUnsupported
	}
	if validationErr := service.validateSubscriptionPlan(planCode); validationErr != nil {
		return validationErr
	}
	state, found, stateErr := service.subscriptionStateRepository.Get(
		ctx,
		service.provider.Code(),
		normalizedUserEmail,
	)
	if stateErr != nil {
		return fmt.Errorf("billing.checkout.subscription.state: %w", stateErr)
	}
	if !found {
		return nil
	}
	if strings.ToLower(strings.TrimSpace(state.Status)) != subscriptionStatusActive {
		return nil
	}
	return ErrBillingSubscriptionManageInPortal
}

func (service *Service) validateSubscriptionPlan(planCode string) error {
	if service == nil || service.provider == nil {
		return ErrBillingProviderUnavailable
	}
	normalizedPlanCode := strings.ToLower(strings.TrimSpace(planCode))
	if normalizedPlanCode == "" {
		return ErrBillingPlanUnsupported
	}
	plans := service.provider.SubscriptionPlans()
	if len(plans) == 0 {
		return nil
	}
	for _, plan := range plans {
		if strings.ToLower(strings.TrimSpace(plan.Code)) == normalizedPlanCode {
			return nil
		}
	}
	return ErrBillingPlanUnsupported
}

func (service *Service) resolvePlanMonthlyCredits(planCode string) (int64, bool) {
	if service == nil || service.provider == nil {
		return 0, false
	}
	normalizedPlanCode := strings.ToLower(strings.TrimSpace(planCode))
	if normalizedPlanCode == "" {
		return 0, false
	}
	for _, plan := range service.provider.SubscriptionPlans() {
		if strings.ToLower(strings.TrimSpace(plan.Code)) == normalizedPlanCode {
			return plan.MonthlyCredits, true
		}
	}
	return 0, false
}

func (service *Service) CreateTopUpCheckout(
	ctx context.Context,
	customer CustomerContext,
	packCode string,
) (CheckoutSession, error) {
	if service == nil || service.provider == nil {
		return CheckoutSession{}, ErrBillingProviderUnavailable
	}
	normalizedCustomer := NormalizeCustomerContext(customer)
	normalizedUserEmail := normalizedCustomer.Email
	if normalizedUserEmail == "" {
		return CheckoutSession{}, ErrBillingUserEmailInvalid
	}
	normalizedPackCode := NormalizePackCode(packCode)
	if normalizedPackCode == "" {
		return CheckoutSession{}, ErrBillingTopUpPackUnknown
	}
	if validationErr := service.validateTopUpCheckoutEligibility(ctx, normalizedUserEmail); validationErr != nil {
		return CheckoutSession{}, validationErr
	}
	session, err := service.provider.CreateTopUpCheckout(ctx, normalizedCustomer, normalizedPackCode)
	if err != nil {
		return CheckoutSession{}, fmt.Errorf("billing.checkout.top_up: %w", err)
	}
	return session, nil
}

func (service *Service) validateTopUpCheckoutEligibility(
	ctx context.Context,
	userEmail string,
) error {
	if service == nil || service.provider == nil {
		return ErrBillingProviderUnavailable
	}
	if NormalizeTopUpEligibilityPolicy(service.topUpEligibilityPolicy) == TopUpEligibilityPolicyUnrestricted {
		return nil
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return ErrBillingUserEmailInvalid
	}
	liveSubscriptions, subscriptionsInspected, inspectErr := service.inspectLiveSubscriptions(
		ctx,
		normalizedUserEmail,
	)
	if inspectErr != nil {
		return fmt.Errorf("billing.checkout.top_up.inspect: %w", inspectErr)
	}
	if subscriptionsInspected {
		if refreshErr := service.refreshSubscriptionStateFromInspection(
			ctx,
			normalizedUserEmail,
			liveSubscriptions,
		); refreshErr != nil {
			return fmt.Errorf("billing.checkout.top_up.state: %w", refreshErr)
		}
		if activeProviderSubscriptionExists(liveSubscriptions) {
			return nil
		}
		return ErrBillingSubscriptionRequired
	}
	if service.subscriptionStateRepository == nil {
		return ErrBillingSubscriptionRequired
	}
	state, found, stateErr := service.subscriptionStateRepository.Get(
		ctx,
		service.provider.Code(),
		normalizedUserEmail,
	)
	if stateErr != nil {
		return fmt.Errorf("billing.checkout.top_up.state: %w", stateErr)
	}
	if !found {
		return ErrBillingSubscriptionRequired
	}
	normalizedStatus := strings.ToLower(strings.TrimSpace(state.Status))
	if normalizedStatus != subscriptionStatusActive {
		return ErrBillingSubscriptionRequired
	}
	return nil
}

func (service *Service) CreatePortalSession(ctx context.Context, userEmail string) (PortalSession, error) {
	if service == nil || service.provider == nil {
		return PortalSession{}, ErrBillingProviderUnavailable
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return PortalSession{}, ErrBillingUserEmailInvalid
	}
	portalSession, err := service.provider.CreateCustomerPortalSession(ctx, normalizedUserEmail)
	if err != nil {
		return PortalSession{}, fmt.Errorf("billing.portal: %w", err)
	}
	return portalSession, nil
}

func (service *Service) ReconcileCheckout(
	ctx context.Context,
	userEmail string,
	transactionID string,
) error {
	if service == nil || service.provider == nil {
		return ErrBillingProviderUnavailable
	}
	if service.webhookProcessor == nil {
		return ErrBillingCheckoutReconciliationUnavailable
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(userEmail))
	if normalizedUserEmail == "" {
		return ErrBillingUserEmailInvalid
	}
	normalizedTransactionID := strings.TrimSpace(transactionID)
	if normalizedTransactionID == "" {
		return ErrBillingCheckoutTransactionInvalid
	}
	checkoutReconcileProvider, hasCheckoutReconcileProvider := service.provider.(CheckoutReconcileProvider)
	if !hasCheckoutReconcileProvider {
		return ErrBillingCheckoutReconciliationUnsupported
	}

	webhookEvent, checkoutUserEmail, eventErr := checkoutReconcileProvider.BuildCheckoutReconcileEvent(ctx, normalizedTransactionID)
	if eventErr != nil {
		return fmt.Errorf("billing.checkout.reconcile.event: %w", eventErr)
	}
	normalizedCheckoutUserEmail := strings.ToLower(strings.TrimSpace(checkoutUserEmail))
	if normalizedCheckoutUserEmail != normalizedUserEmail {
		return fmt.Errorf("%w: %s", ErrBillingCheckoutOwnershipMismatch, normalizedTransactionID)
	}
	if isCheckoutReconcileEventPending(service.provider, webhookEvent.EventType) {
		return ErrBillingCheckoutTransactionPending
	}
	if processErr := service.webhookProcessor.Process(ctx, webhookEvent); processErr != nil {
		return fmt.Errorf("billing.checkout.reconcile.process: %w", processErr)
	}
	return nil
}

func isCheckoutReconcileEventPending(provider CommerceProvider, eventType string) bool {
	if provider == nil {
		return false
	}
	checkoutEventStatusProvider, isCheckoutEventStatusProvider := provider.(CheckoutEventStatusProvider)
	if !isCheckoutEventStatusProvider {
		return false
	}
	return checkoutEventStatusProvider.ResolveCheckoutEventStatus(eventType) == CheckoutEventStatusPending
}
