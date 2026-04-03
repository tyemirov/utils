package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	stripeGrantReferenceSubscriptionPrefix = "stripe:subscription"
	stripeGrantReferenceTopUpPackPrefix    = "stripe:top_up_pack"
)

type stripeGrantDefinition struct {
	Code    string
	Credits int64
}

type stripeCustomerEmailResolver interface {
	ResolveCustomerEmail(context.Context, string) (string, error)
}

type stripeWebhookGrantResolver struct {
	planCreditsByCode     map[string]int64
	packCreditsByCode     map[string]int64
	planGrantByPriceID    map[string]stripeGrantDefinition
	packGrantByPriceID    map[string]stripeGrantDefinition
	customerEmailResolver stripeCustomerEmailResolver
	eventStatusProvider   CheckoutEventStatusProvider
}

type stripeSubscriptionStatusWebhookProcessor struct {
	providerCode          string
	stateRepository       SubscriptionStateRepository
	grantResolver         WebhookGrantResolver
	customerEmailResolver stripeCustomerEmailResolver
	planCodeByPriceID     map[string]string
	eventStatusProvider   CheckoutEventStatusProvider
}

type stripeCheckoutSessionWebhookPayload struct {
	Data stripeCheckoutSessionWebhookPayloadData `json:"data"`
}

type stripeCheckoutSessionWebhookPayloadData struct {
	Object stripeCheckoutSessionWebhookData `json:"object"`
}

type stripeCheckoutSessionWebhookData struct {
	ID              string             `json:"id"`
	Status          string             `json:"status"`
	PaymentStatus   string             `json:"payment_status"`
	Mode            string             `json:"mode"`
	CustomerID      string             `json:"customer"`
	CustomerEmail   string             `json:"customer_email"`
	CustomerDetails stripeCustomerData `json:"customer_details"`
	Metadata        map[string]string  `json:"metadata"`
	SubscriptionID  string             `json:"subscription"`
	CreatedAt       int64              `json:"created"`
}

type stripeCustomerData struct {
	Email string `json:"email"`
}

type stripeSubscriptionWebhookPayload struct {
	Data stripeSubscriptionWebhookPayloadData `json:"data"`
}

type stripeSubscriptionWebhookPayloadData struct {
	Object stripeSubscriptionWebhookData `json:"object"`
}

type stripeSubscriptionWebhookData struct {
	ID               string                  `json:"id"`
	Status           string                  `json:"status"`
	CustomerID       string                  `json:"customer"`
	Metadata         map[string]string       `json:"metadata"`
	Items            stripeSubscriptionItems `json:"items"`
	CurrentPeriodEnd int64                   `json:"current_period_end"`
	CreatedAt        int64                   `json:"created"`
}

type stripeSubscriptionItems struct {
	Data []stripeSubscriptionItem `json:"data"`
}

type stripeSubscriptionItem struct {
	Price stripeSubscriptionItemPrice `json:"price"`
}

type stripeSubscriptionItemPrice struct {
	ID string `json:"id"`
}

func newStripeWebhookGrantResolverFromProvider(provider *StripeProvider) (*stripeWebhookGrantResolver, error) {
	if provider == nil {
		return nil, ErrWebhookGrantResolverProviderUnavailable
	}
	planGrantByPriceID := make(map[string]stripeGrantDefinition, len(provider.plans))
	for _, definition := range provider.plans {
		normalizedPriceID := strings.TrimSpace(definition.PriceID)
		normalizedPlanCode := strings.ToLower(strings.TrimSpace(definition.Plan.Code))
		if normalizedPriceID == "" || normalizedPlanCode == "" || definition.Plan.MonthlyCredits <= 0 {
			continue
		}
		planGrantByPriceID[normalizedPriceID] = stripeGrantDefinition{
			Code:    normalizedPlanCode,
			Credits: definition.Plan.MonthlyCredits,
		}
	}
	packGrantByPriceID := make(map[string]stripeGrantDefinition, len(provider.packs))
	for _, definition := range provider.packs {
		normalizedPriceID := strings.TrimSpace(definition.PriceID)
		normalizedPackCode := NormalizePackCode(definition.Pack.Code)
		if normalizedPriceID == "" || normalizedPackCode == "" || definition.Pack.Credits <= 0 {
			continue
		}
		packGrantByPriceID[normalizedPriceID] = stripeGrantDefinition{
			Code:    normalizedPackCode,
			Credits: definition.Pack.Credits,
		}
	}
	return newStripeWebhookGrantResolverWithCatalog(
		provider.SubscriptionPlans(),
		provider.TopUpPacks(),
		planGrantByPriceID,
		packGrantByPriceID,
		provider.client,
		provider,
	)
}

func newStripeWebhookGrantResolverWithCatalog(
	plans []SubscriptionPlan,
	packs []TopUpPack,
	planGrantByPriceID map[string]stripeGrantDefinition,
	packGrantByPriceID map[string]stripeGrantDefinition,
	customerEmailResolver stripeCustomerEmailResolver,
	eventStatusProvider CheckoutEventStatusProvider,
) (*stripeWebhookGrantResolver, error) {
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
	return &stripeWebhookGrantResolver{
		planCreditsByCode:     planCreditsByCode,
		packCreditsByCode:     packCreditsByCode,
		planGrantByPriceID:    cloneStripeGrantDefinitionsByPriceID(planGrantByPriceID),
		packGrantByPriceID:    cloneStripeGrantDefinitionsByPriceID(packGrantByPriceID),
		customerEmailResolver: customerEmailResolver,
		eventStatusProvider:   eventStatusProvider,
	}, nil
}

func cloneStripeGrantDefinitionsByPriceID(
	source map[string]stripeGrantDefinition,
) map[string]stripeGrantDefinition {
	if len(source) == 0 {
		return map[string]stripeGrantDefinition{}
	}
	target := make(map[string]stripeGrantDefinition, len(source))
	for rawPriceID, grantDefinition := range source {
		normalizedPriceID := strings.TrimSpace(rawPriceID)
		normalizedCode := strings.ToLower(strings.TrimSpace(grantDefinition.Code))
		if normalizedPriceID == "" || normalizedCode == "" || grantDefinition.Credits <= 0 {
			continue
		}
		target[normalizedPriceID] = stripeGrantDefinition{
			Code:    normalizedCode,
			Credits: grantDefinition.Credits,
		}
	}
	return target
}

func (resolver *stripeWebhookGrantResolver) Resolve(
	ctx context.Context,
	event WebhookEvent,
) (WebhookGrant, bool, error) {
	normalizedProviderCode := strings.ToLower(strings.TrimSpace(event.ProviderCode))
	if normalizedProviderCode != ProviderCodeStripe {
		return WebhookGrant{}, false, nil
	}
	if resolver.eventStatusProvider == nil {
		return WebhookGrant{}, false, ErrWebhookGrantResolverUnavailable
	}
	checkoutEventStatus := resolver.eventStatusProvider.ResolveCheckoutEventStatus(event.EventType)
	if checkoutEventStatus != CheckoutEventStatusSucceeded {
		return WebhookGrant{}, false, nil
	}

	payload := stripeCheckoutSessionWebhookPayload{}
	if decodeErr := json.Unmarshal(event.Payload, &payload); decodeErr != nil {
		return WebhookGrant{}, false, ErrWebhookGrantPayloadInvalid
	}
	checkoutSession := payload.Data.Object
	if !isStripeCheckoutSessionPaid(checkoutSession) {
		return WebhookGrant{}, false, nil
	}

	sessionID := strings.TrimSpace(checkoutSession.ID)
	if sessionID == "" {
		return WebhookGrant{}, false, ErrWebhookGrantPayloadInvalid
	}

	priceID := strings.TrimSpace(checkoutSession.Metadata[stripeMetadataPriceIDKey])
	if priceID == "" {
		priceID = metadataValue(
			checkoutSession.Metadata,
			billingMetadataPriceIDKey,
			stripeLegacyMetadataPriceIDKey,
		)
	}
	subjectID := metadataValue(
		checkoutSession.Metadata,
		billingMetadataSubjectIDKey,
		crosswordLegacyMetadataSubjectIDKey,
	)
	userEmail, userEmailErr := resolver.resolveUserEmail(ctx, checkoutSession)
	if userEmailErr != nil {
		return WebhookGrant{}, false, userEmailErr
	}

	purchaseKind := NormalizePurchaseKind(
		metadataValue(
			checkoutSession.Metadata,
			billingMetadataPurchaseKindKey,
			stripeLegacyMetadataPurchaseKindKey,
		),
	)
	if purchaseKind == "" {
		switch strings.ToLower(strings.TrimSpace(checkoutSession.Mode)) {
		case stripeCheckoutModeSubscriptionRaw:
			purchaseKind = stripePurchaseKindSubscription
		case stripeCheckoutModePaymentRaw:
			purchaseKind = stripePurchaseKindTopUpPack
		}
	}
	if purchaseKind == "" {
		purchaseKind = resolver.resolvePurchaseKindFromPriceID(priceID)
	}
	if purchaseKind == "" {
		return WebhookGrant{}, false, ErrWebhookGrantMetadataInvalid
	}

	switch purchaseKind {
	case stripePurchaseKindSubscription:
		planCode := strings.ToLower(
			metadataValue(
				checkoutSession.Metadata,
				billingMetadataPlanCodeKey,
				stripeLegacyMetadataPlanCodeKey,
			),
		)
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
		reference := fmt.Sprintf("%s:%s:%s", stripeGrantReferenceSubscriptionPrefix, sessionID, planCode)
		metadata := map[string]string{
			billingGrantMetadataPurchaseKindKey:  purchaseKind,
			billingGrantMetadataTransactionIDKey: sessionID,
			billingGrantMetadataPlanCodeKey:      planCode,
		}
		subscriptionID := strings.TrimSpace(checkoutSession.SubscriptionID)
		if subscriptionID != "" {
			metadata[billingGrantMetadataSubscriptionIDKey] = subscriptionID
		}
		if priceID != "" {
			metadata[billingGrantMetadataPriceIDKey] = priceID
		}
		return WebhookGrant{
			UserEmail: userEmail,
			SubjectID: subjectID,
			Credits:   planCredits,
			Reason:    reason,
			Reference: reference,
			Metadata:  metadata,
		}, true, nil
	case stripePurchaseKindTopUpPack:
		packCode := NormalizePackCode(
			metadataValue(
				checkoutSession.Metadata,
				billingMetadataPackCodeKey,
				stripeLegacyMetadataPackCodeKey,
			),
		)
		if packCode == "" {
			packGrantDefinition, hasPackGrantDefinition := resolver.packGrantByPriceID[priceID]
			if hasPackGrantDefinition {
				packCode = packGrantDefinition.Code
			}
		}
		packCreditsFromMetadata, packCreditsMetadataErr := parseStripeWebhookMetadataInt64(
			checkoutSession.Metadata,
			billingMetadataPackCreditsKey,
			stripeLegacyMetadataPackCreditsKey,
		)
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
					if packCode == "" {
						packCode = packGrantDefinition.Code
					}
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
			stripeGrantReferenceTopUpPackPrefix,
			sessionID,
			packReferenceCode(packCode),
		)
		metadata := map[string]string{
			billingGrantMetadataPurchaseKindKey:  purchaseKind,
			billingGrantMetadataTransactionIDKey: sessionID,
			billingGrantMetadataPackCodeKey:      packCode,
		}
		if priceID != "" {
			metadata[billingGrantMetadataPriceIDKey] = priceID
		}
		return WebhookGrant{
			UserEmail: userEmail,
			SubjectID: subjectID,
			Credits:   packCredits,
			Reason:    reason,
			Reference: reference,
			Metadata:  metadata,
		}, true, nil
	default:
		return WebhookGrant{}, false, ErrWebhookGrantMetadataInvalid
	}
}

func (resolver *stripeWebhookGrantResolver) resolveUserEmail(
	ctx context.Context,
	checkoutSession stripeCheckoutSessionWebhookData,
) (string, error) {
	userEmail := strings.ToLower(
		metadataValue(
			checkoutSession.Metadata,
			billingMetadataUserEmailKey,
			stripeLegacyMetadataUserEmailKey,
			crosswordLegacyMetadataUserEmailKey,
		),
	)
	if userEmail != "" {
		return userEmail, nil
	}
	userEmail = strings.ToLower(strings.TrimSpace(checkoutSession.CustomerEmail))
	if userEmail != "" {
		return userEmail, nil
	}
	userEmail = strings.ToLower(strings.TrimSpace(checkoutSession.CustomerDetails.Email))
	if userEmail != "" {
		return userEmail, nil
	}
	customerID := strings.TrimSpace(checkoutSession.CustomerID)
	if customerID == "" || resolver.customerEmailResolver == nil {
		return "", ErrWebhookGrantMetadataInvalid
	}
	resolvedCustomerEmail, customerEmailErr := resolver.customerEmailResolver.ResolveCustomerEmail(ctx, customerID)
	if customerEmailErr != nil {
		return "", ErrWebhookGrantMetadataInvalid
	}
	normalizedCustomerEmail := strings.ToLower(strings.TrimSpace(resolvedCustomerEmail))
	if normalizedCustomerEmail == "" {
		return "", ErrWebhookGrantMetadataInvalid
	}
	return normalizedCustomerEmail, nil
}

func (resolver *stripeWebhookGrantResolver) resolvePurchaseKindFromPriceID(priceID string) string {
	normalizedPriceID := strings.TrimSpace(priceID)
	if normalizedPriceID == "" {
		return ""
	}
	if _, hasPlanGrant := resolver.planGrantByPriceID[normalizedPriceID]; hasPlanGrant {
		return stripePurchaseKindSubscription
	}
	if _, hasPackGrant := resolver.packGrantByPriceID[normalizedPriceID]; hasPackGrant {
		return stripePurchaseKindTopUpPack
	}
	return ""
}

func isStripeCheckoutSessionPaid(checkoutSession stripeCheckoutSessionWebhookData) bool {
	return strings.ToLower(strings.TrimSpace(checkoutSession.Status)) == stripeCheckoutStatusComplete &&
		strings.ToLower(strings.TrimSpace(checkoutSession.PaymentStatus)) == stripeCheckoutPaymentStatusPaid
}

func newStripeSubscriptionStatusWebhookProcessor(
	provider *StripeProvider,
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
	return &stripeSubscriptionStatusWebhookProcessor{
		providerCode:          ProviderCodeStripe,
		stateRepository:       stateRepository,
		grantResolver:         grantResolver,
		customerEmailResolver: provider.client,
		planCodeByPriceID:     buildStripePlanCodeByPriceID(provider.plans),
		eventStatusProvider:   provider,
	}, nil
}

func buildStripePlanCodeByPriceID(planDefinitions map[string]stripePlanDefinition) map[string]string {
	if len(planDefinitions) == 0 {
		return map[string]string{}
	}
	planCodeByPriceID := make(map[string]string, len(planDefinitions))
	for _, definition := range planDefinitions {
		normalizedPriceID := strings.TrimSpace(definition.PriceID)
		normalizedPlanCode := strings.ToLower(strings.TrimSpace(definition.Plan.Code))
		if normalizedPriceID == "" || normalizedPlanCode == "" {
			continue
		}
		planCodeByPriceID[normalizedPriceID] = normalizedPlanCode
	}
	return planCodeByPriceID
}

func (processor *stripeSubscriptionStatusWebhookProcessor) Process(
	ctx context.Context,
	event WebhookEvent,
) error {
	if strings.ToLower(strings.TrimSpace(event.ProviderCode)) != processor.providerCode {
		return nil
	}
	normalizedEventType := strings.ToLower(strings.TrimSpace(event.EventType))
	if processor.eventStatusProvider != nil &&
		processor.eventStatusProvider.ResolveCheckoutEventStatus(normalizedEventType) == CheckoutEventStatusSucceeded {
		return processor.processCheckoutSessionCompletedEvent(ctx, event)
	}
	switch normalizedEventType {
	case stripeEventTypeSubscriptionCreated, stripeEventTypeSubscriptionUpdated, stripeEventTypeSubscriptionDeleted:
		return processor.processSubscriptionLifecycleEvent(ctx, event)
	default:
		return nil
	}
}

func (processor *stripeSubscriptionStatusWebhookProcessor) processCheckoutSessionCompletedEvent(
	ctx context.Context,
	event WebhookEvent,
) error {
	grant, shouldGrant, grantResolveErr := processor.grantResolver.Resolve(ctx, event)
	if grantResolveErr != nil || !shouldGrant {
		return grantResolveErr
	}
	purchaseKind := NormalizePurchaseKind(grant.Metadata[billingGrantMetadataPurchaseKindKey])
	if purchaseKind != stripePurchaseKindSubscription {
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
	upsertErr := processor.stateRepository.Upsert(ctx, SubscriptionStateUpsertInput{
		ProviderCode:      processor.providerCode,
		UserEmail:         grant.UserEmail,
		Status:            subscriptionStatusActive,
		ProviderStatus:    stripeSubscriptionStatusActive,
		ActivePlan:        planCode,
		SubscriptionID:    subscriptionID,
		NextBillingAt:     time.Time{},
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

func (processor *stripeSubscriptionStatusWebhookProcessor) processSubscriptionLifecycleEvent(
	ctx context.Context,
	event WebhookEvent,
) error {
	payload := stripeSubscriptionWebhookPayload{}
	if decodeErr := json.Unmarshal(event.Payload, &payload); decodeErr != nil {
		return ErrWebhookGrantPayloadInvalid
	}
	userEmail, userEmailErr := processor.resolveLifecycleUserEmail(ctx, payload.Object())
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
	subscriptionData := payload.Object()
	subscriptionStatus := resolveStripeSubscriptionState(event.EventType, subscriptionData.Status)
	planCode := processor.resolvePlanCode(subscriptionData)
	if subscriptionStatus != subscriptionStatusActive {
		planCode = ""
	}
	upsertErr := processor.stateRepository.Upsert(ctx, SubscriptionStateUpsertInput{
		ProviderCode:      processor.providerCode,
		UserEmail:         userEmail,
		Status:            subscriptionStatus,
		ProviderStatus:    strings.ToLower(strings.TrimSpace(subscriptionData.Status)),
		ActivePlan:        planCode,
		SubscriptionID:    strings.TrimSpace(subscriptionData.ID),
		NextBillingAt:     resolveStripeSubscriptionNextBillingAt(subscriptionData),
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

func (payload stripeSubscriptionWebhookPayload) Object() stripeSubscriptionWebhookData {
	return payload.Data.Object
}

func (processor *stripeSubscriptionStatusWebhookProcessor) resolveLifecycleUserEmail(
	ctx context.Context,
	subscriptionData stripeSubscriptionWebhookData,
) (string, error) {
	userEmail, userEmailErr := processor.resolvePayloadUserEmail(ctx, subscriptionData)
	if userEmailErr == nil {
		return userEmail, nil
	}
	subscriptionID := strings.TrimSpace(subscriptionData.ID)
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

func (processor *stripeSubscriptionStatusWebhookProcessor) resolvePayloadUserEmail(
	ctx context.Context,
	subscriptionData stripeSubscriptionWebhookData,
) (string, error) {
	userEmail := strings.ToLower(
		metadataValue(
			subscriptionData.Metadata,
			billingMetadataUserEmailKey,
			stripeLegacyMetadataUserEmailKey,
		),
	)
	if userEmail != "" {
		return userEmail, nil
	}
	customerID := strings.TrimSpace(subscriptionData.CustomerID)
	if customerID == "" || processor.customerEmailResolver == nil {
		return "", ErrWebhookGrantMetadataInvalid
	}
	resolvedUserEmail, resolveErr := processor.customerEmailResolver.ResolveCustomerEmail(ctx, customerID)
	if resolveErr != nil {
		return "", ErrWebhookGrantMetadataInvalid
	}
	normalizedUserEmail := strings.ToLower(strings.TrimSpace(resolvedUserEmail))
	if normalizedUserEmail == "" {
		return "", ErrWebhookGrantMetadataInvalid
	}
	return normalizedUserEmail, nil
}

func (processor *stripeSubscriptionStatusWebhookProcessor) resolvePlanCode(
	subscriptionData stripeSubscriptionWebhookData,
) string {
	planCode := strings.ToLower(
		metadataValue(
			subscriptionData.Metadata,
			billingMetadataPlanCodeKey,
			stripeLegacyMetadataPlanCodeKey,
		),
	)
	if planCode != "" {
		return planCode
	}
	priceID := resolveStripeSubscriptionPriceID(subscriptionData)
	if priceID == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(processor.planCodeByPriceID[priceID]))
}

func resolveStripeSubscriptionPriceID(subscriptionData stripeSubscriptionWebhookData) string {
	for _, item := range subscriptionData.Items.Data {
		priceID := strings.TrimSpace(item.Price.ID)
		if priceID != "" {
			return priceID
		}
	}
	return ""
}

func resolveStripeSubscriptionState(eventType string, rawStatus string) string {
	normalizedEventType := strings.ToLower(strings.TrimSpace(eventType))
	if normalizedEventType == stripeEventTypeSubscriptionDeleted {
		return subscriptionStatusInactive
	}
	normalizedStatus := strings.ToLower(strings.TrimSpace(rawStatus))
	switch normalizedStatus {
	case stripeSubscriptionStatusActive, stripeSubscriptionStatusTrialing:
		return subscriptionStatusActive
	case stripeSubscriptionStatusPaused,
		stripeSubscriptionStatusCanceled,
		stripeSubscriptionStatusIncomplete,
		stripeSubscriptionStatusIncompleteExpired,
		stripeSubscriptionStatusPastDue,
		stripeSubscriptionStatusUnpaid:
		return subscriptionStatusInactive
	}
	if normalizedEventType == stripeEventTypeSubscriptionCreated {
		return subscriptionStatusActive
	}
	return subscriptionStatusInactive
}

func resolveStripeSubscriptionNextBillingAt(subscriptionData stripeSubscriptionWebhookData) time.Time {
	return parseStripeUnixTimestamp(subscriptionData.CurrentPeriodEnd)
}

func parseStripeWebhookMetadataInt64(metadata map[string]string, keys ...string) (int64, error) {
	rawValue := metadataValue(metadata, keys...)
	if rawValue == "" {
		return 0, nil
	}
	parsedValue, parseErr := strconv.ParseInt(rawValue, 10, 64)
	if parseErr != nil || parsedValue <= 0 {
		return 0, ErrWebhookGrantMetadataInvalid
	}
	return parsedValue, nil
}
