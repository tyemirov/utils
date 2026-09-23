package billing_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/tyemirov/utils/billing"
)

const paddleFinancialFixture = `{
 "id":"txn_01hv8x2axb33yr5y238zfwcn5p","status":"completed",
 "currency_code":"USD","collection_mode":"automatic","invoice_number":"123-45678","discount_id":null,
 "customer_id":"ctm_01hv8x2axb33yr5y238zfwcn5p","custom_data":{"funding_order_id":"order-one","billing_account_id":"account-one"},
 "items":[{"quantity":1,"price":{"id":"pri_01hv8x2axb33yr5y238zfwcn5p","unit_price":{"amount":"9007199254740993","currency_code":"USD"}}}],
 "details":{
  "totals":{"subtotal":"9007199254740993","discount":"0","tax":"90","total":"9007199254741083","credit":"0","credit_to_balance":"0","balance":"0","grand_total":"9007199254741083","grand_total_tax":"90","fee":"30","earnings":"9007199254741053","currency_code":"USD"},
  "payout_totals":{"subtotal":"8000000000000000","discount":"0","tax":"80","total":"8000000000000080","credit":"0","credit_to_balance":"0","balance":"0","grand_total":"8000000000000080","grand_total_tax":"80","fee":"25","earnings":"7999999999999975","currency_code":"EUR"},
  "line_items":[{"id":"txnitm_01hv8x2axb33yr5y238zfwcn5p","price_id":"pri_01hv8x2axb33yr5y238zfwcn5p","quantity":1,"totals":{"subtotal":"9007199254740993","discount":"0","tax":"90","total":"9007199254741083"},"unit_totals":{"subtotal":"9007199254740993","discount":"0","tax":"90","total":"9007199254741083"}}]
 },
 "checkout":{"url":"https://checkout.example/transaction"},
 "payments":[{"payment_attempt_id":"fixture-attempt","amount":"9007199254741083","status":"captured","created_at":"2026-09-23T12:00:00Z","captured_at":"2026-09-23T12:00:01Z","error_code":null}]
}`

func TestPaddleCommerceClientRetainsOrderMetadataAndFinancialEvidence(t *testing.T) {
	metadata := map[string]string{"funding_order_id": "order-one", "billing_account_id": "account-one"}
	var creates atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer fixture-api-key" || request.Header.Get("Paddle-Version") != "1" {
			t.Error("missing shared client authentication or API version")
		}
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/transactions":
			creates.Add(1)
			var input struct {
				CustomerID     string            `json:"customer_id"`
				CustomData     map[string]string `json:"custom_data"`
				CollectionMode string            `json:"collection_mode"`
				Items          []struct {
					PriceID  string `json:"price_id"`
					Quantity int    `json:"quantity"`
				} `json:"items"`
			}
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(input.CustomData, metadata) || input.CustomerID != "ctm_01hv8x2axb33yr5y238zfwcn5p" || input.CollectionMode != "automatic" || len(input.Items) != 1 || input.Items[0].Quantity != 1 || input.Items[0].PriceID != "pri_01hv8x2axb33yr5y238zfwcn5p" {
				t.Errorf("checkout input=%+v", input)
			}
			_, _ = io.WriteString(writer, `{"data":{"id":"txn_01hv8x2axb33yr5y238zfwcn5p"}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/transactions/txn_01hv8x2axb33yr5y238zfwcn5p":
			_, _ = io.WriteString(writer, `{"data":`+paddleFinancialFixture+`}`)
		case request.Method == http.MethodGet && request.URL.Path == "/transactions":
			if request.URL.Query().Get("customer_id") != "ctm_01hv8x2axb33yr5y238zfwcn5p" {
				t.Error("unscoped transaction list")
			}
			_, _ = io.WriteString(writer, `{"data":[`+paddleFinancialFixture+`],"meta":{"pagination":{"has_more":false}}}`)
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := billing.NewPaddleCommerceClient("sandbox", "fixture-api-key", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	id, err := client.CreateTransaction(t.Context(), billing.PaddleTransactionInput{CustomerID: "ctm_01hv8x2axb33yr5y238zfwcn5p", PriceID: "pri_01hv8x2axb33yr5y238zfwcn5p", Metadata: metadata})
	if err != nil || id != "txn_01hv8x2axb33yr5y238zfwcn5p" || creates.Load() != 1 {
		t.Fatalf("created=%s calls=%d error=%v", id, creates.Load(), err)
	}
	transaction, err := client.GetTransaction(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if transaction.CurrencyCode != "USD" || transaction.CollectionMode != "automatic" || transaction.InvoiceNumber == nil || *transaction.InvoiceNumber != "123-45678" {
		t.Fatalf("transaction identity=%+v", transaction)
	}
	totals := transaction.Details.Totals
	if totals == nil || totals.Subtotal != "9007199254740993" || totals.Tax != "90" || totals.Fee == nil || *totals.Fee != "30" || totals.Earnings == nil || *totals.Earnings != "9007199254741053" || totals.Balance != "0" || totals.CurrencyCode != "USD" {
		t.Fatalf("lost exact totals=%+v", totals)
	}
	payout := transaction.Details.PayoutTotals
	if payout == nil || payout.CurrencyCode != "EUR" || payout.Fee == nil || *payout.Fee != "25" {
		t.Fatalf("lost payout=%+v", payout)
	}
	if len(transaction.Items) != 1 || transaction.Items[0].Quantity != 1 || transaction.Items[0].Price.UnitPrice.Amount != "9007199254740993" || transaction.Items[0].Price.UnitPrice.CurrencyCode != "USD" {
		t.Fatalf("lost price=%+v", transaction.Items)
	}
	if len(transaction.Details.LineItems) != 1 || transaction.Details.LineItems[0].Quantity != 1 || transaction.Details.LineItems[0].Totals.Total != "9007199254741083" || transaction.Details.LineItems[0].UnitTotals.Tax != "90" {
		t.Fatalf("lost line amounts=%+v", transaction.Details.LineItems)
	}
	if transaction.Checkout == nil || transaction.Checkout.URL != "https://checkout.example/transaction" || len(transaction.Payments) != 1 || transaction.Payments[0].Amount != "9007199254741083" || transaction.Payments[0].Status != "captured" {
		t.Fatalf("lost payment evidence=%+v", transaction)
	}
	listed, err := client.ListCustomerTransactions(t.Context(), transaction.CustomerID)
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(transaction, listed[0]) {
		t.Fatalf("list differs=%+v error=%v", listed, err)
	}
}

func TestPaddleCommerceClientPreservesUnprocessedFinancialValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"data":{"id":"txn_pending","status":"paid","currency_code":"USD","invoice_number":null,"checkout":null,"details":{"totals":{"subtotal":"500","tax":"0","total":"500","fee":null,"earnings":null,"currency_code":"USD"},"payout_totals":null}}}`)
	}))
	defer server.Close()
	client, err := billing.NewPaddleCommerceClient("sandbox", "fixture-key", server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := client.GetTransaction(t.Context(), "txn_pending")
	if err != nil {
		t.Fatal(err)
	}
	if transaction.Status != "paid" || transaction.Details.Totals == nil || transaction.Details.Totals.Fee != nil || transaction.Details.Totals.Earnings != nil || transaction.Details.PayoutTotals != nil || transaction.InvoiceNumber != nil || transaction.Checkout != nil {
		t.Fatalf("pending evidence=%+v", transaction)
	}
}

func TestPaddleCommerceClientDoesNotRepeatUncertainCheckoutCreation(t *testing.T) {
	for _, outcome := range []string{"unavailable", "disconnect"} {
		t.Run(outcome, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				if outcome == "unavailable" {
					writer.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				connection, _, err := writer.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				if err := connection.Close(); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client, err := billing.NewPaddleCommerceClient("sandbox", "fixture-key", server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.CreateTransaction(context.Background(), billing.PaddleTransactionInput{CustomerID: "ctm_one", PriceID: "pri_one", Metadata: map[string]string{"funding_order_id": "uncertain-order"}})
			if err == nil || calls.Load() != 1 {
				t.Fatalf("uncertain creation repeated: calls=%d error=%v", calls.Load(), err)
			}
		})
	}
}

func TestPaddleCommerceClientRejectsMissingCredentials(t *testing.T) {
	client, err := billing.NewPaddleCommerceClient("sandbox", "", "", nil)
	if client != nil || !errors.Is(err, billing.ErrPaddleAPIKeyEmpty) {
		t.Fatalf("client=%v error=%v", client, err)
	}
}
