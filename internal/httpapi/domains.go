package httpapi

import (
	"net/http"

	"stockroom/internal/ledger"
	"stockroom/internal/links"
)

func (a *API) registerDomains(m *http.ServeMux) {
	items, finance := a.store.Items(), a.store.Finance()
	m.HandleFunc("POST /api/v1/acquisitions", writeLedger(a, 201, func(r *http.Request, p ledger.AcquisitionInput) (any, bool, error) {
		return items.RecordAcquisition(r.Context(), r.Header.Get("Idempotency-Key"), p)
	}))
	m.HandleFunc("GET /api/v1/acquisitions", func(w http.ResponseWriter, r *http.Request) {
		f, ok := a.historyFilter(w, r)
		if !ok {
			return
		}
		out, e := items.Acquisitions(r.Context(), f)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	m.HandleFunc("GET /api/v1/acquisitions/{id}", func(w http.ResponseWriter, r *http.Request) {
		out, e := items.GetAcquisition(r.Context(), r.PathValue("id"))
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, out)
	})
	m.HandleFunc("PUT /api/v1/acquisitions/{id}", writeLedger(a, 200, func(r *http.Request, p ledger.AcquisitionAmendment) (any, bool, error) {
		return items.AmendAcquisition(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	m.HandleFunc("DELETE /api/v1/acquisitions/{id}", writeLedger(a, 200, func(r *http.Request, p ledger.PurchaseDeletion) (any, bool, error) {
		return items.DeleteAcquisition(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	m.HandleFunc("POST /api/v1/items/{id}/events/maintenance", writeLedger(a, 201, func(r *http.Request, p ledger.MaintenanceRecord) (any, bool, error) {
		return items.RecordMaintenance(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	m.HandleFunc("POST /api/v1/items/{id}/events/sale", writeLedger(a, 200, func(r *http.Request, p ledger.SaleRecord) (any, bool, error) {
		return items.Sell(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	m.HandleFunc("POST /api/v1/items/{id}/events/return", writeLedger(a, 200, func(r *http.Request, p ledger.InventoryInput) (any, bool, error) {
		return items.Return(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	m.HandleFunc("POST /api/v1/finance/refunds", writeLedger(a, 201, func(r *http.Request, p ledger.MoneyInput) (any, bool, error) {
		return finance.Record(r.Context(), r.Header.Get("Idempotency-Key"), "REFUND", p)
	}))
	m.HandleFunc("POST /api/v1/links", writeLedger(a, 201, func(r *http.Request, p ledger.LinkInput) (any, bool, error) {
		return a.store.CreateLink(r.Context(), r.Header.Get("Idempotency-Key"), p)
	}))
	m.HandleFunc("GET /api/v1/links", func(w http.ResponseWriter, r *http.Request) {
		f, ok := a.historyFilter(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		out, e := a.store.DomainLinks(r.Context(), q.Get("source_type"), q.Get("source_id"), q.Get("transaction_id"), f.Limit, f.Offset)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	m.HandleFunc("DELETE /api/v1/links/{id}", func(w http.ResponseWriter, r *http.Request) {
		out, replayed, e := a.store.DeleteLink(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"))
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		replayHeader(w, replayed)
		writeJSON(w, 200, out)
	})
	m.HandleFunc("POST /api/v1/financial/transactions/{id}/acquisition", func(w http.ResponseWriter, r *http.Request) {
		var p ledger.AcquisitionInput
		if e := decodeJSON(w, r, &p); e != nil {
			a.badJSON(w, r, e)
			return
		}
		out, replayed, e := links.Backfill(r.Context(), items, finance, a.store, r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
		if e != nil {
			if out.Acquisition.ID == "" {
				a.ledgerError(w, r, e)
				return
			}
			a.logger.Error("acquisition recorded; link failed", "acquisition_id", out.Acquisition.ID, "error", e)
			writeJSON(w, 503, map[string]any{
				"error": map[string]any{"code": "acquisition_link_incomplete", "message": "购入已记录。用原请求和幂等键重试以补齐关联。", "retryable": true, "acquisition": out.Acquisition},
			})
			return
		}
		replayHeader(w, replayed)
		writeJSON(w, 201, out)
	})
}
