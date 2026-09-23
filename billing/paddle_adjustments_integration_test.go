package billing_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tyemirov/utils/billing"
)

const adjustmentTransactionID = "txn_01hv8x2axb33yr5y238zfwcn5p"
const adjustmentFixture = `{"id":"adj_01hv8x2axb33yr5y238zfwcn5p","action":"chargeback_warning","type":"full","status":"approved","transaction_id":"txn_01hv8x2axb33yr5y238zfwcn5p","customer_id":"ctm_01hv8x2axb33yr5y238zfwcn5p","subscription_id":null,"currency_code":"USD","credit_applied_to_balance":null,"reason":"disputed","created_at":"2026-09-23T12:00:00Z","updated_at":"2026-09-23T12:00:01Z","items":[{"id":"adjitm_01hv8x2axb33yr5y238zfwcn5p","item_id":"txnitm_01hv8x2axb33yr5y238zfwcn5p","type":"full","amount":null,"totals":{"subtotal":"9007199254740993","tax":"90","total":"9007199254741083"}}],"totals":{"subtotal":"9007199254740993","tax":"90","total":"9007199254741083","fee":"30","retained_fee":"20","earnings":"-30","currency_code":"USD"},"payout_totals":{"subtotal":"8000000000000000","tax":"80","total":"8000000000000080","fee":"25","retained_fee":"15","earnings":"7999999999999975","currency_code":"EUR","chargeback_fee":{"amount":"1800","original":{"amount":"2000","currency_code":"USD"}}}}`

func TestPaddleCommerceClientReadsAdjustmentPagesAndFinancialEvidence(t *testing.T) {
	var pages atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer adjustment-fixture-key" || request.Header.Get("Paddle-Version") != "1" {
			t.Errorf("invalid shared request: %s %s", request.Method, request.URL)
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/transactions/"+adjustmentTransactionID {
			_, _ = io.WriteString(writer, `{"data":{"id":"`+adjustmentTransactionID+`","details":{"adjusted_totals":{"subtotal":"400","tax":"40","total":"440","grand_total":"440","grand_total_tax":"40","fee":null,"retained_fee":"30","earnings":null,"currency_code":"USD"}}}}`)
			return
		}
		if request.URL.Path != "/adjustments" || request.URL.Query().Get("transaction_id") != adjustmentTransactionID || request.URL.Query().Get("per_page") != "50" {
			t.Errorf("unscoped adjustment list: %s", request.URL)
		}
		pages.Add(1)
		if request.URL.Query().Get("after") == "" {
			_, _ = fmt.Fprintf(writer, `{"data":[%s],"meta":{"pagination":{"has_more":true,"next":"https://api.paddle.com/adjustments?transaction_id=%s&per_page=50&after=adj_01hv8x2axb33yr5y238zfwcn5p"}}}`, adjustmentFixture, adjustmentTransactionID)
			return
		}
		second := strings.ReplaceAll(adjustmentFixture, "adj_01hv8x2axb33yr5y238zfwcn5p", "adj_01hv8x2axb33yr5y238zfwcn5q")
		second = strings.ReplaceAll(second, `"chargeback_warning"`, `"chargeback_warning_reverse"`)
		_, _ = io.WriteString(writer, `{"data":[`+second+`],"meta":{"pagination":{"has_more":false}}}`)
	}))
	defer server.Close()
	client, err := billing.NewPaddleCommerceClient("sandbox", "adjustment-fixture-key", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	adjustments, err := client.ListTransactionAdjustments(t.Context(), adjustmentTransactionID)
	if err != nil || len(adjustments) != 2 || pages.Load() != 2 {
		t.Fatalf("adjustments=%+v pages=%d error=%v", adjustments, pages.Load(), err)
	}
	first := adjustments[0]
	if first.Status != "approved" || first.Action != "chargeback_warning" || first.SubscriptionID != nil || first.CreditAppliedToBalance != nil || first.CurrencyCode != "USD" || first.Totals.Subtotal != "9007199254740993" || first.Totals.Earnings != "-30" || first.Totals.RetainedFee != "20" {
		t.Fatalf("lost adjustment evidence: %+v", first)
	}
	if len(first.Items) != 1 || first.Items[0].ItemID != "txnitm_01hv8x2axb33yr5y238zfwcn5p" || first.Items[0].Amount != nil || first.Items[0].Totals.Total != "9007199254741083" {
		t.Fatalf("lost item: %+v", first.Items)
	}
	if first.PayoutTotals == nil || first.PayoutTotals.CurrencyCode != "EUR" || first.PayoutTotals.ChargebackFee == nil || first.PayoutTotals.ChargebackFee.Original == nil || first.PayoutTotals.ChargebackFee.Original.CurrencyCode != "USD" || first.PayoutTotals.ChargebackFee.Original.Amount != "2000" || adjustments[1].Action != "chargeback_warning_reverse" {
		t.Fatalf("lost payout/reversal: %+v", adjustments)
	}
	transaction, err := client.GetTransaction(t.Context(), adjustmentTransactionID)
	if err != nil || transaction.Details.AdjustedTotals == nil || transaction.Details.AdjustedTotals.Subtotal != "400" || transaction.Details.AdjustedTotals.RetainedFee != "30" || transaction.Details.AdjustedTotals.Fee != nil || transaction.Details.AdjustedTotals.Earnings != nil {
		t.Fatalf("adjusted totals=%+v error=%v", transaction.Details, err)
	}
}

func TestPaddleCommerceClientRejectsUnresolvedAdjustmentLists(t *testing.T) {
	for _, scenario := range []string{"empty-transaction", "provider-error", "invalid-json", "missing-next", "repeated-page", "foreign-transaction", "duplicate-adjustment", "missing-id"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				switch scenario {
				case "provider-error":
					writer.WriteHeader(http.StatusUnauthorized)
					_, _ = io.WriteString(writer, `{"error":{"code":"unauthorized","detail":"controlled failure"}}`)
				case "invalid-json":
					_, _ = io.WriteString(writer, `{`)
				case "missing-next":
					_, _ = io.WriteString(writer, `{"data":[],"meta":{"pagination":{"has_more":true}}}`)
				case "repeated-page":
					_, _ = io.WriteString(writer, `{"data":[],"meta":{"pagination":{"has_more":true,"next":"`+request.URL.String()+`"}}}`)
				case "foreign-transaction":
					_, _ = io.WriteString(writer, `{"data":[`+strings.ReplaceAll(adjustmentFixture, adjustmentTransactionID, "txn_01hv8x2axb33yr5y238zfwcn5q")+`]}`)
				case "duplicate-adjustment":
					_, _ = io.WriteString(writer, `{"data":[`+adjustmentFixture+`,`+adjustmentFixture+`]}`)
				case "missing-id":
					_, _ = io.WriteString(writer, `{"data":[`+strings.ReplaceAll(adjustmentFixture, `"id":"adj_01hv8x2axb33yr5y238zfwcn5p"`, `"id":""`)+`]}`)
				default:
					t.Error("invalid transaction dispatched")
				}
			}))
			defer server.Close()
			client, err := billing.NewPaddleCommerceClient("sandbox", "fixture-key", server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			transactionID := adjustmentTransactionID
			if scenario == "empty-transaction" {
				transactionID = " "
			}
			values, err := client.ListTransactionAdjustments(t.Context(), transactionID)
			if err == nil || values != nil {
				t.Fatalf("accepted incomplete evidence: %+v error=%v", values, err)
			}
			if scenario == "empty-transaction" && calls.Load() != 0 {
				t.Fatal("empty scope dispatched")
			}
		})
	}
}
