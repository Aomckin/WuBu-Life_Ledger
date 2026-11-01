package httpapi

import (
	"net/http"
	"stockroom/internal/ledger"
	"time"
)

func (a *API) registerRunway(m *http.ServeMux) {
	a.registerLiving(m)
	m.HandleFunc("POST /api/v1/planned-cashflows", writeLedger(a, 201, func(r *http.Request, p ledger.PlannedCashflowInput) (any, bool, error) {
		return a.store.SavePlannedCashflow(r.Context(), r.Header.Get("Idempotency-Key"), "", p)
	}))
	m.HandleFunc("PUT /api/v1/planned-cashflows/{id}", writeLedger(a, 200, func(r *http.Request, p ledger.PlannedCashflowInput) (any, bool, error) {
		return a.store.SavePlannedCashflow(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	for _, action := range []string{"complete", "cancel", "skip"} {
		m.HandleFunc("POST /api/v1/planned-cashflows/{id}/"+action, writeLedger(a, 200, func(r *http.Request, p ledger.PlannedAction) (any, bool, error) {
			return a.store.ActPlannedCashflow(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), action, p)
		}))
	}
	m.HandleFunc("GET /api/v1/planned-cashflows", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.UpcomingCashflows(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	m.HandleFunc("GET /api/v1/runway", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.LivingRunway(r.Context(), time.Now())
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, out)
	})
	m.HandleFunc("GET /api/v1/runway/settings", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.RunwaySettings(r.Context())
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, out)
	})
	m.HandleFunc("PATCH /api/v1/runway/settings", writeLedger(a, 200, func(r *http.Request, p ledger.RunwaySettings) (any, bool, error) {
		return a.store.SetRunwaySettings(r.Context(), r.Header.Get("Idempotency-Key"), p)
	}))
	m.HandleFunc("PATCH /api/v1/money/transactions/{id}/nature", writeLedger(a, 200, func(r *http.Request, p ledger.ExpenseNatureInput) (any, bool, error) {
		return a.store.SetExpenseNature(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	m.HandleFunc("GET /api/v1/money/spending-by-nature", func(w http.ResponseWriter, r *http.Request) {
		f, ok := a.moneyFilter(w, r)
		if !ok {
			return
		}
		out, e := a.store.SpendingByNature(r.Context(), f)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, out)
	})
}
