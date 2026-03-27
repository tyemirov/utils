package billing

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewPaddleAPIClientUsesSandboxBaseURL(t *testing.T) {
	apiClient, err := newPaddleAPIClient("sandbox", "pdl_sdbx_apikey_example", "", nil)

	require.NoError(t, err)
	require.Equal(t, paddleAPIBaseURLSandbox, apiClient.baseURL)
}

func TestNewPaddleAPIClientUsesProductionBaseURL(t *testing.T) {
	apiClient, err := newPaddleAPIClient("production", "pdl_live_apikey_example", "", nil)

	require.NoError(t, err)
	require.Equal(t, paddleAPIBaseURLProduction, apiClient.baseURL)
}

func TestNewPaddleAPIClientRejectsUnsupportedEnvironment(t *testing.T) {
	apiClient, err := newPaddleAPIClient("staging", "pdl_sdbx_apikey_example", "", nil)

	require.Nil(t, apiClient)
	require.ErrorIs(t, err, ErrPaddleAPIEnvironmentInvalid)
}

func TestNewPaddleAPIClientIgnoresEnvironmentProxy(t *testing.T) {
	var proxyRequestCount atomic.Int64
	proxyServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		proxyRequestCount.Add(1)
		responseWriter.WriteHeader(http.StatusBadGateway)
	}))
	defer proxyServer.Close()

	targetServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/prices/pri_test", request.URL.Path)
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(
			`{"data":{"id":"pri_test","product_id":"pro_test","name":"Pro","unit_price":{"amount":"2700","currency_code":"USD"},"product":{"id":"pro_test","name":"Pro"},"billing_cycle":{"interval":"month","frequency":1}}}`,
		))
	}))
	defer targetServer.Close()

	t.Setenv("HTTP_PROXY", proxyServer.URL)
	t.Setenv("HTTPS_PROXY", proxyServer.URL)
	t.Setenv("NO_PROXY", "")

	apiClient, err := newPaddleAPIClient("sandbox", "pdl_sdbx_apikey_example", "", nil)
	require.NoError(t, err)
	apiClient.baseURL = targetServer.URL

	_, getPriceErr := apiClient.GetPrice(context.Background(), "pri_test")
	require.NoError(t, getPriceErr)
	require.EqualValues(t, 0, proxyRequestCount.Load())
}

func TestParsePaddleErrorMapsDefaultCheckoutURLNotSet(t *testing.T) {
	err := parsePaddleError(http.StatusBadRequest, []byte(
		`{"error":{"code":"transaction_default_checkout_url_not_set","detail":"configure default checkout URL"}}`,
	))

	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.ErrorIs(t, err, ErrPaddleAPIDefaultCheckoutURL)
	require.Contains(t, err.Error(), "transaction_default_checkout_url_not_set")
}

func TestParsePaddleErrorMapsPriceNotFound(t *testing.T) {
	err := parsePaddleError(http.StatusNotFound, []byte(
		`{"error":{"code":"not_found","detail":"Price pri_123 not found."}}`,
	))

	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.ErrorIs(t, err, ErrPaddleAPIPriceNotFound)
	require.Contains(t, err.Error(), "Price pri_123 not found.")
}

func TestParsePaddleErrorMapsTransactionNotFound(t *testing.T) {
	err := parsePaddleError(http.StatusNotFound, []byte(
		`{"error":{"code":"not_found","detail":"Transaction txn_123 not found."}}`,
	))

	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.ErrorIs(t, err, ErrPaddleAPITransactionNotFound)
	require.Contains(t, err.Error(), "Transaction txn_123 not found.")
}

func TestParsePaddleErrorMapsSubscriptionNotFound(t *testing.T) {
	err := parsePaddleError(http.StatusNotFound, []byte(
		`{"error":{"code":"not_found","detail":"Subscription sub_123 not found."}}`,
	))

	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.ErrorIs(t, err, ErrPaddleAPISubscriptionNotFound)
	require.Contains(t, err.Error(), "Subscription sub_123 not found.")
}

func TestParsePaddleErrorMapsRateLimitAsTransient(t *testing.T) {
	err := parsePaddleError(http.StatusTooManyRequests, []byte(
		`{"error":{"code":"too_many_requests","detail":"Rate limit reached"}}`,
	))

	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.ErrorIs(t, err, ErrPaddleAPITransient)
	require.ErrorIs(t, err, ErrPaddleAPIRateLimited)
}

func TestPaddleAPIClientSendsUserAgentHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, paddleUserAgentPoodleScanner, request.Header.Get(paddleHeaderUserAgent))
		require.Equal(t, paddleAPIVersion, request.Header.Get(paddleHeaderAPIVersion))
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(
			`{"data":{"id":"pri_test","product_id":"pro_test","name":"Pro","unit_price":{"amount":"2700","currency_code":"USD"},"product":{"id":"pro_test","name":"Pro"},"billing_cycle":{"interval":"month","frequency":1}}}`,
		))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.GetPrice(context.Background(), "pri_test")
	require.NoError(t, err)
}

func TestPaddleAPIClientFindCustomerIDByEmailReturnsEmptyWhenNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/customers", request.URL.Path)
		require.Equal(t, "user@example.com", request.URL.Query().Get("email"))
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	customerID, err := apiClient.FindCustomerIDByEmail(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.Equal(t, "", customerID)
}

func TestPaddleAPIClientListCustomerTransactionsIncludesCustomerFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/transactions", request.URL.Path)
		require.Equal(t, "ctm_123", request.URL.Query().Get("customer_id"))
		require.Equal(t, "100", request.URL.Query().Get("per_page"))
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":[{"id":"txn_123","status":"completed"}]}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	transactions, err := apiClient.ListCustomerTransactions(context.Background(), "ctm_123")
	require.NoError(t, err)
	require.Len(t, transactions, 1)
	require.Equal(t, "txn_123", transactions[0].ID)
}

func TestPaddleAPIClientListCustomerTransactionsIncludesAllPages(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/transactions", request.URL.Path)
		require.Equal(t, "ctm_123", request.URL.Query().Get("customer_id"))
		require.Equal(t, "100", request.URL.Query().Get("per_page"))
		afterCursor := request.URL.Query().Get("after")
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		if afterCursor == "" {
			_, _ = responseWriter.Write([]byte(`{"data":[{"id":"txn_001","status":"completed"}],"meta":{"pagination":{"next":"https://api.paddle.com/transactions?customer_id=ctm_123&per_page=100&after=txn_001","has_more":true}}}`))
			requestCount.Add(1)
			return
		}
		require.Equal(t, "txn_001", afterCursor)
		_, _ = responseWriter.Write([]byte(`{"data":[{"id":"txn_002","status":"completed"}],"meta":{"pagination":{"next":"","has_more":false}}}`))
		requestCount.Add(1)
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	transactions, err := apiClient.ListCustomerTransactions(context.Background(), "ctm_123")
	require.NoError(t, err)
	require.Len(t, transactions, 2)
	require.Equal(t, "txn_001", transactions[0].ID)
	require.Equal(t, "txn_002", transactions[1].ID)
	require.EqualValues(t, 2, requestCount.Load())
}

func TestPaddleAPIClientListCustomerTransactionsRejectsInvalidPaginationNextPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":[{"id":"txn_001","status":"completed"}],"meta":{"pagination":{"next":"?after=txn_001","has_more":true}}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.ListCustomerTransactions(context.Background(), "ctm_123")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIPaginationInvalid)
}

func TestPaddleAPIClientListCustomerSubscriptionsIncludesCustomerFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/subscriptions", request.URL.Path)
		require.Equal(t, "ctm_123", request.URL.Query().Get("customer_id"))
		require.Equal(t, "100", request.URL.Query().Get("per_page"))
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":[{"id":"sub_123","status":"active"}]}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	subscriptions, err := apiClient.ListCustomerSubscriptions(context.Background(), "ctm_123")
	require.NoError(t, err)
	require.Len(t, subscriptions, 1)
	require.Equal(t, "sub_123", subscriptions[0].ID)
}

func TestPaddleAPIClientListCustomerSubscriptionsIncludesAllPages(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/subscriptions", request.URL.Path)
		require.Equal(t, "ctm_123", request.URL.Query().Get("customer_id"))
		require.Equal(t, "100", request.URL.Query().Get("per_page"))
		afterCursor := request.URL.Query().Get("after")
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		if afterCursor == "" {
			_, _ = responseWriter.Write([]byte(`{"data":[{"id":"sub_001","status":"active"}],"meta":{"pagination":{"next":"https://api.paddle.com/subscriptions?customer_id=ctm_123&per_page=100&after=sub_001","has_more":true}}}`))
			requestCount.Add(1)
			return
		}
		require.Equal(t, "sub_001", afterCursor)
		_, _ = responseWriter.Write([]byte(`{"data":[{"id":"sub_002","status":"canceled"}],"meta":{"pagination":{"next":"","has_more":false}}}`))
		requestCount.Add(1)
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	subscriptions, err := apiClient.ListCustomerSubscriptions(context.Background(), "ctm_123")
	require.NoError(t, err)
	require.Len(t, subscriptions, 2)
	require.Equal(t, "sub_001", subscriptions[0].ID)
	require.Equal(t, "sub_002", subscriptions[1].ID)
	require.EqualValues(t, 2, requestCount.Load())
}

func TestPaddleAPIClientListCustomerTransactionsIgnoresNextPathWhenHasMoreIsFalse(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		require.Equal(t, "/transactions", request.URL.Path)
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":[{"id":"txn_001","status":"completed"}],"meta":{"pagination":{"next":"https://api.paddle.com/transactions?customer_id=ctm_123&per_page=100&after=txn_001","has_more":false}}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	transactions, err := apiClient.ListCustomerTransactions(context.Background(), "ctm_123")
	require.NoError(t, err)
	require.Len(t, transactions, 1)
	require.Equal(t, "txn_001", transactions[0].ID)
	require.EqualValues(t, 1, requestCount.Load())
}

func TestPaddleAPIClientListCustomerTransactionsRejectsEmptyCustomerID(t *testing.T) {
	apiClient := &paddleAPIClient{
		baseURL:    "https://sandbox-api.paddle.com",
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: &http.Client{},
	}

	_, err := apiClient.ListCustomerTransactions(context.Background(), " ")
	require.ErrorIs(t, err, ErrPaddleAPICustomerNotFound)
}

func TestPaddleAPIClientListCustomerSubscriptionsRejectsEmptyCustomerID(t *testing.T) {
	apiClient := &paddleAPIClient{
		baseURL:    "https://sandbox-api.paddle.com",
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: &http.Client{},
	}

	_, err := apiClient.ListCustomerSubscriptions(context.Background(), "")
	require.ErrorIs(t, err, ErrPaddleAPICustomerNotFound)
}

func TestPaddleAPIClientGetPriceRetriesRateLimitedResponse(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		if requestCount.Load() == 1 {
			responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
			responseWriter.Header().Set(paddleHeaderRetryAfter, "0")
			responseWriter.WriteHeader(http.StatusTooManyRequests)
			_, _ = responseWriter.Write([]byte(`{"error":{"code":"too_many_requests","detail":"Rate limit reached"}}`))
			return
		}
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(
			`{"data":{"id":"pri_test","product_id":"pro_test","name":"Pro","unit_price":{"amount":"2700","currency_code":"USD"},"product":{"id":"pro_test","name":"Pro"},"billing_cycle":{"interval":"month","frequency":1}}}`,
		))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	priceDetails, err := apiClient.GetPrice(context.Background(), "pri_test")
	require.NoError(t, err)
	require.Equal(t, "pri_test", priceDetails.ID)
	require.EqualValues(t, 2700, priceDetails.PriceCents)
	require.EqualValues(t, 2, requestCount.Load())
}

func TestPaddleAPIClientGetPriceRateLimitedRetryWaitDeadlineIsTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		responseWriter.Header().Set(paddleHeaderRetryAfter, "3600")
		responseWriter.WriteHeader(http.StatusTooManyRequests)
		_, _ = responseWriter.Write([]byte(`{"error":{"code":"too_many_requests","detail":"Rate limit reached"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}
	requestContext, cancelRequestContext := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelRequestContext()

	_, err := apiClient.GetPrice(requestContext, "pri_test")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.ErrorIs(t, err, ErrPaddleAPITransient)
	require.ErrorIs(t, err, ErrPaddleAPIRateLimited)
}

func TestPaddleAPIClientListPricesUsesIDFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/prices", request.URL.Path)
		queryValues := request.URL.Query()
		require.Equal(t, "pri_one,pri_two", queryValues.Get("id"))
		require.Equal(t, "2", queryValues.Get("per_page"))
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(
			`{"data":[` +
				`{"id":"pri_one","product_id":"pro_1","name":"Pro","unit_price":{"amount":"2700","currency_code":"USD"},"product":{"id":"pro_1","name":"Pro"},"billing_cycle":{"interval":"month","frequency":1}},` +
				`{"id":"pri_two","product_id":"pro_2","name":"Pack","unit_price":{"amount":"1000","currency_code":"USD"},"product":{"id":"pro_2","name":"Pack"},"billing_cycle":null}` +
				`]}`,
		))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	priceDetailsByID, err := apiClient.ListPrices(context.Background(), []string{"pri_one", "pri_two"})
	require.NoError(t, err)
	require.Len(t, priceDetailsByID, 2)
	require.EqualValues(t, 2700, priceDetailsByID["pri_one"].PriceCents)
	require.Equal(t, "month", priceDetailsByID["pri_one"].BillingCycle.Interval)
	require.EqualValues(t, 1, priceDetailsByID["pri_one"].BillingCycle.Frequency)
	require.EqualValues(t, 1000, priceDetailsByID["pri_two"].PriceCents)
	require.Equal(t, "", priceDetailsByID["pri_two"].BillingCycle.Interval)
	require.EqualValues(t, 0, priceDetailsByID["pri_two"].BillingCycle.Frequency)
}

func TestPaddleAPIClientGetPriceFailsAfterRateLimitRetryExhausted(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		responseWriter.Header().Set(paddleHeaderRetryAfter, "0")
		responseWriter.WriteHeader(http.StatusTooManyRequests)
		_, _ = responseWriter.Write([]byte(`{"error":{"code":"too_many_requests","detail":"Rate limit reached"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.GetPrice(context.Background(), "pri_test")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.Contains(t, err.Error(), "too_many_requests")
	require.EqualValues(t, paddleAPIRequestMaxAttempts, requestCount.Load())
}

func TestPaddleAPIClientCreateTransactionDoesNotRetryRateLimitedResponse(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		require.Equal(t, http.MethodPost, request.Method)
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		responseWriter.WriteHeader(http.StatusTooManyRequests)
		_, _ = responseWriter.Write([]byte(`{"error":{"code":"too_many_requests","detail":"Rate limit reached"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.CreateTransaction(context.Background(), paddleTransactionInput{
		CustomerID: "ctm_123",
		PriceID:    "pri_test",
		Metadata: map[string]string{
			"product_scanner_user_email": "user@example.com",
		},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPITransient)
	require.ErrorIs(t, err, ErrPaddleAPIRateLimited)
	require.EqualValues(t, 1, requestCount.Load())
}

func TestPaddleAPIClientResolveCustomerIDFindsExisting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		if request.URL.Path == "/customers" && request.Method == http.MethodGet {
			_, _ = responseWriter.Write([]byte(`{"data":[{"id":"ctm_existing"}]}`))
			return
		}
		responseWriter.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	customerID, err := apiClient.ResolveCustomerID(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.Equal(t, "ctm_existing", customerID)
}

func TestPaddleAPIClientResolveCustomerIDCreatesNew(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		if request.URL.Path == "/customers" && request.Method == http.MethodGet {
			_, _ = responseWriter.Write([]byte(`{"data":[]}`))
			return
		}
		if request.URL.Path == "/customers" && request.Method == http.MethodPost {
			_, _ = responseWriter.Write([]byte(`{"data":{"id":"ctm_new"}}`))
			return
		}
		responseWriter.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	customerID, err := apiClient.ResolveCustomerID(context.Background(), "new@example.com")
	require.NoError(t, err)
	require.Equal(t, "ctm_new", customerID)
}

func TestPaddleAPIClientResolveCustomerIDHandlesAlreadyExists(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		if request.URL.Path == "/customers" && request.Method == http.MethodGet {
			callCount++
			if callCount == 1 {
				_, _ = responseWriter.Write([]byte(`{"data":[]}`))
				return
			}
			_, _ = responseWriter.Write([]byte(`{"data":[{"id":"ctm_race"}]}`))
			return
		}
		if request.URL.Path == "/customers" && request.Method == http.MethodPost {
			responseWriter.WriteHeader(http.StatusConflict)
			_, _ = responseWriter.Write([]byte(`{"error":{"code":"customer_already_exists","detail":"Customer already exists"}}`))
			return
		}
		responseWriter.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	customerID, err := apiClient.ResolveCustomerID(context.Background(), "race@example.com")
	require.NoError(t, err)
	require.Equal(t, "ctm_race", customerID)
}

func TestPaddleAPIClientGetTransactionReturnsData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/transactions/txn_test_1", request.URL.Path)
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"id":"txn_test_1","status":"completed"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	transaction, err := apiClient.GetTransaction(context.Background(), "txn_test_1")
	require.NoError(t, err)
	require.Equal(t, "txn_test_1", transaction.ID)
	require.Equal(t, "completed", transaction.Status)
}

func TestPaddleAPIClientGetTransactionRejectsEmptyID(t *testing.T) {
	apiClient := &paddleAPIClient{
		baseURL:    "https://sandbox-api.paddle.com",
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: &http.Client{},
	}

	_, err := apiClient.GetTransaction(context.Background(), "  ")
	require.ErrorIs(t, err, ErrPaddleAPITransactionNotFound)
}

func TestPaddleAPIClientResolveCustomerEmailReturnsEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/customers/ctm_test_1", request.URL.Path)
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"id":"ctm_test_1","email":"found@example.com"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	email, err := apiClient.ResolveCustomerEmail(context.Background(), "ctm_test_1")
	require.NoError(t, err)
	require.Equal(t, "found@example.com", email)
}

func TestPaddleAPIClientResolveCustomerEmailRejectsEmptyID(t *testing.T) {
	apiClient := &paddleAPIClient{
		baseURL:    "https://sandbox-api.paddle.com",
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: &http.Client{},
	}

	_, err := apiClient.ResolveCustomerEmail(context.Background(), "  ")
	require.ErrorIs(t, err, ErrPaddleAPICustomerNotFound)
}

func TestPaddleAPIClientGetSubscriptionReturnsData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/subscriptions/sub_test_1", request.URL.Path)
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"id":"sub_test_1","status":"active"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	subscription, err := apiClient.GetSubscription(context.Background(), "sub_test_1")
	require.NoError(t, err)
	require.Equal(t, "sub_test_1", subscription.ID)
	require.Equal(t, "active", subscription.Status)
}

func TestPaddleAPIClientGetSubscriptionRejectsEmptyID(t *testing.T) {
	apiClient := &paddleAPIClient{
		baseURL:    "https://sandbox-api.paddle.com",
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: &http.Client{},
	}

	_, err := apiClient.GetSubscription(context.Background(), "")
	require.ErrorIs(t, err, ErrPaddleAPISubscriptionNotFound)
}

func TestPaddleAPIClientCreateCustomerPortalURLReturnsURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/customers/ctm_test_1/portal-sessions", request.URL.Path)
		require.Equal(t, http.MethodPost, request.Method)
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"urls":{"general":{"overview":"https://portal.paddle.com/session_abc"}}}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	portalURL, err := apiClient.CreateCustomerPortalURL(context.Background(), "ctm_test_1")
	require.NoError(t, err)
	require.Equal(t, "https://portal.paddle.com/session_abc", portalURL)
}

func TestPaddleAPIClientCreateCustomerPortalURLRejectsEmptyURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"urls":{"general":{"overview":""}}}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.CreateCustomerPortalURL(context.Background(), "ctm_test_1")
	require.ErrorIs(t, err, ErrPaddleAPIPortalURLNotFound)
}

func TestPaddleAPIClientCreateCustomerReturnsID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/customers", request.URL.Path)
		require.Equal(t, http.MethodPost, request.Method)
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"id":"ctm_created"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	customerID, err := apiClient.createCustomer(context.Background(), "new@example.com")
	require.NoError(t, err)
	require.Equal(t, "ctm_created", customerID)
}

func TestPaddleAPIClientCreateCustomerRejectsEmptyEmail(t *testing.T) {
	apiClient := &paddleAPIClient{
		baseURL:    "https://sandbox-api.paddle.com",
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: &http.Client{},
	}

	_, err := apiClient.createCustomer(context.Background(), "  ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestResolvePaddleAPIBaseURLWithOverride(t *testing.T) {
	baseURL, err := resolvePaddleAPIBaseURL("sandbox", "https://custom-api.paddle.com")
	require.NoError(t, err)
	require.Equal(t, "https://custom-api.paddle.com", baseURL)
}

func TestParsePaddleError5xxServerErrors(t *testing.T) {
	err := parsePaddleError(http.StatusInternalServerError, []byte(
		`{"error":{"code":"internal_error","detail":"Something went wrong"}}`,
	))
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.ErrorIs(t, err, ErrPaddleAPITransient)
}

func TestParsePaddleErrorUnknownErrorCodes(t *testing.T) {
	err := parsePaddleError(http.StatusBadRequest, []byte(
		`{"error":{"code":"some_unknown_error","detail":"Something unexpected"}}`,
	))
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.Contains(t, err.Error(), "some_unknown_error")
}

func TestParsePaddleError5xxWithUnparsableBody(t *testing.T) {
	err := parsePaddleError(http.StatusBadGateway, []byte(`not json`))
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.ErrorIs(t, err, ErrPaddleAPITransient)
}

func TestParsePaddleErrorRateLimitWithUnparsableBody(t *testing.T) {
	err := parsePaddleError(http.StatusTooManyRequests, []byte(`not json`))
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.ErrorIs(t, err, ErrPaddleAPITransient)
	require.ErrorIs(t, err, ErrPaddleAPIRateLimited)
}

func TestParsePaddleError4xxWithUnparsableBody(t *testing.T) {
	err := parsePaddleError(http.StatusBadRequest, []byte(`not json`))
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.False(t, errors.Is(err, ErrPaddleAPITransient))
}

func TestNewPaddleAPIClientRejectsEmptyAPIKey(t *testing.T) {
	_, err := newPaddleAPIClient("sandbox", "  ", "", nil)
	require.ErrorIs(t, err, ErrPaddleAPIKeyEmpty)
}

func TestNewPaddleAPIClientRejectsInvalidBaseURL(t *testing.T) {
	_, err := newPaddleAPIClient("sandbox", "pdl_key", "://invalid", nil)
	require.ErrorIs(t, err, ErrPaddleAPIBaseURLInvalid)
}

func TestPaddleAPIClientCreateTransactionReturnsID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/transactions", request.URL.Path)
		require.Equal(t, http.MethodPost, request.Method)
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"id":"txn_created"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	txnID, err := apiClient.CreateTransaction(context.Background(), paddleTransactionInput{
		CustomerID: "ctm_123",
		PriceID:    "pri_test",
		Metadata:   map[string]string{"key": "value"},
	})
	require.NoError(t, err)
	require.Equal(t, "txn_created", txnID)
}

func TestPaddleAPIClientListPricesRejectsEmptyPriceIDs(t *testing.T) {
	apiClient := &paddleAPIClient{
		baseURL:    "https://sandbox-api.paddle.com",
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: &http.Client{},
	}

	_, err := apiClient.ListPrices(context.Background(), []string{})
	require.ErrorIs(t, err, ErrPaddleAPIPriceNotFound)
}

func TestPaddleAPIClientGetPriceRejectsEmptyID(t *testing.T) {
	apiClient := &paddleAPIClient{
		baseURL:    "https://sandbox-api.paddle.com",
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: &http.Client{},
	}

	_, err := apiClient.GetPrice(context.Background(), "  ")
	require.ErrorIs(t, err, ErrPaddleAPIPriceNotFound)
}

func TestPaddleAPIClientDoJSONRequestNilClient(t *testing.T) {
	var apiClient *paddleAPIClient
	err := apiClient.doJSONRequest(context.Background(), http.MethodGet, "/test", nil, nil)
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)
}

func TestPaddleAPIClientFindCustomerIDByEmailRejectsEmptyEmail(t *testing.T) {
	apiClient := &paddleAPIClient{
		baseURL:    "https://sandbox-api.paddle.com",
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: &http.Client{},
	}

	_, err := apiClient.findCustomerIDByEmail(context.Background(), "  ")
	require.ErrorIs(t, err, ErrBillingUserEmailInvalid)
}

func TestPaddleAPIClientGetTransactionEmptyResponseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"id":"","status":"completed"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.GetTransaction(context.Background(), "txn_test")
	require.ErrorIs(t, err, ErrPaddleAPITransactionNotFound)
}

func TestPaddleAPIClientGetSubscriptionEmptyResponseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"id":"","status":"active"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.GetSubscription(context.Background(), "sub_test")
	require.ErrorIs(t, err, ErrPaddleAPISubscriptionNotFound)
}

func TestPaddleAPIClientResolveCustomerEmailEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"id":"ctm_test","email":""}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.ResolveCustomerEmail(context.Background(), "ctm_test")
	require.ErrorIs(t, err, ErrPaddleAPICustomerNotFound)
}

func TestPaddleAPIClientCreateTransactionEmptyResponseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"id":""}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.CreateTransaction(context.Background(), paddleTransactionInput{
		CustomerID: "ctm_123",
		PriceID:    "pri_test",
	})
	require.ErrorIs(t, err, ErrPaddleAPITransactionNotFound)
}

func TestPaddleAPIClientCreateCustomerEmptyResponseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"id":""}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.createCustomer(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrPaddleAPICustomerNotFound)
}

func TestPaddleAPIClientFindCustomerIDByEmailEmptyResponseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":[{"id":""}]}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.findCustomerIDByEmail(context.Background(), "user@example.com")
	require.ErrorIs(t, err, ErrPaddleAPICustomerNotFound)
}

func TestPaddleAPIClientGetPriceEmptyResponseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"id":"","product_id":"pro_test"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.GetPrice(context.Background(), "pri_test")
	require.ErrorIs(t, err, ErrPaddleAPIPriceNotFound)
}

func TestPaddleAPIClientResolveCustomerIDFindError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		_, _ = responseWriter.Write([]byte(`{"error":{"code":"internal_error","detail":"Server error"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.ResolveCustomerID(context.Background(), "user@example.com")
	require.Error(t, err)
}

func TestPaddleAPIClientResolveCustomerIDCreateFailsNonAlreadyExists(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		callCount++
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		if request.Method == http.MethodGet {
			_, _ = responseWriter.Write([]byte(`{"data":[]}`))
			return
		}
		if request.Method == http.MethodPost {
			responseWriter.WriteHeader(http.StatusBadRequest)
			_, _ = responseWriter.Write([]byte(`{"error":{"code":"validation_error","detail":"Invalid email"}}`))
			return
		}
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.ResolveCustomerID(context.Background(), "user@example.com")
	require.Error(t, err)
}

func TestPaddleAPIClientListPricesWithEmptyPriceIDInList(t *testing.T) {
	apiClient := &paddleAPIClient{
		baseURL:    "https://sandbox-api.paddle.com",
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: &http.Client{},
	}

	_, err := apiClient.ListPrices(context.Background(), []string{"pri_one", "  "})
	require.ErrorIs(t, err, ErrPaddleAPIPriceNotFound)
}

func TestPaddleAPIClientDoJSONRequestNilHTTPClient(t *testing.T) {
	apiClient := &paddleAPIClient{
		baseURL: "https://sandbox-api.paddle.com",
		apiKey:  "pdl_sdbx_apikey_example",
	}
	err := apiClient.doJSONRequest(context.Background(), http.MethodGet, "/test", nil, nil)
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)
}

func TestPaddleAPIClientDoJSONRequestBadResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		responseWriter.WriteHeader(http.StatusBadRequest)
		_, _ = responseWriter.Write([]byte(`{"error":{"code":"bad_request","detail":"Invalid request"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	err := apiClient.doJSONRequest(context.Background(), http.MethodGet, "/test", nil, nil)
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
}

func TestPaddleAPIClientDoJSONRequestInvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`not-json`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	var result paddleGetCustomerResponse
	err := apiClient.doJSONRequest(context.Background(), http.MethodGet, "/test", nil, &result)
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
}

func TestParsePaddleErrorEmptyCodeAndDetail(t *testing.T) {
	err := parsePaddleError(http.StatusBadRequest, []byte(
		`{"error":{"code":"","detail":""}}`,
	))
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.Contains(t, err.Error(), "unknown")
}

func TestPaddleAPIClientResolveCustomerIDCreateFailsRetryFindEmpty(t *testing.T) {
	// Test the case where create fails with customer_already_exists but the retry find still returns empty
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		if request.Method == http.MethodGet {
			callCount++
			// Both first and second find return empty
			_, _ = responseWriter.Write([]byte(`{"data":[]}`))
			return
		}
		if request.Method == http.MethodPost {
			responseWriter.WriteHeader(http.StatusConflict)
			_, _ = responseWriter.Write([]byte(`{"error":{"code":"customer_already_exists","detail":"Customer already exists"}}`))
			return
		}
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.ResolveCustomerID(context.Background(), "race@example.com")
	require.Error(t, err)
	require.True(t, callCount >= 2) // At least 2 GET calls
}

func TestPaddleAPIClientGetTransactionAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		responseWriter.WriteHeader(http.StatusNotFound)
		_, _ = responseWriter.Write([]byte(`{"error":{"code":"not_found","detail":"Transaction txn_xxx not found."}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.GetTransaction(context.Background(), "txn_xxx")
	require.ErrorIs(t, err, ErrPaddleAPITransactionNotFound)
}

func TestPaddleAPIClientResolveCustomerEmailAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		responseWriter.WriteHeader(http.StatusInternalServerError)
		_, _ = responseWriter.Write([]byte(`{"error":{"code":"internal_error","detail":"Server error"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.ResolveCustomerEmail(context.Background(), "ctm_xxx")
	require.Error(t, err)
}

func TestPaddleAPIClientGetSubscriptionAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		responseWriter.WriteHeader(http.StatusNotFound)
		_, _ = responseWriter.Write([]byte(`{"error":{"code":"not_found","detail":"Subscription sub_xxx not found."}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.GetSubscription(context.Background(), "sub_xxx")
	require.ErrorIs(t, err, ErrPaddleAPISubscriptionNotFound)
}

func TestPaddleAPIClientCreateCustomerPortalURLAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		responseWriter.WriteHeader(http.StatusInternalServerError)
		_, _ = responseWriter.Write([]byte(`{"error":{"code":"internal_error","detail":"Server error"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.CreateCustomerPortalURL(context.Background(), "ctm_xxx")
	require.Error(t, err)
}

func TestPaddleAPIClientListPricesResolvePriceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		// Return price with empty ID which causes resolvePaddlePriceDetails to fail
		_, _ = responseWriter.Write([]byte(`{"data":[{"id":"","product_id":"pro_1"}]}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.ListPrices(context.Background(), []string{"pri_one"})
	require.Error(t, err)
}

func TestPaddleAPIClientListCustomerTransactionsDuplicatePage(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		// Always return the same next page to trigger duplicate detection
		_, _ = responseWriter.Write([]byte(`{"data":[{"id":"txn_001","status":"completed"}],"meta":{"pagination":{"next":"/transactions?customer_id=ctm_123&per_page=100","has_more":true}}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.ListCustomerTransactions(context.Background(), "ctm_123")
	require.ErrorIs(t, err, ErrPaddleAPIPaginationInvalid)
}

func TestPaddleAPIClientListCustomerTransactionsHasMoreEmptyNext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":[{"id":"txn_001"}],"meta":{"pagination":{"next":"","has_more":true}}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.ListCustomerTransactions(context.Background(), "ctm_123")
	require.ErrorIs(t, err, ErrPaddleAPIPaginationInvalid)
}

func TestPaddleAPIClientListCustomerTransactionsRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		responseWriter.WriteHeader(http.StatusInternalServerError)
		_, _ = responseWriter.Write([]byte(`{"error":{"code":"internal_error","detail":"Server error"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.ListCustomerTransactions(context.Background(), "ctm_123")
	require.Error(t, err)
}

func TestResolvePaddlePriceDetailsWithUnitPriceParseError(t *testing.T) {
	_, err := resolvePaddlePriceDetails(paddlePriceAPIModel{
		ID: "pri_test",
		UnitPrice: &struct {
			Amount string `json:"amount"`
		}{Amount: "not_a_number"},
	})
	require.ErrorIs(t, err, ErrPaddleAPIPriceNotFound)
}

func TestResolvePaddleListNextPathWithFullURL(t *testing.T) {
	path, err := resolvePaddleListNextPath(paddleListPagination{
		HasMore: true,
		Next:    "https://api.paddle.com/transactions?after=txn_001",
	})
	require.NoError(t, err)
	require.Equal(t, "/transactions?after=txn_001", path)
}

func TestResolvePaddleListNextPathWithRelativePath(t *testing.T) {
	path, err := resolvePaddleListNextPath(paddleListPagination{
		HasMore: true,
		Next:    "transactions?after=txn_001",
	})
	require.NoError(t, err)
	require.Equal(t, "/transactions?after=txn_001", path)
}

func TestPaddleAPIClientDoJSONRequestWithPostBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, paddleContentTypeApplicationJSON, request.Header.Get(paddleHeaderContentType))
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(`{"data":{"id":"test"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	var result paddleCreateCustomerResponse
	err := apiClient.doJSONRequest(context.Background(), http.MethodPost, "/customers", map[string]string{"email": "test@example.com"}, &result)
	require.NoError(t, err)
	require.Equal(t, "test", result.Data.ID)
}

func TestPaddleAPIClientDoJSONRequestEmptyResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	err := apiClient.doJSONRequest(context.Background(), http.MethodDelete, "/test", nil, nil)
	require.NoError(t, err)
}

func TestParsePaddleErrorWithMessageFallback(t *testing.T) {
	err := parsePaddleError(http.StatusBadRequest, []byte(
		`{"error":{"code":"validation_error","message":"Validation failed"}}`,
	))
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.Contains(t, err.Error(), "Validation failed")
}

func TestResolvePaddleListNextPathNoQuery(t *testing.T) {
	path, err := resolvePaddleListNextPath(paddleListPagination{
		HasMore: true,
		Next:    "/transactions",
	})
	require.NoError(t, err)
	require.Equal(t, "/transactions", path)
}

func TestPaddleAPIClientResolveCustomerIDRetryFindError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		if request.Method == http.MethodGet {
			callCount++
			if callCount == 1 {
				_, _ = responseWriter.Write([]byte(`{"data":[]}`))
				return
			}
			// Second find fails
			responseWriter.WriteHeader(http.StatusInternalServerError)
			_, _ = responseWriter.Write([]byte(`{"error":{"code":"internal_error","detail":"Server error"}}`))
			return
		}
		if request.Method == http.MethodPost {
			responseWriter.WriteHeader(http.StatusConflict)
			_, _ = responseWriter.Write([]byte(`{"error":{"code":"customer_already_exists","detail":"Customer already exists"}}`))
			return
		}
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.ResolveCustomerID(context.Background(), "user@example.com")
	require.Error(t, err)
}

func TestPaddleAPIClientListPricesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		responseWriter.WriteHeader(http.StatusInternalServerError)
		_, _ = responseWriter.Write([]byte(`{"error":{"code":"internal_error","detail":"Server error"}}`))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	_, err := apiClient.ListPrices(context.Background(), []string{"pri_one"})
	require.Error(t, err)
}

func TestPaddleAPIClientListPricesDeduplicatesIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/prices", request.URL.Path)
		queryValues := request.URL.Query()
		require.Equal(t, "pri_one", queryValues.Get("id"))
		require.Equal(t, "1", queryValues.Get("per_page"))
		responseWriter.Header().Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
		_, _ = responseWriter.Write([]byte(
			`{"data":[{"id":"pri_one","product_id":"pro_1","name":"Pro","unit_price":{"amount":"2700","currency_code":"USD"},"product":{"id":"pro_1","name":"Pro"},"billing_cycle":{"interval":"month","frequency":1}}]}`,
		))
	}))
	defer server.Close()

	apiClient := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "pdl_sdbx_apikey_example",
		httpClient: server.Client(),
	}

	priceDetailsByID, err := apiClient.ListPrices(context.Background(), []string{"pri_one", "pri_one"})
	require.NoError(t, err)
	require.Len(t, priceDetailsByID, 1)
}

// Coverage gap tests for paddle_api_client.go

func TestResolvePaddleListNextPathEmptyPathAfterParse(t *testing.T) {
	_, err := resolvePaddleListNextPath(paddleListPagination{
		HasMore: true,
		Next:    "?query=only",
	})
	require.ErrorIs(t, err, ErrPaddleAPIPaginationInvalid)
}

func TestResolvePaddleListNextPathNoPrefixSlash(t *testing.T) {
	path, err := resolvePaddleListNextPath(paddleListPagination{
		HasMore: true,
		Next:    "subscriptions?cursor=abc",
	})
	require.NoError(t, err)
	require.Equal(t, "/subscriptions?cursor=abc", path)
}

func TestDoJSONRequestNilClient(t *testing.T) {
	var client *paddleAPIClient
	err := client.doJSONRequest(context.Background(), "GET", "/test", nil, nil)
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)
}

func TestDoJSONRequestNilHTTPClient(t *testing.T) {
	client := &paddleAPIClient{
		baseURL:    "https://example.com",
		apiKey:     "key",
		httpClient: nil,
	}
	err := client.doJSONRequest(context.Background(), "GET", "/test", nil, nil)
	require.ErrorIs(t, err, ErrPaddleProviderClientUnavailable)
}

func TestDoJSONRequestRateLimitedWaitError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","detail":"slow down"}}`))
	}))
	defer server.Close()

	client := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	responsePayload := map[string]interface{}{}
	err := client.doJSONRequest(ctx, http.MethodGet, "/test", nil, &responsePayload)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrPaddleAPITransient) || errors.Is(err, ErrPaddleAPIRateLimited) || errors.Is(err, ErrPaddleAPIRequestFailed))
}

func TestDoJSONRequestEmptyResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	err := client.doJSONRequest(context.Background(), http.MethodGet, "/test", nil, nil)
	require.NoError(t, err)
}

func TestDoJSONRequestDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	client := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	responsePayload := map[string]interface{}{}
	err := client.doJSONRequest(context.Background(), http.MethodGet, "/test", nil, &responsePayload)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
}

func TestResolvePaddleListNextPathURLParseErrorControlChar(t *testing.T) {
	_, err := resolvePaddleListNextPath(paddleListPagination{
		HasMore: true,
		Next:    "://invalid-url\x7f",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIPaginationInvalid)
}

func TestDoJSONRequestNilResponsePayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":"ok"}`))
	}))
	defer server.Close()

	client := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	err := client.doJSONRequest(context.Background(), http.MethodGet, "/test", nil, nil)
	require.NoError(t, err)
}

func TestDoJSONRequestEmptyResponseBodyNoPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	responsePayload := map[string]interface{}{}
	err := client.doJSONRequest(context.Background(), http.MethodGet, "/test", nil, &responsePayload)
	require.NoError(t, err)
}

func TestDoJSONRequestRateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","detail":"too many requests"}}`))
	}))
	defer server.Close()

	client := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	err := client.doJSONRequest(context.Background(), http.MethodPost, "/test", nil, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIRateLimited)
}

func TestDoJSONRequestNilResponsePayloadReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":"ignored"}`))
	}))
	defer server.Close()

	client := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	err := client.doJSONRequest(context.Background(), http.MethodGet, "/test", nil, nil)
	require.NoError(t, err)
}

func TestDoJSONRequestEmptyResponseBodyReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}
	responsePayload := map[string]interface{}{}
	err := client.doJSONRequest(context.Background(), http.MethodGet, "/test", nil, &responsePayload)
	require.NoError(t, err)
}

// TestDoJSONRequestMarshalError covers line 528: json.Marshal fails on an unmarshalable payload.
func TestDoJSONRequestMarshalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &paddleAPIClient{
		baseURL:    server.URL,
		apiKey:     "test_key",
		httpClient: server.Client(),
	}

	// Channels cannot be marshaled to JSON — this triggers the json.Marshal error at line 528.
	unmarshalable := make(chan int)
	err := client.doJSONRequest(context.Background(), http.MethodPost, "/test", unmarshalable, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.Contains(t, err.Error(), "marshal request")
}

// TestDoJSONRequestNewRequestError covers line 546: http.NewRequestWithContext fails due to invalid URL.
func TestDoJSONRequestNewRequestError(t *testing.T) {
	// A URL with a null byte causes http.NewRequestWithContext to return an error.
	client := &paddleAPIClient{
		baseURL: "http://bad\x00host.example.com",
		apiKey:  "test_key",
		httpClient: &http.Client{},
	}

	err := client.doJSONRequest(context.Background(), http.MethodPost, "/test", nil, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.Contains(t, err.Error(), "create request")
}

// errReadCloser is a custom io.ReadCloser used to simulate Read and/or Close errors.
type errReadCloser struct {
	readErr  error
	closeErr error
}

func (e *errReadCloser) Read(_ []byte) (int, error) {
	if e.readErr != nil {
		return 0, e.readErr
	}
	return 0, io.EOF
}

func (e *errReadCloser) Close() error {
	return e.closeErr
}

// errTransport is an http.RoundTripper that returns a canned response with a custom body.
type errTransport struct {
	statusCode int
	body       *errReadCloser
}

func (t *errTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: t.statusCode,
		Header:     make(http.Header),
		Body:       t.body,
	}, nil
}

// TestDoJSONRequestBodyCloseError covers lines 570-572: response.Body.Close() returns an error.
func TestDoJSONRequestBodyCloseError(t *testing.T) {
	closeErr := errors.New("simulated close error")
	transport := &errTransport{
		statusCode: http.StatusOK,
		body: &errReadCloser{
			readErr:  nil,
			closeErr: closeErr,
		},
	}
	client := &paddleAPIClient{
		baseURL: "http://example.com",
		apiKey:  "test_key",
		httpClient: &http.Client{
			Transport: transport,
		},
	}

	responsePayload := map[string]interface{}{}
	err := client.doJSONRequest(context.Background(), http.MethodPost, "/test", nil, &responsePayload)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.Contains(t, err.Error(), "close response")
}

// TestDoJSONRequestBodyReadError covers lines 573-575: io.ReadAll returns an error while Close succeeds.
func TestDoJSONRequestBodyReadError(t *testing.T) {
	readErr := errors.New("simulated read error")
	transport := &errTransport{
		statusCode: http.StatusOK,
		body: &errReadCloser{
			readErr:  readErr,
			closeErr: nil,
		},
	}
	client := &paddleAPIClient{
		baseURL: "http://example.com",
		apiKey:  "test_key",
		httpClient: &http.Client{
			Transport: transport,
		},
	}

	responsePayload := map[string]interface{}{}
	err := client.doJSONRequest(context.Background(), http.MethodPost, "/test", nil, &responsePayload)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPaddleAPIRequestFailed)
	require.Contains(t, err.Error(), "read response")
}
