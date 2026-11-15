package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"stockroom/internal/ledger"
	"strings"
	"testing"
)

func TestFinancialWeeklySourceBoundary(t *testing.T) {
	h := testHandler(t)
	// Retired ingress must stay absent, including when an old client retries.
	for _, route := range []struct{ method, path string }{
		{"POST", "/api/v1/collector/events"},
		{"POST", "/api/v1/collector/heartbeat"},
		{"GET", "/api/v1/financial/collectors"},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`)))
		if w.Code != 404 {
			t.Fatalf("retired route %s returned %d", route.path, w.Code)
		}
	}
	keyedRequest(t, h, "POST", "/api/v1/financial/events", `{"provider":"WECHAT","capture_source":"ANDROID_NOTIFICATION","raw_text":"retired source"}`, "retired-source", 422)
	keyedRequest(t, h, "POST", "/api/v1/financial/events", `{"provider":"WECHAT","capture_source":"MANUAL","source_device":"retired-field","raw_text":"evidence"}`, "retired-field", 400)
}

func TestFinancialPageAndWriteValidation(t *testing.T) {
	h := testHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/financial-inbox", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "财务收件箱") {
		t.Fatal(w.Code, w.Body.String())
	}
	keyedRequest(t, h, "POST", "/api/v1/financial/events", `{"provider":"OTHER","capture_source":"MANUAL","raw_text":"evidence"}`, "capture", 201)
	keyedRequest(t, h, "POST", "/api/v1/financial/events", `{"provider":"OTHER","capture_source":"MANUAL","raw_text":"changed"}`, "capture", 409)
}
func TestFinancialNearbyAmbiguityCannotBeHiddenByPagination(t *testing.T) {
	h := testHandler(t)
	a := performJSON[ledger.Account](t, h, "POST", "/api/v1/accounts", `{"name":"near-time","type":"BANK","initial_balance":100}`, 201)
	body := `{"account_id":"` + a.ID + `","amount":3,"merchant":"水店","occurred_at":"2026-11-02T04:00:00Z"}`
	for _, key := range []string{"near-a", "near-b"} {
		keyedRequest(t, h, "POST", "/api/v1/money/expenses", body, key, 201)
	}
	var result struct {
		Items       []ledger.AnnotatedTransaction `json:"items"`
		MatchStatus string                        `json:"match_status"`
	}
	w := keyedRequest(t, h, "GET", "/api/v1/financial/transactions?account_id="+a.ID+"&occurred_at=2026-11-02T04:00:00Z&window_minutes=10&limit=1&offset=1", "", "", 200)
	if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if len(result.Items) != 1 || result.MatchStatus != "REVIEW_NEEDED" {
		t.Fatal(result)
	}
}
func TestRawFinancialEventResponseMatchesClosedContract(t *testing.T) {
	h := testHandler(t)
	w := keyedRequest(t, h, "POST", "/api/v1/financial/events", `{"provider":"OTHER","capture_source":"MANUAL","raw_text":"待核对"}`, "contract", 201)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	api := performJSON[map[string]any](t, h, "GET", "/api/v1/openapi.json", "", 200)
	schemas := api["components"].(map[string]any)["schemas"].(map[string]any)
	props := schemas["RawFinancialEvent"].(map[string]any)["properties"].(map[string]any)
	for key := range out {
		if _, ok := props[key]; !ok {
			t.Errorf("missing response schema field %s", key)
		}
	}
}
