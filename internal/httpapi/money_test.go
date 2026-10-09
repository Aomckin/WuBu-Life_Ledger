package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"stockroom/internal/ledger"
)

func TestV02HTTPRealityEvents(t *testing.T) {
	h := testHandler(t)
	create := func(body, key string) ledger.Account {
		w := keyedRequest(t, h, "POST", "/api/v1/accounts", body, key, 201)
		var out ledger.Account
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	a := create(`{"name":"微信","type":"WALLET","initial_balance":100}`, "account-a")
	b := create(`{"name":"支付宝","type":"WALLET","initial_balance":0}`, "account-b")
	keyedRequest(t, h, "PATCH", "/api/v1/money/settings", `{"default_payment_account":"`+a.ID+`"}`, "default", 200)
	w := keyedRequest(t, h, "POST", "/api/v1/purchases/record", `{"items":[{"name":"可乐","spec":"2L","quantity":1,"unit":"瓶","price":5.5}]}`, "buy", 201)
	var purchase struct {
		PurchaseID string `json:"purchase_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &purchase)
	out := performJSON[ledger.Account](t, h, "GET", "/api/v1/money/balance?account_id="+a.ID, "", 200)
	if out.CurrentBalance != 9450 {
		t.Fatal(out)
	}
	transfer := `{"from_account":"` + a.ID + `","to_account":"` + b.ID + `","amount":50}`
	keyedRequest(t, h, "POST", "/api/v1/money/transfers", transfer, "transfer", 201)
	retry := keyedRequest(t, h, "POST", "/api/v1/money/transfers", transfer, "transfer", 201)
	if retry.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal(retry.Header())
	}
	keyedRequest(t, h, "POST", "/api/v1/money/refunds", `{"purchase_event_id":"`+purchase.PurchaseID+`","amount":5.5}`, "refund", 201)
	sum := performJSON[ledger.MoneySummary](t, h, "GET", "/api/v1/money/expense-summary?period=month&group_by=category", "", 200)
	if sum.NetExpense != 0 || sum.Income != 0 || sum.TransferOut != 5000 {
		t.Fatal(sum)
	}
	keyedRequest(t, h, "POST", "/api/v1/money/adjustments", `{"account_id":"`+a.ID+`","actual_balance":-0.08,"reason":"核对"}`, "adjust", 201)
	out = performJSON[ledger.Account](t, h, "GET", "/api/v1/accounts/"+a.ID, "", 200)
	if out.CurrentBalance != -8 {
		t.Fatal(out)
	}
	keyedRequest(t, h, "POST", "/api/v1/money/transfers", `{"from_account":"`+a.ID+`","to_account":"`+a.ID+`","amount":50}`, "same-account", 422)
	keyedRequest(t, h, "POST", "/api/v1/money/income", `{"account_id":"missing","amount":50}`, "missing", 404)
	keyedRequest(t, h, "GET", "/api/v1/money/summary?period=month&from=2026-10-01T00:00:00Z", "", "", 422)
	keyedRequest(t, h, "GET", "/api/v1/money/transactions?type=UNKNOWN", "", "", 422)
	keyedRequest(t, h, "POST", "/api/v1/money/income", `{"amount":1.001}`, "fraction", 400)
	// All newly documented routes are actually available.
	for _, path := range []string{"/api/v1/accounts", "/api/v1/money/settings", "/api/v1/money/categories", "/api/v1/money/income-summary", "/api/v1/money/category-summary", "/api/v1/money/merchant-summary", "/api/v1/durables"} {
		keyedRequest(t, h, http.MethodGet, path, "", "", 200)
	}
}
