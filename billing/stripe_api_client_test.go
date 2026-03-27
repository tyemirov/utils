package billing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStripeAPIClientCreateCheckoutSessionSendsExpectedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v1/checkout/sessions", request.URL.Path)
		require.Equal(t, "Bearer sk_test_123", request.Header.Get(stripeHeaderAuthorization))
		require.Equal(t, stripeUserAgentPoodleScanner, request.Header.Get(stripeHeaderUserAgent))
		require.Equal(t, stripeContentTypeFormURLEncoded, request.Header.Get(stripeHeaderContentType))
		require.NoError(t, request.ParseForm())
		require.Equal(t, "subscription", request.Form.Get("mode"))
		require.Equal(t, "cus_test_123", request.Form.Get("customer"))
		require.Equal(t, "price_pro", request.Form.Get("line_items[0][price]"))
		require.Equal(t, "1", request.Form.Get("line_items[0][quantity]"))
		require.Equal(t, "user@example.com", request.Form.Get("metadata[poodle_scanner_user_email]"))
		_, writeErr := responseWriter.Write([]byte(`{"id":"cs_test_123"}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	checkoutSessionID, checkoutErr := apiClient.CreateCheckoutSession(context.Background(), stripeCheckoutSessionInput{
		CustomerID: "cus_test_123",
		PriceID:    "price_pro",
		Mode:       stripeCheckoutModeSubscription,
		SuccessURL: "https://app.local/success",
		CancelURL:  "https://app.local/cancel",
		Metadata: map[string]string{
			stripeMetadataUserEmailKey: "user@example.com",
		},
	})
	require.NoError(t, checkoutErr)
	require.Equal(t, "cs_test_123", checkoutSessionID)
}

func TestStripeAPIClientGetPriceNotFoundReturnsPriceNotFoundError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/prices/price_missing", request.URL.Path)
		responseWriter.WriteHeader(http.StatusNotFound)
		_, writeErr := responseWriter.Write([]byte(`{"error":{"code":"resource_missing","message":"No such price"}}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, getPriceErr := apiClient.GetPrice(context.Background(), "price_missing")
	require.Error(t, getPriceErr)
	require.ErrorIs(t, getPriceErr, ErrStripeAPIPriceNotFound)
}

func TestStripeAPIClientFindCustomerIDReturnsEmptyWhenMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/customers", request.URL.Path)
		require.Equal(t, "user@example.com", request.URL.Query().Get("email"))
		require.Equal(t, "1", request.URL.Query().Get("limit"))
		_, writeErr := responseWriter.Write([]byte(`{"data":[]}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	customerID, customerErr := apiClient.FindCustomerID(context.Background(), "User@Example.com")
	require.NoError(t, customerErr)
	require.Equal(t, "", customerID)
}

func TestStripeAPIClientListSubscriptionsSendsExpectedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/subscriptions", request.URL.Path)
		require.Equal(t, "cus_test_123", request.URL.Query().Get("customer"))
		require.Equal(t, "all", request.URL.Query().Get("status"))
		require.Equal(t, "100", request.URL.Query().Get("limit"))
		_, writeErr := responseWriter.Write([]byte(`{"data":[{"id":"sub_test_123","status":"active","customer":"cus_test_123","created":1761023491}],"has_more":false}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	subscriptions, subscriptionsErr := apiClient.ListSubscriptions(context.Background(), "cus_test_123")
	require.NoError(t, subscriptionsErr)
	require.Len(t, subscriptions, 1)
	require.Equal(t, "sub_test_123", subscriptions[0].ID)
	require.Equal(t, "active", subscriptions[0].Status)
}

func TestStripeAPIClientGetPriceRetriesRateLimitedResponse(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		if requestCount.Load() == 1 {
			responseWriter.Header().Set(stripeHeaderRetryAfter, "0")
			responseWriter.WriteHeader(http.StatusTooManyRequests)
			_, writeErr := responseWriter.Write([]byte(`{"error":{"code":"rate_limit","message":"Too many requests"}}`))
			require.NoError(t, writeErr)
			return
		}
		_, writeErr := responseWriter.Write([]byte(`{"id":"price_pro","type":"recurring","unit_amount":2700,"recurring":{"interval":"month"}}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	priceResponse, getPriceErr := apiClient.GetPrice(context.Background(), "price_pro")
	require.NoError(t, getPriceErr)
	require.Equal(t, "price_pro", priceResponse.ID)
	require.EqualValues(t, 2700, priceResponse.UnitAmount)
	require.EqualValues(t, 2, requestCount.Load())
}

func TestStripeAPIClientGetPriceRateLimitedRetryWaitDeadlineIsTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(stripeHeaderRetryAfter, "3600")
		responseWriter.WriteHeader(http.StatusTooManyRequests)
		_, writeErr := responseWriter.Write([]byte(`{"error":{"code":"rate_limit","message":"Too many requests"}}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	requestContext, cancelRequestContext := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelRequestContext()

	_, getPriceErr := apiClient.GetPrice(requestContext, "price_pro")
	require.Error(t, getPriceErr)
	require.ErrorIs(t, getPriceErr, ErrStripeAPIRequestFailed)
	require.ErrorIs(t, getPriceErr, ErrStripeAPITransient)
	require.ErrorIs(t, getPriceErr, ErrStripeAPIRateLimited)
}

func TestStripeAPIClientCreateCheckoutSessionDoesNotRetryRateLimitedResponse(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		require.Equal(t, http.MethodPost, request.Method)
		responseWriter.Header().Set(stripeHeaderRetryAfter, "0")
		responseWriter.WriteHeader(http.StatusTooManyRequests)
		_, writeErr := responseWriter.Write([]byte(`{"error":{"code":"rate_limit","message":"Too many requests"}}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, checkoutErr := apiClient.CreateCheckoutSession(context.Background(), stripeCheckoutSessionInput{
		CustomerID: "cus_test_123",
		PriceID:    "price_pro",
		Mode:       stripeCheckoutModeSubscription,
		SuccessURL: "https://app.local/success",
		CancelURL:  "https://app.local/cancel",
		Metadata: map[string]string{
			stripeMetadataUserEmailKey: "user@example.com",
		},
	})
	require.Error(t, checkoutErr)
	require.ErrorIs(t, checkoutErr, ErrStripeAPITransient)
	require.ErrorIs(t, checkoutErr, ErrStripeAPIRateLimited)
	require.EqualValues(t, 1, requestCount.Load())
}

func TestStripeAPIClientResolveCustomerIDFindsExisting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/customers", request.URL.Path)
		_, writeErr := responseWriter.Write([]byte(`{"data":[{"id":"cus_existing"}]}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	customerID, err := apiClient.ResolveCustomerID(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.Equal(t, "cus_existing", customerID)
}

func TestStripeAPIClientResolveCustomerIDCreatesNew(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		if requestCount.Load() == 1 {
			require.Equal(t, http.MethodGet, request.Method)
			_, writeErr := responseWriter.Write([]byte(`{"data":[]}`))
			require.NoError(t, writeErr)
			return
		}
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v1/customers", request.URL.Path)
		_, writeErr := responseWriter.Write([]byte(`{"id":"cus_new_123","email":"user@example.com"}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	customerID, err := apiClient.ResolveCustomerID(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.Equal(t, "cus_new_123", customerID)
	require.EqualValues(t, 2, requestCount.Load())
}

func TestStripeAPIClientResolveCustomerIDFindErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		_, writeErr := responseWriter.Write([]byte(`{"error":{"code":"server_error","message":"internal"}}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, err := apiClient.ResolveCustomerID(context.Background(), "user@example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeAPITransient)
}

func TestStripeAPIClientResolveCustomerEmailReturnsEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/customers/cus_123", request.URL.Path)
		_, writeErr := responseWriter.Write([]byte(`{"id":"cus_123","email":"User@Example.com"}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	email, err := apiClient.ResolveCustomerEmail(context.Background(), "cus_123")
	require.NoError(t, err)
	require.Equal(t, "user@example.com", email)
}

func TestStripeAPIClientResolveCustomerEmailRejectsEmpty(t *testing.T) {
	apiClient, clientErr := newStripeAPIClient("sk_test_123", nil)
	require.NoError(t, clientErr)

	_, err := apiClient.ResolveCustomerEmail(context.Background(), "")
	require.ErrorIs(t, err, ErrStripeAPICustomerNotFound)
}

func TestStripeAPIClientResolveCustomerEmailRejectsEmptyEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		_, writeErr := responseWriter.Write([]byte(`{"id":"cus_123","email":""}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, err := apiClient.ResolveCustomerEmail(context.Background(), "cus_123")
	require.ErrorIs(t, err, ErrStripeAPICustomerNotFound)
}

func TestStripeAPIClientGetCheckoutSessionReturnsData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/checkout/sessions/cs_test_1", request.URL.Path)
		_, writeErr := responseWriter.Write([]byte(`{"id":"cs_test_1","status":"complete","payment_status":"paid"}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	session, err := apiClient.GetCheckoutSession(context.Background(), "cs_test_1")
	require.NoError(t, err)
	require.Equal(t, "cs_test_1", session.ID)
}

func TestStripeAPIClientGetCheckoutSessionRejectsEmptyID(t *testing.T) {
	apiClient, clientErr := newStripeAPIClient("sk_test_123", nil)
	require.NoError(t, clientErr)

	_, err := apiClient.GetCheckoutSession(context.Background(), "")
	require.ErrorIs(t, err, ErrStripeAPICheckoutSessionNotFound)
}

func TestStripeAPIClientCreateCustomerPortalURLReturnsURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v1/billing_portal/sessions", request.URL.Path)
		_, writeErr := responseWriter.Write([]byte(`{"url":"https://billing.stripe.com/session/test_portal"}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	portalURL, err := apiClient.CreateCustomerPortalURL(context.Background(), stripePortalSessionInput{
		CustomerID: "cus_123",
		ReturnURL:  "https://app.local/return",
	})
	require.NoError(t, err)
	require.Equal(t, "https://billing.stripe.com/session/test_portal", portalURL)
}

func TestStripeAPIClientCreateCustomerPortalURLRejectsMissingInputs(t *testing.T) {
	apiClient, clientErr := newStripeAPIClient("sk_test_123", nil)
	require.NoError(t, clientErr)

	_, err := apiClient.CreateCustomerPortalURL(context.Background(), stripePortalSessionInput{
		CustomerID: "",
		ReturnURL:  "https://app.local/return",
	})
	require.ErrorIs(t, err, ErrStripeAPIPortalURLNotFound)

	_, err = apiClient.CreateCustomerPortalURL(context.Background(), stripePortalSessionInput{
		CustomerID: "cus_123",
		ReturnURL:  "",
	})
	require.ErrorIs(t, err, ErrStripeAPIPortalURLNotFound)
}

func TestStripeAPIClientCreateCustomerPortalURLRejectsEmptyURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		_, writeErr := responseWriter.Write([]byte(`{"url":""}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, err := apiClient.CreateCustomerPortalURL(context.Background(), stripePortalSessionInput{
		CustomerID: "cus_123",
		ReturnURL:  "https://app.local/return",
	})
	require.ErrorIs(t, err, ErrStripeAPIPortalURLNotFound)
}

func TestStripeAPIClientCreateCustomerReturnsID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v1/customers", request.URL.Path)
		require.NoError(t, request.ParseForm())
		require.Equal(t, "user@example.com", request.Form.Get("email"))
		_, writeErr := responseWriter.Write([]byte(`{"id":"cus_created_1","email":"user@example.com"}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	customerID, err := apiClient.createCustomer(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.Equal(t, "cus_created_1", customerID)
}

func TestStripeAPIClientCreateCustomerRejectsEmptyEmail(t *testing.T) {
	apiClient, clientErr := newStripeAPIClient("sk_test_123", nil)
	require.NoError(t, clientErr)

	_, err := apiClient.createCustomer(context.Background(), "")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestParseStripeMetadataInt64ValidValue(t *testing.T) {
	metadata := map[string]string{"credits": "42"}
	value, err := parseStripeMetadataInt64(metadata, "credits")
	require.NoError(t, err)
	require.EqualValues(t, 42, value)
}

func TestParseStripeMetadataInt64MissingKey(t *testing.T) {
	metadata := map[string]string{}
	value, err := parseStripeMetadataInt64(metadata, "credits")
	require.NoError(t, err)
	require.EqualValues(t, 0, value)
}

func TestParseStripeMetadataInt64InvalidFormat(t *testing.T) {
	metadata := map[string]string{"credits": "abc"}
	_, err := parseStripeMetadataInt64(metadata, "credits")
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestParseStripeMetadataInt64NegativeValue(t *testing.T) {
	metadata := map[string]string{"credits": "-5"}
	_, err := parseStripeMetadataInt64(metadata, "credits")
	require.ErrorIs(t, err, ErrWebhookGrantMetadataInvalid)
}

func TestStripeAPIClientListSubscriptionsPaginates(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		if requestCount.Load() == 1 {
			_, writeErr := responseWriter.Write([]byte(`{"data":[{"id":"sub_1","status":"active","customer":"cus_1","created":1761023491}],"has_more":true}`))
			require.NoError(t, writeErr)
			return
		}
		require.Equal(t, "sub_1", request.URL.Query().Get("starting_after"))
		_, writeErr := responseWriter.Write([]byte(`{"data":[{"id":"sub_2","status":"canceled","customer":"cus_1","created":1761023492}],"has_more":false}`))
		require.NoError(t, writeErr)
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	subscriptions, err := apiClient.ListSubscriptions(context.Background(), "cus_1")
	require.NoError(t, err)
	require.Len(t, subscriptions, 2)
	require.Equal(t, "sub_1", subscriptions[0].ID)
	require.Equal(t, "sub_2", subscriptions[1].ID)
	require.EqualValues(t, 2, requestCount.Load())
}

func TestStripeAPIClientListSubscriptionsEmptyCustomer(t *testing.T) {
	apiClient, clientErr := newStripeAPIClient("sk_test_123", nil)
	require.NoError(t, clientErr)

	subscriptions, err := apiClient.ListSubscriptions(context.Background(), "")
	require.NoError(t, err)
	require.Empty(t, subscriptions)
}

func TestNewStripeAPIClientRejectsEmptyAPIKey(t *testing.T) {
	_, err := newStripeAPIClient("", nil)
	require.ErrorIs(t, err, ErrStripeAPIKeyEmpty)

	_, err = newStripeAPIClient("   ", nil)
	require.ErrorIs(t, err, ErrStripeAPIKeyEmpty)
}

func TestResolveStripeAPIMissingResourceErrorAllPrefixes(t *testing.T) {
	require.ErrorIs(t, resolveStripeAPIMissingResourceError("/v1/customers/cus_123"), ErrStripeAPICustomerNotFound)
	require.ErrorIs(t, resolveStripeAPIMissingResourceError("/v1/checkout/sessions/cs_123"), ErrStripeAPICheckoutSessionNotFound)
	require.ErrorIs(t, resolveStripeAPIMissingResourceError("/v1/prices/price_123"), ErrStripeAPIPriceNotFound)
	require.Nil(t, resolveStripeAPIMissingResourceError("/v1/unknown/resource"))
}

func TestParseStripeAPIError5xxIsTransient(t *testing.T) {
	err := parseStripeAPIError(http.StatusBadGateway, "/v1/prices/price_1", []byte(`{}`))
	require.ErrorIs(t, err, ErrStripeAPITransient)
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

func TestParseStripeAPIErrorMissingEnvelope(t *testing.T) {
	err := parseStripeAPIError(http.StatusBadRequest, "/v1/customers", []byte(`not json`))
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
	require.Contains(t, err.Error(), "unknown")
}

func TestFormatStripeMetadataEmptyKeysValues(t *testing.T) {
	result := formatStripeMetadata(nil)
	require.Empty(t, result)

	result = formatStripeMetadata(map[string]string{})
	require.Empty(t, result)

	result = formatStripeMetadata(map[string]string{"": "value", "key": ""})
	require.Empty(t, result)

	result = formatStripeMetadata(map[string]string{"key": "value"})
	require.Equal(t, map[string]string{"key": "value"}, result)
}

func TestParseStripeUnixTimestampZeroAndNegative(t *testing.T) {
	require.True(t, parseStripeUnixTimestamp(0).IsZero())
	require.True(t, parseStripeUnixTimestamp(-1).IsZero())

	parsed := parseStripeUnixTimestamp(1700000000)
	require.False(t, parsed.IsZero())
	require.Equal(t, int64(1700000000), parsed.Unix())
}

func TestDoFormRequestNilClientErrorPath(t *testing.T) {
	var client *stripeAPIClient
	err := client.doFormRequest(context.Background(), http.MethodGet, "/v1/test", nil, nil)
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

// Coverage gap tests for stripe_api_client.go

func TestStripeResolveCustomerEmailEmpty(t *testing.T) {
	client, err := newStripeAPIClient("sk_test_key", http.DefaultClient)
	require.NoError(t, err)
	_, resolveErr := client.ResolveCustomerEmail(context.Background(), "  ")
	require.ErrorIs(t, resolveErr, ErrStripeAPICustomerNotFound)
}

func TestStripeCreateCheckoutSessionMissingFields(t *testing.T) {
	client, err := newStripeAPIClient("sk_test_key", http.DefaultClient)
	require.NoError(t, err)
	_, sessionErr := client.CreateCheckoutSession(context.Background(), stripeCheckoutSessionInput{
		CustomerID: "",
		PriceID:    "price_123",
		Mode:       "subscription",
		SuccessURL: "https://example.com",
		CancelURL:  "https://example.com",
	})
	require.ErrorIs(t, sessionErr, ErrStripeAPICheckoutSessionNotFound)

	_, sessionErr = client.CreateCheckoutSession(context.Background(), stripeCheckoutSessionInput{
		CustomerID: "cus_123",
		PriceID:    "price_123",
		Mode:       "subscription",
		SuccessURL: "",
		CancelURL:  "https://example.com",
	})
	require.ErrorIs(t, sessionErr, ErrStripeAPICheckoutSessionNotFound)
}

func TestStripeGetCheckoutSessionEmpty(t *testing.T) {
	client, err := newStripeAPIClient("sk_test_key", http.DefaultClient)
	require.NoError(t, err)
	_, getErr := client.GetCheckoutSession(context.Background(), "  ")
	require.ErrorIs(t, getErr, ErrStripeAPICheckoutSessionNotFound)
}

func TestStripeCreateCustomerPortalURLEmpty(t *testing.T) {
	client, err := newStripeAPIClient("sk_test_key", http.DefaultClient)
	require.NoError(t, err)
	_, portalErr := client.CreateCustomerPortalURL(context.Background(), stripePortalSessionInput{
		CustomerID: "",
		ReturnURL:  "https://example.com",
	})
	require.ErrorIs(t, portalErr, ErrStripeAPIPortalURLNotFound)
}

func TestStripeGetPriceEmpty(t *testing.T) {
	client, err := newStripeAPIClient("sk_test_key", http.DefaultClient)
	require.NoError(t, err)
	_, priceErr := client.GetPrice(context.Background(), "  ")
	require.ErrorIs(t, priceErr, ErrStripeAPIPriceNotFound)
}

func TestStripeListSubscriptionsEmptyCustomer(t *testing.T) {
	client, err := newStripeAPIClient("sk_test_key", http.DefaultClient)
	require.NoError(t, err)
	subs, listErr := client.ListSubscriptions(context.Background(), "  ")
	require.NoError(t, listErr)
	require.Empty(t, subs)
}

func TestStripeFindCustomerIDByEmailEmpty(t *testing.T) {
	client, err := newStripeAPIClient("sk_test_key", http.DefaultClient)
	require.NoError(t, err)
	_, findErr := client.findCustomerIDByEmail(context.Background(), "  ")
	require.ErrorIs(t, findErr, ErrBillingUserEmailInvalid)
}

func TestStripeCreateCustomerEmpty(t *testing.T) {
	client, err := newStripeAPIClient("sk_test_key", http.DefaultClient)
	require.NoError(t, err)
	_, createErr := client.createCustomer(context.Background(), "  ")
	require.ErrorIs(t, createErr, ErrBillingUserEmailInvalid)
}

func TestStripeDoFormRequestNilClient(t *testing.T) {
	var client *stripeAPIClient
	err := client.doFormRequest(context.Background(), "GET", "/test", nil, nil)
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

func TestStripeDoFormRequestNilHTTPClient(t *testing.T) {
	client := &stripeAPIClient{
		baseURL:    "https://example.com",
		apiKey:     "key",
		httpClient: nil,
	}
	err := client.doFormRequest(context.Background(), "GET", "/test", nil, nil)
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

func TestStripeDoFormRequestEmptyResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	err := client.doFormRequest(context.Background(), http.MethodGet, "/test", nil, nil)
	require.NoError(t, err)
}

func TestStripeDoFormRequestDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	responsePayload := map[string]interface{}{}
	err := client.doFormRequest(context.Background(), http.MethodGet, "/test", nil, &responsePayload)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

func TestStripeDoFormRequestRateLimitedError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit","code":"rate_limited","message":"slow down"}}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	responsePayload := map[string]interface{}{}
	err := client.doFormRequest(ctx, http.MethodGet, "/test", nil, &responsePayload)
	require.Error(t, err)
}

func TestStripeListSubscriptionsPaginationHasMoreEmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"has_more":true}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.ListSubscriptions(context.Background(), "cus_123")
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

func TestStripeListSubscriptionsPaginationEmptyLastID(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":""}],"has_more":true}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.ListSubscriptions(context.Background(), "cus_123")
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

func TestStripeFindCustomerIDByEmailEmptyID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":""}]}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.findCustomerIDByEmail(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrStripeAPICustomerNotFound)
}

func TestStripeCreateCustomerEmptyID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":""}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.createCustomer(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrStripeAPICustomerNotFound)
}

func TestStripeResolveCustomerEmailEmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"cus_123","email":""}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.ResolveCustomerEmail(context.Background(), "cus_123")
	require.ErrorIs(t, err, ErrStripeAPICustomerNotFound)
}

func TestStripeGetCheckoutSessionEmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":""}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.GetCheckoutSession(context.Background(), "cs_123")
	require.ErrorIs(t, err, ErrStripeAPICheckoutSessionNotFound)
}

func TestStripeCreateCustomerPortalURLEmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"url":""}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.CreateCustomerPortalURL(context.Background(), stripePortalSessionInput{
		CustomerID: "cus_123",
		ReturnURL:  "https://example.com",
	})
	require.ErrorIs(t, err, ErrStripeAPIPortalURLNotFound)
}

func TestStripeGetPriceEmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":""}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.GetPrice(context.Background(), "price_123")
	require.ErrorIs(t, err, ErrStripeAPIPriceNotFound)
}

func TestStripeCreateCheckoutSessionEmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":""}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.CreateCheckoutSession(context.Background(), stripeCheckoutSessionInput{
		CustomerID: "cus_123",
		PriceID:    "price_123",
		Mode:       "subscription",
		SuccessURL: "https://example.com/success",
		CancelURL:  "https://example.com/cancel",
	})
	require.ErrorIs(t, err, ErrStripeAPICheckoutSessionNotFound)
}

func TestStripeResolveCustomerEmailRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"internal error"}}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.ResolveCustomerEmail(context.Background(), "cus_123")
	require.Error(t, err)
}

func TestStripeCreateCheckoutSessionSkipsEmptyMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"cs_test"}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	sessionID, err := client.CreateCheckoutSession(context.Background(), stripeCheckoutSessionInput{
		CustomerID: "cus_123",
		PriceID:    "price_123",
		Mode:       "subscription",
		SuccessURL: "https://example.com/success",
		CancelURL:  "https://example.com/cancel",
		Metadata:   map[string]string{"": "empty_key", "key": ""},
	})
	require.NoError(t, err)
	require.Equal(t, "cs_test", sessionID)
}

func TestStripeListSubscriptionsHasMoreEmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"has_more":true}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.ListSubscriptions(context.Background(), "cus_123")
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

func TestStripeListSubscriptionsHasMoreEmptyLastID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":""}],"has_more":true}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.ListSubscriptions(context.Background(), "cus_123")
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

func TestStripeCreateCustomerEmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":""}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	_, err := client.createCustomer(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrStripeAPICustomerNotFound)
}

func TestStripeDoFormRequestRateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	client := &stripeAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	err := client.doFormRequest(context.Background(), http.MethodPost, "/test", nil, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeAPIRateLimited)
}

func TestStripeGetCheckoutSessionServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"server_error","message":"internal"}}`))
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, err := apiClient.GetCheckoutSession(context.Background(), "cs_test_1")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeAPITransient)
}

func TestStripeCreateCustomerPortalURLServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"server_error","message":"internal"}}`))
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, err := apiClient.CreateCustomerPortalURL(context.Background(), stripePortalSessionInput{
		CustomerID: "cus_123",
		ReturnURL:  "https://app.local/return",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeAPITransient)
}

func TestStripeListSubscriptionsServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"server_error","message":"internal"}}`))
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, err := apiClient.ListSubscriptions(context.Background(), "cus_123")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeAPITransient)
}

func TestStripeCreateCustomerServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"server_error","message":"internal"}}`))
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, err := apiClient.createCustomer(context.Background(), "user@example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeAPITransient)
}

func TestStripeGetCheckoutSessionEmptyIDResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"","status":"open"}`))
	}))
	defer server.Close()

	apiClient, clientErr := newStripeAPIClient("sk_test_123", server.Client())
	require.NoError(t, clientErr)
	apiClient.baseURL = server.URL

	_, err := apiClient.GetCheckoutSession(context.Background(), "cs_test_1")
	require.ErrorIs(t, err, ErrStripeAPICheckoutSessionNotFound)
}
