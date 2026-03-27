package billing

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// custom round-tripper helpers for body error injection
// ---------------------------------------------------------------------------

type errCloseBody struct {
	data     []byte
	offset   int
	closeErr error
}

func (b *errCloseBody) Read(p []byte) (int, error) {
	if b.offset >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.offset:])
	b.offset += n
	return n, nil
}

func (b *errCloseBody) Close() error {
	return b.closeErr
}

type errReadBody struct {
	readErr error
}

func (b *errReadBody) Read(_ []byte) (int, error) {
	return 0, b.readErr
}

func (b *errReadBody) Close() error {
	return nil
}

type fixedResponseRoundTripper struct {
	response *http.Response
	err      error
}

func (rt *fixedResponseRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return rt.response, rt.err
}

// ---------------------------------------------------------------------------
// stripe_api_client.go — line 401
// http.NewRequestWithContext error (invalid method)
// ---------------------------------------------------------------------------

func TestStripeAPIClientDoFormRequestInvalidMethodReturnsError(t *testing.T) {
	apiClient, clientErr := newStripeAPIClient("sk_test_123", &http.Client{})
	require.NoError(t, clientErr)
	// An HTTP method containing a space is rejected by http.NewRequestWithContext.
	err := apiClient.doFormRequest(
		context.Background(),
		"INVALID METHOD",
		"/v1/customers",
		nil,
		nil,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

// ---------------------------------------------------------------------------
// stripe_api_client.go — lines 423-425
// response.Body.Close() returns an error
// ---------------------------------------------------------------------------

func TestStripeAPIClientDoFormRequestBodyCloseErrorReturnsError(t *testing.T) {
	closeErr := errors.New("close error")
	body := &errCloseBody{
		data:     []byte(`{"id":"cus_123"}`),
		closeErr: closeErr,
	}
	transport := &fixedResponseRoundTripper{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
		},
	}
	apiClient, clientErr := newStripeAPIClient("sk_test_123", &http.Client{Transport: transport})
	require.NoError(t, clientErr)
	apiClient.baseURL = "http://stripe.test"

	responsePayload := &stripeCustomerResponse{}
	err := apiClient.doFormRequest(context.Background(), http.MethodGet, "/v1/customers/cus_123", nil, responsePayload)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

// ---------------------------------------------------------------------------
// stripe_api_client.go — lines 426-428
// io.ReadAll error (body read fails, close succeeds)
// ---------------------------------------------------------------------------

func TestStripeAPIClientDoFormRequestBodyReadErrorReturnsError(t *testing.T) {
	readErr := errors.New("read error")
	body := &errReadBody{readErr: readErr}
	transport := &fixedResponseRoundTripper{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
		},
	}
	apiClient, clientErr := newStripeAPIClient("sk_test_123", &http.Client{Transport: transport})
	require.NoError(t, clientErr)
	apiClient.baseURL = "http://stripe.test"

	responsePayload := &stripeCustomerResponse{}
	err := apiClient.doFormRequest(context.Background(), http.MethodGet, "/v1/customers/cus_123", nil, responsePayload)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStripeAPIRequestFailed)
}

// ---------------------------------------------------------------------------
// stripe_provider.go — line 539
// sort tiebreaker: same isActive status, different OccurredAt timestamps
// ---------------------------------------------------------------------------

func TestBuildStripeSyncSubscriptionEventsSortByOccurredAt(t *testing.T) {
	now := time.Now().UTC()
	earlier := now.Add(-time.Hour)
	// Both subscriptions are inactive (canceled); they have different timestamps.
	// The sort tiebreaker at line 539 (leftOccurredAt.Before(rightOccurredAt)) must
	// be reached because isActive is equal and timestamps differ.
	subscriptions := []stripeSubscriptionWebhookData{
		{ID: "sub_later", Status: stripeSubscriptionStatusCanceled, CreatedAt: now.Unix()},
		{ID: "sub_earlier", Status: stripeSubscriptionStatusCanceled, CreatedAt: earlier.Unix()},
	}
	events, err := buildStripeSyncSubscriptionEvents("user@example.com", subscriptions, now)
	require.NoError(t, err)
	require.Len(t, events, 2)
	// Earlier OccurredAt should sort before later OccurredAt.
	require.Contains(t, events[0].EventID, "sub_earlier")
	require.Contains(t, events[1].EventID, "sub_later")
}
