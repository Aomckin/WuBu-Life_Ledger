package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"stockroom/internal/ledger"
	"strconv"
	"strings"
)

func (a *API) registerFinancial(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/financial/settings", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.FinancialSettings(r.Context())
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, out)
	})
	m.HandleFunc("PATCH /api/v1/financial/settings", writeFinancial(a, 200, func(r *http.Request, p ledger.FinancialSettings) (any, bool, error) {
		return a.store.SetFinancialSettings(r.Context(), r.Header.Get("Idempotency-Key"), p)
	}))

	m.HandleFunc("GET /financial-inbox", func(w http.ResponseWriter, r *http.Request) {
		b, e := a.staticFS.Open("financial.html")
		if e != nil {
			http.NotFound(w, r)
			return
		}
		defer b.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, e := io.ReadAll(b)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		w.Write(data)
	})
	m.HandleFunc("POST /api/v1/financial/semantic-evidence", writeFinancial(a, 201, func(r *http.Request, p ledger.SemanticEvidence) (any, bool, error) {
		return a.store.AttachSemanticEvidence(r.Context(), r.Header.Get("Idempotency-Key"), p)
	}))
	m.HandleFunc("POST /api/v1/financial/merchant-rules", writeFinancial(a, 201, func(r *http.Request, p ledger.MerchantRule) (any, bool, error) {
		return a.store.SaveMerchantRule(r.Context(), r.Header.Get("Idempotency-Key"), "", p)
	}))
	m.HandleFunc("PUT /api/v1/financial/merchant-rules/{id}", writeFinancial(a, 200, func(r *http.Request, p ledger.MerchantRule) (any, bool, error) {
		return a.store.SaveMerchantRule(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	for _, kind := range []string{"semantic-evidence", "merchant-rules"} {
		m.HandleFunc("GET /api/v1/financial/"+kind, func(w http.ResponseWriter, r *http.Request) {
			out, e := a.store.FinancialAuxiliary(r.Context(), kind)
			if e != nil {
				a.ledgerError(w, r, e)
				return
			}
			writeJSON(w, 200, map[string]any{"items": out})
		})
	}
	m.HandleFunc("POST /api/v1/financial/events", writeFinancial(a, 201, func(r *http.Request, p ledger.RawFinancialInput) (any, bool, error) {
		if p.EvidenceKey != "" || p.ImportBatchID != "" || p.RowNumber != 0 {
			return nil, false, ledger.ErrInvalid
		}
		return a.store.CaptureFinancial(r.Context(), r.Header.Get("Idempotency-Key"), p)
	}))
	m.HandleFunc("POST /api/v1/financial/events/{id}/review", writeFinancial(a, 200, func(r *http.Request, p ledger.FinancialReview) (any, bool, error) {
		return a.store.ReviewFinancial(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	m.HandleFunc("PUT /api/v1/financial/transactions/{id}/annotation", writeFinancial(a, 200, func(r *http.Request, p ledger.TransactionAnnotation) (any, bool, error) {
		return a.store.AnnotateTransaction(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), p)
	}))
	m.HandleFunc("GET /api/v1/financial/events", func(w http.ResponseWriter, r *http.Request) {
		h, ok := a.historyFilter(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		out, e := a.store.RawFinancialEvents(r.Context(), q.Get("status"), ledger.RawFinancialQuery{HistoryFilter: h, Provider: q.Get("provider"), CaptureSource: q.Get("capture_source"), Merchant: q.Get("merchant")})
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	m.HandleFunc("GET /api/v1/financial/transactions", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		window := 30
		var e error
		if q.Get("window_minutes") != "" {
			window, e = strconv.Atoi(q.Get("window_minutes"))
			if e != nil {
				a.ledgerError(w, r, ledger.ErrInvalid)
				return
			}
		}
		f, ok := a.moneyFilter(w, r)
		if !ok {
			return
		}
		filter := ledger.FinancialQuery{MoneyFilter: f, AnnotationStatus: q.Get("annotation_status")}
		if q.Get("amount") != "" {
			var amount ledger.Money
			if e = json.Unmarshal([]byte(q.Get("amount")), &amount); e != nil || amount <= 0 {
				a.ledgerError(w, r, ledger.ErrInvalid)
				return
			}
			filter.Amount = &amount
		}
		out, e := a.store.FinancialTransactions(r.Context(), q.Get("occurred_at"), window, q.Get("unannotated_only") == "true", filter)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		response := map[string]any{"items": out}
		if q.Get("occurred_at") != "" {
			candidates := filter
			candidates.Limit = 2
			candidates.Offset = 0
			matches, e := a.store.FinancialTransactions(r.Context(), q.Get("occurred_at"), window, q.Get("unannotated_only") == "true", candidates)
			if e != nil {
				a.ledgerError(w, r, e)
				return
			}
			status := "NO_MATCH"
			if len(matches) == 1 {
				status = "CANDIDATE_FOUND"
			} else if len(matches) > 1 {
				status = "REVIEW_NEEDED"
			}
			response["match_status"] = status
		}
		writeJSON(w, 200, response)
	})
	m.HandleFunc("GET /api/v1/financial/reconciliation", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.AccountReconciliation(r.Context(), r.URL.Query().Get("account_id"))
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	m.HandleFunc("GET /api/v1/financial/inbox", func(w http.ResponseWriter, r *http.Request) {
		h, ok := a.historyFilter(w, r)
		if !ok {
			return
		}
		events, e := a.store.RawFinancialEvents(r.Context(), "", ledger.RawFinancialQuery{HistoryFilter: h})
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		items, e := a.store.FinancialTransactions(r.Context(), "", 1440, true, ledger.FinancialQuery{MoneyFilter: ledger.MoneyFilter{HistoryFilter: h}})
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		review, e := a.store.FinancialTransactions(r.Context(), "", 1440, false, ledger.FinancialQuery{MoneyFilter: ledger.MoneyFilter{HistoryFilter: h}, AnnotationStatus: "REVIEW_NEEDED"})
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"events": events, "unannotated": items, "annotation_review": review})
	})
}

func writeFinancial[T any](a *API, status int, fn func(*http.Request, T) (any, bool, error)) http.HandlerFunc {
	next := writeLedger(a, status, fn)
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
			a.ledgerError(w, r, ledger.ErrInvalid)
			return
		}
		next(w, r)
	}
}
