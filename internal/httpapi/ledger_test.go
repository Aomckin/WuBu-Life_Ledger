package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
	"stockroom/internal/web"
)

const acceptancePurchase = `{"merchant":"乐尔乐","purchased_at":"2026-10-05T20:30:00+08:00","total_amount":229,"source":"agent","category":"food","items":[{"name":"无糖可乐 2L","quantity":4,"unit":"瓶","unit_price":5.5,"subtotal":22,"item_type":"CONSUMABLE"},{"name":"泡面","quantity":2,"unit":"包","unit_price":4,"subtotal":8,"item_type":"CONSUMABLE"},{"name":"耳机","quantity":1,"unit":"个","unit_price":199,"subtotal":199,"item_type":"DURABLE"}]}`

func keyedRequest(t *testing.T, h http.Handler, method, path, body, key string, want int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s -> %d want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	return w
}
func TestLedgerHTTPAcceptance(t *testing.T) {
	h := testHandler(t)
	first := keyedRequest(t, h, "POST", "/api/v1/purchases", acceptancePurchase, "A", 201)
	var p ledger.Purchase
	if e := json.Unmarshal(first.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	second := keyedRequest(t, h, "POST", "/api/v1/purchases", acceptancePurchase, "A", 201)
	if second.Header().Get("Idempotency-Replayed") != "true" || first.Body.String() != second.Body.String() {
		t.Fatal("purchase response not replayed")
	}
	keyedRequest(t, h, "POST", "/api/v1/purchases", strings.Replace(acceptancePurchase, `"total_amount":229`, `"total_amount":228`, 1), "A", 409)
	id := p.Items[0].ItemID
	base := "/api/v1/items/" + id
	r := keyedRequest(t, h, "POST", base+"/consume", `{"quantity":1}`, "B", 200)
	var action ledger.InventoryResult
	json.Unmarshal(r.Body.Bytes(), &action)
	if action.Item.Quantity != 3 || action.Event.Type != "CONSUME" {
		t.Fatal(action)
	}
	keyedRequest(t, h, "POST", base+"/consume", `{"quantity":1}`, "B", 200)
	r = keyedRequest(t, h, "POST", base+"/adjust", `{"quantity":5}`, "C", 200)
	var i inventory.Item
	json.Unmarshal(r.Body.Bytes(), &i)
	if i.Quantity != 5 {
		t.Fatal(i)
	}
	keyedRequest(t, h, "POST", base+"/adjust", `{"quantity":5}`, "C", 200)
	keyedRequest(t, h, "POST", base+"/adjust", `{"delta":0}`, "bad", 422)
	keyedRequest(t, h, "POST", base+"/adjust", `{"quantity":5,"delta":1}`, "bad", 422)
	keyedRequest(t, h, "POST", base+"/aliases", `{"alias":"零度可乐大瓶"}`, "alias", 201)
	resolved := performJSON[inventory.Item](t, h, "GET", "/api/v1/items/resolve?name=零度可乐大瓶", "", 200)
	if resolved.ID != id {
		t.Fatal(resolved)
	}
	keyedRequest(t, h, "POST", base+"/open", `{}`, "open", 200)
	keyedRequest(t, h, "POST", base+"/finish", `{}`, "finish", 200)
	keyedRequest(t, h, "POST", base+"/consume", `{"quantity":1}`, "negative", 409)
	durable := "/api/v1/items/" + p.Items[2].ItemID
	keyedRequest(t, h, "POST", durable+"/maintenance", `{"title":"换后胎","cost":65,"occurred_at":"2026-10-05T21:00:00+08:00"}`, "D", 201)
	keyedRequest(t, h, "POST", durable+"/maintenance", `{"title":"换后胎","cost":65,"occurred_at":"2026-10-05T21:00:00+08:00"}`, "D", 201)
	d := performJSON[ledger.Dashboard](t, h, "GET", "/api/v1/ledger/summary?month=2026-10", "", 200)
	if d.MonthExpense != 29400 || len(d.RecentPurchases) != 1 || len(d.RecentMaintenance) != 1 {
		t.Fatal(d)
	}
	for _, path := range []string{"/api/v1/inventory", base + "/prices", base + "/purchases", base + "/inventory-events", base + "/aliases", "/api/v1/inventory-events", "/api/v1/maintenance", durable + "/maintenance", durable + "/durable", "/api/v1/purchases/" + p.ID, "/api/v1/expenses?category=food&from=2026-10-01T00:00:00%2B08:00&to=2026-11-01T00:00:00%2B08:00", "/inventory", "/assets/ledger.js", "/assets/ledger.css"} {
		keyedRequest(t, h, "GET", path, "", "", 200)
	}
	keyedRequest(t, h, "GET", "/api/v1/purchases?limit=0", "", "", 422)
	keyedRequest(t, h, "GET", "/api/v1/expenses?from=yesterday", "", "", 422)
	keyedRequest(t, h, "DELETE", base, "", "", 409)
	keyedRequest(t, h, "POST", "/api/v1/purchases", strings.Replace(acceptancePurchase, `"total_amount":229`, `"total_amount":229.001`, 1), "bad-money", 400)
	keyedRequest(t, h, "POST", "/api/v1/purchases", `{"unexpected":true}`, "bad-json", 400)
}

func TestOpenAPILedgerContract(t *testing.T) {
	b, e := web.ReadFile("openapi.json")
	if e != nil {
		t.Fatal(e)
	}
	var d map[string]any
	if e = json.Unmarshal(b, &d); e != nil {
		t.Fatal(e)
	}
	if d["openapi"] != "3.1.0" {
		t.Fatal("OpenAPI version")
	}
	paths := d["paths"].(map[string]any)
	ids := map[string]bool{}
	for _, entry := range paths {
		for method, raw := range entry.(map[string]any) {
			if method == "parameters" {
				continue
			}
			op, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := op["operationId"].(string)
			if id == "" || ids[id] {
				t.Fatalf("missing/duplicate operationId %s", id)
			}
			ids[id] = true
		}
	}
	for _, id := range []string{"record_purchase_event", "consume_item", "adjust_inventory", "open_item", "finish_item", "add_maintenance_event", "search_items", "get_item", "get_inventory", "get_purchase_history", "get_price_history", "get_maintenance_history", "get_recent_purchases", "create_account", "transfer_money", "record_income", "record_refund", "adjust_balance", "sell_item", "retire_item", "move_item", "get_durable_items", "get_item_total_cost"} {
		if !ids[id] {
			t.Error("missing operation", id)
		}
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if r, ok := x["$ref"].(string); ok && strings.HasPrefix(r, "#/") {
				var target any = d
				for _, seg := range strings.Split(strings.TrimPrefix(r, "#/"), "/") {
					m, ok := target.(map[string]any)
					if !ok {
						t.Fatal("broken ref", r)
					}
					target, ok = m[seg]
					if !ok {
						t.Fatal("missing ref", r)
					}
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(d)
}
