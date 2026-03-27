package billing

import (
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
	stripeAPIBaseURL                   = "https://api.stripe.com"
	stripeHeaderAuthorization          = "Authorization"
	stripeHeaderContentType            = "Content-Type"
	stripeHeaderRetryAfter             = httpRetryAfterHeader
	stripeHeaderUserAgent              = "User-Agent"
	stripeContentTypeFormURLEncoded    = "application/x-www-form-urlencoded"
	stripeUserAgentPoodleScanner       = "PoodleScanner/1.0 (+https://poodlescanner.com)"
	stripeCheckoutModeSubscription     = "subscription"
	stripeCheckoutModePayment          = "payment"
	stripeCheckoutSessionQuantity      = "1"
	stripeCheckoutSessionsEndpointPath = "/v1/checkout/sessions"
	stripeSubscriptionsEndpointPath    = "/v1/subscriptions"

	stripeAPIRequestMaxAttempts = 4
	stripeAPIRetryBaseDelay     = 500 * time.Millisecond
	stripeAPIRetryMaxDelay      = 5 * time.Second
)

var (
	ErrStripeAPIKeyEmpty                = errors.New("billing.stripe.api.key.empty")
	ErrStripeAPIRequestFailed           = errors.New("billing.stripe.api.request.failed")
	ErrStripeAPITransient               = errors.New("billing.stripe.api.transient")
	ErrStripeAPIRateLimited             = errors.New("billing.stripe.api.rate_limited")
	ErrStripeAPICustomerNotFound        = errors.New("billing.stripe.api.customer.not_found")
	ErrStripeAPICheckoutSessionNotFound = errors.New("billing.stripe.api.checkout_session.not_found")
	ErrStripeAPIPortalURLNotFound       = errors.New("billing.stripe.api.portal_url.not_found")
	ErrStripeAPIPriceNotFound           = errors.New("billing.stripe.api.price.not_found")
)

type stripeAPIClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type stripeCheckoutSessionInput struct {
	CustomerID string
	PriceID    string
	Mode       string
	SuccessURL string
	CancelURL  string
	Metadata   map[string]string
}

type stripePortalSessionInput struct {
	CustomerID string
	ReturnURL  string
}

type stripeAPIErrorEnvelope struct {
	Error struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type stripeListCustomersResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type stripeListSubscriptionsResponse struct {
	Data    []stripeSubscriptionWebhookData `json:"data"`
	HasMore bool                            `json:"has_more"`
}

type stripeCustomerResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type stripeCreateCheckoutSessionResponse struct {
	ID string `json:"id"`
}

type stripeCreatePortalSessionResponse struct {
	URL string `json:"url"`
}

type stripePriceResponse struct {
	ID         string                `json:"id"`
	Type       string                `json:"type"`
	UnitAmount int64                 `json:"unit_amount"`
	Recurring  *stripePriceRecurring `json:"recurring"`
}

type stripePriceRecurring struct {
	Interval string `json:"interval"`
}

func newStripeAPIClient(apiKey string, httpClient *http.Client) (*stripeAPIClient, error) {
	normalizedAPIKey := strings.TrimSpace(apiKey)
	if normalizedAPIKey == "" {
		return nil, ErrStripeAPIKeyEmpty
	}
	resolvedHTTPClient := httpClient
	if resolvedHTTPClient == nil {
		resolvedHTTPClient = newDirectBillingHTTPClient()
	}
	return &stripeAPIClient{
		baseURL:    stripeAPIBaseURL,
		apiKey:     normalizedAPIKey,
		httpClient: resolvedHTTPClient,
	}, nil
}

func (client *stripeAPIClient) ResolveCustomerID(ctx context.Context, email string) (string, error) {
	customerID, customerIDErr := client.findCustomerIDByEmail(ctx, email)
	if customerIDErr != nil {
		return "", customerIDErr
	}
	if customerID != "" {
		return customerID, nil
	}
	return client.createCustomer(ctx, email)
}

func (client *stripeAPIClient) FindCustomerID(ctx context.Context, email string) (string, error) {
	return client.findCustomerIDByEmail(ctx, email)
}

func (client *stripeAPIClient) ResolveCustomerEmail(ctx context.Context, customerID string) (string, error) {
	normalizedCustomerID := strings.TrimSpace(customerID)
	if normalizedCustomerID == "" {
		return "", ErrStripeAPICustomerNotFound
	}
	path := "/v1/customers/" + url.PathEscape(normalizedCustomerID)
	responsePayload := stripeCustomerResponse{}
	if requestErr := client.doFormRequest(ctx, http.MethodGet, path, nil, &responsePayload); requestErr != nil {
		return "", requestErr
	}
	normalizedEmail := strings.ToLower(strings.TrimSpace(responsePayload.Email))
	if normalizedEmail == "" {
		return "", ErrStripeAPICustomerNotFound
	}
	return normalizedEmail, nil
}

func (client *stripeAPIClient) CreateCheckoutSession(
	ctx context.Context,
	input stripeCheckoutSessionInput,
) (string, error) {
	normalizedCustomerID := strings.TrimSpace(input.CustomerID)
	normalizedPriceID := strings.TrimSpace(input.PriceID)
	normalizedMode := strings.ToLower(strings.TrimSpace(input.Mode))
	normalizedSuccessURL := strings.TrimSpace(input.SuccessURL)
	normalizedCancelURL := strings.TrimSpace(input.CancelURL)
	if normalizedCustomerID == "" || normalizedPriceID == "" || normalizedMode == "" {
		return "", ErrStripeAPICheckoutSessionNotFound
	}
	if normalizedSuccessURL == "" || normalizedCancelURL == "" {
		return "", ErrStripeAPICheckoutSessionNotFound
	}

	formValues := url.Values{}
	formValues.Set("mode", normalizedMode)
	formValues.Set("customer", normalizedCustomerID)
	formValues.Set("success_url", normalizedSuccessURL)
	formValues.Set("cancel_url", normalizedCancelURL)
	formValues.Set("line_items[0][price]", normalizedPriceID)
	formValues.Set("line_items[0][quantity]", stripeCheckoutSessionQuantity)
	for metadataKey, metadataValue := range input.Metadata {
		normalizedMetadataKey := strings.TrimSpace(metadataKey)
		normalizedMetadataValue := strings.TrimSpace(metadataValue)
		if normalizedMetadataKey == "" || normalizedMetadataValue == "" {
			continue
		}
		formValues.Set("metadata["+normalizedMetadataKey+"]", normalizedMetadataValue)
	}

	responsePayload := stripeCreateCheckoutSessionResponse{}
	if requestErr := client.doFormRequest(
		ctx,
		http.MethodPost,
		stripeCheckoutSessionsEndpointPath,
		formValues,
		&responsePayload,
	); requestErr != nil {
		return "", requestErr
	}
	sessionID := strings.TrimSpace(responsePayload.ID)
	if sessionID == "" {
		return "", ErrStripeAPICheckoutSessionNotFound
	}
	return sessionID, nil
}

func (client *stripeAPIClient) GetCheckoutSession(
	ctx context.Context,
	sessionID string,
) (stripeCheckoutSessionWebhookData, error) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return stripeCheckoutSessionWebhookData{}, ErrStripeAPICheckoutSessionNotFound
	}
	path := stripeCheckoutSessionsEndpointPath + "/" + url.PathEscape(normalizedSessionID)
	responsePayload := stripeCheckoutSessionWebhookData{}
	if requestErr := client.doFormRequest(ctx, http.MethodGet, path, nil, &responsePayload); requestErr != nil {
		return stripeCheckoutSessionWebhookData{}, requestErr
	}
	if strings.TrimSpace(responsePayload.ID) == "" {
		return stripeCheckoutSessionWebhookData{}, ErrStripeAPICheckoutSessionNotFound
	}
	return responsePayload, nil
}

func (client *stripeAPIClient) CreateCustomerPortalURL(
	ctx context.Context,
	input stripePortalSessionInput,
) (string, error) {
	normalizedCustomerID := strings.TrimSpace(input.CustomerID)
	normalizedReturnURL := strings.TrimSpace(input.ReturnURL)
	if normalizedCustomerID == "" || normalizedReturnURL == "" {
		return "", ErrStripeAPIPortalURLNotFound
	}
	formValues := url.Values{}
	formValues.Set("customer", normalizedCustomerID)
	formValues.Set("return_url", normalizedReturnURL)
	responsePayload := stripeCreatePortalSessionResponse{}
	if requestErr := client.doFormRequest(
		ctx,
		http.MethodPost,
		"/v1/billing_portal/sessions",
		formValues,
		&responsePayload,
	); requestErr != nil {
		return "", requestErr
	}
	portalURL := strings.TrimSpace(responsePayload.URL)
	if portalURL == "" {
		return "", ErrStripeAPIPortalURLNotFound
	}
	return portalURL, nil
}

func (client *stripeAPIClient) GetPrice(
	ctx context.Context,
	priceID string,
) (stripePriceResponse, error) {
	normalizedPriceID := strings.TrimSpace(priceID)
	if normalizedPriceID == "" {
		return stripePriceResponse{}, ErrStripeAPIPriceNotFound
	}
	path := "/v1/prices/" + url.PathEscape(normalizedPriceID)
	responsePayload := stripePriceResponse{}
	if requestErr := client.doFormRequest(ctx, http.MethodGet, path, nil, &responsePayload); requestErr != nil {
		return stripePriceResponse{}, requestErr
	}
	if strings.TrimSpace(responsePayload.ID) == "" {
		return stripePriceResponse{}, ErrStripeAPIPriceNotFound
	}
	return responsePayload, nil
}

func (client *stripeAPIClient) ListSubscriptions(
	ctx context.Context,
	customerID string,
) ([]stripeSubscriptionWebhookData, error) {
	normalizedCustomerID := strings.TrimSpace(customerID)
	if normalizedCustomerID == "" {
		return []stripeSubscriptionWebhookData{}, nil
	}

	resolvedSubscriptions := make([]stripeSubscriptionWebhookData, 0)
	startingAfter := ""
	for {
		formValues := url.Values{}
		formValues.Set("customer", normalizedCustomerID)
		formValues.Set("status", "all")
		formValues.Set("limit", "100")
		if startingAfter != "" {
			formValues.Set("starting_after", startingAfter)
		}
		responsePayload := stripeListSubscriptionsResponse{}
		if requestErr := client.doFormRequest(
			ctx,
			http.MethodGet,
			stripeSubscriptionsEndpointPath,
			formValues,
			&responsePayload,
		); requestErr != nil {
			return nil, requestErr
		}
		resolvedSubscriptions = append(resolvedSubscriptions, responsePayload.Data...)
		if !responsePayload.HasMore {
			break
		}
		if len(responsePayload.Data) == 0 {
			return nil, ErrStripeAPIRequestFailed
		}
		nextStartingAfter := strings.TrimSpace(responsePayload.Data[len(responsePayload.Data)-1].ID)
		if nextStartingAfter == "" {
			return nil, ErrStripeAPIRequestFailed
		}
		startingAfter = nextStartingAfter
	}
	return resolvedSubscriptions, nil
}

func (client *stripeAPIClient) findCustomerIDByEmail(ctx context.Context, email string) (string, error) {
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	if normalizedEmail == "" {
		return "", ErrBillingUserEmailInvalid
	}
	formValues := url.Values{}
	formValues.Set("email", normalizedEmail)
	formValues.Set("limit", "1")
	responsePayload := stripeListCustomersResponse{}
	if requestErr := client.doFormRequest(ctx, http.MethodGet, "/v1/customers", formValues, &responsePayload); requestErr != nil {
		return "", requestErr
	}
	if len(responsePayload.Data) == 0 {
		return "", nil
	}
	customerID := strings.TrimSpace(responsePayload.Data[0].ID)
	if customerID == "" {
		return "", ErrStripeAPICustomerNotFound
	}
	return customerID, nil
}

func (client *stripeAPIClient) createCustomer(ctx context.Context, email string) (string, error) {
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	if normalizedEmail == "" {
		return "", ErrBillingUserEmailInvalid
	}
	formValues := url.Values{}
	formValues.Set("email", normalizedEmail)
	responsePayload := stripeCustomerResponse{}
	if requestErr := client.doFormRequest(ctx, http.MethodPost, "/v1/customers", formValues, &responsePayload); requestErr != nil {
		return "", requestErr
	}
	customerID := strings.TrimSpace(responsePayload.ID)
	if customerID == "" {
		return "", ErrStripeAPICustomerNotFound
	}
	return customerID, nil
}

func (client *stripeAPIClient) doFormRequest(
	ctx context.Context,
	method string,
	path string,
	formValues url.Values,
	responsePayload interface{},
) error {
	if client == nil || client.httpClient == nil {
		return ErrStripeAPIRequestFailed
	}

	normalizedPath := "/" + strings.TrimLeft(strings.TrimSpace(path), "/")
	fullURL := strings.TrimRight(client.baseURL, "/") + normalizedPath
	var requestBody io.Reader
	if strings.EqualFold(method, http.MethodGet) {
		if len(formValues) > 0 {
			fullURL = fullURL + "?" + formValues.Encode()
		}
	} else {
		encodedBody := ""
		if formValues != nil {
			encodedBody = formValues.Encode()
		}
		requestBody = strings.NewReader(encodedBody)
	}

	requestBodyRaw := ""
	if requestBody != nil {
		requestBodyRaw = formValues.Encode()
	}
	retryConfig := httpRetryConfig{
		MaxAttempts: stripeAPIRequestMaxAttempts,
		BaseDelay:   stripeAPIRetryBaseDelay,
		MaxDelay:    stripeAPIRetryMaxDelay,
	}
	response, responseErr := doHTTPRequestWithRetry(
		client.httpClient,
		func() (*http.Request, error) {
			var attemptBody io.Reader
			if requestBodyRaw != "" {
				attemptBody = strings.NewReader(requestBodyRaw)
			}
			request, requestErr := http.NewRequestWithContext(ctx, method, fullURL, attemptBody)
			if requestErr != nil {
				return nil, fmt.Errorf("%w: create request: %v", ErrStripeAPIRequestFailed, requestErr)
			}
			request.Header.Set(stripeHeaderAuthorization, "Bearer "+client.apiKey)
			request.Header.Set(stripeHeaderUserAgent, stripeUserAgentPoodleScanner)
			if !strings.EqualFold(method, http.MethodGet) {
				request.Header.Set(stripeHeaderContentType, stripeContentTypeFormURLEncoded)
			}
			return request, nil
		},
		retryConfig,
	)
	if responseErr != nil {
		requestErr := fmt.Errorf("%w: do request: %v", ErrStripeAPIRequestFailed, responseErr)
		if statusCode, hasStatusCode := retryWaitStatusCode(responseErr); hasStatusCode && statusCode == http.StatusTooManyRequests {
			return wrapStripeTransientError(errors.Join(requestErr, ErrStripeAPIRateLimited))
		}
		return wrapStripeTransientError(requestErr)
	}

	responseBody, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if closeErr != nil {
		return fmt.Errorf("%w: close response: %v", ErrStripeAPIRequestFailed, closeErr)
	}
	if readErr != nil {
		return fmt.Errorf("%w: read response: %v", ErrStripeAPIRequestFailed, readErr)
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return parseStripeAPIError(response.StatusCode, normalizedPath, responseBody)
	}
	if responsePayload == nil || len(responseBody) == 0 {
		return nil
	}
	if decodeErr := json.Unmarshal(responseBody, responsePayload); decodeErr != nil {
		return fmt.Errorf("%w: decode response: %v", ErrStripeAPIRequestFailed, decodeErr)
	}
	return nil
}

func parseStripeAPIError(statusCode int, path string, body []byte) error {
	errorEnvelope := stripeAPIErrorEnvelope{}
	_ = json.Unmarshal(body, &errorEnvelope)
	errorCode := strings.TrimSpace(errorEnvelope.Error.Code)
	errorMessage := strings.TrimSpace(errorEnvelope.Error.Message)
	if errorCode == "" {
		errorCode = "unknown"
	}
	if errorMessage == "" {
		errorMessage = fmt.Sprintf("status=%d", statusCode)
	}
	requestErr := fmt.Errorf("%w: status=%d: %s: %s", ErrStripeAPIRequestFailed, statusCode, errorCode, errorMessage)
	if statusCode == http.StatusTooManyRequests {
		return wrapStripeTransientError(errors.Join(requestErr, ErrStripeAPIRateLimited))
	}
	if statusCode >= http.StatusInternalServerError && statusCode < 600 {
		return wrapStripeTransientError(requestErr)
	}
	if statusCode == http.StatusNotFound {
		missingResourceErr := resolveStripeAPIMissingResourceError(path)
		if missingResourceErr != nil {
			return fmt.Errorf("%w: %w", requestErr, missingResourceErr)
		}
	}
	return requestErr
}

func resolveStripeAPIMissingResourceError(path string) error {
	if strings.HasPrefix(path, "/v1/customers/") {
		return ErrStripeAPICustomerNotFound
	}
	if strings.HasPrefix(path, stripeCheckoutSessionsEndpointPath+"/") {
		return ErrStripeAPICheckoutSessionNotFound
	}
	if strings.HasPrefix(path, "/v1/prices/") {
		return ErrStripeAPIPriceNotFound
	}
	return nil
}

func wrapStripeTransientError(err error) error {
	return errors.Join(ErrStripeAPITransient, err)
}

func parseStripeUnixTimestamp(rawTimestamp int64) time.Time {
	if rawTimestamp <= 0 {
		return time.Time{}
	}
	return time.Unix(rawTimestamp, 0).UTC()
}

func formatStripeMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return map[string]string{}
	}
	formattedMetadata := make(map[string]string, len(metadata))
	for metadataKey, metadataValue := range metadata {
		normalizedMetadataKey := strings.TrimSpace(metadataKey)
		normalizedMetadataValue := strings.TrimSpace(metadataValue)
		if normalizedMetadataKey == "" || normalizedMetadataValue == "" {
			continue
		}
		formattedMetadata[normalizedMetadataKey] = normalizedMetadataValue
	}
	return formattedMetadata
}

func parseStripeMetadataInt64(metadata map[string]string, key string) (int64, error) {
	rawValue := strings.TrimSpace(metadata[key])
	if rawValue == "" {
		return 0, nil
	}
	parsedValue, parseErr := strconv.ParseInt(rawValue, 10, 64)
	if parseErr != nil || parsedValue <= 0 {
		return 0, ErrWebhookGrantMetadataInvalid
	}
	return parsedValue, nil
}
