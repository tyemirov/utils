package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	paddleEnvironmentSandbox         = "sandbox"
	paddleEnvironmentProduction      = "production"
	paddleAPIBaseURLSandbox          = "https://sandbox-api.paddle.com"
	paddleAPIBaseURLProduction       = "https://api.paddle.com"
	paddleHeaderAuthorization        = "Authorization"
	paddleHeaderContentType          = "Content-Type"
	paddleHeaderAccept               = "Accept"
	paddleHeaderAPIVersion           = "Paddle-Version"
	paddleHeaderUserAgent            = "User-Agent"
	paddleContentTypeApplicationJSON = "application/json"
	paddleAPIVersion                 = "1"
	paddleUserAgentPoodleScanner     = "PoodleScanner/1.0 (+https://poodlescanner.com)"

	paddleAPIErrorCodeTransactionDefaultCheckoutURLNotSet = "transaction_default_checkout_url_not_set"
	paddleAPIErrorCodeNotFound                            = "not_found"

	paddleHeaderRetryAfter = "Retry-After"

	paddleAPIRequestMaxAttempts = 4
	paddleAPIRetryBaseDelay     = 500 * time.Millisecond
	paddleAPIRetryMaxDelay      = 5 * time.Second
	paddleListPageSize          = 100
)

var (
	ErrPaddleAPIKeyEmpty             = errors.New("billing.paddle.api.key.empty")
	ErrPaddleAPIEnvironmentInvalid   = errors.New("billing.paddle.api.environment.invalid")
	ErrPaddleAPIBaseURLInvalid       = errors.New("billing.paddle.api.base_url.invalid")
	ErrPaddleAPIPaginationInvalid    = errors.New("billing.paddle.api.pagination.invalid")
	ErrPaddleAPIRequestFailed        = errors.New("billing.paddle.api.request.failed")
	ErrPaddleAPITransient            = errors.New("billing.paddle.api.transient")
	ErrPaddleAPIRateLimited          = errors.New("billing.paddle.api.rate_limited")
	ErrPaddleAPICustomerNotFound     = errors.New("billing.paddle.api.customer.not_found")
	ErrPaddleAPITransactionNotFound  = errors.New("billing.paddle.api.transaction.not_found")
	ErrPaddleAPISubscriptionNotFound = errors.New("billing.paddle.api.subscription.not_found")
	ErrPaddleAPIPortalURLNotFound    = errors.New("billing.paddle.api.portal_url.not_found")
	ErrPaddleAPIDefaultCheckoutURL   = errors.New("billing.paddle.api.default_checkout_url.not_set")
	ErrPaddleAPIPriceNotFound        = errors.New("billing.paddle.api.price.not_found")
)

type paddleAPIClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type paddleAPIError struct {
	Code    string `json:"code"`
	Detail  string `json:"detail"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

type paddleAPIErrorEnvelope struct {
	Error paddleAPIError `json:"error"`
}

type paddleCustomer struct {
	ID string `json:"id"`
}

type paddleListCustomersResponse struct {
	Data []paddleCustomer `json:"data"`
}

type paddleListResponse[T any] struct {
	Data []T                    `json:"data"`
	Meta paddleListResponseMeta `json:"meta"`
}

type paddleListResponseMeta struct {
	Pagination paddleListPagination `json:"pagination"`
}

type paddleListPagination struct {
	Next    string `json:"next"`
	HasMore bool   `json:"has_more"`
}

type paddleCreateCustomerRequest struct {
	Email string `json:"email"`
}

type paddleCreateCustomerResponse struct {
	Data paddleCustomer `json:"data"`
}

type paddleGetCustomerResponse struct {
	Data struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"data"`
}

type paddleCreateTransactionRequest struct {
	Items          []paddleCreateTransactionItem `json:"items"`
	CollectionMode string                        `json:"collection_mode"`
	CustomerID     string                        `json:"customer_id"`
	CustomData     map[string]string             `json:"custom_data,omitempty"`
}

type paddleCreateTransactionItem struct {
	PriceID  string `json:"price_id"`
	Quantity int    `json:"quantity"`
}

type paddleCreateTransactionResponse struct {
	Data struct {
		ID string `json:"id"`
	} `json:"data"`
}

type paddleCreatePortalSessionResponse struct {
	Data struct {
		URLs struct {
			General struct {
				Overview string `json:"overview"`
			} `json:"general"`
		} `json:"urls"`
	} `json:"data"`
}

type paddleGetTransactionResponse struct {
	Data paddleTransactionCompletedWebhookData `json:"data"`
}

type paddleGetSubscriptionResponse struct {
	Data paddleSubscriptionWebhookData `json:"data"`
}

type paddlePriceAPIModel struct {
	ID        string `json:"id"`
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	UnitPrice *struct {
		Amount string `json:"amount"`
	} `json:"unit_price"`
	Product struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"product"`
	BillingCycle *struct {
		Interval  string `json:"interval"`
		Frequency int64  `json:"frequency"`
	} `json:"billing_cycle"`
}

type paddleGetPriceResponse struct {
	Data paddlePriceAPIModel `json:"data"`
}

type paddleListPricesResponse struct {
	Data []paddlePriceAPIModel `json:"data"`
}

func newPaddleAPIClient(
	environment string,
	apiKey string,
	baseURLOverride string,
	httpClient *http.Client,
) (*paddleAPIClient, error) {
	normalizedAPIKey := strings.TrimSpace(apiKey)
	if normalizedAPIKey == "" {
		return nil, ErrPaddleAPIKeyEmpty
	}
	resolvedBaseURL, resolveErr := resolvePaddleAPIBaseURL(environment, baseURLOverride)
	if resolveErr != nil {
		return nil, resolveErr
	}
	parsedBaseURL, parseErr := url.Parse(resolvedBaseURL)
	if parseErr != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		return nil, ErrPaddleAPIBaseURLInvalid
	}
	resolvedHTTPClient := httpClient
	if resolvedHTTPClient == nil {
		resolvedHTTPClient = newDirectBillingHTTPClient()
	}
	return &paddleAPIClient{
		baseURL:    parsedBaseURL.String(),
		apiKey:     normalizedAPIKey,
		httpClient: resolvedHTTPClient,
	}, nil
}

func resolvePaddleAPIBaseURL(environment string, baseURLOverride string) (string, error) {
	normalizedBaseURLOverride := strings.TrimSpace(baseURLOverride)
	if normalizedBaseURLOverride != "" {
		return normalizedBaseURLOverride, nil
	}
	normalizedEnvironment := strings.ToLower(strings.TrimSpace(environment))
	switch normalizedEnvironment {
	case paddleEnvironmentSandbox:
		return paddleAPIBaseURLSandbox, nil
	case paddleEnvironmentProduction:
		return paddleAPIBaseURLProduction, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrPaddleAPIEnvironmentInvalid, environment)
	}
}

func (client *paddleAPIClient) ResolveCustomerID(ctx context.Context, email string) (string, error) {
	customerID, err := client.findCustomerIDByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	if customerID != "" {
		return customerID, nil
	}

	createdCustomerID, createErr := client.createCustomer(ctx, email)
	if createErr == nil {
		return createdCustomerID, nil
	}
	if strings.Contains(strings.ToLower(createErr.Error()), "customer_already_exists") {
		existingCustomerID, lookupErr := client.findCustomerIDByEmail(ctx, email)
		if lookupErr != nil {
			return "", lookupErr
		}
		if existingCustomerID != "" {
			return existingCustomerID, nil
		}
	}
	return "", createErr
}

func (client *paddleAPIClient) FindCustomerIDByEmail(ctx context.Context, email string) (string, error) {
	return client.findCustomerIDByEmail(ctx, email)
}

func (client *paddleAPIClient) CreateTransaction(ctx context.Context, input paddleTransactionInput) (string, error) {
	requestPayload := paddleCreateTransactionRequest{
		Items: []paddleCreateTransactionItem{
			{
				PriceID:  strings.TrimSpace(input.PriceID),
				Quantity: 1,
			},
		},
		CollectionMode: "automatic",
		CustomerID:     strings.TrimSpace(input.CustomerID),
		CustomData:     input.Metadata,
	}
	responsePayload := paddleCreateTransactionResponse{}
	if err := client.doJSONRequest(ctx, http.MethodPost, "/transactions", requestPayload, &responsePayload); err != nil {
		return "", err
	}
	transactionID := strings.TrimSpace(responsePayload.Data.ID)
	if transactionID == "" {
		return "", ErrPaddleAPITransactionNotFound
	}
	return transactionID, nil
}

func (client *paddleAPIClient) ListCustomerTransactions(
	ctx context.Context,
	customerID string,
) ([]paddleTransactionCompletedWebhookData, error) {
	return listPaddleCustomerResources[paddleTransactionCompletedWebhookData](ctx, client, "/transactions", customerID)
}

func (client *paddleAPIClient) ListCustomerSubscriptions(
	ctx context.Context,
	customerID string,
) ([]paddleSubscriptionWebhookData, error) {
	return listPaddleCustomerResources[paddleSubscriptionWebhookData](ctx, client, "/subscriptions", customerID)
}

func listPaddleCustomerResources[T any](
	ctx context.Context,
	client *paddleAPIClient,
	endpointPath string,
	customerID string,
) ([]T, error) {
	normalizedCustomerID := strings.TrimSpace(customerID)
	if normalizedCustomerID == "" {
		return nil, ErrPaddleAPICustomerNotFound
	}
	query := url.Values{}
	query.Set("customer_id", normalizedCustomerID)
	query.Set("per_page", strconv.Itoa(paddleListPageSize))
	nextPagePath := endpointPath + "?" + query.Encode()
	seenPagePaths := map[string]struct{}{}
	resolvedResources := []T{}
	for nextPagePath != "" {
		if _, duplicatePage := seenPagePaths[nextPagePath]; duplicatePage {
			return nil, fmt.Errorf("%w: duplicate next page %q", ErrPaddleAPIPaginationInvalid, nextPagePath)
		}
		seenPagePaths[nextPagePath] = struct{}{}
		responsePayload := paddleListResponse[T]{}
		if requestErr := client.doJSONRequest(ctx, http.MethodGet, nextPagePath, nil, &responsePayload); requestErr != nil {
			return nil, requestErr
		}
		resolvedResources = append(resolvedResources, responsePayload.Data...)
		resolvedNextPagePath, nextPageErr := resolvePaddleListNextPath(responsePayload.Meta.Pagination)
		if nextPageErr != nil {
			return nil, nextPageErr
		}
		nextPagePath = resolvedNextPagePath
	}
	return resolvedResources, nil
}

func resolvePaddleListNextPath(pagination paddleListPagination) (string, error) {
	if !pagination.HasMore {
		return "", nil
	}
	normalizedNextPagePath := strings.TrimSpace(pagination.Next)
	if normalizedNextPagePath == "" {
		return "", fmt.Errorf("%w: next page path is empty when has_more is true", ErrPaddleAPIPaginationInvalid)
	}
	parsedNextPagePath, parseErr := url.Parse(normalizedNextPagePath)
	if parseErr != nil {
		return "", fmt.Errorf("%w: parse next page: %v", ErrPaddleAPIPaginationInvalid, parseErr)
	}
	normalizedPath := strings.TrimSpace(parsedNextPagePath.Path)
	if normalizedPath == "" {
		return "", fmt.Errorf("%w: next page path is empty", ErrPaddleAPIPaginationInvalid)
	}
	if !strings.HasPrefix(normalizedPath, "/") {
		normalizedPath = "/" + normalizedPath
	}
	if parsedNextPagePath.RawQuery != "" {
		return normalizedPath + "?" + parsedNextPagePath.RawQuery, nil
	}
	return normalizedPath, nil
}

func (client *paddleAPIClient) GetTransaction(
	ctx context.Context,
	transactionID string,
) (paddleTransactionCompletedWebhookData, error) {
	normalizedTransactionID := strings.TrimSpace(transactionID)
	if normalizedTransactionID == "" {
		return paddleTransactionCompletedWebhookData{}, ErrPaddleAPITransactionNotFound
	}
	path := fmt.Sprintf("/transactions/%s", url.PathEscape(normalizedTransactionID))
	responsePayload := paddleGetTransactionResponse{}
	if err := client.doJSONRequest(ctx, http.MethodGet, path, nil, &responsePayload); err != nil {
		return paddleTransactionCompletedWebhookData{}, err
	}
	resolvedTransactionID := strings.TrimSpace(responsePayload.Data.ID)
	if resolvedTransactionID == "" {
		return paddleTransactionCompletedWebhookData{}, ErrPaddleAPITransactionNotFound
	}
	return responsePayload.Data, nil
}

func (client *paddleAPIClient) ResolveCustomerEmail(ctx context.Context, customerID string) (string, error) {
	normalizedCustomerID := strings.TrimSpace(customerID)
	if normalizedCustomerID == "" {
		return "", ErrPaddleAPICustomerNotFound
	}
	path := fmt.Sprintf("/customers/%s", url.PathEscape(normalizedCustomerID))
	responsePayload := paddleGetCustomerResponse{}
	if err := client.doJSONRequest(ctx, http.MethodGet, path, nil, &responsePayload); err != nil {
		return "", err
	}
	customerEmail := strings.TrimSpace(responsePayload.Data.Email)
	if customerEmail == "" {
		return "", ErrPaddleAPICustomerNotFound
	}
	return customerEmail, nil
}

func (client *paddleAPIClient) GetSubscription(
	ctx context.Context,
	subscriptionID string,
) (paddleSubscriptionWebhookData, error) {
	normalizedSubscriptionID := strings.TrimSpace(subscriptionID)
	if normalizedSubscriptionID == "" {
		return paddleSubscriptionWebhookData{}, ErrPaddleAPISubscriptionNotFound
	}
	path := fmt.Sprintf("/subscriptions/%s", url.PathEscape(normalizedSubscriptionID))
	responsePayload := paddleGetSubscriptionResponse{}
	if err := client.doJSONRequest(ctx, http.MethodGet, path, nil, &responsePayload); err != nil {
		return paddleSubscriptionWebhookData{}, err
	}
	resolvedSubscriptionID := strings.TrimSpace(responsePayload.Data.ID)
	if resolvedSubscriptionID == "" {
		return paddleSubscriptionWebhookData{}, ErrPaddleAPISubscriptionNotFound
	}
	return responsePayload.Data, nil
}

func (client *paddleAPIClient) GetPrice(ctx context.Context, priceID string) (paddlePriceDetails, error) {
	normalizedPriceID := strings.TrimSpace(priceID)
	if normalizedPriceID == "" {
		return paddlePriceDetails{}, ErrPaddleAPIPriceNotFound
	}
	path := fmt.Sprintf("/prices/%s", url.PathEscape(normalizedPriceID))
	responsePayload := paddleGetPriceResponse{}
	if err := client.doJSONRequest(ctx, http.MethodGet, path, nil, &responsePayload); err != nil {
		return paddlePriceDetails{}, err
	}
	priceDetails, resolveErr := resolvePaddlePriceDetails(responsePayload.Data)
	if resolveErr != nil {
		return paddlePriceDetails{}, ErrPaddleAPIPriceNotFound
	}
	return priceDetails, nil
}

func (client *paddleAPIClient) ListPrices(
	ctx context.Context,
	priceIDs []string,
) (map[string]paddlePriceDetails, error) {
	normalizedIDs := make([]string, 0, len(priceIDs))
	seenIDs := make(map[string]struct{}, len(priceIDs))
	for _, rawPriceID := range priceIDs {
		normalizedPriceID := strings.TrimSpace(rawPriceID)
		if normalizedPriceID == "" {
			return nil, ErrPaddleAPIPriceNotFound
		}
		if _, alreadySeen := seenIDs[normalizedPriceID]; alreadySeen {
			continue
		}
		seenIDs[normalizedPriceID] = struct{}{}
		normalizedIDs = append(normalizedIDs, normalizedPriceID)
	}
	if len(normalizedIDs) == 0 {
		return nil, ErrPaddleAPIPriceNotFound
	}

	query := url.Values{}
	query.Set("per_page", strconv.Itoa(len(normalizedIDs)))
	query.Set("id", strings.Join(normalizedIDs, ","))
	pathWithQuery := "/prices?" + query.Encode()
	responsePayload := paddleListPricesResponse{}
	if requestErr := client.doJSONRequest(ctx, http.MethodGet, pathWithQuery, nil, &responsePayload); requestErr != nil {
		return nil, requestErr
	}
	priceDetailsByID := make(map[string]paddlePriceDetails, len(responsePayload.Data))
	for _, priceModel := range responsePayload.Data {
		resolvedPriceDetails, resolveErr := resolvePaddlePriceDetails(priceModel)
		if resolveErr != nil {
			return nil, resolveErr
		}
		priceDetailsByID[resolvedPriceDetails.ID] = resolvedPriceDetails
	}
	return priceDetailsByID, nil
}

func (client *paddleAPIClient) CreateCustomerPortalURL(ctx context.Context, customerID string) (string, error) {
	path := fmt.Sprintf("/customers/%s/portal-sessions", url.PathEscape(strings.TrimSpace(customerID)))
	responsePayload := paddleCreatePortalSessionResponse{}
	if err := client.doJSONRequest(ctx, http.MethodPost, path, map[string]string{}, &responsePayload); err != nil {
		return "", err
	}
	portalURL := strings.TrimSpace(responsePayload.Data.URLs.General.Overview)
	if portalURL == "" {
		return "", ErrPaddleAPIPortalURLNotFound
	}
	return portalURL, nil
}

func (client *paddleAPIClient) findCustomerIDByEmail(ctx context.Context, email string) (string, error) {
	normalizedEmail := strings.TrimSpace(email)
	if normalizedEmail == "" {
		return "", ErrBillingUserEmailInvalid
	}
	query := url.Values{}
	query.Set("email", normalizedEmail)
	pathWithQuery := "/customers?" + query.Encode()
	responsePayload := paddleListCustomersResponse{}
	if err := client.doJSONRequest(ctx, http.MethodGet, pathWithQuery, nil, &responsePayload); err != nil {
		return "", err
	}
	if len(responsePayload.Data) == 0 {
		return "", nil
	}
	customerID := strings.TrimSpace(responsePayload.Data[0].ID)
	if customerID == "" {
		return "", ErrPaddleAPICustomerNotFound
	}
	return customerID, nil
}

func (client *paddleAPIClient) createCustomer(ctx context.Context, email string) (string, error) {
	normalizedEmail := strings.TrimSpace(email)
	if normalizedEmail == "" {
		return "", ErrBillingUserEmailInvalid
	}
	requestPayload := paddleCreateCustomerRequest{
		Email: normalizedEmail,
	}
	responsePayload := paddleCreateCustomerResponse{}
	if err := client.doJSONRequest(ctx, http.MethodPost, "/customers", requestPayload, &responsePayload); err != nil {
		return "", err
	}
	customerID := strings.TrimSpace(responsePayload.Data.ID)
	if customerID == "" {
		return "", ErrPaddleAPICustomerNotFound
	}
	return customerID, nil
}

func (client *paddleAPIClient) doJSONRequest(
	ctx context.Context,
	method string,
	path string,
	requestPayload interface{},
	responsePayload interface{},
) error {
	if client == nil || client.httpClient == nil {
		return ErrPaddleProviderClientUnavailable
	}

	fullURL := strings.TrimRight(client.baseURL, "/") + "/" + strings.TrimLeft(path, "/")

	var requestBody []byte
	if requestPayload != nil {
		bodyBytes, marshalErr := json.Marshal(requestPayload)
		if marshalErr != nil {
			return fmt.Errorf("%w: marshal request: %v", ErrPaddleAPIRequestFailed, marshalErr)
		}
		requestBody = bodyBytes
	}
	retryConfig := httpRetryConfig{
		MaxAttempts: paddleAPIRequestMaxAttempts,
		BaseDelay:   paddleAPIRetryBaseDelay,
		MaxDelay:    paddleAPIRetryMaxDelay,
	}
	response, responseErr := doHTTPRequestWithRetry(
		client.httpClient,
		func() (*http.Request, error) {
			var bodyReader io.Reader
			if requestBody != nil {
				bodyReader = bytes.NewReader(requestBody)
			}
			request, requestErr := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
			if requestErr != nil {
				return nil, fmt.Errorf("%w: create request: %v", ErrPaddleAPIRequestFailed, requestErr)
			}
			request.Header.Set(paddleHeaderAuthorization, "Bearer "+client.apiKey)
			request.Header.Set(paddleHeaderAccept, paddleContentTypeApplicationJSON)
			request.Header.Set(paddleHeaderAPIVersion, paddleAPIVersion)
			request.Header.Set(paddleHeaderUserAgent, paddleUserAgentPoodleScanner)
			if requestPayload != nil {
				request.Header.Set(paddleHeaderContentType, paddleContentTypeApplicationJSON)
			}
			return request, nil
		},
		retryConfig,
	)
	if responseErr != nil {
		requestErr := fmt.Errorf("%w: do request: %v", ErrPaddleAPIRequestFailed, responseErr)
		if statusCode, hasStatusCode := retryWaitStatusCode(responseErr); hasStatusCode && statusCode == http.StatusTooManyRequests {
			return wrapPaddleTransientError(errors.Join(requestErr, ErrPaddleAPIRateLimited))
		}
		return wrapPaddleTransientError(requestErr)
	}

	responseBytes, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if closeErr != nil {
		return fmt.Errorf("%w: close response: %v", ErrPaddleAPIRequestFailed, closeErr)
	}
	if readErr != nil {
		return fmt.Errorf("%w: read response: %v", ErrPaddleAPIRequestFailed, readErr)
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return parsePaddleError(response.StatusCode, responseBytes)
	}

	if responsePayload == nil || len(responseBytes) == 0 {
		return nil
	}
	if decodeErr := json.Unmarshal(responseBytes, responsePayload); decodeErr != nil {
		return fmt.Errorf("%w: decode response: %v", ErrPaddleAPIRequestFailed, decodeErr)
	}
	return nil
}

func wrapPaddleTransientError(err error) error {
	return errors.Join(ErrPaddleAPITransient, err)
}

func parsePaddleError(statusCode int, body []byte) error {
	errorEnvelope := paddleAPIErrorEnvelope{}
	if unmarshalErr := json.Unmarshal(body, &errorEnvelope); unmarshalErr != nil {
		requestErr := fmt.Errorf("%w: status=%d", ErrPaddleAPIRequestFailed, statusCode)
		if statusCode == http.StatusTooManyRequests {
			return wrapPaddleTransientError(errors.Join(requestErr, ErrPaddleAPIRateLimited))
		}
		if statusCode >= http.StatusInternalServerError && statusCode < 600 {
			return wrapPaddleTransientError(requestErr)
		}
		return requestErr
	}

	errorCode := strings.TrimSpace(errorEnvelope.Error.Code)
	errorDetail := strings.TrimSpace(errorEnvelope.Error.Detail)
	errorMessage := strings.TrimSpace(errorEnvelope.Error.Message)
	if errorDetail == "" {
		errorDetail = errorMessage
	}
	if errorCode == "" {
		errorCode = "unknown"
	}
	if errorDetail == "" {
		errorDetail = fmt.Sprintf("status=%d", statusCode)
	}

	if statusCode == http.StatusTooManyRequests {
		requestErr := fmt.Errorf("%w: %s: %s", ErrPaddleAPIRequestFailed, errorCode, errorDetail)
		return wrapPaddleTransientError(errors.Join(requestErr, ErrPaddleAPIRateLimited))
	}
	if statusCode >= http.StatusInternalServerError && statusCode < 600 {
		requestErr := fmt.Errorf("%w: status=%d: %s: %s", ErrPaddleAPIRequestFailed, statusCode, errorCode, errorDetail)
		return wrapPaddleTransientError(requestErr)
	}

	if errorCode == paddleAPIErrorCodeTransactionDefaultCheckoutURLNotSet {
		return fmt.Errorf(
			"%w: %w: %s: %s",
			ErrPaddleAPIRequestFailed,
			ErrPaddleAPIDefaultCheckoutURL,
			errorCode,
			errorDetail,
		)
	}
	errorDetailLower := strings.ToLower(errorDetail)
	if errorCode == paddleAPIErrorCodeNotFound &&
		strings.Contains(errorDetailLower, "transaction ") &&
		strings.Contains(errorDetailLower, "not found") {
		return fmt.Errorf(
			"%w: %w: %s: %s",
			ErrPaddleAPIRequestFailed,
			ErrPaddleAPITransactionNotFound,
			errorCode,
			errorDetail,
		)
	}
	if errorCode == paddleAPIErrorCodeNotFound &&
		strings.Contains(errorDetailLower, "subscription ") &&
		strings.Contains(errorDetailLower, "not found") {
		return fmt.Errorf(
			"%w: %w: %s: %s",
			ErrPaddleAPIRequestFailed,
			ErrPaddleAPISubscriptionNotFound,
			errorCode,
			errorDetail,
		)
	}
	if errorCode == paddleAPIErrorCodeNotFound &&
		strings.Contains(errorDetailLower, "price ") &&
		strings.Contains(errorDetailLower, "not found") {
		return fmt.Errorf(
			"%w: %w: %s: %s",
			ErrPaddleAPIRequestFailed,
			ErrPaddleAPIPriceNotFound,
			errorCode,
			errorDetail,
		)
	}

	return fmt.Errorf("%w: %s: %s", ErrPaddleAPIRequestFailed, errorCode, errorDetail)
}

func resolvePaddlePriceDetails(priceModel paddlePriceAPIModel) (paddlePriceDetails, error) {
	resolvedPriceID := strings.TrimSpace(priceModel.ID)
	if resolvedPriceID == "" {
		return paddlePriceDetails{}, ErrPaddleAPIPriceNotFound
	}
	resolvedPriceDetails := paddlePriceDetails{
		ID:          resolvedPriceID,
		ProductID:   strings.TrimSpace(priceModel.ProductID),
		ProductName: strings.TrimSpace(priceModel.Product.Name),
		PriceName:   strings.TrimSpace(priceModel.Name),
	}
	if priceModel.UnitPrice != nil {
		resolvedAmount := strings.TrimSpace(priceModel.UnitPrice.Amount)
		if resolvedAmount != "" {
			priceCents, parseErr := strconv.ParseInt(resolvedAmount, 10, 64)
			if parseErr != nil {
				return paddlePriceDetails{}, ErrPaddleAPIPriceNotFound
			}
			resolvedPriceDetails.PriceCents = priceCents
		}
	}
	if priceModel.BillingCycle != nil {
		resolvedPriceDetails.BillingCycle = paddlePriceBillingCycle{
			Interval:  strings.TrimSpace(priceModel.BillingCycle.Interval),
			Frequency: priceModel.BillingCycle.Frequency,
		}
	}
	return resolvedPriceDetails, nil
}
