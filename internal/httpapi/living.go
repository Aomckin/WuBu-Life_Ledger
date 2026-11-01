package httpapi

import (
	"net/http"
	"stockroom/internal/ledger"
	"time"
)

func (a *API) registerLiving(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/residences", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.Residences(r.Context())
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	m.HandleFunc("POST /api/v1/residences", writeLedger(a, 201, func(r *http.Request, p ledger.ResidenceInput) (any, bool, error) {
		return a.store.SaveResidence(r.Context(), r.Header.Get("Idempotency-Key"), "", p)
	}))
	m.HandleFunc("PUT /api/v1/residences/{id}", writeLedger(a, 200, func(r *http.Request, p ledger.ResidenceInput) (any, bool, error) {
		return a.store.SaveResidence(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	m.HandleFunc("POST /api/v1/residences/{id}/cost-links", writeLedger(a, 200, func(r *http.Request, p ledger.ResidenceLink) (any, bool, error) {
		return a.store.LinkResidenceCost(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	for _, path := range []string{"cost", "relocation-cost"} {
		m.HandleFunc("GET /api/v1/residences/{id}/"+path, func(w http.ResponseWriter, r *http.Request) {
			out, e := a.store.ResidenceCost(r.Context(), r.PathValue("id"))
			if e != nil {
				a.ledgerError(w, r, e)
				return
			}
			writeJSON(w, 200, out)
		})
	}
	m.HandleFunc("GET /api/v1/residences/{id}/durables", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.ResidenceDurables(r.Context(), r.PathValue("id"))
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	for path, kind := range map[string]string{"deposits": "DEPOSIT", "receivables": "RECEIVABLE"} {
		m.HandleFunc("POST /api/v1/"+path, writeLedger(a, 201, func(r *http.Request, p ledger.ClaimInput) (any, bool, error) {
			return a.store.CreateClaim(r.Context(), r.Header.Get("Idempotency-Key"), kind, p)
		}))
		m.HandleFunc("GET /api/v1/"+path, func(w http.ResponseWriter, r *http.Request) {
			out, e := a.store.Claims(r.Context(), kind)
			if e != nil {
				a.ledgerError(w, r, e)
				return
			}
			writeJSON(w, 200, map[string]any{"items": out})
		})
		for _, action := range []string{"return", "forfeit"} {
			m.HandleFunc("POST /api/v1/"+path+"/{id}/"+action, writeLedger(a, 200, func(r *http.Request, p ledger.ClaimSettlement) (any, bool, error) {
				return a.store.SettleClaim(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), kind, action, p)
			}))
		}
	}
	m.HandleFunc("GET /api/v1/items/{id}/consumption-forecast", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.ConsumptionForecast(r.Context(), r.PathValue("id"), time.Now())
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, out)
	})
	m.HandleFunc("GET /api/v1/items-running-low", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.ItemsRunningLow(r.Context(), time.Now())
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
}
