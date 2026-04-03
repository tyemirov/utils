package billing_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/tyemirov/utils/billing"
)

type exampleCommerceProvider struct{}

func (exampleCommerceProvider) Code() string {
	return billing.ProviderCodePaddle
}

func (exampleCommerceProvider) SubscriptionPlans() []billing.SubscriptionPlan {
	return []billing.SubscriptionPlan{
		{
			Code:           "pro",
			Label:          "Pro",
			MonthlyCredits: 1000,
			PriceDisplay:   "$10.00",
			BillingPeriod:  "monthly",
		},
	}
}

func (exampleCommerceProvider) TopUpPacks() []billing.TopUpPack {
	return nil
}

func (exampleCommerceProvider) PublicConfig() billing.PublicConfig {
	return billing.PublicConfig{
		ProviderCode: billing.ProviderCodePaddle,
		Environment:  "sandbox",
		ClientToken:  "client_token_demo",
	}
}

func (exampleCommerceProvider) BuildUserSyncEvents(context.Context, string) ([]billing.WebhookEvent, error) {
	return nil, nil
}

func (exampleCommerceProvider) CreateSubscriptionCheckout(
	context.Context,
	billing.CustomerContext,
	string,
) (billing.CheckoutSession, error) {
	return billing.CheckoutSession{
		ProviderCode:  billing.ProviderCodePaddle,
		TransactionID: "txn_123",
		CheckoutMode:  billing.CheckoutModeOverlay,
	}, nil
}

func (exampleCommerceProvider) CreateTopUpCheckout(
	context.Context,
	billing.CustomerContext,
	string,
) (billing.CheckoutSession, error) {
	return billing.CheckoutSession{}, billing.ErrBillingTopUpPackUnknown
}

func (exampleCommerceProvider) CreateCustomerPortalSession(
	context.Context,
	string,
) (billing.PortalSession, error) {
	return billing.PortalSession{
		ProviderCode: billing.ProviderCodePaddle,
		URL:          "https://billing.example.test/portal",
	}, nil
}

type exampleSubscriptionStateRepository struct {
	states map[string]billing.SubscriptionState
}

func (repository *exampleSubscriptionStateRepository) Upsert(
	_ context.Context,
	input billing.SubscriptionStateUpsertInput,
) error {
	if repository.states == nil {
		repository.states = map[string]billing.SubscriptionState{}
	}
	repository.states[input.UserEmail] = billing.SubscriptionState{
		ProviderCode:        input.ProviderCode,
		UserEmail:           input.UserEmail,
		Status:              input.Status,
		ProviderStatus:      input.ProviderStatus,
		ActivePlan:          input.ActivePlan,
		SubscriptionID:      input.SubscriptionID,
		NextBillingAt:       input.NextBillingAt,
		LastEventID:         input.LastEventID,
		LastEventType:       input.LastEventType,
		LastEventOccurredAt: input.EventOccurredAt,
		LastTransactionID:   input.LastTransactionID,
	}
	return nil
}

func (repository *exampleSubscriptionStateRepository) Get(
	_ context.Context,
	_ string,
	userEmail string,
) (billing.SubscriptionState, bool, error) {
	state, found := repository.states[userEmail]
	return state, found, nil
}

func (repository *exampleSubscriptionStateRepository) GetBySubscriptionID(
	_ context.Context,
	_ string,
	subscriptionID string,
) (billing.SubscriptionState, bool, error) {
	for _, state := range repository.states {
		if state.SubscriptionID == subscriptionID {
			return state, true, nil
		}
	}
	return billing.SubscriptionState{}, false, nil
}

type exampleWebhookProvider struct{}

func (exampleWebhookProvider) Code() string {
	return billing.ProviderCodePaddle
}

func (exampleWebhookProvider) SignatureHeaderName() string {
	return "X-Test-Signature"
}

func (exampleWebhookProvider) VerifySignature(signatureHeader string, _ []byte) error {
	if strings.TrimSpace(signatureHeader) != "ok" {
		return errors.New("invalid signature")
	}
	return nil
}

func (exampleWebhookProvider) ParseWebhookEvent(_ []byte) (billing.WebhookEventMetadata, error) {
	return billing.WebhookEventMetadata{
		EventID:    "evt_123",
		EventType:  "subscription.updated",
		OccurredAt: time.Unix(1700000000, 0).UTC(),
	}, nil
}

func ExampleService() {
	repository := &exampleSubscriptionStateRepository{
		states: map[string]billing.SubscriptionState{
			"subscriber@example.com": {
				ProviderCode:   billing.ProviderCodePaddle,
				UserEmail:      "subscriber@example.com",
				Status:         "active",
				ProviderStatus: "active",
				ActivePlan:     "pro",
				SubscriptionID: "sub_123",
			},
		},
	}
	service := billing.NewService(exampleCommerceProvider{}, repository)

	summary, _ := service.GetSubscriptionSummary(context.Background(), "subscriber@example.com")
	fmt.Println(summary.ProviderCode, summary.Status, summary.ActivePlan)

	checkout, _ := service.CreateSubscriptionCheckout(
		context.Background(),
		billing.CustomerContext{Email: "new@example.com"},
		"pro",
	)
	fmt.Println(checkout.ProviderCode, checkout.TransactionID, checkout.CheckoutMode)

	// Output:
	// paddle active pro
	// paddle txn_123 overlay
}

func ExampleWebhookHandler() {
	processedEventType := ""
	handler := billing.NewWebhookHandler(
		exampleWebhookProvider{},
		billing.WebhookProcessorFunc(func(_ context.Context, event billing.WebhookEvent) error {
			processedEventType = event.EventType
			return nil
		}),
	)

	request := httptest.NewRequest(http.MethodPost, "/billing/webhook", strings.NewReader(`{"ok":true}`))
	request.Header.Set("X-Test-Signature", "ok")

	responseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(responseRecorder, request)

	fmt.Println(responseRecorder.Code, strings.TrimSpace(responseRecorder.Body.String()), processedEventType)

	// Output:
	// 200 ok subscription.updated
}
