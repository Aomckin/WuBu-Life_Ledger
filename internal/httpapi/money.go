package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"stockroom/internal/ledger"
	"stockroom/internal/store"
)

// writeLedger shares strict decoding, errors and replay headers for high-level events.
func writeLedger[T any](a *API, status int, fn func(*http.Request, T) (any, bool, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p T
		if e := decodeJSON(w, r, &p); e != nil {
			a.badJSON(w, r, e)
			return
		}
		out, replay, e := fn(r, p)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		replayHeader(w, replay)
		writeJSON(w, status, out)
	}
}
func (a *API) registerMoney(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/accounts", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.Accounts(r.Context())
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	m.HandleFunc("POST /api/v1/accounts", writeLedger(a, 201, func(r *http.Request, p ledger.AccountInput) (any, bool, error) {
		return a.store.CreateAccount(r.Context(), r.Header.Get("Idempotency-Key"), p)
	}))
	m.HandleFunc("GET /api/v1/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.GetAccount(r.Context(), r.PathValue("id"))
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, out)
	})
	m.HandleFunc("PATCH /api/v1/accounts/{id}", writeLedger(a, 200, func(r *http.Request, p ledger.AccountPatch) (any, bool, error) {
		return a.store.PatchAccount(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	m.HandleFunc("GET /api/v1/money/balance", func(w http.ResponseWriter, r *http.Request) {
		if id := r.URL.Query().Get("account_id"); id != "" {
			out, e := a.store.GetAccount(r.Context(), id)
			if e != nil {
				a.ledgerError(w, r, e)
				return
			}
			writeJSON(w, 200, out)
			return
		}
		out, e := a.store.Balances(r.Context())
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, out)
	})
	m.HandleFunc("GET /api/v1/money/settings", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.MoneySettings(r.Context())
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, out)
	})
	m.HandleFunc("PATCH /api/v1/money/settings", writeLedger(a, 200, func(r *http.Request, p ledger.MoneySettings) (any, bool, error) {
		return a.store.SetMoneySettings(r.Context(), r.Header.Get("Idempotency-Key"), p)
	}))
	for path, kind := range map[string]string{"expenses": "EXPENSE", "income": "INCOME", "transfers": "TRANSFER", "refunds": "REFUND", "adjustments": "ADJUSTMENT", "reconcile": "ADJUSTMENT"} {
		m.HandleFunc("POST /api/v1/money/"+path, writeLedger(a, 201, func(r *http.Request, p ledger.MoneyInput) (any, bool, error) {
			return a.store.MoneyAction(r.Context(), r.Header.Get("Idempotency-Key"), kind, p)
		}))
	}
	m.HandleFunc("GET /api/v1/money/transactions", func(w http.ResponseWriter, r *http.Request) {
		f, ok := a.moneyFilter(w, r)
		if !ok {
			return
		}
		out, e := a.store.Transactions(r.Context(), f)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		historyJSON(w, f.HistoryFilter, out)
	})
	for _, path := range []string{"summary", "expense-summary", "income-summary", "category-summary", "merchant-summary"} {
		m.HandleFunc("GET /api/v1/money/"+path, func(w http.ResponseWriter, r *http.Request) {
			f, ok := a.moneyFilter(w, r)
			if !ok {
				return
			}
			switch path {
			case "income-summary":
				f.Type = "INCOME"
			case "category-summary":
				f.GroupBy = "category"
			case "merchant-summary":
				f.GroupBy = "merchant"
			}
			out, e := a.store.MoneySummary(r.Context(), f)
			if e != nil {
				a.ledgerError(w, r, e)
				return
			}
			writeJSON(w, 200, out)
		})
	}
	m.HandleFunc("GET /api/v1/money/categories", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.MoneyCategories(r.Context())
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	m.HandleFunc("POST /api/v1/money/categories", writeLedger(a, 200, func(r *http.Request, p store.CategoryInput) (any, bool, error) {
		return a.store.SaveMoneyCategory(r.Context(), r.Header.Get("Idempotency-Key"), p)
	}))
	m.HandleFunc("GET /api/v1/durables", func(w http.ResponseWriter, r *http.Request) {
		f, ok := a.moneyFilter(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		out, e := a.store.Durables(r.Context(), q.Get("status"), q.Get("location"), q.Get("search"), f.HistoryFilter)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		historyJSON(w, f.HistoryFilter, out)
	})
	m.HandleFunc("GET /api/v1/items/{id}/durable-events", func(w http.ResponseWriter, r *http.Request) {
		f, ok := a.historyFilter(w, r)
		if !ok {
			return
		}
		if _, e := a.store.DurableDetails(r.Context(), f.ItemID); e != nil {
			a.ledgerError(w, r, e)
			return
		}
		out, e := a.store.DurableEvents(r.Context(), f)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		historyJSON(w, f, out)
	})
	m.HandleFunc("GET /api/v1/items/{id}/total-cost", func(w http.ResponseWriter, r *http.Request) {
		f, ok := a.moneyFilter(w, r)
		if !ok {
			return
		}
		out, e := a.store.ItemCost(r.Context(), r.PathValue("id"), f.HistoryFilter)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, out)
	})
	for _, action := range []string{"sell", "retire", "move", "status"} {
		m.HandleFunc("POST /api/v1/items/{id}/"+action, writeLedger(a, 200, func(r *http.Request, p ledger.DurableActionInput) (any, bool, error) {
			return a.store.DurableAction(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), action, p)
		}))
	}
	m.HandleFunc("POST /api/v1/money/import/preview", writeLedger(a, 200, func(r *http.Request, p store.MoneyImportInput) (any, bool, error) {
		out, e := a.store.PreviewMoneyImport(r.Context(), p)
		return out, false, e
	}))
	m.HandleFunc("POST /api/v1/money/import", writeLedger(a, 201, func(r *http.Request, p store.MoneyImportInput) (any, bool, error) {
		return a.store.ImportMoney(r.Context(), r.Header.Get("Idempotency-Key"), p)
	}))
}
func (a *API) moneyFilter(w http.ResponseWriter, r *http.Request) (ledger.MoneyFilter, bool) {
	h, ok := a.historyFilter(w, r)
	if !ok {
		return ledger.MoneyFilter{}, false
	}
	q := r.URL.Query()
	from, to, e := ledger.PeriodRange(q.Get("period"), h.From, h.To, time.Now())
	if e != nil {
		a.ledgerError(w, r, e)
		return ledger.MoneyFilter{}, false
	}
	h.From, h.To = from, to
	f := ledger.MoneyFilter{HistoryFilter: h, AccountID: q.Get("account_id"), Type: q.Get("type"), Merchant: q.Get("merchant"), GroupBy: q.Get("group_by"), ToAccount: q.Get("to_account")}
	for _, v := range []*string{&f.AccountID, &f.ToAccount} {
		if *v != "" {
			acc, e := a.store.GetAccount(r.Context(), *v)
			if e != nil {
				a.ledgerError(w, r, e)
				return f, false
			}
			*v = acc.ID
		}
	}
	if len(f.Merchant) > 200 || len(f.Type) > 30 {
		a.ledgerError(w, r, fmt.Errorf("%w: filter too long", ledger.ErrInvalid))
		return f, false
	}
	return f, true
}
