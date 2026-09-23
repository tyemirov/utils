package billing

import (
	"fmt"
	"net/http"
)

// NewPaddleCommerceClient exposes the shared Paddle transport for applications
// that own checkout orders and financial reconciliation. An empty baseURL uses
// the selected environment's Paddle endpoint. A nil httpClient uses the shared
// direct HTTP transport. Transaction creation is not automatically retried.
func NewPaddleCommerceClient(environment, apiKey, baseURL string, httpClient *http.Client) (PaddleCommerceClient, error) {
	client, err := newPaddleAPIClient(environment, apiKey, baseURL, httpClient)
	if err != nil {
		return nil, fmt.Errorf("create Paddle commerce client: %w", err)
	}
	return client, nil
}

// PaddleMoney retains an exact provider amount in the currency's lowest unit.
type PaddleMoney struct {
	Amount       string `json:"amount"`
	CurrencyCode string `json:"currency_code"`
}

// PaddleTransactionTotals retains provider-calculated transaction or payout
// totals without numeric conversion. Nil Fee and Earnings mean unavailable,
// not zero. Payout totals use their own CurrencyCode.
type PaddleTransactionTotals struct {
	Subtotal        string  `json:"subtotal"`
	Discount        string  `json:"discount"`
	Tax             string  `json:"tax"`
	Total           string  `json:"total"`
	Credit          string  `json:"credit"`
	CreditToBalance string  `json:"credit_to_balance"`
	Balance         string  `json:"balance"`
	GrandTotal      string  `json:"grand_total"`
	GrandTotalTax   string  `json:"grand_total_tax"`
	Fee             *string `json:"fee"`
	Earnings        *string `json:"earnings"`
	CurrencyCode    string  `json:"currency_code"`
}

// PaddleLineTotals retains exact line or unit amounts before and after tax.
type PaddleLineTotals struct {
	Subtotal string `json:"subtotal"`
	Discount string `json:"discount"`
	Tax      string `json:"tax"`
	Total    string `json:"total"`
}

// PaddleTransactionCheckout contains the provider-hosted checkout address.
type PaddleTransactionCheckout struct {
	URL string `json:"url"`
}

// PaddleTransactionPayment retains payment-attempt evidence without card data.
type PaddleTransactionPayment struct {
	PaymentAttemptID string  `json:"payment_attempt_id"`
	Amount           string  `json:"amount"`
	Status           string  `json:"status"`
	CreatedAt        string  `json:"created_at"`
	CapturedAt       *string `json:"captured_at"`
	ErrorCode        *string `json:"error_code"`
}
