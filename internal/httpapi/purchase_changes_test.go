package httpapi

import (
	"encoding/json"
	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
	"testing"
)

func TestPurchaseChangeHTTP(t *testing.T) {
	h := testHandler(t)
	w := keyedRequest(t, h, "POST", "/api/v1/acquisitions", `{"source":"agent","items":[{"name":"无糖可乐2L","quantity":2,"unit":"L","item_type":"CONSUMABLE"}]}`, "create", 201)
	var p ledger.Purchase
	if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	line := p.Items[0]
	change := ledger.AcquisitionAmendment{ExpectedUpdatedAt: p.UpdatedAt, AcquisitionInput: ledger.AcquisitionInput{Source: p.Source, OccurredAt: p.PurchasedAt, Items: []ledger.AcquisitionLine{{ID: line.ID, ItemID: line.ItemID, Name: line.Name, Quantity: line.Quantity, Unit: line.Unit, ItemType: line.ItemType, ProductSpec: line.ProductSpec}}}}
	change.Items[0].Quantity = 1
	change.Items[0].Unit = "瓶"
	body, e := json.Marshal(change)
	if e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/acquisitions/" + p.ID
	first := keyedRequest(t, h, "PUT", path, string(body), "edit", 200)
	var updated ledger.Purchase
	json.Unmarshal(first.Body.Bytes(), &updated)
	if updated.Items[0].Unit != "瓶" || updated.Items[0].Quantity != 1 {
		t.Fatal(updated)
	}
	replay := keyedRequest(t, h, "PUT", path, string(body), "edit", 200)
	if replay.Header().Get("Idempotency-Replayed") != "true" || first.Body.String() != replay.Body.String() {
		t.Fatal("amend replay failed")
	}
	keyedRequest(t, h, "PUT", path, string(body), "stale", 409)
	item := performJSON[inventory.Item](t, h, "GET", "/api/v1/items/"+p.Items[0].ItemID, "", 200)
	if item.Quantity != 1 || item.Unit != "瓶" {
		t.Fatal(item)
	}
	keyedRequest(t, h, "DELETE", path, `{}`, "no-version", 422)
	deletion, _ := json.Marshal(ledger.PurchaseDeletion{ExpectedUpdatedAt: updated.UpdatedAt})
	keyedRequest(t, h, "DELETE", path, string(deletion), "delete", 200)
	replay = keyedRequest(t, h, "DELETE", path, string(deletion), "delete", 200)
	if replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("delete replay failed")
	}
	keyedRequest(t, h, "GET", path, "", "", 404)
	item = performJSON[inventory.Item](t, h, "GET", "/api/v1/items/"+p.Items[0].ItemID, "", 200)
	if item.Quantity != 0 {
		t.Fatal(item)
	}
}
