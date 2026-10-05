package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"stockroom/internal/ai"
	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
	"stockroom/internal/media"
	"stockroom/internal/store"
	"stockroom/internal/web"
)

const maxBodyBytes = 1 << 20

type API struct {
	store         *store.Store
	version       string
	logger        *slog.Logger
	static        http.Handler
	staticFS      fs.FS
	index         []byte
	inventoryPage []byte
	inbox         []byte
	quicklist     []byte
	openAPI       []byte
	categories    map[string]bool
	analyzer      ai.Analyzer
	media         *media.Processor
	analysisMu    sync.Mutex
}

type errorBody struct {
	Error     errorDetail `json:"error"`
	RequestID string      `json:"request_id"`
}

type errorDetail struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type contextKey string

const requestIDKey contextKey = "request_id"

func New(database *store.Store, version string, logger *slog.Logger, analyzer ai.Analyzer) (http.Handler, error) {
	if client, ok := analyzer.(*ai.Client); ok && client == nil {
		analyzer = nil
	}
	staticFS, err := web.Static()
	if err != nil {
		return nil, fmt.Errorf("open embedded assets: %w", err)
	}
	index, err := web.ReadFile("index.html")
	if err != nil {
		return nil, fmt.Errorf("read embedded index: %w", err)
	}
	ledgerPage, err := web.ReadFile("ledger.html")
	if err != nil {
		return nil, err
	}
	inbox, err := web.ReadFile("inbox.html")
	if err != nil {
		return nil, fmt.Errorf("read embedded inbox: %w", err)
	}
	quicklist, err := web.ReadFile("quicklist.html")
	if err != nil {
		return nil, fmt.Errorf("read embedded quicklist: %w", err)
	}
	openAPI, err := web.ReadFile("openapi.json")
	if err != nil {
		return nil, fmt.Errorf("read embedded OpenAPI document: %w", err)
	}
	categories := make(map[string]bool, len(inventory.Categories))
	for _, category := range inventory.Categories {
		categories[category.Value] = true
	}

	var transcriber ai.Transcriber
	if configured, ok := analyzer.(ai.Transcriber); ok {
		transcriber = configured
	}
	api := &API{
		store: database, version: version, logger: logger,
		static: http.FileServer(http.FS(staticFS)), staticFS: staticFS,
		index: ledgerPage, inventoryPage: index, inbox: inbox, quicklist: quicklist, openAPI: openAPI, categories: categories,
		analyzer: analyzer, media: media.New(transcriber),
	}
	mux := http.NewServeMux()
	api.registerLedger(mux)
	mux.HandleFunc("GET /healthz", api.health)
	mux.HandleFunc("GET /api/v1", api.apiRoot)
	mux.HandleFunc("GET /api/v1/meta", api.meta)
	mux.HandleFunc("GET /api/v1/summary", api.summary)
	mux.HandleFunc("GET /api/v1/openapi.json", api.openAPIDocument)
	mux.HandleFunc("GET /api/v1/items", api.listItems)
	mux.HandleFunc("POST /api/v1/items", api.createItem)
	mux.HandleFunc("GET /api/v1/items/{id}", api.getItem)
	mux.HandleFunc("PATCH /api/v1/items/{id}", api.updateItem)
	mux.HandleFunc("DELETE /api/v1/items/{id}", api.deleteItem)
	mux.HandleFunc("POST /api/v1/items/{id}/adjust", api.adjustItem)
	mux.HandleFunc("GET /api/v1/intake", api.listIntakes)
	mux.HandleFunc("POST /api/v1/intake", api.createIntake)
	mux.HandleFunc("POST /api/v1/intake/batches", api.createIntakeBatch)
	mux.HandleFunc("GET /api/v1/intake/batches/{id}", api.getIntakeBatch)
	mux.HandleFunc("POST /api/v1/intake/batches/{id}/analyze", api.analyzeIntakeBatch)
	mux.HandleFunc("POST /api/v1/intake/batches/{id}/apply-ready", api.applyReadyIntakeBatch)
	mux.HandleFunc("POST /api/v1/intake/apply-ready", api.applyAllReadyIntakes)
	mux.HandleFunc("GET /api/v1/intake/settings", api.intakeSettings)
	mux.HandleFunc("PATCH /api/v1/intake/settings", api.updateIntakeSettings)
	mux.HandleFunc("GET /api/v1/intake/{id}", api.getIntake)
	mux.HandleFunc("PATCH /api/v1/intake/{id}", api.updateIntake)
	mux.HandleFunc("POST /api/v1/intake/{id}/resolve", api.resolveIntake)
	mux.HandleFunc("POST /api/v1/intake/analyze-all", api.analyzeAllIntakes)
	mux.HandleFunc("POST /api/v1/intake/add-all", api.addAllIntakes)
	mux.HandleFunc("POST /api/v1/intake/analyze-and-add-all", api.analyzeAndAddAllIntakes)
	mux.HandleFunc("POST /api/v1/intake/{id}/analysis", api.analyzeIntake)
	mux.HandleFunc("GET /api/v1/intake/{id}/attachments/{attachment_id}", api.getIntakeAttachment)
	mux.HandleFunc("/api/", api.apiNotFound)
	mux.HandleFunc("GET /kiquicklist", api.serveQuicklist)
	mux.HandleFunc("GET /inbox", api.serveInbox)
	mux.HandleFunc("/", api.serveApp)
	return api.middleware(mux), nil
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
		a.logger.Error("health check failed", "error", err, "request_id", requestID(r.Context()))
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unavailable", "database": "unavailable", "version": a.version,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "database": "ok", "version": a.version})
}

func (a *API) apiRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "物簿 Life Ledger API", "version": "v1", "app_version": "0.1.0", "purchases": "/api/v1/purchases", "expenses": "/api/v1/expenses", "inventory_events": "/api/v1/inventory-events", "maintenance": "/api/v1/maintenance", "ledger_summary": "/api/v1/ledger/summary",
		"items": "/api/v1/items", "intake": "/api/v1/intake", "summary": "/api/v1/summary",
		"analysis": "/api/v1/intake/{id}/analysis", "batches": "/api/v1/intake/batches", "apply_ready": "/api/v1/intake/apply-ready", "inbox_settings": "/api/v1/intake/settings", "analyze_all": "/api/v1/intake/analyze-all", "add_all": "/api/v1/intake/add-all", "analyze_and_add_all": "/api/v1/intake/analyze-and-add-all", "metadata": "/api/v1/meta", "openapi": "/api/v1/openapi.json",
	})
}

func (a *API) meta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"categories": inventory.Categories, "suggested_units": inventory.SuggestedUnits,
		"suggested_locations": inventory.SuggestedLocations,
		"limits": map[string]int{
			"name": inventory.MaxNameLength, "location": inventory.MaxLocationLength,
			"unit": inventory.MaxUnitLength, "notes": inventory.MaxNotesLength,
			"unit_price_cents": inventory.MaxUnitPriceCents,
		},
	})
}

func (a *API) summary(w http.ResponseWriter, r *http.Request) {
	result, err := a.store.Summary(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) openAPIDocument(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.oai.openapi+json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(a.openAPI)
}

func (a *API) listItems(w http.ResponseWriter, r *http.Request) {
	filter, fields := a.parseListFilter(r)
	if len(fields) > 0 {
		a.validationError(w, r, fields)
		return
	}
	result, err := a.store.List(r.Context(), filter)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) createItem(w http.ResponseWriter, r *http.Request) {
	var input inventory.ItemInput
	if err := decodeJSON(w, r, &input); err != nil {
		a.badJSON(w, r, err)
		return
	}
	input, err := inventory.NormalizeAndValidate(input)
	if err != nil {
		a.handleDomainError(w, r, err)
		return
	}
	id, err := newID()
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	item, err := a.store.Create(r.Context(), id, input)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/items/"+item.ID)
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) getItem(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		a.handleStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) updateItem(w http.ResponseWriter, r *http.Request) {
	current, err := a.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		a.handleStoreError(w, r, err)
		return
	}
	var patch inventory.ItemPatch
	if err := decodeJSON(w, r, &patch); err != nil {
		a.badJSON(w, r, err)
		return
	}
	input, err := inventory.NormalizeAndValidate(inventory.ApplyPatch(current, patch))
	if err != nil {
		a.handleDomainError(w, r, err)
		return
	}
	item, err := a.store.Update(r.Context(), current.ID, input)
	if err != nil {
		a.handleStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) deleteItem(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Delete(r.Context(), r.PathValue("id")); err != nil {
		a.handleStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) adjustItem(w http.ResponseWriter, r *http.Request) {
	var payload ledger.InventoryInput
	if err := decodeJSON(w, r, &payload); err != nil {
		a.badJSON(w, r, err)
		return
	}
	if payload.Delta != nil {
		delta, err := inventory.ValidateAdjustment(*payload.Delta)
		if err != nil {
			a.validationError(w, r, map[string]string{"delta": err.Error()})
			return
		}
		payload.Delta = &delta
	}
	if _, err := a.store.ApplyDueAutoConsumption(r.Context()); err != nil {
		a.ledgerError(w, r, err)
		return
	}
	action := "ADJUST"
	// Preserve legacy negative-delta consume behavior; absolute corrections remain ADJUST.
	if payload.Delta != nil && *payload.Delta < 0 && payload.Quantity == nil {
		amount := -*payload.Delta
		payload.Delta = nil
		payload.Quantity = &amount
		action = "CONSUME"
	}
	result, replay, err := a.store.InventoryAction(r.Context(), r.Header.Get("Idempotency-Key"), r.PathValue("id"), action, payload)
	if err != nil {
		a.ledgerError(w, r, err)
		return
	}
	replayHeader(w, replay)
	writeJSON(w, http.StatusOK, result.Item)
}

func (a *API) parseListFilter(r *http.Request) (inventory.ListFilter, map[string]string) {
	query := r.URL.Query()
	filter := inventory.ListFilter{
		Search: strings.TrimSpace(query.Get("search")), Category: strings.TrimSpace(query.Get("category")),
		Location: strings.TrimSpace(query.Get("location")), Status: strings.TrimSpace(query.Get("status")),
		Limit: 100,
	}
	fields := map[string]string{}
	if len([]rune(filter.Search)) > 100 {
		fields["search"] = "Search must be at most 100 characters."
	}
	if filter.Category != "" && !a.categories[filter.Category] {
		fields["category"] = "Choose a supported category."
	}
	if filter.Status != "" && filter.Status != "low" && filter.Status != "out" && filter.Status != "expiring" && filter.Status != "expired" && filter.Status != "short_lived" {
		fields["status"] = "Status must be low, out, expiring, expired, or short_lived."
	}
	if value := query.Get("hide_out"); value != "" {
		if value != "true" && value != "false" {
			fields["hide_out"] = "hide_out must be true or false."
		} else {
			filter.ExcludeOut = value == "true"
		}
	}
	if value := query.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 500 {
			fields["limit"] = "Limit must be an integer between 1 and 500."
		} else {
			filter.Limit = limit
		}
	}
	if value := query.Get("offset"); value != "" {
		offset, err := strconv.Atoi(value)
		if err != nil || offset < 0 {
			fields["offset"] = "Offset must be a non-negative integer."
		} else {
			filter.Offset = offset
		}
	}
	return filter, fields
}

func (a *API) serveApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		a.errorResponse(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.", nil)
		return
	}
	if r.URL.Path == "/" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(a.index)
		}
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		if _, err := fs.Stat(a.staticFS, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/fonts/") {
			w.Header().Set("Cache-Control", "public, max-age=604800")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		}
		a.static.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func (a *API) serveInbox(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(a.inbox)
	}
}

func (a *API) serveQuicklist(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(a.quicklist)
	}
}

func (a *API) apiNotFound(w http.ResponseWriter, r *http.Request) {
	a.errorResponse(w, r, http.StatusNotFound, "not_found", "API route not found.", nil)
}

func (a *API) handleDomainError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *inventory.ValidationError
	if errors.As(err, &validation) {
		a.validationError(w, r, validation.Fields)
		return
	}
	a.internalError(w, r, err)
}

func (a *API) handleStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ledger.ErrConflict):
		a.errorResponse(w, r, http.StatusConflict, "ledger_conflict", err.Error(), nil)
	case errors.Is(err, store.ErrNotFound):
		a.errorResponse(w, r, http.StatusNotFound, "not_found", "Item not found.", nil)
	case errors.Is(err, store.ErrNegativeQuantity):
		a.errorResponse(w, r, http.StatusConflict, "negative_quantity", "Adjustment would make quantity negative.", nil)
	default:
		a.internalError(w, r, err)
	}
}

func (a *API) validationError(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	a.errorResponse(w, r, http.StatusUnprocessableEntity, "validation_error", "Check the highlighted fields.", fields)
}

func (a *API) badJSON(w http.ResponseWriter, r *http.Request, err error) {
	a.errorResponse(w, r, http.StatusBadRequest, "invalid_json", err.Error(), nil)
}

func (a *API) internalError(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error("request failed", "error", err, "request_id", requestID(r.Context()))
	a.errorResponse(w, r, http.StatusInternalServerError, "internal_error", "Unexpected server error.", nil)
}

func (a *API) errorResponse(w http.ResponseWriter, r *http.Request, status int, code, message string, fields map[string]string) {
	writeJSON(w, status, errorBody{
		Error: errorDetail{Code: code, Message: message, Fields: fields}, RequestID: requestID(r.Context()),
	})
}

func (a *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if id == "" || len(id) > 100 {
			id = randomHex(12)
		}
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(self), microphone=(self), geolocation=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'self'; frame-ancestors 'none'; img-src 'self' data: blob:; media-src 'self' blob:; object-src 'none'; script-src 'self'; style-src 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			w.Header().Set("Cache-Control", "no-store")
		}

		recorder := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if recovered := recover(); recovered != nil {
				a.logger.Error("panic recovered", "error", recovered, "request_id", id)
				if !recorder.wroteHeader {
					a.errorResponse(recorder, r, http.StatusInternalServerError, "internal_error", "Unexpected server error.", nil)
				}
			}
			a.logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", recorder.status,
				"bytes", recorder.bytes, "duration_ms", time.Since(started).Milliseconds(), "request_id", id)
		}()
		next.ServeHTTP(recorder, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += n
	return n, err
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid JSON body: only one JSON object is allowed")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(value[0:4]), hex.EncodeToString(value[4:6]),
		hex.EncodeToString(value[6:8]), hex.EncodeToString(value[8:10]), hex.EncodeToString(value[10:16])), nil
}

func randomHex(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(value)
}

func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}
