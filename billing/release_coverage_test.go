package billing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type stubInspectableCommerceProvider struct {
	*stubCommerceProvider
	inspectedSubscriptions []ProviderSubscription
	inspectErr             error
	receivedInspectEmail   string
}

func (provider *stubInspectableCommerceProvider) InspectSubscriptions(
	_ context.Context,
	userEmail string,
) ([]ProviderSubscription, error) {
	provider.receivedInspectEmail = userEmail
	if provider.inspectErr != nil {
		return nil, provider.inspectErr
	}
	return append([]ProviderSubscription(nil), provider.inspectedSubscriptions...), nil
}

type stubStripeCheckoutCoverageClient struct {
	*stubStripeCommerceClient
	checkoutSessions        []stripeCheckoutSessionWebhookData
	listCheckoutSessionsErr error
}

func (client *stubStripeCheckoutCoverageClient) ListCheckoutSessions(
	_ context.Context,
	customerID string,
) ([]stripeCheckoutSessionWebhookData, error) {
	client.receivedListCustomer = customerID
	if client.listCheckoutSessionsErr != nil {
		return nil, client.listCheckoutSessionsErr
	}
	return append([]stripeCheckoutSessionWebhookData(nil), client.checkoutSessions...), nil
}

func TestMetadataHelperCoverage(t *testing.T) {
	require.Equal(t, "", metadataValue(nil, "billing_user_email"))
	require.Equal(
		t,
		"user@example.com",
		metadataValue(
			map[string]string{
				"billing_user_email": "  user@example.com  ",
			},
			" ",
			"missing",
			"billing_user_email",
		),
	)
	require.Equal(t, "", metadataValue(map[string]string{"billing_user_email": " "}, "billing_user_email"))

	require.Equal(t, "", webhookMetadataValueAny(nil, "billing_user_email"))
	require.Equal(
		t,
		"user@example.com",
		webhookMetadataValueAny(
			map[string]interface{}{
				"billing_user_email": "  user@example.com  ",
			},
			" ",
			"missing",
			"billing_user_email",
		),
	)
	require.Equal(t, "42", webhookMetadataValueAny(map[string]interface{}{"credits": float64(42)}, "credits"))
	require.Equal(t, "42.5", webhookMetadataValueAny(map[string]interface{}{"credits": float64(42.5)}, "credits"))
	require.Equal(t, "true", webhookMetadataValueAny(map[string]interface{}{"active": true}, "active"))
}

func TestNormalizeTopUpEligibilityPolicyCoverage(t *testing.T) {
	require.Equal(
		t,
		TopUpEligibilityPolicyUnrestricted,
		NormalizeTopUpEligibilityPolicy(" UNRESTRICTED "),
	)
	require.Equal(
		t,
		TopUpEligibilityPolicyRequiresActiveSubscription,
		NormalizeTopUpEligibilityPolicy("something-else"),
	)
}

func TestSubscriptionInspectionHelpersCoverage(t *testing.T) {
	now := time.Date(2026, time.April, 3, 12, 0, 0, 0, time.UTC)
	olderInactive := ProviderSubscription{
		SubscriptionID: "sub_001",
		Status:         subscriptionStatusInactive,
		ProviderStatus: "canceled",
		OccurredAt:     now.Add(-time.Hour),
	}
	newerActive := ProviderSubscription{
		SubscriptionID: "sub_002",
		PlanCode:       "pro",
		Status:         subscriptionStatusActive,
		ProviderStatus: "active",
		OccurredAt:     now,
	}
	sameTimeHigherID := ProviderSubscription{
		SubscriptionID: "sub_999",
		Status:         subscriptionStatusActive,
		ProviderStatus: "active",
		OccurredAt:     now,
	}

	require.Equal(t, -1, canonicalProviderSubscriptionIndex(nil))
	canonical, found := canonicalProviderSubscription([]ProviderSubscription{olderInactive, newerActive})
	require.True(t, found)
	require.Equal(t, "sub_002", canonical.SubscriptionID)
	require.True(t, activeProviderSubscriptionExists([]ProviderSubscription{olderInactive, newerActive}))
	require.True(t, isProviderSubscriptionActive(ProviderSubscription{Status: " ACTIVE "}))
	require.True(t, providerSubscriptionPreferred(newerActive, olderInactive))
	require.True(t, providerSubscriptionPreferred(sameTimeHigherID, newerActive))
	require.False(t, providerSubscriptionPreferred(olderInactive, ProviderSubscription{
		SubscriptionID: "sub_003",
		Status:         subscriptionStatusInactive,
		OccurredAt:     now,
	}))
}

func TestPaddleCatalogHelperCoverage(t *testing.T) {
	explicitPlans := []PlanCatalogItem{{
		Code:           "starter",
		Label:          "",
		PriceID:        "pri_starter",
		MonthlyCredits: 500,
		PriceCents:     900,
	}}
	builtPlans := buildPaddlePlanCatalogItems(PaddleProviderSettings{Plans: explicitPlans})
	require.Equal(t, explicitPlans, builtPlans)
	builtPlans[0].Code = "mutated"
	require.Equal(t, "starter", explicitPlans[0].Code)

	legacyPlans := buildPaddlePlanCatalogItems(PaddleProviderSettings{
		ProMonthlyPriceID: "pri_pro",
		SubscriptionMonthlyCredits: map[string]int64{
			PlanCodePro: 1000,
		},
		SubscriptionMonthlyPrices: map[string]int64{
			PlanCodePro: 2700,
		},
	})
	require.Len(t, legacyPlans, 1)
	require.Equal(t, PlanCodePro, legacyPlans[0].Code)
	require.Equal(t, paddlePlanProLabel, defaultPlanLabel(" pro "))
	require.Equal(t, paddlePlanPlusLabel, defaultPlanLabel("PLUS"))
	require.Equal(t, "Starter", defaultPlanLabel("starter"))

	explicitPacks := []PackCatalogItem{{
		Code:       "bonus_pack",
		Label:      "Bonus Pack",
		PriceID:    "pri_bonus_pack",
		Credits:    700,
		PriceCents: 1100,
	}}
	builtExplicitPacks := buildPaddlePackCatalogItems(PaddleProviderSettings{Packs: explicitPacks})
	require.Equal(t, explicitPacks, builtExplicitPacks)

	builtLegacyPacks := buildPaddlePackCatalogItems(PaddleProviderSettings{
		TopUpPackPriceIDs: map[string]string{
			"":             "ignored",
			"starter_pack": "pri_starter_pack",
			"z_pack":       "pri_z_pack",
		},
		TopUpPackCredits: map[string]int64{
			"starter_pack": 1200,
			"z_pack":       800,
		},
		TopUpPackPrices: map[string]int64{
			"starter_pack": 1500,
			"z_pack":       900,
		},
	})
	require.Len(t, builtLegacyPacks, 2)
	require.Equal(t, "starter_pack", builtLegacyPacks[0].Code)
	require.Equal(t, "Starter Pack", builtLegacyPacks[0].Label)
}

func TestPaddleProviderPackOnlyAndInspectCoverage(t *testing.T) {
	packOnlyProvider, err := NewPaddleProvider(
		PaddleProviderSettings{
			Environment: "sandbox",
			APIKey:      "pdl_test_123",
			ClientToken: "pk_test_123",
			Packs: []PackCatalogItem{{
				Code:       "bonus_pack",
				PriceID:    "pri_bonus_pack",
				Credits:    700,
				PriceCents: 1100,
			}},
		},
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, err)
	require.Empty(t, packOnlyProvider.SubscriptionPlans())
	require.Len(t, packOnlyProvider.TopUpPacks(), 1)

	client := &stubPaddleCommerceClient{
		resolvedCustomerID: "cus_123",
		listSubscriptions: []paddleSubscriptionWebhookData{
			{ID: "", Status: "active"},
			{
				ID:        "sub_123",
				Status:    "active",
				UpdatedAt: "2026-04-03T12:00:00Z",
				Items: []paddleSubscriptionWebhookItem{
					{PriceID: "pri_plus"},
				},
			},
		},
	}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, err)

	subscriptions, inspectErr := provider.InspectSubscriptions(context.Background(), " User@Example.com ")
	require.NoError(t, inspectErr)
	require.Equal(t, "user@example.com", client.receivedFindCustomerEmail)
	require.Len(t, subscriptions, 1)
	require.Equal(t, "sub_123", subscriptions[0].SubscriptionID)
	require.Equal(t, PlanCodePlus, subscriptions[0].PlanCode)
	require.Equal(t, subscriptionStatusActive, subscriptions[0].Status)

	client.resolvedCustomerID = ""
	emptySubscriptions, emptyErr := provider.InspectSubscriptions(context.Background(), "user@example.com")
	require.NoError(t, emptyErr)
	require.Empty(t, emptySubscriptions)
}

func TestNewPaddleProviderSkipsBlankCatalogCodes(t *testing.T) {
	provider, err := NewPaddleProvider(
		PaddleProviderSettings{
			Environment: "sandbox",
			APIKey:      "pdl_test_123",
			ClientToken: "pk_test_123",
			Plans: []PlanCatalogItem{
				{Code: "", PriceID: "pri_ignored", MonthlyCredits: 1, PriceCents: 1},
				{Code: "starter", PriceID: "pri_starter", MonthlyCredits: 500, PriceCents: 900},
			},
			Packs: []PackCatalogItem{
				{Code: "", PriceID: "pri_ignored_pack", Credits: 1, PriceCents: 1},
				{Code: "bonus_pack", PriceID: "pri_bonus_pack", Credits: 700, PriceCents: 1100},
			},
		},
		&stubPaddleVerifier{},
		&stubPaddleCommerceClient{},
	)
	require.NoError(t, err)
	require.Len(t, provider.SubscriptionPlans(), 1)
	require.Len(t, provider.TopUpPacks(), 1)
}

func TestPaddleSyncSubscriptionStatusCoverage(t *testing.T) {
	require.Equal(t, "", extractPaddleSyncSubscriptionStatus(WebhookEvent{Payload: []byte("not-json")}))
	require.Equal(
		t,
		"active",
		extractPaddleSyncSubscriptionStatus(WebhookEvent{
			Payload: []byte(`{"data":{"status":"active"}}`),
		}),
	)
}

func TestPaddleProviderInspectSubscriptionsErrorCoverage(t *testing.T) {
	var nilProvider *PaddleProvider
	_, err := nilProvider.InspectSubscriptions(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)

	client := &stubPaddleCommerceClient{}
	provider, err := NewPaddleProvider(testPaddleProviderSettings(), &stubPaddleVerifier{}, client)
	require.NoError(t, err)

	_, err = provider.InspectSubscriptions(context.Background(), " ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)

	client.resolveCustomerErr = errors.New("customer lookup failed")
	_, err = provider.InspectSubscriptions(context.Background(), "user@example.com")
	require.ErrorContains(t, err, "billing.paddle.customer.find")

	client.resolveCustomerErr = nil
	client.resolvedCustomerID = "cus_123"
	client.listSubscriptionsErr = errors.New("subscription list failed")
	_, err = provider.InspectSubscriptions(context.Background(), "user@example.com")
	require.ErrorContains(t, err, "billing.paddle.subscriptions.list")
}

func TestServiceInspectorCoverage(t *testing.T) {
	var nilService *Service
	require.Nil(t, nilService.WithTopUpEligibilityPolicy(TopUpEligibilityPolicyUnrestricted))

	planService := NewService(&stubCommerceProvider{
		code: ProviderCodePaddle,
		plans: []SubscriptionPlan{
			{Code: PlanCodePro, MonthlyCredits: 1000},
		},
	})
	require.NoError(t, planService.validateSubscriptionPlan(" pro "))
	require.NoError(t, NewService(&stubCommerceProvider{code: ProviderCodePaddle}).validateSubscriptionPlan("starter"))
	credits, found := planService.resolvePlanMonthlyCredits("pro")
	require.True(t, found)
	require.Equal(t, int64(1000), credits)

	noInspectorSubscriptions, inspected, inspectErr := planService.inspectLiveSubscriptions(context.Background(), "user@example.com")
	require.NoError(t, inspectErr)
	require.False(t, inspected)
	require.Nil(t, noInspectorSubscriptions)

	inspectableProvider := &stubInspectableCommerceProvider{
		stubCommerceProvider: &stubCommerceProvider{
			code: ProviderCodePaddle,
			plans: []SubscriptionPlan{
				{Code: PlanCodePro, MonthlyCredits: 1000},
			},
		},
		inspectErr: errors.New("inspect failed"),
	}
	inspectService := NewService(inspectableProvider, &stubSubscriptionStateRepository{})
	_, inspected, inspectErr = inspectService.inspectLiveSubscriptions(context.Background(), "user@example.com")
	require.True(t, inspected)
	require.ErrorContains(t, inspectErr, "inspect failed")
	require.Equal(t, "user@example.com", inspectableProvider.receivedInspectEmail)
	_, _, inspectErr = inspectService.inspectLiveSubscriptions(context.Background(), " ")
	require.ErrorIs(t, inspectErr, ErrBillingUserEmailInvalid)

	refreshRepository := &stubSubscriptionStateRepository{}
	refreshService := NewService(&stubCommerceProvider{code: ProviderCodePaddle}, refreshRepository)
	require.NoError(t, refreshService.refreshSubscriptionStateFromInspection(context.Background(), "user@example.com", nil))
	require.Len(t, refreshRepository.inputs, 1)
	require.Equal(t, subscriptionStatusInactive, refreshRepository.inputs[0].Status)

	now := time.Date(2026, time.April, 3, 12, 0, 0, 0, time.UTC)
	require.NoError(
		t,
		refreshService.refreshSubscriptionStateFromInspection(context.Background(), "user@example.com", []ProviderSubscription{
			{
				SubscriptionID: "sub_old",
				Status:         subscriptionStatusInactive,
				OccurredAt:     now.Add(-time.Hour),
			},
			{
				SubscriptionID: "sub_active",
				PlanCode:       PlanCodePro,
				Status:         subscriptionStatusActive,
				ProviderStatus: "active",
				NextBillingAt:  now.Add(24 * time.Hour),
				OccurredAt:     now,
			},
		}),
	)
	require.Len(t, refreshRepository.inputs, 2)
	require.Equal(t, "sub_active", refreshRepository.inputs[1].SubscriptionID)
	require.Equal(t, PlanCodePro, refreshRepository.inputs[1].ActivePlan)

	activeProvider := &stubInspectableCommerceProvider{
		stubCommerceProvider: &stubCommerceProvider{
			code: ProviderCodePaddle,
			plans: []SubscriptionPlan{
				{Code: PlanCodePro, MonthlyCredits: 1000},
			},
			subscriptionCheckoutSession: CheckoutSession{
				ProviderCode:  ProviderCodePaddle,
				TransactionID: "txn_should_not_be_used",
				CheckoutMode:  CheckoutModeOverlay,
			},
		},
		inspectedSubscriptions: []ProviderSubscription{{
			SubscriptionID: "sub_active",
			PlanCode:       PlanCodePro,
			Status:         subscriptionStatusActive,
			ProviderStatus: "active",
			OccurredAt:     now,
		}},
	}
	activeRepository := &stubSubscriptionStateRepository{}
	activeService := NewService(activeProvider, activeRepository)
	_, checkoutErr := activeService.CreateSubscriptionCheckout(context.Background(), CustomerContext{Email: "user@example.com"}, PlanCodePro)
	require.ErrorIs(t, checkoutErr, ErrBillingSubscriptionManageInPortal)
	require.Len(t, activeRepository.inputs, 1)
	require.Equal(t, "", activeProvider.receivedSubscriptionPlan)

	inactiveProvider := &stubInspectableCommerceProvider{
		stubCommerceProvider: &stubCommerceProvider{
			code: ProviderCodePaddle,
			plans: []SubscriptionPlan{
				{Code: PlanCodePro, MonthlyCredits: 1000},
			},
			subscriptionCheckoutSession: CheckoutSession{
				ProviderCode:  ProviderCodePaddle,
				TransactionID: "txn_subscription",
				CheckoutMode:  CheckoutModeOverlay,
			},
		},
		inspectedSubscriptions: []ProviderSubscription{{
			SubscriptionID: "sub_inactive",
			Status:         subscriptionStatusInactive,
			ProviderStatus: "canceled",
			OccurredAt:     now,
		}},
	}
	inactiveRepository := &stubSubscriptionStateRepository{}
	inactiveService := NewService(inactiveProvider, inactiveRepository)
	session, checkoutErr := inactiveService.CreateSubscriptionCheckout(
		context.Background(),
		CustomerContext{Email: " User@Example.com ", SubjectID: "subject-123"},
		" PRO ",
	)
	require.NoError(t, checkoutErr)
	require.Equal(t, "user@example.com", inactiveProvider.receivedSubscriptionEmail)
	require.Equal(t, PlanCodePro, inactiveProvider.receivedSubscriptionPlan)
	require.Equal(t, "txn_subscription", session.TransactionID)

	unrestrictedService := NewService(&stubCommerceProvider{code: ProviderCodePaddle}).WithTopUpEligibilityPolicy(TopUpEligibilityPolicyUnrestricted)
	require.Equal(t, TopUpEligibilityPolicyUnrestricted, unrestrictedService.topUpEligibilityPolicy)
	require.NoError(t, unrestrictedService.validateTopUpCheckoutEligibility(context.Background(), "user@example.com"))

	activeTopUpProvider := &stubInspectableCommerceProvider{
		stubCommerceProvider: &stubCommerceProvider{code: ProviderCodePaddle},
		inspectedSubscriptions: []ProviderSubscription{{
			SubscriptionID: "sub_active",
			Status:         subscriptionStatusActive,
			OccurredAt:     now,
		}},
	}
	require.NoError(
		t,
		NewService(activeTopUpProvider, &stubSubscriptionStateRepository{}).validateTopUpCheckoutEligibility(context.Background(), "user@example.com"),
	)

	inactiveTopUpRepository := &stubSubscriptionStateRepository{}
	inactiveTopUpProvider := &stubInspectableCommerceProvider{
		stubCommerceProvider: &stubCommerceProvider{code: ProviderCodePaddle},
		inspectedSubscriptions: []ProviderSubscription{{
			SubscriptionID: "sub_inactive",
			Status:         subscriptionStatusInactive,
			OccurredAt:     now,
		}},
	}
	topUpErr := NewService(inactiveTopUpProvider, inactiveTopUpRepository).validateTopUpCheckoutEligibility(context.Background(), "user@example.com")
	require.ErrorIs(t, topUpErr, ErrBillingSubscriptionRequired)
	require.Len(t, inactiveTopUpRepository.inputs, 1)
}

func TestServiceErrorCoverage(t *testing.T) {
	var nilService *Service
	_, _, err := nilService.inspectLiveSubscriptions(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrBillingProviderUnavailable)
	require.ErrorIs(t, nilService.refreshSubscriptionStateFromInspection(context.Background(), "user@example.com", nil), ErrBillingProviderUnavailable)
	require.ErrorIs(t, nilService.validateSubscriptionPlan("pro"), ErrBillingProviderUnavailable)

	repository := &stubSubscriptionStateRepository{upsertErr: errors.New("upsert failed")}
	service := NewService(&stubCommerceProvider{
		code: ProviderCodePaddle,
		plans: []SubscriptionPlan{
			{Code: PlanCodePro, MonthlyCredits: 1000},
		},
	}, repository)
	require.NoError(t, NewService(&stubCommerceProvider{code: ProviderCodePaddle}).refreshSubscriptionStateFromInspection(context.Background(), "user@example.com", nil))
	require.ErrorIs(t, service.refreshSubscriptionStateFromInspection(context.Background(), " ", nil), ErrBillingUserEmailInvalid)
	require.ErrorContains(t, service.refreshSubscriptionStateFromInspection(context.Background(), "user@example.com", nil), "upsert failed")
	require.ErrorIs(t, service.validateSubscriptionPlan(" "), ErrBillingPlanUnsupported)

	inspectErrorService := NewService(&stubInspectableCommerceProvider{
		stubCommerceProvider: &stubCommerceProvider{
			code: ProviderCodePaddle,
			plans: []SubscriptionPlan{
				{Code: PlanCodePro, MonthlyCredits: 1000},
			},
		},
		inspectErr: errors.New("inspect failed"),
	}, &stubSubscriptionStateRepository{})
	_, err = inspectErrorService.CreateSubscriptionCheckout(context.Background(), CustomerContext{Email: "user@example.com"}, "pro")
	require.ErrorContains(t, err, "billing.checkout.subscription.inspect")

	_, err = service.CreateSubscriptionCheckout(context.Background(), CustomerContext{Email: "user@example.com"}, "unknown")
	require.ErrorIs(t, err, ErrBillingPlanUnsupported)

	_, err = NewService(&stubCommerceProvider{code: ProviderCodePaddle}, repository).CreateSubscriptionCheckout(
		context.Background(),
		CustomerContext{Email: "user@example.com"},
		"",
	)
	require.ErrorIs(t, err, ErrBillingPlanUnsupported)

	refreshErrorService := NewService(&stubInspectableCommerceProvider{
		stubCommerceProvider: &stubCommerceProvider{
			code: ProviderCodePaddle,
			plans: []SubscriptionPlan{
				{Code: PlanCodePro, MonthlyCredits: 1000},
			},
		},
		inspectedSubscriptions: []ProviderSubscription{{
			SubscriptionID: "sub_active",
			Status:         subscriptionStatusActive,
			OccurredAt:     time.Date(2026, time.April, 3, 12, 0, 0, 0, time.UTC),
		}},
	}, &stubSubscriptionStateRepository{upsertErr: errors.New("state write failed")})
	_, err = refreshErrorService.CreateSubscriptionCheckout(context.Background(), CustomerContext{Email: "user@example.com"}, "pro")
	require.ErrorContains(t, err, "billing.checkout.subscription.state")

	inspectTopUpService := NewService(&stubInspectableCommerceProvider{
		stubCommerceProvider: &stubCommerceProvider{code: ProviderCodePaddle},
		inspectErr:           errors.New("inspect failed"),
	}, &stubSubscriptionStateRepository{})
	err = inspectTopUpService.validateTopUpCheckoutEligibility(context.Background(), "user@example.com")
	require.ErrorContains(t, err, "billing.checkout.top_up.inspect")

	refreshTopUpService := NewService(&stubInspectableCommerceProvider{
		stubCommerceProvider: &stubCommerceProvider{code: ProviderCodePaddle},
		inspectedSubscriptions: []ProviderSubscription{{
			SubscriptionID: "sub_inactive",
			Status:         subscriptionStatusInactive,
			OccurredAt:     time.Date(2026, time.April, 3, 12, 0, 0, 0, time.UTC),
		}},
	}, &stubSubscriptionStateRepository{upsertErr: errors.New("state write failed")})
	err = refreshTopUpService.validateTopUpCheckoutEligibility(context.Background(), "user@example.com")
	require.ErrorContains(t, err, "billing.checkout.top_up.state")
}

func TestStripeCatalogAndProviderCoverage(t *testing.T) {
	explicitPlans := []PlanCatalogItem{{
		Code:           "starter",
		Label:          "",
		PriceID:        "price_starter",
		MonthlyCredits: 500,
		PriceCents:     900,
	}}
	builtPlans := buildStripePlanCatalogItems(StripeProviderSettings{Plans: explicitPlans})
	require.Equal(t, explicitPlans, builtPlans)

	legacyPlans := buildStripePlanCatalogItems(StripeProviderSettings{
		PlusMonthlyPriceID: "price_plus",
		SubscriptionMonthlyCredits: map[string]int64{
			PlanCodePlus: 5000,
		},
		SubscriptionMonthlyPrices: map[string]int64{
			PlanCodePlus: 9000,
		},
	})
	require.Len(t, legacyPlans, 1)
	require.Equal(t, PlanCodePlus, legacyPlans[0].Code)
	require.Equal(t, stripePlanProLabel, defaultStripePlanLabel("pro"))
	require.Equal(t, stripePlanPlusLabel, defaultStripePlanLabel("PLUS"))
	require.Equal(t, "Starter", defaultStripePlanLabel("starter"))

	explicitPacks := []PackCatalogItem{{
		Code:       "bonus_pack",
		Label:      "Bonus Pack",
		PriceID:    "price_bonus_pack",
		Credits:    700,
		PriceCents: 1100,
	}}
	builtExplicitPacks := buildStripePackCatalogItems(StripeProviderSettings{Packs: explicitPacks})
	require.Equal(t, explicitPacks, builtExplicitPacks)

	builtLegacyPacks := buildStripePackCatalogItems(StripeProviderSettings{
		TopUpPackPriceIDs: map[string]string{
			"":             "ignored",
			"starter_pack": "price_starter_pack",
			"z_pack":       "price_z_pack",
		},
		TopUpPackCredits: map[string]int64{
			"starter_pack": 1200,
			"z_pack":       800,
		},
		TopUpPackPrices: map[string]int64{
			"starter_pack": 1500,
			"z_pack":       900,
		},
	})
	require.Len(t, builtLegacyPacks, 2)
	require.Equal(t, "starter_pack", builtLegacyPacks[0].Code)
	require.Equal(t, "Starter Pack", builtLegacyPacks[0].Label)

	packOnlyProvider, err := NewStripeProvider(
		StripeProviderSettings{
			Environment:        "sandbox",
			APIKey:             "sk_test_123",
			ClientToken:        "pk_test_123",
			CheckoutSuccessURL: "https://app.local/success",
			CheckoutCancelURL:  "https://app.local/cancel",
			PortalReturnURL:    "https://app.local/portal",
			Packs: []PackCatalogItem{{
				Code:       "bonus_pack",
				PriceID:    "price_bonus_pack",
				Credits:    700,
				PriceCents: 1100,
			}},
		},
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	require.NoError(t, err)
	require.Empty(t, packOnlyProvider.SubscriptionPlans())
	require.Len(t, packOnlyProvider.TopUpPacks(), 1)
}

func TestNewStripeProviderSkipsBlankCatalogCodesAndInjectedClientError(t *testing.T) {
	provider, err := NewStripeProvider(
		StripeProviderSettings{
			Environment:        "sandbox",
			APIKey:             "sk_test_123",
			ClientToken:        "pk_test_123",
			CheckoutSuccessURL: "https://app.local/success",
			CheckoutCancelURL:  "https://app.local/cancel",
			PortalReturnURL:    "https://app.local/portal",
			Plans: []PlanCatalogItem{
				{Code: "", PriceID: "price_ignored", MonthlyCredits: 1, PriceCents: 1},
				{Code: "starter", PriceID: "price_starter", MonthlyCredits: 500, PriceCents: 900},
			},
			Packs: []PackCatalogItem{
				{Code: "", PriceID: "price_ignored_pack", Credits: 1, PriceCents: 1},
				{Code: "bonus_pack", PriceID: "price_bonus_pack", Credits: 700, PriceCents: 1100},
			},
		},
		&stubStripeVerifier{},
		&stubStripeCommerceClient{},
	)
	require.NoError(t, err)
	require.Len(t, provider.SubscriptionPlans(), 1)
	require.Len(t, provider.TopUpPacks(), 1)

	originalNewStripeAPIClientFunc := newStripeAPIClientFunc
	t.Cleanup(func() {
		newStripeAPIClientFunc = originalNewStripeAPIClientFunc
	})
	newStripeAPIClientFunc = func(string, *http.Client) (*stripeAPIClient, error) {
		return nil, errors.New("client init failed")
	}

	_, err = NewStripeProvider(
		StripeProviderSettings{
			Environment:        "sandbox",
			APIKey:             "sk_test_123",
			ClientToken:        "pk_test_123",
			CheckoutSuccessURL: "https://app.local/success",
			CheckoutCancelURL:  "https://app.local/cancel",
			PortalReturnURL:    "https://app.local/portal",
		},
		&stubStripeVerifier{},
		nil,
	)
	require.ErrorContains(t, err, "client init failed")
}

func TestStripeInspectSubscriptionsAndCheckoutSyncCoverage(t *testing.T) {
	now := time.Date(2026, time.April, 3, 12, 0, 0, 0, time.UTC)
	client := &stubStripeCheckoutCoverageClient{
		stubStripeCommerceClient: &stubStripeCommerceClient{
			foundCustomerID: "cus_123",
			subscriptions: []stripeSubscriptionWebhookData{
				{ID: "", Status: stripeSubscriptionStatusCanceled, CreatedAt: now.Unix()},
				{
					ID:         "sub_123",
					Status:     stripeSubscriptionStatusActive,
					CreatedAt:  now.Unix(),
					CustomerID: "cus_123",
					Items: stripeSubscriptionItems{
						Data: []stripeSubscriptionItem{
							{Price: stripeSubscriptionItemPrice{ID: "price_plus"}},
						},
					},
				},
			},
		},
		checkoutSessions: []stripeCheckoutSessionWebhookData{
			{ID: "", Status: stripeCheckoutStatusComplete, PaymentStatus: stripeCheckoutPaymentStatusPaid, CreatedAt: now.Unix()},
			{ID: "cs_unpaid", Status: "open", PaymentStatus: "unpaid", CreatedAt: now.Unix()},
			{ID: "cs_paid", Status: stripeCheckoutStatusComplete, PaymentStatus: stripeCheckoutPaymentStatusPaid, CreatedAt: now.Unix()},
		},
	}
	provider, err := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, client)
	require.NoError(t, err)

	subscriptions, inspectErr := provider.InspectSubscriptions(context.Background(), " User@Example.com ")
	require.NoError(t, inspectErr)
	require.Equal(t, "user@example.com", client.receivedFindEmail)
	require.Len(t, subscriptions, 1)
	require.Equal(t, "sub_123", subscriptions[0].SubscriptionID)
	require.Equal(t, PlanCodePlus, subscriptions[0].PlanCode)
	require.Equal(t, subscriptionStatusActive, subscriptions[0].Status)

	client.foundCustomerID = ""
	emptySubscriptions, emptyErr := provider.InspectSubscriptions(context.Background(), "user@example.com")
	require.NoError(t, emptyErr)
	require.Empty(t, emptySubscriptions)

	client.foundCustomerID = "cus_123"
	syncEvents, syncErr := provider.buildUserCheckoutSyncEvents(context.Background(), "cus_123")
	require.NoError(t, syncErr)
	require.Equal(t, "cus_123", client.receivedListCustomer)
	require.Len(t, syncEvents, 1)
	require.Equal(t, "sync:checkout:cs_paid:completed", syncEvents[0].EventID)
}

func TestStripeProviderErrorCoverage(t *testing.T) {
	var nilProvider *StripeProvider
	_, err := nilProvider.InspectSubscriptions(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrStripeProviderClientUnavailable)

	client := &stubStripeCheckoutCoverageClient{
		stubStripeCommerceClient: &stubStripeCommerceClient{},
	}
	provider, err := NewStripeProvider(testStripeProviderSettings(), &stubStripeVerifier{}, client)
	require.NoError(t, err)

	_, err = provider.InspectSubscriptions(context.Background(), " ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)

	client.findCustomerIDErr = errors.New("customer lookup failed")
	_, err = provider.InspectSubscriptions(context.Background(), "user@example.com")
	require.ErrorContains(t, err, "billing.stripe.customer.find")

	client.findCustomerIDErr = nil
	client.foundCustomerID = "cus_123"
	client.listSubscriptionsErr = errors.New("subscription list failed")
	_, err = provider.InspectSubscriptions(context.Background(), "user@example.com")
	require.ErrorContains(t, err, "billing.stripe.sync.subscription.list")

	client.listSubscriptionsErr = nil
	client.listCheckoutSessionsErr = errors.New("checkout list failed")
	_, err = provider.BuildUserSyncEvents(context.Background(), "user@example.com")
	require.ErrorContains(t, err, "billing.stripe.sync.checkout.list")

	client.listCheckoutSessionsErr = nil
	client.checkoutSessions = []stripeCheckoutSessionWebhookData{
		{ID: "cs_b", Status: stripeCheckoutStatusComplete, PaymentStatus: stripeCheckoutPaymentStatusPaid, CreatedAt: 1712149200},
		{ID: "cs_a", Status: stripeCheckoutStatusComplete, PaymentStatus: stripeCheckoutPaymentStatusPaid, CreatedAt: 1712149200},
	}
	syncEvents, err := provider.buildUserCheckoutSyncEvents(context.Background(), "cus_123")
	require.NoError(t, err)
	require.Len(t, syncEvents, 2)
	require.Equal(t, "sync:checkout:cs_a:completed", syncEvents[0].EventID)

	client.checkoutSessions = []stripeCheckoutSessionWebhookData{
		{ID: "cs_later", Status: stripeCheckoutStatusComplete, PaymentStatus: stripeCheckoutPaymentStatusPaid, CreatedAt: 1712149200},
		{ID: "cs_earlier", Status: stripeCheckoutStatusComplete, PaymentStatus: stripeCheckoutPaymentStatusPaid, CreatedAt: 1712145600},
	}
	syncEvents, err = provider.buildUserCheckoutSyncEvents(context.Background(), "cus_123")
	require.NoError(t, err)
	require.Len(t, syncEvents, 2)
	require.Equal(t, "sync:checkout:cs_earlier:completed", syncEvents[0].EventID)

	originalMarshalFunc := jsonMarshalFunc
	t.Cleanup(func() {
		jsonMarshalFunc = originalMarshalFunc
	})
	jsonMarshalFunc = func(any) ([]byte, error) {
		return nil, errors.New("marshal failed")
	}
	_, err = provider.buildUserCheckoutSyncEvents(context.Background(), "cus_123")
	require.ErrorContains(t, err, "billing.stripe.sync.checkout.payload")

	jsonMarshalFunc = originalMarshalFunc
	client.checkoutSessions = []stripeCheckoutSessionWebhookData{
		{ID: "cs_zero_time", Status: stripeCheckoutStatusComplete, PaymentStatus: stripeCheckoutPaymentStatusPaid, CreatedAt: 0},
	}
	_, err = provider.buildUserCheckoutSyncEvents(context.Background(), "cus_123")
	require.ErrorIs(t, err, ErrStripeWebhookPayloadInvalid)
}

func TestBuildStripeSyncSubscriptionEventsFromInspectedCoverage(t *testing.T) {
	now := time.Date(2026, time.April, 3, 12, 0, 0, 0, time.UTC)
	events, err := buildStripeSyncSubscriptionEventsFromInspected("user@example.com", []stripeInspectedSubscription{
		{
			payload: stripeSubscriptionWebhookData{
				ID:        "",
				Status:    stripeSubscriptionStatusCanceled,
				CreatedAt: now.Unix(),
			},
			normalized: ProviderSubscription{
				SubscriptionID: "",
				Status:         subscriptionStatusInactive,
				ProviderStatus: stripeSubscriptionStatusCanceled,
				OccurredAt:     now,
			},
		},
		{
			payload: stripeSubscriptionWebhookData{
				ID:        "sub_123",
				Status:    stripeSubscriptionStatusActive,
				CreatedAt: now.Unix(),
			},
			normalized: ProviderSubscription{
				SubscriptionID: "sub_123",
				Status:         subscriptionStatusActive,
				ProviderStatus: stripeSubscriptionStatusActive,
				OccurredAt:     now,
			},
		},
	}, now)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "sync:subscription:sub_123:active", events[0].EventID)
}

func TestStripeAPIClientListCheckoutSessionsCoverage(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestCount++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/checkout/sessions", request.URL.Path)
		require.Equal(t, "cus_test_123", request.URL.Query().Get("customer"))
		require.Equal(t, "100", request.URL.Query().Get("limit"))
		if requestCount == 1 {
			_, writeErr := responseWriter.Write([]byte(`{"data":[{"id":"cs_1","status":"complete","payment_status":"paid","created":1712145600}],"has_more":true}`))
			require.NoError(t, writeErr)
			return
		}
		require.Equal(t, "cs_1", request.URL.Query().Get("starting_after"))
		_, writeErr := responseWriter.Write([]byte(`{"data":[{"id":"cs_2","status":"complete","payment_status":"paid","created":1712149200}],"has_more":false}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	sessions, err := apiClient.ListCheckoutSessions(context.Background(), "cus_test_123")
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	require.Equal(t, "cs_2", sessions[1].ID)

	emptySessions, emptyErr := apiClient.ListCheckoutSessions(context.Background(), " ")
	require.NoError(t, emptyErr)
	require.Empty(t, emptySessions)
}

func TestStripeAPIClientListCheckoutSessionsRejectsMissingPaginationData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		_, writeErr := responseWriter.Write([]byte(`{"data":[],"has_more":true}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, err := apiClient.ListCheckoutSessions(context.Background(), "cus_test_123")
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

func TestStripeAPIClientListCheckoutSessionsRejectsMissingCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		_, writeErr := responseWriter.Write([]byte(`{"data":[{"id":"","status":"complete","payment_status":"paid","created":1712145600}],"has_more":true}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, err := apiClient.ListCheckoutSessions(context.Background(), "cus_test_123")
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

func TestStripeAPIClientListCheckoutSessionsRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		_, writeErr := responseWriter.Write([]byte(`{"error":{"message":"boom"}}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, err := apiClient.ListCheckoutSessions(context.Background(), "cus_test_123")
	require.Error(t, err)
}
