package billing

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const paddleAdjustmentPageSize = "50"

// ErrPaddleAPIAdjustmentInvalid identifies incomplete or cross-transaction evidence.
var ErrPaddleAPIAdjustmentInvalid = errors.New("billing.paddle.api.adjustment.invalid")

// PaddleAdjustment contains the processor's current refund or reversal state.
// Monetary values remain exact decimal strings in their currency's lowest unit.
type PaddleAdjustment struct {
	ID                     string                        `json:"id"`
	Action                 string                        `json:"action"`
	Type                   string                        `json:"type"`
	Status                 string                        `json:"status"`
	TransactionID          string                        `json:"transaction_id"`
	SubscriptionID         *string                       `json:"subscription_id"`
	CustomerID             string                        `json:"customer_id"`
	Reason                 string                        `json:"reason"`
	CurrencyCode           string                        `json:"currency_code"`
	CreditAppliedToBalance *bool                         `json:"credit_applied_to_balance"`
	Items                  []PaddleAdjustmentItem        `json:"items"`
	Totals                 PaddleAdjustmentTotals        `json:"totals"`
	PayoutTotals           *PaddleAdjustmentPayoutTotals `json:"payout_totals"`
	CreatedAt              string                        `json:"created_at"`
	UpdatedAt              string                        `json:"updated_at"`
}

// PaddleAdjustmentItem links an adjustment amount to an original transaction item.
type PaddleAdjustmentItem struct {
	ID     string                     `json:"id"`
	ItemID string                     `json:"item_id"`
	Type   string                     `json:"type"`
	Amount *string                    `json:"amount"`
	Totals PaddleAdjustmentItemTotals `json:"totals"`
}

// PaddleAdjustmentItemTotals separates the principal and tax for an adjustment item.
type PaddleAdjustmentItemTotals struct {
	Subtotal string `json:"subtotal"`
	Tax      string `json:"tax"`
	Total    string `json:"total"`
}

// PaddleAdjustmentTotals retains signed earnings and separately retained fees.
type PaddleAdjustmentTotals struct {
	Subtotal     string `json:"subtotal"`
	Tax          string `json:"tax"`
	Total        string `json:"total"`
	Fee          string `json:"fee"`
	RetainedFee  string `json:"retained_fee"`
	Earnings     string `json:"earnings"`
	CurrencyCode string `json:"currency_code"`
}

// PaddleAdjustmentPayoutTotals uses its own payout currency.
type PaddleAdjustmentPayoutTotals struct {
	PaddleAdjustmentTotals
	ChargebackFee *PaddleChargebackFee `json:"chargeback_fee"`
}

// PaddleChargebackFee retains an optional amount before currency conversion.
type PaddleChargebackFee struct {
	Amount   string       `json:"amount"`
	Original *PaddleMoney `json:"original"`
}

// PaddleAdjustedTransactionTotals retains transaction totals after adjustments.
// Nil Fee and Earnings indicate unavailable values, not zero amounts.
type PaddleAdjustedTransactionTotals struct {
	Subtotal      string  `json:"subtotal"`
	Tax           string  `json:"tax"`
	Total         string  `json:"total"`
	GrandTotal    string  `json:"grand_total"`
	GrandTotalTax string  `json:"grand_total_tax"`
	Fee           *string `json:"fee"`
	RetainedFee   string  `json:"retained_fee"`
	Earnings      *string `json:"earnings"`
	CurrencyCode  string  `json:"currency_code"`
}

func (client *paddleAPIClient) ListTransactionAdjustments(ctx context.Context, transactionID string) ([]PaddleAdjustment, error) {
	transactionID = strings.TrimSpace(transactionID)
	if transactionID == "" {
		return nil, ErrPaddleAPITransactionNotFound
	}
	query := url.Values{"transaction_id": {transactionID}, "per_page": {paddleAdjustmentPageSize}}
	adjustments, err := listPaddleResources[PaddleAdjustment](ctx, client, "/adjustments?"+query.Encode())
	if err != nil {
		return nil, fmt.Errorf("list Paddle adjustments for %s: %w", transactionID, err)
	}
	seen := make(map[string]bool, len(adjustments))
	for _, adjustment := range adjustments {
		if adjustment.ID == "" || adjustment.TransactionID != transactionID || seen[adjustment.ID] {
			return nil, fmt.Errorf("%w: transaction %s", ErrPaddleAPIAdjustmentInvalid, transactionID)
		}
		seen[adjustment.ID] = true
	}
	return adjustments, nil
}
