package httpapi

import (
	"encoding/json"
	"stockroom/internal/inventory"
	"testing"
)

func TestAgentPurchaseSummaryAndStructuredResolverErrors(t *testing.T) {
	h := testHandler(t)
	body := `{"items":[{"name":"无糖可乐","spec":"2L","quantity":1,"unit":"瓶"}]}`
	var data struct {
		ID    string `json:"id"`
		Items []struct {
			ItemID         string  `json:"item_id"`
			InventoryAfter float64 `json:"inventory_after"`
			QuantityAdded  float64 `json:"quantity_added"`
		} `json:"items"`
	}
	first := keyedRequest(t, h, "POST", "/api/v1/acquisitions", body, "stable", 201)
	if e := json.Unmarshal(first.Body.Bytes(), &data); e != nil || data.ID == "" || data.Items[0].InventoryAfter != 1 || data.Items[0].QuantityAdded != 1 {
		t.Fatal(first.Body.String(), e)
	}
	retry := keyedRequest(t, h, "POST", "/api/v1/acquisitions", body, "stable", 201)
	if retry.Body.String() != first.Body.String() || retry.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal(retry.Body.String())
	}
	keyedRequest(t, h, "POST", "/api/v1/acquisitions", `{"items":[{"name":"无糖可乐","spec":"500ml","quantity":1,"unit":"瓶"}]}`, "small", 201)
	errResponse := keyedRequest(t, h, "POST", "/api/v1/items/consume", `{"name":"无糖可乐","quantity":1,"source":"agent"}`, "ambiguous", 409)
	var failure errorBody
	json.Unmarshal(errResponse.Body.Bytes(), &failure)
	if failure.Error.Code != "ITEM_AMBIGUOUS" || failure.Error.Retryable || failure.Error.SuggestedAction != "ask_user_to_select_candidate" || len(failure.Error.Candidates) != 2 {
		t.Fatal(errResponse.Body.String())
	}
	errResponse = keyedRequest(t, h, "POST", "/api/v1/items/consume", `{"name":"没买过","quantity":1,"source":"agent"}`, "missing", 404)
	json.Unmarshal(errResponse.Body.Bytes(), &failure)
	if failure.Error.Code != "ITEM_NOT_FOUND" || failure.Error.SuggestedAction != "create_or_resolve_item" {
		t.Fatal(errResponse.Body.String())
	}
	keyedRequest(t, h, "POST", "/api/v1/items/"+data.Items[0].ItemID+"/aliases", `{"alias":"零度可乐大瓶"}`, "alias", 201)
	item := performJSON[inventory.Item](t, h, "GET", "/api/v1/items/resolve?name=零度可乐大瓶", "", 200)
	if item.ID != data.Items[0].ItemID || item.Spec != "2L" {
		t.Fatal(item)
	}
	keyedRequest(t, h, "POST", "/api/v1/items/consume", `{"name":"零度可乐大瓶","quantity":1,"source":"agent"}`, "drink", 200)
	keyedRequest(t, h, "POST", "/api/v1/acquisitions", `{"items":[{"name":"非法","spec":"2L","quantity":1,"unit":"瓶","price":5.555}]}`, "invalid", 400)
}
