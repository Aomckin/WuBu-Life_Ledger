package httpapi

import (
	"encoding/json"
	"stockroom/internal/ledger"
	"testing"
)

func TestV021HTTPAccountClasses(t *testing.T) {
	h := testHandler(t)
	a := performJSON[ledger.Account](t, h, "POST", "/api/v1/accounts", `{"name":"微信","type":"WALLET","initial_balance":500}`, 201)
	b := performJSON[ledger.Account](t, h, "POST", "/api/v1/accounts", `{"name":"茶颜会员","type":"MERCHANT_BALANCE","account_class":"CONSUMPTION","merchant":"茶颜悦色"}`, 201)
	body := `{"from_account":"` + a.ID + `","to_account":"` + b.ID + `","paid_amount":100,"credited_amount":110}`
	keyedRequest(t, h, "POST", "/api/v1/money/top-ups", body, "topup", 201)
	w := keyedRequest(t, h, "POST", "/api/v1/money/top-ups", body, "topup", 201)
	if w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal(w.Header())
	}
	balance := performJSON[ledger.BalanceSummary](t, h, "GET", "/api/v1/money/balance", "", 200)
	if balance.LiquidBalance != 40000 || balance.ConsumptionBalance != 11000 {
		t.Fatal(balance)
	}
	stats := performJSON[ledger.AccountStatistics](t, h, "GET", "/api/v1/accounts/"+b.ID+"/statistics", "", 200)
	if stats.TopUpPrincipal != 10000 || stats.Bonus != 1000 {
		t.Fatal(stats)
	}
	w = keyedRequest(t, h, "GET", "/api/v1/accounts?account_class=CONSUMPTION&active=true", "", "", 200)
	var list struct {
		Items []ledger.Account `json:"items"`
	}
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].ID != b.ID {
		t.Fatal(list)
	}
	keyedRequest(t, h, "PATCH", "/api/v1/money/settings", `{"default_payment_account":"`+b.ID+`"}`, "default", 200)
	mismatch := `{"merchant":"麦当劳","total":20,"items":[{"name":"奶茶","quantity":1,"unit":"杯","price":20}]}`
	w = keyedRequest(t, h, "POST", "/api/v1/purchases/record", mismatch, "wrong-merchant", 409)
	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["error"].(map[string]any)["code"] != "ACCOUNT_MERCHANT_MISMATCH" {
		t.Fatal(response)
	}
	keyedRequest(t, h, "POST", "/api/v1/money/bonuses", `{"account_id":"`+a.ID+`","amount":5}`, "wrong-bonus", 422)
	keyedRequest(t, h, "POST", "/api/v1/money/bonuses", `{"account_id":"`+b.ID+`","amount":5}`, "gift", 201)
	keyedRequest(t, h, "GET", "/api/v1/accounts?account_class=UNKNOWN", "", "", 422)
	keyedRequest(t, h, "GET", "/api/v1/accounts?active=invalid", "", "", 422)
	keyedRequest(t, h, "POST", "/api/v1/money/top-ups", `{"from_account":"`+a.ID+`","to_account":"`+b.ID+`","paid_amount":1.001,"credited_amount":2}`, "fraction", 400)
}
