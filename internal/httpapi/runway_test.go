package httpapi

import (
	"stockroom/internal/ledger"
	"testing"
)

func TestV03StrictHTTPAndReplay(t *testing.T) {
	h := testHandler(t)
	a := performJSON[ledger.Account](t, h, "POST", "/api/v1/accounts", `{"name":"v03 银行","type":"BANK","initial_balance":6000}`, 201)
	keyedRequest(t, h, "POST", "/api/v1/planned-cashflows", `{"name":"无日期","direction":"OUTFLOW","amount":100,"due_at":"tomorrow","mandatory":true}`, "invalid-date", 422)
	keyedRequest(t, h, "POST", "/api/v1/planned-cashflows", `{"name":"负金额","direction":"OUTFLOW","amount":-1,"due_at":"2026-11-18T00:00:00+08:00"}`, "negative", 400)
	keyedRequest(t, h, "POST", "/api/v1/planned-cashflows", `{"name":"额外字段","direction":"OUTFLOW","amount":1,"due_at":"2026-11-18T00:00:00+08:00","unexpected":true}`, "extra", 400)
	p := performJSON[ledger.PlannedCashflow](t, h, "POST", "/api/v1/planned-cashflows", `{"name":"房租","direction":"OUTFLOW","amount":2980,"due_at":"2026-11-18T00:00:00+08:00","mandatory":true,"account_id":"`+a.ID+`"}`, 201)
	balance := performJSON[ledger.Account](t, h, "GET", "/api/v1/accounts/"+a.ID, "", 200)
	if balance.CurrentBalance != 600000 {
		t.Fatal(balance)
	}
	path := "/api/v1/planned-cashflows/" + p.ID + "/complete"
	keyedRequest(t, h, "POST", path, `{}`, "v03-pay", 200)
	w := keyedRequest(t, h, "POST", path, `{}`, "v03-pay", 200)
	if w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal(w.Header())
	}
	keyedRequest(t, h, "POST", path, `{}`, "v03-second", 409)
	keyedRequest(t, h, "PUT", "/api/v1/planned-cashflows/"+p.ID, `{"name":"改已付","direction":"OUTFLOW","amount":100,"due_at":"2026-11-18T00:00:00+08:00"}`, "v03-edit", 409)
	balance = performJSON[ledger.Account](t, h, "GET", "/api/v1/accounts/"+a.ID, "", 200)
	if balance.CurrentBalance != 302000 {
		t.Fatal(balance)
	}
	keyedRequest(t, h, "PATCH", "/api/v1/runway/settings", `{"safety_buffer":-1,"runway_horizon_days":30}`, "bad-buffer", 400)
	keyedRequest(t, h, "GET", "/api/v1/planned-cashflows?from=bad", "", "", 422)
}
