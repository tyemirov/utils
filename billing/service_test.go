package billing

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testCustomer(email string) CustomerContext {
	return CustomerContext{Email: email}
}

type stubCommerceProvider struct {
	code                        string
	publicConfig                PublicConfig
	plans                       []SubscriptionPlan
	packs                       []TopUpPack
	subscriptionCheckoutSession CheckoutSession
	subscriptionCheckoutErr     error
	topUpCheckoutSession        CheckoutSession
	topUpCheckoutErr            error
	portalSession               PortalSession
	portalErr                   error
	receivedSubscriptionEmail   string
	receivedSubscriptionPlan    string
	receivedTopUpEmail          string
	receivedTopUpPack           string
	receivedPortalEmail         string
	reconcileEvent              WebhookEvent
	reconcileUserEmail          string
	reconcileErr                error
	receivedReconcileTxnID      string
	syncEvents                  []WebhookEvent
	syncErr                     error
	receivedSyncUserEmail       string
}

type stubServiceSubscriptionStateRepository struct {
	state    SubscriptionState
	found    bool
	getErr   error
	getCalls int
}

func (repository *stubServiceSubscriptionStateRepository) Upsert(
	_ context.Context,
	_ SubscriptionStateUpsertInput,
) error {
	return nil
}

func (repository *stubServiceSubscriptionStateRepository) Get(
	_ context.Context,
	_ string,
	_ string,
) (SubscriptionState, bool, error) {
	repository.getCalls++
	if repository.getErr != nil {
		return SubscriptionState{}, false, repository.getErr
	}
	return repository.state, repository.found, nil
}

func (repository *stubServiceSubscriptionStateRepository) GetBySubscriptionID(
	_ context.Context,
	_ string,
	_ string,
) (SubscriptionState, bool, error) {
	return SubscriptionState{}, false, nil
}

func (provider *stubCommerceProvider) Code() string {
	return provider.code
}

func (provider *stubCommerceProvider) SubscriptionPlans() []SubscriptionPlan {
	return provider.plans
}

func (provider *stubCommerceProvider) TopUpPacks() []TopUpPack {
	return provider.packs
}

func (provider *stubCommerceProvider) PublicConfig() PublicConfig {
	return provider.publicConfig
}

func (provider *stubCommerceProvider) BuildUserSyncEvents(
	_ context.Context,
	userEmail string,
) ([]WebhookEvent, error) {
	provider.receivedSyncUserEmail = userEmail
	if provider.syncErr != nil {
		return nil, provider.syncErr
	}
	return provider.syncEvents, nil
}

func (provider *stubCommerceProvider) CreateSubscriptionCheckout(
	_ context.Context,
	customer CustomerContext,
	planCode string,
) (CheckoutSession, error) {
	provider.receivedSubscriptionEmail = customer.Email
	provider.receivedSubscriptionPlan = planCode
	if provider.subscriptionCheckoutErr != nil {
		return CheckoutSession{}, provider.subscriptionCheckoutErr
	}
	return provider.subscriptionCheckoutSession, nil
}

func (provider *stubCommerceProvider) CreateTopUpCheckout(
	_ context.Context,
	customer CustomerContext,
	packCode string,
) (CheckoutSession, error) {
	provider.receivedTopUpEmail = customer.Email
	provider.receivedTopUpPack = packCode
	if provider.topUpCheckoutErr != nil {
		return CheckoutSession{}, provider.topUpCheckoutErr
	}
	return provider.topUpCheckoutSession, nil
}

func (provider *stubCommerceProvider) CreateCustomerPortalSession(
	_ context.Context,
	userEmail string,
) (PortalSession, error) {
	provider.receivedPortalEmail = userEmail
	if provider.portalErr != nil {
		return PortalSession{}, provider.portalErr
	}
	return provider.portalSession, nil
}

func (provider *stubCommerceProvider) BuildCheckoutReconcileEvent(
	_ context.Context,
	transactionID string,
) (WebhookEvent, string, error) {
	provider.receivedReconcileTxnID = transactionID
	if provider.reconcileErr != nil {
		return WebhookEvent{}, "", provider.reconcileErr
	}
	return provider.reconcileEvent, provider.reconcileUserEmail, nil
}

func (provider *stubCommerceProvider) ResolveCheckoutEventStatus(eventType string) CheckoutEventStatus {
	normalizedEventType := strings.ToLower(strings.TrimSpace(eventType))
	switch normalizedEventType {
	case paddleEventTypeTransactionUpdated, stripeEventTypeCheckoutSessionPending:
		return CheckoutEventStatusPending
	case paddleEventTypeTransactionCompleted,
		paddleEventTypeTransactionPaid,
		stripeEventTypeCheckoutSessionCompleted,
		stripeEventTypeCheckoutSessionAsyncPaymentSucceeded:
		return CheckoutEventStatusSucceeded
	case stripeEventTypeCheckoutSessionAsyncPaymentFailed:
		return CheckoutEventStatusFailed
	case stripeEventTypeCheckoutSessionExpired:
		return CheckoutEventStatusExpired
	default:
		return CheckoutEventStatusUnknown
	}
}

type stubServiceWebhookProcessor struct {
	processErr     error
	receivedEvents []WebhookEvent
}

func (processor *stubServiceWebhookProcessor) Process(_ context.Context, event WebhookEvent) error {
	processor.receivedEvents = append(processor.receivedEvents, event)
	if processor.processErr != nil {
		return processor.processErr
	}
	return nil
}

func TestBillingServiceSummaryReturnsProviderUnavailableWhenProviderMissing(t *testing.T) {
	service := NewService(nil)

	_, err := service.GetSubscriptionSummary(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrBillingProviderUnavailable)
}

func TestBillingServiceSummaryReturnsProviderData(t *testing.T) {
	service := NewService(&stubCommerceProvider{
		code: ProviderCodePaddle,
		publicConfig: PublicConfig{
			ProviderCode: ProviderCodePaddle,
			Environment:  "sandbox",
			ClientToken:  "token",
		},
		plans: []SubscriptionPlan{
			{Code: PlanCodePro},
		},
		packs: []TopUpPack{
			{Code: PackCodeTopUp},
		},
	})

	summary, err := service.GetSubscriptionSummary(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.True(t, summary.Enabled)
	require.Equal(t, ProviderCodePaddle, summary.ProviderCode)
	require.Equal(t, "sandbox", summary.Environment)
	require.Equal(t, "token", summary.ClientToken)
	require.Equal(t, subscriptionStatusInactive, summary.Status)
	require.False(t, summary.IsDelinquent)
	require.Equal(t, "", summary.DelinquentReason)
	require.Len(t, summary.Plans, 1)
	require.Len(t, summary.TopUpPacks, 1)
}

func TestBillingServiceSummaryReadsStateWithoutSyncingProvider(t *testing.T) {
	provider := &stubCommerceProvider{
		code: ProviderCodePaddle,
		publicConfig: PublicConfig{
			ProviderCode: ProviderCodePaddle,
			Environment:  "sandbox",
			ClientToken:  "token",
		},
		syncEvents: []WebhookEvent{
			{
				ProviderCode: ProviderCodePaddle,
				EventID:      "sync:subscription:sub_123:active",
				EventType:    paddleEventTypeSubscriptionUpdated,
				OccurredAt:   time.Date(2026, time.February, 19, 9, 0, 0, 0, time.UTC),
				Payload:      []byte(`{"data":{"id":"sub_123","status":"active"}}`),
			},
		},
	}
	stateRepository := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{
			ProviderCode: ProviderCodePaddle,
			UserEmail:    "user@example.com",
			Status:       subscriptionStatusInactive,
		},
	}
	processor := &stubServiceWebhookProcessor{}
	service := NewServiceWithWebhookProcessor(provider, processor, stateRepository)

	summary, err := service.GetSubscriptionSummary(context.Background(), " User@Example.com ")
	require.NoError(t, err)
	require.Equal(t, subscriptionStatusInactive, summary.Status)
	require.Equal(t, "", provider.receivedSyncUserEmail)
	require.Len(t, processor.receivedEvents, 0)
	require.Equal(t, 1, stateRepository.getCalls)
}

func TestBillingServiceSyncUserBillingEventsDelegatesToProviderAndProcessor(t *testing.T) {
	provider := &stubCommerceProvider{
		code: ProviderCodePaddle,
		syncEvents: []WebhookEvent{
			{
				ProviderCode: ProviderCodePaddle,
				EventID:      "sync:transaction:txn_123",
				EventType:    paddleEventTypeTransactionCompleted,
				OccurredAt:   time.Date(2026, time.February, 19, 9, 0, 0, 0, time.UTC),
				Payload: []byte(
					`{"data":{"id":"txn_123","status":"completed","custom_data":{"product_scanner_user_email":"user@example.com","product_scanner_purchase_kind":"top_up_pack","product_scanner_pack_code":"top_up","product_scanner_pack_credits":"2400"}}}`,
				),
			},
		},
	}
	processor := &stubServiceWebhookProcessor{}
	service := NewServiceWithWebhookProcessor(provider, processor)

	err := service.SyncUserBillingEvents(context.Background(), " User@Example.com ")
	require.NoError(t, err)
	require.Equal(t, "user@example.com", provider.receivedSyncUserEmail)
	require.Len(t, processor.receivedEvents, 1)
	require.Equal(t, "sync:transaction:txn_123", processor.receivedEvents[0].EventID)
}

func TestBillingServiceSyncUserBillingEventsReturnsSyncFailure(t *testing.T) {
	service := NewService(&stubCommerceProvider{
		code:    ProviderCodePaddle,
		syncErr: errors.New("provider sync failed"),
	})

	err := service.SyncUserBillingEvents(context.Background(), "user@example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBillingUserSyncFailed)
}

func TestBillingServiceSyncUserBillingEventsReturnsErrorWhenSyncEventsExistWithoutProcessor(t *testing.T) {
	service := NewService(&stubCommerceProvider{
		code: ProviderCodePaddle,
		syncEvents: []WebhookEvent{
			{
				ProviderCode: ProviderCodePaddle,
				EventID:      "sync:transaction:txn_123",
				EventType:    paddleEventTypeTransactionCompleted,
				OccurredAt:   time.Date(2026, time.February, 19, 9, 0, 0, 0, time.UTC),
				Payload:      []byte(`{"data":{"id":"txn_123","status":"completed"}}`),
			},
		},
	})

	err := service.SyncUserBillingEvents(context.Background(), "user@example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBillingUserSyncFailed)
}

func TestBillingServiceSyncUserBillingEventsSkipsNonRetryableSyncProcessorErrors(t *testing.T) {
	provider := &stubCommerceProvider{
		code: ProviderCodePaddle,
		syncEvents: []WebhookEvent{
			{
				ProviderCode: ProviderCodePaddle,
				EventID:      "sync:transaction:txn_123",
				EventType:    paddleEventTypeTransactionCompleted,
				OccurredAt:   time.Date(2026, time.February, 19, 9, 0, 0, 0, time.UTC),
				Payload:      []byte(`{"data":{"id":"txn_123","status":"completed"}}`),
			},
		},
	}
	processor := &stubServiceWebhookProcessor{
		processErr: ErrWebhookGrantMetadataInvalid,
	}
	service := NewServiceWithWebhookProcessor(provider, processor)

	err := service.SyncUserBillingEvents(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.Len(t, processor.receivedEvents, 1)
}

func TestBillingServiceSummaryReturnsStateFromRepository(t *testing.T) {
	nextBillingAt := time.Date(2026, time.February, 28, 12, 0, 0, 0, time.UTC)
	lastEventOccurredAt := time.Date(2026, time.February, 19, 12, 30, 0, 0, time.UTC)
	stateRepository := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{
			Status:              subscriptionStatusActive,
			ProviderStatus:      paddleSubscriptionStatusTrialing,
			ActivePlan:          PlanCodePlus,
			SubscriptionID:      "sub_123",
			NextBillingAt:       nextBillingAt,
			LastEventType:       paddleEventTypeSubscriptionUpdated,
			LastEventOccurredAt: lastEventOccurredAt,
		},
	}
	service := NewService(&stubCommerceProvider{
		code: ProviderCodePaddle,
		publicConfig: PublicConfig{
			ProviderCode: ProviderCodePaddle,
			Environment:  "sandbox",
			ClientToken:  "token",
		},
	}, stateRepository)

	summary, err := service.GetSubscriptionSummary(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.Equal(t, subscriptionStatusActive, summary.Status)
	require.Equal(t, paddleSubscriptionStatusTrialing, summary.ProviderStatus)
	require.Equal(t, PlanCodePlus, summary.ActivePlan)
	require.Equal(t, "sub_123", summary.SubscriptionID)
	require.Equal(t, nextBillingAt, summary.NextBillingAt)
	require.Equal(t, paddleEventTypeSubscriptionUpdated, summary.LastEventType)
	require.Equal(t, lastEventOccurredAt, summary.LastEventOccurredAt)
	require.False(t, summary.IsDelinquent)
	require.Equal(t, "", summary.DelinquentReason)
	require.Equal(t, 1, stateRepository.getCalls)
}

func TestBillingServiceSummaryMarksDelinquentFromPastDueStatus(t *testing.T) {
	stateRepository := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{
			Status:              subscriptionStatusInactive,
			ProviderStatus:      paddleSubscriptionStatusPastDue,
			ActivePlan:          "",
			SubscriptionID:      "sub_123",
			LastEventType:       paddleEventTypeSubscriptionUpdated,
			LastEventOccurredAt: time.Date(2026, time.February, 19, 12, 30, 0, 0, time.UTC),
		},
	}
	service := NewService(&stubCommerceProvider{
		code: ProviderCodePaddle,
		publicConfig: PublicConfig{
			ProviderCode: ProviderCodePaddle,
			Environment:  "sandbox",
			ClientToken:  "token",
		},
	}, stateRepository)

	summary, err := service.GetSubscriptionSummary(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.True(t, summary.IsDelinquent)
	require.Equal(t, paddleSubscriptionStatusPastDue, summary.DelinquentReason)
	require.Equal(t, 1, stateRepository.getCalls)
}

func TestBillingServiceSummaryMarksDelinquentFromPaymentFailureEvent(t *testing.T) {
	stateRepository := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{
			Status:            subscriptionStatusInactive,
			ProviderStatus:    "",
			ActivePlan:        "",
			SubscriptionID:    "sub_123",
			LastEventType:     stripeEventTypeCheckoutSessionAsyncPaymentFailed,
			NextBillingAt:     time.Time{},
			LastEventID:       "evt_1",
			UserEmail:         "user@example.com",
			LastTransactionID: "txn_1",
		},
	}
	service := NewService(&stubCommerceProvider{
		code: ProviderCodeStripe,
		publicConfig: PublicConfig{
			ProviderCode: ProviderCodeStripe,
			Environment:  "sandbox",
			ClientToken:  "token",
		},
	}, stateRepository)

	summary, err := service.GetSubscriptionSummary(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.True(t, summary.IsDelinquent)
	require.Equal(t, "payment_failed", summary.DelinquentReason)
}

func TestBillingServiceSummaryReturnsInvalidUserEmailWhenMissing(t *testing.T) {
	service := NewService(&stubCommerceProvider{
		code: ProviderCodePaddle,
	})

	_, err := service.GetSubscriptionSummary(context.Background(), " ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestBillingServiceSyncUserBillingEventsReturnsInvalidUserEmailWhenMissing(t *testing.T) {
	service := NewService(&stubCommerceProvider{
		code: ProviderCodePaddle,
	})

	err := service.SyncUserBillingEvents(context.Background(), " ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestBillingServiceSubscriptionCheckoutDelegatesToProvider(t *testing.T) {
	provider := &stubCommerceProvider{
		subscriptionCheckoutSession: CheckoutSession{
			ProviderCode:  ProviderCodePaddle,
			TransactionID: "txn_123",
			CheckoutMode:  CheckoutModeOverlay,
		},
	}
	service := NewService(provider)

	session, err := service.CreateSubscriptionCheckout(context.Background(), testCustomer(" user@example.com "), " PRO ")
	require.NoError(t, err)
	require.Equal(t, "user@example.com", provider.receivedSubscriptionEmail)
	require.Equal(t, "pro", provider.receivedSubscriptionPlan)
	require.Equal(t, "txn_123", session.TransactionID)
}

func TestBillingServiceSubscriptionCheckoutRejectsActiveSamePlan(t *testing.T) {
	provider := &stubCommerceProvider{
		subscriptionCheckoutSession: CheckoutSession{
			ProviderCode:  ProviderCodePaddle,
			TransactionID: "txn_123",
			CheckoutMode:  CheckoutModeOverlay,
		},
		plans: []SubscriptionPlan{
			{Code: PlanCodePro, MonthlyCredits: 1000},
			{Code: PlanCodePlus, MonthlyCredits: 10000},
		},
	}
	stateRepository := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{
			Status:     subscriptionStatusActive,
			ActivePlan: PlanCodePro,
		},
	}
	service := NewService(provider, stateRepository)

	_, err := service.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), "pro")
	require.ErrorIs(t, err, ErrBillingSubscriptionActive)
	require.Equal(t, "", provider.receivedSubscriptionPlan)
}

func TestBillingServiceSubscriptionCheckoutRejectsDowngradeWhenActive(t *testing.T) {
	provider := &stubCommerceProvider{
		subscriptionCheckoutSession: CheckoutSession{
			ProviderCode:  ProviderCodePaddle,
			TransactionID: "txn_123",
			CheckoutMode:  CheckoutModeOverlay,
		},
		plans: []SubscriptionPlan{
			{Code: PlanCodePro, MonthlyCredits: 1000},
			{Code: PlanCodePlus, MonthlyCredits: 10000},
		},
	}
	stateRepository := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{
			Status:     subscriptionStatusActive,
			ActivePlan: PlanCodePlus,
		},
	}
	service := NewService(provider, stateRepository)

	_, err := service.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), "pro")
	require.ErrorIs(t, err, ErrBillingSubscriptionUpgrade)
	require.Equal(t, "", provider.receivedSubscriptionPlan)
}

func TestBillingServiceSubscriptionCheckoutAllowsUpgradeWhenActive(t *testing.T) {
	provider := &stubCommerceProvider{
		subscriptionCheckoutSession: CheckoutSession{
			ProviderCode:  ProviderCodePaddle,
			TransactionID: "txn_123",
			CheckoutMode:  CheckoutModeOverlay,
		},
		plans: []SubscriptionPlan{
			{Code: PlanCodePro, MonthlyCredits: 1000},
			{Code: PlanCodePlus, MonthlyCredits: 10000},
		},
	}
	stateRepository := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{
			Status:     subscriptionStatusActive,
			ActivePlan: PlanCodePro,
		},
	}
	service := NewService(provider, stateRepository)

	session, err := service.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), "plus")
	require.ErrorIs(t, err, ErrBillingSubscriptionManageInPortal)
	require.Equal(t, "", provider.receivedSubscriptionPlan)
	require.Equal(t, "", session.TransactionID)
}

func TestBillingServiceTopUpCheckoutDelegatesToProvider(t *testing.T) {
	provider := &stubCommerceProvider{
		topUpCheckoutSession: CheckoutSession{
			ProviderCode:  ProviderCodePaddle,
			TransactionID: "txn_credits",
			CheckoutMode:  CheckoutModeOverlay,
		},
	}
	stateRepository := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{
			Status: subscriptionStatusActive,
		},
	}
	service := NewService(provider, stateRepository)

	session, err := service.CreateTopUpCheckout(context.Background(), testCustomer(" user@example.com "), " Top_Up ")
	require.NoError(t, err)
	require.Equal(t, "user@example.com", provider.receivedTopUpEmail)
	require.Equal(t, PackCodeTopUp, provider.receivedTopUpPack)
	require.Equal(t, "txn_credits", session.TransactionID)
}

func TestBillingServiceTopUpCheckoutRejectsInactiveSubscription(t *testing.T) {
	provider := &stubCommerceProvider{}
	stateRepository := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{
			Status: subscriptionStatusInactive,
		},
	}
	service := NewService(provider, stateRepository)

	_, err := service.CreateTopUpCheckout(context.Background(), testCustomer("user@example.com"), PackCodeTopUp)
	require.ErrorIs(t, err, ErrBillingSubscriptionRequired)
	require.Equal(t, "", provider.receivedTopUpPack)
}

func TestBillingServiceTopUpCheckoutRejectsMissingSubscriptionState(t *testing.T) {
	provider := &stubCommerceProvider{}
	service := NewService(provider)

	_, err := service.CreateTopUpCheckout(context.Background(), testCustomer("user@example.com"), PackCodeTopUp)
	require.ErrorIs(t, err, ErrBillingSubscriptionRequired)
	require.Equal(t, "", provider.receivedTopUpPack)
}

func TestBillingServicePortalDelegatesToProvider(t *testing.T) {
	provider := &stubCommerceProvider{
		portalSession: PortalSession{
			ProviderCode: ProviderCodePaddle,
			URL:          "https://portal.example.com",
		},
	}
	service := NewService(provider)

	portalSession, err := service.CreatePortalSession(context.Background(), " user@example.com ")
	require.NoError(t, err)
	require.Equal(t, "user@example.com", provider.receivedPortalEmail)
	require.Equal(t, "https://portal.example.com", portalSession.URL)
}

func TestBillingServiceReturnsProviderUnavailableWhenMissing(t *testing.T) {
	service := NewService(nil)

	_, err := service.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), "pro")
	require.ErrorIs(t, err, ErrBillingProviderUnavailable)
}

func TestBillingServiceWrapsProviderErrors(t *testing.T) {
	provider := &stubCommerceProvider{
		subscriptionCheckoutErr: errors.New("upstream failed"),
	}
	service := NewService(provider)

	_, err := service.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), "pro")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.checkout.subscription")
}

func TestBillingServiceReconcileCheckoutDelegatesToProviderAndProcessor(t *testing.T) {
	provider := &stubCommerceProvider{
		reconcileEvent: WebhookEvent{
			ProviderCode: ProviderCodePaddle,
			EventID:      "reconcile:txn_123:completed",
			EventType:    "transaction.completed",
			Payload:      []byte(`{"data":{"id":"txn_123","status":"completed"}}`),
		},
		reconcileUserEmail: "user@example.com",
	}
	processor := &stubServiceWebhookProcessor{}
	service := NewServiceWithWebhookProcessor(provider, processor)

	reconcileErr := service.ReconcileCheckout(context.Background(), " user@example.com ", " txn_123 ")
	require.NoError(t, reconcileErr)
	require.Equal(t, "txn_123", provider.receivedReconcileTxnID)
	require.Len(t, processor.receivedEvents, 1)
	require.Equal(t, "reconcile:txn_123:completed", processor.receivedEvents[0].EventID)
}

func TestBillingServiceReconcileCheckoutReturnsPendingForUnpaidTransaction(t *testing.T) {
	provider := &stubCommerceProvider{
		reconcileEvent: WebhookEvent{
			ProviderCode: ProviderCodePaddle,
			EventID:      "reconcile:txn_123:updated",
			EventType:    paddleEventTypeTransactionUpdated,
			Payload:      []byte(`{"data":{"id":"txn_123","status":"ready"}}`),
		},
		reconcileUserEmail: "user@example.com",
	}
	processor := &stubServiceWebhookProcessor{}
	service := NewServiceWithWebhookProcessor(provider, processor)

	reconcileErr := service.ReconcileCheckout(context.Background(), "user@example.com", "txn_123")
	require.ErrorIs(t, reconcileErr, ErrBillingCheckoutTransactionPending)
	require.Len(t, processor.receivedEvents, 0)
}

func TestBillingServiceReconcileCheckoutRejectsMismatchedUser(t *testing.T) {
	provider := &stubCommerceProvider{
		reconcileEvent: WebhookEvent{
			ProviderCode: ProviderCodePaddle,
			EventID:      "reconcile:txn_123:completed",
			EventType:    "transaction.completed",
			Payload:      []byte(`{"data":{"id":"txn_123","status":"completed"}}`),
		},
		reconcileUserEmail: "other@example.com",
	}
	service := NewServiceWithWebhookProcessor(provider, &stubServiceWebhookProcessor{})

	reconcileErr := service.ReconcileCheckout(context.Background(), "user@example.com", "txn_123")
	require.ErrorIs(t, reconcileErr, ErrBillingCheckoutOwnershipMismatch)
}

func TestBillingServiceReconcileCheckoutRequiresProcessor(t *testing.T) {
	service := NewService(&stubCommerceProvider{})

	reconcileErr := service.ReconcileCheckout(context.Background(), "user@example.com", "txn_123")
	require.ErrorIs(t, reconcileErr, ErrBillingCheckoutReconciliationUnavailable)
}

func TestBillingServiceReconcileCheckoutRequiresTransactionID(t *testing.T) {
	service := NewServiceWithWebhookProcessor(&stubCommerceProvider{}, &stubServiceWebhookProcessor{})

	reconcileErr := service.ReconcileCheckout(context.Background(), "user@example.com", " ")
	require.ErrorIs(t, reconcileErr, ErrBillingCheckoutTransactionInvalid)
}

func TestServiceProviderCodeReturnsProviderCode(t *testing.T) {
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle})
	require.Equal(t, ProviderCodePaddle, service.ProviderCode())
}

func TestServiceProviderCodeReturnsEmptyWhenNilService(t *testing.T) {
	var service *Service
	require.Equal(t, "", service.ProviderCode())
}

func TestServiceProviderCodeReturnsEmptyWhenNilProvider(t *testing.T) {
	service := NewService(nil)
	require.Equal(t, "", service.ProviderCode())
}

// Coverage gap tests for service.go

func TestBillingServiceSummaryReturnsInvalidEmailError(t *testing.T) {
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle})
	_, err := service.GetSubscriptionSummary(context.Background(), "  ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestBillingServiceSummaryReturnsStateError(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{
		getErr: errors.New("db connection lost"),
	}
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle}, stateRepo)
	_, err := service.GetSubscriptionSummary(context.Background(), "user@example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.subscription.summary.state")
}

func TestResolveSubscriptionDelinquencyPaymentFailedEventTypes(t *testing.T) {
	isDelinquent, reason := resolveSubscriptionDelinquency("", "invoice.payment_failed")
	require.True(t, isDelinquent)
	require.Equal(t, "payment_failed", reason)

	isDelinquent, reason = resolveSubscriptionDelinquency("", "invoice.payment_action_required")
	require.True(t, isDelinquent)
	require.Equal(t, "payment_failed", reason)

	isDelinquent, reason = resolveSubscriptionDelinquency("", "custom_payment_failed_event")
	require.True(t, isDelinquent)
	require.Equal(t, "payment_failed", reason)

	isDelinquent, reason = resolveSubscriptionDelinquency("", "some.other.event")
	require.False(t, isDelinquent)
	require.Equal(t, "", reason)

	isDelinquent, reason = resolveSubscriptionDelinquency("past_due", "")
	require.True(t, isDelinquent)
	require.Equal(t, "past_due", reason)

	isDelinquent, reason = resolveSubscriptionDelinquency("unpaid", "")
	require.True(t, isDelinquent)
	require.Equal(t, "unpaid", reason)

	isDelinquent, reason = resolveSubscriptionDelinquency("incomplete", "")
	require.True(t, isDelinquent)
	require.Equal(t, "incomplete", reason)

	isDelinquent, reason = resolveSubscriptionDelinquency("incomplete_expired", "")
	require.True(t, isDelinquent)
	require.Equal(t, "incomplete_expired", reason)

	isDelinquent, reason = resolveSubscriptionDelinquency("payment_failed", "")
	require.True(t, isDelinquent)
	require.Equal(t, "payment_failed", reason)

	isDelinquent, reason = resolveSubscriptionDelinquency("delinquent", "")
	require.True(t, isDelinquent)
	require.Equal(t, "delinquent", reason)
}

func TestBillingServiceSyncUserBillingEventsNilService(t *testing.T) {
	var service *Service
	err := service.SyncUserBillingEvents(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrBillingProviderUnavailable)
}

func TestBillingServiceSyncUserBillingEventsInvalidEmail(t *testing.T) {
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle})
	err := service.SyncUserBillingEvents(context.Background(), " ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestBillingServiceSyncUserBillingEventsEmptySyncEventsNoError(t *testing.T) {
	provider := &stubCommerceProvider{
		code:       ProviderCodePaddle,
		syncEvents: []WebhookEvent{},
	}
	service := NewService(provider)
	err := service.SyncUserBillingEvents(context.Background(), "user@example.com")
	require.NoError(t, err)
}

func TestBillingServiceSyncUserBillingEventsProcessErrorSkipsNonRetryable(t *testing.T) {
	provider := &stubCommerceProvider{
		code: ProviderCodePaddle,
		syncEvents: []WebhookEvent{
			{ProviderCode: ProviderCodePaddle, EventID: "sync:1", EventType: "transaction.completed", Payload: []byte(`{}`)},
			{ProviderCode: ProviderCodePaddle, EventID: "sync:2", EventType: "transaction.completed", Payload: []byte(`{}`)},
		},
	}
	callCount := 0
	processor := WebhookProcessorFunc(func(_ context.Context, _ WebhookEvent) error {
		callCount++
		if callCount == 1 {
			return ErrWebhookGrantPayloadInvalid
		}
		return nil
	})
	service := NewServiceWithWebhookProcessor(provider, processor)
	err := service.SyncUserBillingEvents(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.Equal(t, 2, callCount)
}

func TestBillingServiceSyncUserBillingEventsProcessErrorReturnsRetryable(t *testing.T) {
	provider := &stubCommerceProvider{
		code: ProviderCodePaddle,
		syncEvents: []WebhookEvent{
			{ProviderCode: ProviderCodePaddle, EventID: "sync:1", EventType: "transaction.completed", Payload: []byte(`{}`)},
		},
	}
	processor := WebhookProcessorFunc(func(_ context.Context, _ WebhookEvent) error {
		return errors.New("transient error")
	})
	service := NewServiceWithWebhookProcessor(provider, processor)
	err := service.SyncUserBillingEvents(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrBillingUserSyncFailed)
}

func TestBillingServiceCreateSubscriptionCheckoutNilService(t *testing.T) {
	var service *Service
	_, err := service.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), "pro")
	require.ErrorIs(t, err, ErrBillingProviderUnavailable)
}

func TestBillingServiceCreateSubscriptionCheckoutInvalidEmail(t *testing.T) {
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle})
	_, err := service.CreateSubscriptionCheckout(context.Background(), testCustomer(" "), "pro")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestBillingServiceCreateSubscriptionCheckoutEmptyPlan(t *testing.T) {
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle})
	_, err := service.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), " ")
	require.ErrorIs(t, err, ErrBillingPlanUnsupported)
}

func TestBillingServiceCreateSubscriptionCheckoutProviderError(t *testing.T) {
	service := NewService(&stubCommerceProvider{
		code:                    ProviderCodePaddle,
		subscriptionCheckoutErr: errors.New("provider error"),
	})
	_, err := service.CreateSubscriptionCheckout(context.Background(), testCustomer("user@example.com"), "pro")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.checkout.subscription")
}

func TestValidateSubscriptionCheckoutTransitionNilStateRepo(t *testing.T) {
	service := NewService(&stubCommerceProvider{
		code:  ProviderCodePaddle,
		plans: []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	})
	err := service.validateSubscriptionCheckoutTransition(context.Background(), "user@example.com", "pro")
	require.NoError(t, err)
}

func TestValidateSubscriptionCheckoutTransitionStateError(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{
		getErr: errors.New("db error"),
	}
	service := NewService(&stubCommerceProvider{
		code:  ProviderCodePaddle,
		plans: []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	}, stateRepo)
	err := service.validateSubscriptionCheckoutTransition(context.Background(), "user@example.com", "pro")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.checkout.subscription.state")
}

func TestValidateSubscriptionCheckoutTransitionNotFoundOK(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{found: false}
	service := NewService(&stubCommerceProvider{
		code:  ProviderCodePaddle,
		plans: []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	}, stateRepo)
	err := service.validateSubscriptionCheckoutTransition(context.Background(), "user@example.com", "pro")
	require.NoError(t, err)
}

func TestValidateSubscriptionCheckoutTransitionActiveNoCurrentPlan(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{Status: subscriptionStatusActive, ActivePlan: ""},
	}
	service := NewService(&stubCommerceProvider{
		code:  ProviderCodePaddle,
		plans: []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	}, stateRepo)
	err := service.validateSubscriptionCheckoutTransition(context.Background(), "user@example.com", "pro")
	require.ErrorIs(t, err, ErrBillingSubscriptionActive)
}

func TestValidateSubscriptionCheckoutTransitionSamePlan(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{Status: subscriptionStatusActive, ActivePlan: "pro"},
	}
	service := NewService(&stubCommerceProvider{
		code:  ProviderCodePaddle,
		plans: []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	}, stateRepo)
	err := service.validateSubscriptionCheckoutTransition(context.Background(), "user@example.com", "pro")
	require.ErrorIs(t, err, ErrBillingSubscriptionActive)
}

func TestValidateSubscriptionCheckoutTransitionCurrentPlanUnknownReturnsActive(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{Status: subscriptionStatusActive, ActivePlan: "unknown_plan"},
	}
	service := NewService(&stubCommerceProvider{
		code:  ProviderCodePaddle,
		plans: []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	}, stateRepo)
	err := service.validateSubscriptionCheckoutTransition(context.Background(), "user@example.com", "pro")
	require.ErrorIs(t, err, ErrBillingSubscriptionActive)
}

func TestValidateSubscriptionCheckoutTransitionDowngrade(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{Status: subscriptionStatusActive, ActivePlan: "plus"},
	}
	service := NewService(&stubCommerceProvider{
		code: ProviderCodePaddle,
		plans: []SubscriptionPlan{
			{Code: "pro", MonthlyCredits: 1000},
			{Code: "plus", MonthlyCredits: 10000},
		},
	}, stateRepo)
	err := service.validateSubscriptionCheckoutTransition(context.Background(), "user@example.com", "pro")
	require.ErrorIs(t, err, ErrBillingSubscriptionUpgrade)
}

func TestValidateSubscriptionCheckoutTransitionUnknownPlan(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{found: false}
	service := NewService(&stubCommerceProvider{
		code:  ProviderCodePaddle,
		plans: []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	}, stateRepo)
	err := service.validateSubscriptionCheckoutTransition(context.Background(), "user@example.com", "unknown")
	require.ErrorIs(t, err, ErrBillingPlanUnsupported)
}

func TestValidateSubscriptionCheckoutTransitionInactiveStatus(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{Status: subscriptionStatusInactive, ActivePlan: "pro"},
	}
	service := NewService(&stubCommerceProvider{
		code:  ProviderCodePaddle,
		plans: []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	}, stateRepo)
	err := service.validateSubscriptionCheckoutTransition(context.Background(), "user@example.com", "pro")
	require.NoError(t, err)
}

func TestResolvePlanMonthlyCreditsNilService(t *testing.T) {
	var service *Service
	credits, found := service.resolvePlanMonthlyCredits("pro")
	require.False(t, found)
	require.Equal(t, int64(0), credits)
}

func TestResolvePlanMonthlyCreditsEmptyPlanCode(t *testing.T) {
	service := NewService(&stubCommerceProvider{
		code:  ProviderCodePaddle,
		plans: []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	})
	credits, found := service.resolvePlanMonthlyCredits(" ")
	require.False(t, found)
	require.Equal(t, int64(0), credits)
}

func TestResolvePlanMonthlyCreditsNotFoundPlan(t *testing.T) {
	service := NewService(&stubCommerceProvider{
		code:  ProviderCodePaddle,
		plans: []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	})
	credits, found := service.resolvePlanMonthlyCredits("unknown")
	require.False(t, found)
	require.Equal(t, int64(0), credits)
}

func TestBillingServiceCreateTopUpCheckoutNilService(t *testing.T) {
	var service *Service
	_, err := service.CreateTopUpCheckout(context.Background(), testCustomer("user@example.com"), "top_up")
	require.ErrorIs(t, err, ErrBillingProviderUnavailable)
}

func TestBillingServiceCreateTopUpCheckoutInvalidEmail(t *testing.T) {
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle})
	_, err := service.CreateTopUpCheckout(context.Background(), testCustomer(" "), "top_up")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestBillingServiceCreateTopUpCheckoutEmptyPack(t *testing.T) {
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle})
	_, err := service.CreateTopUpCheckout(context.Background(), testCustomer("user@example.com"), " ")
	require.ErrorIs(t, err, ErrBillingTopUpPackUnknown)
}

func TestBillingServiceCreateTopUpCheckoutProviderError(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{Status: subscriptionStatusActive},
	}
	service := NewService(&stubCommerceProvider{
		code:             ProviderCodePaddle,
		topUpCheckoutErr: errors.New("provider error"),
	}, stateRepo)
	_, err := service.CreateTopUpCheckout(context.Background(), testCustomer("user@example.com"), "top_up")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.checkout.top_up")
}

func TestValidateTopUpCheckoutEligibilityNilService(t *testing.T) {
	var service *Service
	err := service.validateTopUpCheckoutEligibility(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrBillingProviderUnavailable)
}

func TestValidateTopUpCheckoutEligibilityNilStateRepo(t *testing.T) {
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle})
	err := service.validateTopUpCheckoutEligibility(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrBillingSubscriptionRequired)
}

func TestValidateTopUpCheckoutEligibilityInvalidEmail(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{found: false}
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle}, stateRepo)
	err := service.validateTopUpCheckoutEligibility(context.Background(), " ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestValidateTopUpCheckoutEligibilityStateError(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{getErr: errors.New("db error")}
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle}, stateRepo)
	err := service.validateTopUpCheckoutEligibility(context.Background(), "user@example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.checkout.top_up.state")
}

func TestValidateTopUpCheckoutEligibilityNotFound(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{found: false}
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle}, stateRepo)
	err := service.validateTopUpCheckoutEligibility(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrBillingSubscriptionRequired)
}

func TestValidateTopUpCheckoutEligibilityInactiveStatus(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{Status: subscriptionStatusInactive},
	}
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle}, stateRepo)
	err := service.validateTopUpCheckoutEligibility(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrBillingSubscriptionRequired)
}

func TestBillingServiceCreatePortalSessionNilService(t *testing.T) {
	var service *Service
	_, err := service.CreatePortalSession(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrBillingProviderUnavailable)
}

func TestBillingServiceCreatePortalSessionInvalidEmail(t *testing.T) {
	service := NewService(&stubCommerceProvider{code: ProviderCodePaddle})
	_, err := service.CreatePortalSession(context.Background(), " ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestBillingServiceCreatePortalSessionProviderError(t *testing.T) {
	service := NewService(&stubCommerceProvider{
		code:      ProviderCodePaddle,
		portalErr: errors.New("provider error"),
	})
	_, err := service.CreatePortalSession(context.Background(), "user@example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.portal")
}

func TestBillingServiceReconcileCheckoutNilService(t *testing.T) {
	var service *Service
	err := service.ReconcileCheckout(context.Background(), "user@example.com", "txn_123")
	require.ErrorIs(t, err, ErrBillingProviderUnavailable)
}

func TestBillingServiceReconcileCheckoutInvalidEmail(t *testing.T) {
	service := NewServiceWithWebhookProcessor(
		&stubCommerceProvider{code: ProviderCodePaddle},
		&stubServiceWebhookProcessor{},
	)
	err := service.ReconcileCheckout(context.Background(), " ", "txn_123")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestBillingServiceReconcileCheckoutBuildEventError(t *testing.T) {
	provider := &stubCommerceProvider{
		code:         ProviderCodePaddle,
		reconcileErr: errors.New("transaction not found"),
	}
	processor := &stubServiceWebhookProcessor{}
	service := NewServiceWithWebhookProcessor(provider, processor)
	err := service.ReconcileCheckout(context.Background(), "user@example.com", "txn_123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.checkout.reconcile.event")
}

func TestBillingServiceReconcileCheckoutProcessError(t *testing.T) {
	provider := &stubCommerceProvider{
		code: ProviderCodePaddle,
		reconcileEvent: WebhookEvent{
			ProviderCode: ProviderCodePaddle,
			EventID:      "reconcile:txn_123:completed",
			EventType:    "transaction.completed",
			Payload:      []byte(`{}`),
		},
		reconcileUserEmail: "user@example.com",
	}
	processor := &stubServiceWebhookProcessor{processErr: errors.New("process error")}
	service := NewServiceWithWebhookProcessor(provider, processor)
	err := service.ReconcileCheckout(context.Background(), "user@example.com", "txn_123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "billing.checkout.reconcile.process")
}

func TestIsCheckoutReconcileEventPendingNilProvider(t *testing.T) {
	require.False(t, isCheckoutReconcileEventPending(nil, "transaction.completed"))
}

type nonCheckoutEventStatusProvider struct {
	stubCommerceProvider
}

func TestIsCheckoutReconcileEventPendingProviderWithoutInterface(t *testing.T) {
	provider := &nonCheckoutEventStatusProvider{}
	require.False(t, isCheckoutReconcileEventPending(provider, "transaction.completed"))
}

type simpleCommerceProviderNoReconcile struct {
	code string
}

func (p *simpleCommerceProviderNoReconcile) Code() string { return p.code }
func (p *simpleCommerceProviderNoReconcile) SubscriptionPlans() []SubscriptionPlan {
	return nil
}
func (p *simpleCommerceProviderNoReconcile) TopUpPacks() []TopUpPack { return nil }
func (p *simpleCommerceProviderNoReconcile) PublicConfig() PublicConfig {
	return PublicConfig{}
}
func (p *simpleCommerceProviderNoReconcile) BuildUserSyncEvents(_ context.Context, _ string) ([]WebhookEvent, error) {
	return nil, nil
}
func (p *simpleCommerceProviderNoReconcile) CreateSubscriptionCheckout(_ context.Context, _ CustomerContext, _ string) (CheckoutSession, error) {
	return CheckoutSession{}, nil
}
func (p *simpleCommerceProviderNoReconcile) CreateTopUpCheckout(_ context.Context, _ CustomerContext, _ string) (CheckoutSession, error) {
	return CheckoutSession{}, nil
}
func (p *simpleCommerceProviderNoReconcile) CreateCustomerPortalSession(_ context.Context, _ string) (PortalSession, error) {
	return PortalSession{}, nil
}

func TestBillingServiceReconcileCheckoutUnsupportedProvider(t *testing.T) {
	provider := &simpleCommerceProviderNoReconcile{code: ProviderCodePaddle}
	processor := &stubServiceWebhookProcessor{}
	service := NewServiceWithWebhookProcessor(provider, processor)
	err := service.ReconcileCheckout(context.Background(), "user@example.com", "txn_123")
	require.ErrorIs(t, err, ErrBillingCheckoutReconciliationUnsupported)
}

func TestIsCheckoutReconcileEventPendingWithPendingStatus(t *testing.T) {
	provider := &stubCommerceProvider{code: ProviderCodePaddle}
	require.True(t, isCheckoutReconcileEventPending(provider, "transaction.updated"))
}

func TestIsCheckoutReconcileEventPendingWithSucceededStatus(t *testing.T) {
	provider := &stubCommerceProvider{code: ProviderCodePaddle}
	require.False(t, isCheckoutReconcileEventPending(provider, "transaction.completed"))
}

func TestValidateSubscriptionCheckoutTransitionNilService(t *testing.T) {
	var service *Service
	err := service.validateSubscriptionCheckoutTransition(context.Background(), "user@example.com", "pro")
	require.ErrorIs(t, err, ErrBillingProviderUnavailable)
}

func TestValidateSubscriptionCheckoutTransitionEmptyEmail(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{found: false}
	service := NewService(&stubCommerceProvider{
		code:  ProviderCodePaddle,
		plans: []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	}, stateRepo)
	err := service.validateSubscriptionCheckoutTransition(context.Background(), "", "pro")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestValidateSubscriptionCheckoutTransitionEmptyPlan(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{found: false}
	service := NewService(&stubCommerceProvider{
		code:  ProviderCodePaddle,
		plans: []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	}, stateRepo)
	err := service.validateSubscriptionCheckoutTransition(context.Background(), "user@example.com", "")
	require.ErrorIs(t, err, ErrBillingPlanUnsupported)
}

func TestGetSubscriptionSummaryWithDelinquentState(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{
		found: true,
		state: SubscriptionState{
			Status:         subscriptionStatusActive,
			ProviderStatus: "past_due",
			ActivePlan:     "pro",
			LastEventType:  "invoice.payment_failed",
		},
	}
	service := NewService(&stubCommerceProvider{
		code:         ProviderCodePaddle,
		publicConfig: PublicConfig{ProviderCode: ProviderCodePaddle},
	}, stateRepo)
	summary, err := service.GetSubscriptionSummary(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.True(t, summary.IsDelinquent)
}

func TestGetSubscriptionSummaryStateNotFound(t *testing.T) {
	stateRepo := &stubServiceSubscriptionStateRepository{found: false}
	service := NewService(&stubCommerceProvider{
		code:         ProviderCodePaddle,
		publicConfig: PublicConfig{ProviderCode: ProviderCodePaddle},
		plans:        []SubscriptionPlan{{Code: "pro", MonthlyCredits: 1000}},
	}, stateRepo)
	summary, err := service.GetSubscriptionSummary(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.Equal(t, subscriptionStatusInactive, summary.Status)
}

func TestIsCheckoutReconcileEventPendingNonStatusProvider(t *testing.T) {
	// stubCommerceProviderWithoutCheckoutStatus does not implement CheckoutEventStatusProvider
	provider := &stubCommerceProviderNoCheckoutStatus{code: "test"}
	require.False(t, isCheckoutReconcileEventPending(provider, "transaction.updated"))
}

type stubCommerceProviderNoCheckoutStatus struct {
	code string
}

func (p *stubCommerceProviderNoCheckoutStatus) Code() string { return p.code }
func (p *stubCommerceProviderNoCheckoutStatus) SubscriptionPlans() []SubscriptionPlan {
	return nil
}
func (p *stubCommerceProviderNoCheckoutStatus) TopUpPacks() []TopUpPack { return nil }
func (p *stubCommerceProviderNoCheckoutStatus) PublicConfig() PublicConfig {
	return PublicConfig{}
}
func (p *stubCommerceProviderNoCheckoutStatus) BuildUserSyncEvents(context.Context, string) ([]WebhookEvent, error) {
	return nil, nil
}
func (p *stubCommerceProviderNoCheckoutStatus) CreateSubscriptionCheckout(context.Context, CustomerContext, string) (CheckoutSession, error) {
	return CheckoutSession{}, nil
}
func (p *stubCommerceProviderNoCheckoutStatus) CreateTopUpCheckout(context.Context, CustomerContext, string) (CheckoutSession, error) {
	return CheckoutSession{}, nil
}
func (p *stubCommerceProviderNoCheckoutStatus) CreateCustomerPortalSession(context.Context, string) (PortalSession, error) {
	return PortalSession{}, nil
}
