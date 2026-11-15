package httpapi

import (
	"io"
	"mime"
	"net/http"
	"stockroom/internal/ledger"
	"stockroom/internal/statement"
	"strconv"
	"strings"
)

func (a *API) registerStatements(m *http.ServeMux) {
	m.HandleFunc("GET /weekly-sync", func(w http.ResponseWriter, r *http.Request) {
		f, e := a.staticFS.Open("weekly.html")
		if e != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.Copy(w, f)
	})
	m.HandleFunc("GET /api/v1/statements/templates", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"items": statement.Templates, "parser_version": statement.Version})
	})
	for _, action := range []string{"preview", "commit"} {
		m.HandleFunc("POST /api/v1/statements/{provider}/"+action, func(w http.ResponseWriter, r *http.Request) {
			var p ledger.StatementInput
			if e := decodeJSONLimit(w, r, &p, 7<<20); e != nil {
				a.badJSON(w, r, e)
				return
			}
			if action == "preview" {
				out, e := a.store.PreviewStatement(r.Context(), r.PathValue("provider"), p)
				if e != nil {
					a.ledgerError(w, r, e)
					return
				}
				writeJSON(w, 200, out)
				return
			}
			if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
				a.ledgerError(w, r, ledger.ErrInvalid)
				return
			}
			out, replay, e := a.store.CommitStatement(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("provider"), p)
			if e != nil {
				a.ledgerError(w, r, e)
				return
			}
			replayHeader(w, replay)
			writeJSON(w, 201, out)
		})
	}
	m.HandleFunc("GET /api/v1/statements/batches", func(w http.ResponseWriter, r *http.Request) {
		limit, offset := 50, 0
		var e error
		if v := r.URL.Query().Get("limit"); v != "" {
			limit, e = strconv.Atoi(v)
			if e != nil {
				a.ledgerError(w, r, ledger.ErrInvalid)
				return
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			offset, e = strconv.Atoi(v)
			if e != nil {
				a.ledgerError(w, r, ledger.ErrInvalid)
				return
			}
		}
		out, e := a.store.StatementBatches(r.Context(), r.URL.Query().Get("sync_id"), limit, offset)
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	m.HandleFunc("GET /api/v1/statements/summary", func(w http.ResponseWriter, r *http.Request) {
		out, e := a.store.StatementSummary(r.Context(), r.URL.Query().Get("sync_id"))
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		writeJSON(w, 200, out)
	})
	m.HandleFunc("GET /api/v1/statements/batches/{id}/file", func(w http.ResponseWriter, r *http.Request) {
		name, data, e := a.store.StatementFile(r.Context(), r.PathValue("id"))
		if e != nil {
			a.ledgerError(w, r, e)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		w.WriteHeader(200)
		_, _ = w.Write(data)
	})
}
