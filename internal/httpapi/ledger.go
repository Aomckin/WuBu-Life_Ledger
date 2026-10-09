package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"stockroom/internal/ledger"
	"stockroom/internal/store"
)

func (a *API) registerLedger(m *http.ServeMux) {
	a.registerMoney(m)
	m.HandleFunc("POST /api/v1/purchases", a.recordPurchase)
	m.HandleFunc("POST /api/v1/purchases/record", a.agentPurchase(false))
	m.HandleFunc("POST /api/v1/purchases/validate", a.agentPurchase(true))
	for _, action := range []string{"consume", "adjust", "open", "finish", "discard"} {
		m.HandleFunc("POST /api/v1/items/"+action, a.inventoryAction(strings.ToUpper(action)))
	}
	m.HandleFunc("GET /api/v1/purchases", a.listPurchases)
	m.HandleFunc("GET /api/v1/purchases/{id}", a.getPurchase)
	m.HandleFunc("PUT /api/v1/purchases/{id}", a.amendPurchase)
	m.HandleFunc("DELETE /api/v1/purchases/{id}", a.deletePurchase)
	m.HandleFunc("GET /api/v1/inventory", a.listItems)
	m.HandleFunc("GET /api/v1/inventory-events", a.inventoryHistory)
	m.HandleFunc("GET /api/v1/items/resolve", a.resolveItem)
	m.HandleFunc("GET /api/v1/items/{id}/inventory-events", a.inventoryHistory)
	m.HandleFunc("GET /api/v1/items/{id}/purchases", a.listPurchases)
	m.HandleFunc("GET /api/v1/items/{id}/prices", a.priceHistory)
	m.HandleFunc("POST /api/v1/items/{id}/consume", a.inventoryAction("CONSUME"))
	m.HandleFunc("POST /api/v1/items/{id}/open", a.inventoryAction("OPEN"))
	m.HandleFunc("POST /api/v1/items/{id}/finish", a.inventoryAction("FINISH"))
	m.HandleFunc("POST /api/v1/items/{id}/discard", a.inventoryAction("DISCARD"))
	m.HandleFunc("POST /api/v1/items/{id}/maintenance", a.addMaintenance)
	m.HandleFunc("GET /api/v1/items/{id}/maintenance", a.maintenanceHistory)
	m.HandleFunc("GET /api/v1/maintenance", a.maintenanceHistory)
	m.HandleFunc("GET /api/v1/items/{id}/durable", a.getDurable)
	m.HandleFunc("PATCH /api/v1/items/{id}/durable", a.patchDurable)
	m.HandleFunc("POST /api/v1/items/{id}/aliases", a.addAlias)
	m.HandleFunc("GET /api/v1/items/{id}/aliases", a.listAliases)
	m.HandleFunc("GET /api/v1/expenses", a.listExpenses)
	m.HandleFunc("GET /api/v1/ledger/summary", a.ledgerSummary)
	// Retain the mother project's editable inventory at /inventory.
	m.HandleFunc("GET /inventory", a.serveInventory)
}

func (a *API) ledgerError(w http.ResponseWriter, r *http.Request, e error) {
	var resolution *store.ResolutionError
	if errors.As(e, &resolution) {
		status, action := 404, "create_or_resolve_item"
		if resolution.Code == "ITEM_AMBIGUOUS" {
			status, action = 409, "ask_user_to_select_candidate"
		}
		writeJSON(w, status, errorBody{Error: errorDetail{Code: resolution.Code, Message: resolution.Message, SuggestedAction: action, Candidates: resolution.Candidates}, RequestID: requestID(r.Context())})
		return
	}
	switch {
	case errors.Is(e, ledger.ErrInvalid):
		a.errorResponse(w, r, 422, "validation_error", e.Error(), nil)
	case errors.Is(e, ledger.ErrIdempotency):
		a.errorResponse(w, r, 409, "idempotency_conflict", e.Error(), nil)
	case errors.Is(e, ledger.ErrConflict):
		a.errorResponse(w, r, 409, "ledger_conflict", e.Error(), nil)
	default:
		a.handleStoreError(w, r, e)
	}
}
func replayHeader(w http.ResponseWriter, replay bool) {
	if replay {
		w.Header().Set("Idempotency-Replayed", "true")
	}
}

func (a *API) agentPurchase(validate bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input ledger.AgentPurchaseInput
		if e := decodeJSON(w, r, &input); e != nil {
			a.badJSON(w, r, e)
			return
		}
		var out ledger.Purchase
		var replay bool
		var e error
		if validate {
			out, e = a.store.ValidatePurchase(r.Context(), input.PurchaseInput())
		} else {
			out, replay, e = a.store.RecordPurchase(r.Context(), r.Header.Get("Idempotency-Key"), input.PurchaseInput())
		}
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		replayHeader(w, replay)
		status := 201
		if validate {
			status = 200
		}
		writeJSON(w, status, map[string]any{"account_id": out.AccountID, "status": out.Status, "purchase_id": out.ID, "expense_amount": out.TotalAmount, "items": out.Items, "warnings": out.Warnings, "validation_status": out.ValidationStatus})
	}
}
func (a *API) recordPurchase(w http.ResponseWriter, r *http.Request) {
	var input ledger.PurchaseInput
	if e := decodeJSON(w, r, &input); e != nil {
		a.badJSON(w, r, e)
		return
	}
	if r.URL.Query().Get("dry_run") == "true" {
		out, e := a.store.ValidatePurchase(r.Context(), input)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, out)
		return
	}
	out, replay, e := a.store.RecordPurchase(r.Context(), r.Header.Get("Idempotency-Key"), input)
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	replayHeader(w, replay)
	w.Header().Set("Location", "/api/v1/purchases/"+out.ID)
	writeJSON(w, 201, out)
}
func (a *API) getPurchase(w http.ResponseWriter, r *http.Request) {
	out, e := a.store.GetPurchase(r.Context(), r.PathValue("id"))
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	writeJSON(w, 200, out)
}
func (a *API) amendPurchase(w http.ResponseWriter, r *http.Request) {
	var input ledger.PurchaseAmendment
	if e := decodeJSON(w, r, &input); e != nil {
		a.badJSON(w, r, e)
		return
	}
	out, replay, e := a.store.AmendPurchase(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), input)
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	replayHeader(w, replay)
	writeJSON(w, 200, out)
}
func (a *API) deletePurchase(w http.ResponseWriter, r *http.Request) {
	var input ledger.PurchaseDeletion
	if e := decodeJSON(w, r, &input); e != nil {
		a.badJSON(w, r, e)
		return
	}
	out, replay, e := a.store.DeletePurchase(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), input)
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	replayHeader(w, replay)
	writeJSON(w, 200, out)
}
func (a *API) inventoryAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input ledger.InventoryInput
		if e := decodeJSON(w, r, &input); e != nil {
			a.badJSON(w, r, e)
			return
		}
		out, replay, e := a.store.InventoryAction(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), action, input)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		replayHeader(w, replay)
		writeJSON(w, 200, out)
	}
}
func (a *API) historyFilter(w http.ResponseWriter, r *http.Request) (ledger.HistoryFilter, bool) {
	q := r.URL.Query()
	f := ledger.HistoryFilter{ItemID: r.PathValue("id"), From: q.Get("from"), To: q.Get("to"), Category: q.Get("category"), Limit: 100}
	if f.ItemID == "" {
		f.ItemID = q.Get("item_id")
	}
	fields := map[string]string{}
	for _, v := range []struct{ key, value string }{{"from", f.From}, {"to", f.To}} {
		if v.value != "" {
			if _, e := time.Parse(time.RFC3339Nano, v.value); e != nil {
				fields[v.key] = "Use RFC3339 timestamp including timezone."
			}
		}
	}
	if len(f.Category) > 100 {
		fields["category"] = "Category too long."
	}
	if f.From != "" && f.To != "" {
		from, e1 := time.Parse(time.RFC3339Nano, f.From)
		to, e2 := time.Parse(time.RFC3339Nano, f.To)
		if e1 == nil && e2 == nil && !from.Before(to) {
			fields["to"] = "to must be later than from (exclusive)."
		}
	}
	if q.Get("limit") != "" {
		v, e := strconv.Atoi(q.Get("limit"))
		if e != nil || v < 1 || v > 500 {
			fields["limit"] = "Use 1 to 500."
		} else {
			f.Limit = v
		}
	}
	if q.Get("offset") != "" {
		v, e := strconv.Atoi(q.Get("offset"))
		if e != nil || v < 0 {
			fields["offset"] = "Use a nonnegative integer."
		} else {
			f.Offset = v
		}
	}
	if len(fields) > 0 {
		a.validationError(w, r, fields)
		return f, false
	}
	return f, true
}
func historyJSON(w http.ResponseWriter, f ledger.HistoryFilter, items any) {
	writeJSON(w, 200, map[string]any{"items": items, "limit": f.Limit, "offset": f.Offset})
}
func (a *API) listPurchases(w http.ResponseWriter, r *http.Request) {
	f, ok := a.historyFilter(w, r)
	if !ok {
		return
	}
	out, e := a.store.Purchases(r.Context(), f)
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	historyJSON(w, f, out)
}
func (a *API) inventoryHistory(w http.ResponseWriter, r *http.Request) {
	f, ok := a.historyFilter(w, r)
	if !ok {
		return
	}
	if _, e := a.store.ApplyDueAutoConsumption(r.Context()); e != nil {
		a.ledgerError(w, r, e)
		return
	}
	out, e := a.store.InventoryEvents(r.Context(), f)
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	historyJSON(w, f, out)
}
func (a *API) priceHistory(w http.ResponseWriter, r *http.Request) {
	f, ok := a.historyFilter(w, r)
	if !ok {
		return
	}
	if _, e := a.store.Get(r.Context(), f.ItemID); e != nil {
		a.ledgerError(w, r, e)
		return
	}
	out, e := a.store.PriceHistory(r.Context(), f)
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	historyJSON(w, f, out)
}
func (a *API) listExpenses(w http.ResponseWriter, r *http.Request) {
	f, ok := a.historyFilter(w, r)
	if !ok {
		return
	}
	out, e := a.store.Expenses(r.Context(), f)
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	total, e := a.store.ExpenseTotal(r.Context(), f)
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": out, "total_amount": total, "currency": "CNY", "limit": f.Limit, "offset": f.Offset})
}
func (a *API) addMaintenance(w http.ResponseWriter, r *http.Request) {
	var p ledger.MaintenanceInput
	if e := decodeJSON(w, r, &p); e != nil {
		a.badJSON(w, r, e)
		return
	}
	out, replay, e := a.store.AddMaintenance(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	replayHeader(w, replay)
	writeJSON(w, 201, out)
}
func (a *API) maintenanceHistory(w http.ResponseWriter, r *http.Request) {
	f, ok := a.historyFilter(w, r)
	if !ok {
		return
	}
	out, e := a.store.MaintenanceHistory(r.Context(), f)
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	historyJSON(w, f, out)
}
func (a *API) getDurable(w http.ResponseWriter, r *http.Request) {
	out, e := a.store.DurableDetails(r.Context(), r.PathValue("id"))
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	writeJSON(w, 200, out)
}
func (a *API) patchDurable(w http.ResponseWriter, r *http.Request) {
	var p ledger.DurablePatch
	if e := decodeJSON(w, r, &p); e != nil {
		a.badJSON(w, r, e)
		return
	}
	out, replay, e := a.store.UpdateDurable(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	replayHeader(w, replay)
	writeJSON(w, 200, out)
}
func (a *API) addAlias(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Alias string `json:"alias"`
	}
	if e := decodeJSON(w, r, &p); e != nil {
		a.badJSON(w, r, e)
		return
	}
	out, replay, e := a.store.AddAlias(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p.Alias)
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	replayHeader(w, replay)
	writeJSON(w, 201, out)
}
func (a *API) listAliases(w http.ResponseWriter, r *http.Request) {
	out, e := a.store.Aliases(r.Context(), r.PathValue("id"))
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
func (a *API) resolveItem(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" || len([]rune(name)) > 100 {
		a.ledgerError(w, r, fmt.Errorf("%w: name required (max 100 chars)", ledger.ErrInvalid))
		return
	}
	if _, e := a.store.ApplyDueAutoConsumption(r.Context()); e != nil {
		a.ledgerError(w, r, e)
		return
	}
	out, e := a.store.ResolveItem(r.Context(), name, r.URL.Query().Get("spec"))
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	writeJSON(w, 200, out)
}
func (a *API) ledgerSummary(w http.ResponseWriter, r *http.Request) {
	out, e := a.store.Dashboard(r.Context(), r.URL.Query().Get("month"))
	if e != nil {
		a.ledgerError(w, r, e)
		return
	}
	writeJSON(w, 200, out)
}
func (a *API) serveInventory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	w.Write(a.inventoryPage)
}
