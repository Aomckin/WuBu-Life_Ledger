package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"stockroom/internal/ai"
	"stockroom/internal/intake"
	"stockroom/internal/inventory"
	"stockroom/internal/store"
)

func TestInventoryAPILifecycle(t *testing.T) {
	handler := testHandler(t)
	created := performJSON[inventory.Item](t, handler, http.MethodPost, "/api/v1/items", `{
        "name":"Dish soap","category":"cleaning","icon":"soap","quantity":2,
        "unit":"bottles","unit_price_cents":299,"location":"Utility closet","low_stock_threshold":1,"short_lived":true,"notes":"Lemon"
    }`, http.StatusCreated)
	if created.ID == "" || created.Name != "Dish soap" || created.UnitPriceCents == nil || *created.UnitPriceCents != 299 || !created.ShortLived || created.AutoConsumeAt == nil {
		t.Fatalf("created item = %#v", created)
	}

	adjusted := performJSON[inventory.Item](t, handler, http.MethodPost, "/api/v1/items/"+created.ID+"/adjust", `{"delta":-1}`, http.StatusOK)
	if adjusted.Quantity != 1 || adjusted.Version != 2 {
		t.Fatalf("adjusted item = %#v", adjusted)
	}
	summary := performJSON[inventory.Summary](t, handler, http.MethodGet, "/api/v1/summary", "", http.StatusOK)
	if summary.InventoryValueCents != 299 {
		t.Fatalf("summary = %#v", summary)
	}

	listed := performJSON[inventory.ListResult](t, handler, http.MethodGet, "/api/v1/items?search=lemon&status=low", "", http.StatusOK)
	if listed.Total != 1 || len(listed.Items) != 1 {
		t.Fatalf("listed items = %#v", listed)
	}
	shortLived := performJSON[inventory.ListResult](t, handler, http.MethodGet, "/api/v1/items?status=short_lived", "", http.StatusOK)
	if shortLived.Total != 1 {
		t.Fatalf("short-lived items = %#v", shortLived)
	}
	used := performJSON[inventory.Item](t, handler, http.MethodPatch, "/api/v1/items/"+created.ID, `{"quantity":0,"clear_auto_consume_at":true}`, http.StatusOK)
	if used.Quantity != 0 || used.AutoConsumeAt != nil || !used.ShortLived {
		t.Fatalf("used item = %#v", used)
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/items/"+created.ID, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestAPIValidationAndSecurityHeaders(t *testing.T) {
	handler := testHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/items", bytes.NewBufferString(`{"name":"","quantity":-1}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Security-Policy") == "" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing security headers: %#v", response.Header())
	}
	if response.Header().Get("Permissions-Policy") != "camera=(self), microphone=(self), geolocation=()" {
		t.Fatalf("permissions policy = %q", response.Header().Get("Permissions-Policy"))
	}

	var body errorBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error.Fields["name"] == "" || body.Error.Fields["quantity"] == "" || body.RequestID == "" {
		t.Fatalf("error body = %#v", body)
	}
}

func TestListItemsCanHideOutOfStock(t *testing.T) {
	handler := testHandler(t)
	performJSON[inventory.Item](t, handler, http.MethodPost, "/api/v1/items", `{"name":"Available soap","quantity":1}`, http.StatusCreated)
	performJSON[inventory.Item](t, handler, http.MethodPost, "/api/v1/items", `{"name":"Empty soap","quantity":0}`, http.StatusCreated)
	hidden := performJSON[inventory.ListResult](t, handler, http.MethodGet, "/api/v1/items?hide_out=true", "", http.StatusOK)
	if hidden.Total != 1 || len(hidden.Items) != 1 || hidden.Items[0].Name != "Available soap" {
		t.Fatalf("hidden out-of-stock list = %#v", hidden)
	}
	all := performJSON[inventory.ListResult](t, handler, http.MethodGet, "/api/v1/items?hide_out=false", "", http.StatusOK)
	if all.Total != 2 {
		t.Fatalf("all inventory list = %#v", all)
	}
}

func TestIntakeAPILifecycle(t *testing.T) {
	handler := testHandler(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("source", intake.SourcePurchasePhoto); err != nil {
		t.Fatalf("write source field: %v", err)
	}
	if err := writer.WriteField("text", "Weekly shop"); err != nil {
		t.Fatalf("write text field: %v", err)
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="files"; filename="shopping.mp4"`)
	header.Set("Content-Type", "video/mp4")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	fileContent := []byte("test-video-content")
	if _, err := part.Write(fileContent); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/intake", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("POST intake status = %d, body = %s", response.Code, response.Body.String())
	}
	var created intake.Entry
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode intake response: %v", err)
	}
	if created.Status != intake.StatusPending || len(created.Attachments) != 1 || created.Attachments[0].URL == "" {
		t.Fatalf("created intake = %#v", created)
	}

	attachmentRequest := httptest.NewRequest(http.MethodGet, created.Attachments[0].URL, nil)
	attachmentResponse := httptest.NewRecorder()
	handler.ServeHTTP(attachmentResponse, attachmentRequest)
	if attachmentResponse.Code != http.StatusOK || !bytes.Equal(attachmentResponse.Body.Bytes(), fileContent) {
		t.Fatalf("GET attachment status = %d, body = %q", attachmentResponse.Code, attachmentResponse.Body.Bytes())
	}
	if attachmentResponse.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("attachment Content-Type = %q", attachmentResponse.Header().Get("Content-Type"))
	}

	reviewed := performJSON[intake.Entry](t, handler, http.MethodPatch, "/api/v1/intake/"+created.ID, `{"status":"reviewed"}`, http.StatusOK)
	if reviewed.Status != intake.StatusReviewed || len(reviewed.Attachments) != 0 {
		t.Fatalf("reviewed intake = %#v", reviewed)
	}
	deletedAttachment := httptest.NewRecorder()
	handler.ServeHTTP(deletedAttachment, httptest.NewRequest(http.MethodGet, created.Attachments[0].URL, nil))
	if deletedAttachment.Code != http.StatusNotFound {
		t.Fatalf("reviewed attachment status = %d", deletedAttachment.Code)
	}
	listed := performJSON[intake.ListResult](t, handler, http.MethodGet, "/api/v1/intake?status=reviewed", "", http.StatusOK)
	if listed.Total != 1 || len(listed.Entries) != 1 {
		t.Fatalf("listed intakes = %#v", listed)
	}

	command := performJSON[intake.Entry](t, handler, http.MethodPost, "/api/v1/intake", `{"source":"command","text":"Used one bottle of dish soap"}`, http.StatusCreated)
	if command.Source != intake.SourceCommand || command.Text == "" || len(command.Attachments) != 0 {
		t.Fatalf("command intake = %#v", command)
	}
}

func TestStaticAppAndHealth(t *testing.T) {
	handler := testHandler(t)
	for _, path := range []string{"/", "/inbox", "/kiquicklist", "/healthz", "/api/v1/openapi.json", "/assets/app.js", "/assets/inbox.js", "/assets/inbox.css", "/assets/quicklist.js"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, body = %s", path, response.Code, response.Body.String())
		}
	}
}

func TestTextIntakeAnalysisIsReviewOnly(t *testing.T) {
	handler := testHandlerWithAnalyzer(t, fakeAnalyzer{})
	command := performJSON[intake.Entry](t, handler, http.MethodPost, "/api/v1/intake", `{"source":"command","text":"Used one bottle of dish soap"}`, http.StatusCreated)
	result := performJSON[intakeAnalysisResponse](t, handler, http.MethodPost, "/api/v1/intake/"+command.ID+"/analysis", "", http.StatusOK)
	if result.IntakeID != command.ID || result.Model != "test-model" || result.Analysis.NeedsReview {
		t.Fatalf("analysis = %#v", result)
	}
	if len(result.Analysis.Suggestions) != 1 || result.Analysis.Suggestions[0].Intent != "decrease" {
		t.Fatalf("analysis suggestions = %#v", result.Analysis.Suggestions)
	}
	items := performJSON[inventory.ListResult](t, handler, http.MethodGet, "/api/v1/items", "", http.StatusOK)
	if items.Total != 0 {
		t.Fatalf("analysis changed inventory: %#v", items)
	}
}

func TestBulkInboxActionsAnalyzeThenAddWithoutManualInventoryEntry(t *testing.T) {
	handler := testHandlerWithAnalyzer(t, fakeAnalyzer{})
	item := performJSON[inventory.Item](t, handler, http.MethodPost, "/api/v1/items", `{"name":"Dish soap","quantity":3,"unit":"bottle"}`, http.StatusCreated)
	for range 2 {
		performJSON[intake.Entry](t, handler, http.MethodPost, "/api/v1/intake", `{"source":"command","text":"Used one bottle of dish soap"}`, http.StatusCreated)
	}
	analyzed := performJSON[intakeBulkResponse](t, handler, http.MethodPost, "/api/v1/intake/analyze-all", "", http.StatusOK)
	if analyzed.Analyzed != 2 || analyzed.Added != 0 || len(analyzed.Failed) != 0 {
		t.Fatalf("analyze all = %#v", analyzed)
	}
	unchanged := performJSON[inventory.Item](t, handler, http.MethodGet, "/api/v1/items/"+item.ID, "", http.StatusOK)
	if unchanged.Quantity != 3 {
		t.Fatalf("analyze all changed quantity: %#v", unchanged)
	}
	added := performJSON[intakeBulkResponse](t, handler, http.MethodPost, "/api/v1/intake/add-all", "", http.StatusOK)
	if added.Added != 2 || added.Reviewed != 2 || len(added.Failed) != 0 {
		t.Fatalf("add all = %#v", added)
	}
	updated := performJSON[inventory.Item](t, handler, http.MethodGet, "/api/v1/items/"+item.ID, "", http.StatusOK)
	if updated.Quantity != 1 {
		t.Fatalf("add all quantity = %#v", updated)
	}
	pending := performJSON[intake.ListResult](t, handler, http.MethodGet, "/api/v1/intake?status=pending", "", http.StatusOK)
	if pending.Total != 0 {
		t.Fatalf("pending inbox after add all = %#v", pending)
	}
}

func TestImageIntakeAnalysisIsPersistedAndReviewOnly(t *testing.T) {
	handler := testHandlerWithAnalyzer(t, fakeAnalyzer{})
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("source", intake.SourcePurchasePhoto); err != nil {
		t.Fatalf("write source field: %v", err)
	}
	if err := writer.WriteField("text", "Weekly shop"); err != nil {
		t.Fatalf("write text field: %v", err)
	}
	fileHeader := make(textproto.MIMEHeader)
	fileHeader.Set("Content-Disposition", `form-data; name="files"; filename="receipt.jpg"`)
	fileHeader.Set("Content-Type", "image/jpeg")
	file, err := writer.CreatePart(fileHeader)
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := file.Write([]byte("small-test-image")); err != nil {
		t.Fatalf("write file field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/intake", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create image intake status = %d; body = %s", response.Code, response.Body.String())
	}
	var created intake.Entry
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created image intake: %v", err)
	}

	result := performJSON[intakeAnalysisResponse](t, handler, http.MethodPost, "/api/v1/intake/"+created.ID+"/analysis", "", http.StatusOK)
	if result.Model != "test-model" || result.Analysis.NeedsReview || len(result.Analysis.Suggestions) != 1 {
		t.Fatalf("image analysis = %#v", result)
	}
	stored := performJSON[intake.Entry](t, handler, http.MethodGet, "/api/v1/intake/"+created.ID, "", http.StatusOK)
	if stored.Analysis == nil || stored.Analysis.Model != "test-model" || stored.Analysis.Summary == "" {
		t.Fatalf("stored image analysis = %#v", stored.Analysis)
	}
	items := performJSON[inventory.ListResult](t, handler, http.MethodGet, "/api/v1/items", "", http.StatusOK)
	if items.Total != 0 {
		t.Fatalf("image analysis changed inventory: %#v", items)
	}
}

func TestTextIntakeAnalysisRequiresConfiguredAI(t *testing.T) {
	handler := testHandler(t)
	command := performJSON[intake.Entry](t, handler, http.MethodPost, "/api/v1/intake", `{"source":"command","text":"Used one bottle of dish soap"}`, http.StatusCreated)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/intake/"+command.ID+"/analysis", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("analysis status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestIntakeBatchCreatesSeparateEntriesAndKeepsExistingQueuedEntriesManual(t *testing.T) {
	handler := testHandlerWithAnalyzer(t, fakeAnalyzer{})
	settings := performJSON[intake.Settings](t, handler, http.MethodGet, "/api/v1/intake/settings", "", http.StatusOK)
	if !settings.AutoAnalyze {
		t.Fatalf("default inbox settings = %#v", settings)
	}
	settings = performJSON[intake.Settings](t, handler, http.MethodPatch, "/api/v1/intake/settings", `{"auto_analyze":false}`, http.StatusOK)
	if settings.AutoAnalyze {
		t.Fatalf("updated inbox settings = %#v", settings)
	}

	batch := performIntakeBatch(t, handler, "Weekly shop", []testBatchFile{
		{Name: "receipt.pdf", MediaType: "application/pdf", Data: []byte("pdf")},
		{Name: "shelf.jpg", MediaType: "image/jpeg", Data: []byte("image")},
	}, http.StatusCreated)
	if batch.BatchID == "" || batch.AutoAnalyze || batch.AnalysisScheduled || len(batch.Entries) != 2 {
		t.Fatalf("created batch = %#v", batch)
	}
	if batch.Entries[0].BatchID != batch.BatchID || batch.Entries[1].BatchID != batch.BatchID || batch.Entries[0].ID == batch.Entries[1].ID {
		t.Fatalf("batch entries = %#v", batch.Entries)
	}
	if batch.Entries[0].WorkflowState != intake.WorkflowQueued || batch.Entries[1].WorkflowState != intake.WorkflowQueued {
		t.Fatalf("batch workflow states = %#v", batch.Entries)
	}
	if batch.Entries[0].Source != intake.SourceReceipt || batch.Entries[1].Source != intake.SourcePurchasePhoto {
		t.Fatalf("batch sources = %#v", batch.Entries)
	}
	detail := performJSON[intakeBatchDetail](t, handler, http.MethodGet, "/api/v1/intake/batches/"+batch.BatchID, "", http.StatusOK)
	if detail.BatchID != batch.BatchID || len(detail.Entries) != 2 {
		t.Fatalf("loaded batch detail = %#v", detail)
	}

	performJSON[intake.Settings](t, handler, http.MethodPatch, "/api/v1/intake/settings", `{"auto_analyze":true}`, http.StatusOK)
	time.Sleep(30 * time.Millisecond)
	stillQueued := performJSON[intake.ListResult](t, handler, http.MethodGet, "/api/v1/intake?status=pending&batch_id="+batch.BatchID+"&limit=10", "", http.StatusOK)
	if len(stillQueued.Entries) != 2 || stillQueued.Entries[0].WorkflowState != intake.WorkflowQueued || stillQueued.Entries[1].WorkflowState != intake.WorkflowQueued {
		t.Fatalf("existing batch changed after enabling automation = %#v", stillQueued.Entries)
	}
}

func TestTypedNilAIClientDoesNotScheduleBatchAnalysis(t *testing.T) {
	var unavailable *ai.Client
	handler := testHandlerWithAnalyzer(t, unavailable)
	batch := performIntakeBatch(t, handler, "Used one bottle of dish soap", nil, http.StatusCreated)
	if !batch.AutoAnalyze || batch.AnalysisScheduled || len(batch.Entries) != 1 || batch.Entries[0].WorkflowState != intake.WorkflowQueued {
		t.Fatalf("batch with unavailable local AI = %#v", batch)
	}
}

func TestNewBatchAutomaticallyMovesFromAnalyzingToReady(t *testing.T) {
	analyzer := blockingAnalyzer{started: make(chan struct{}, 1), release: make(chan struct{})}
	handler := testHandlerWithAnalyzer(t, analyzer)
	batch := performIntakeBatch(t, handler, "Used one bottle of dish soap", nil, http.StatusCreated)
	if !batch.AutoAnalyze || !batch.AnalysisScheduled {
		t.Fatalf("automatic batch response = %#v", batch)
	}
	select {
	case <-analyzer.started:
	case <-time.After(3 * time.Second):
		t.Fatal("automatic batch analysis did not start")
	}
	analyzing := waitForBatchWorkflow(t, handler, batch.BatchID, intake.WorkflowAnalyzing)
	if analyzing.Analysis != nil {
		t.Fatalf("analyzing entry already has analysis = %#v", analyzing)
	}
	close(analyzer.release)
	ready := waitForBatchWorkflow(t, handler, batch.BatchID, intake.WorkflowReady)
	if ready.Analysis == nil || ready.Analysis.NeedsReview {
		t.Fatalf("automatically ready entry = %#v", ready)
	}
}

func TestIntakeBatchRejectsMoreThanTenFiles(t *testing.T) {
	handler := testHandler(t)
	files := make([]testBatchFile, 0, intake.MaxBatchFiles+1)
	for index := 0; index <= intake.MaxBatchFiles; index++ {
		files = append(files, testBatchFile{Name: "receipt-" + strconv.Itoa(index) + ".pdf", MediaType: "application/pdf", Data: []byte("pdf")})
	}
	performIntakeBatch(t, handler, "", files, http.StatusUnprocessableEntity)
	entries := performJSON[intake.ListResult](t, handler, http.MethodGet, "/api/v1/intake?status=pending", "", http.StatusOK)
	if entries.Total != 0 {
		t.Fatalf("rejected batch created entries = %#v", entries)
	}
}

func TestBatchAnalysisAppliesReadyChangesOnce(t *testing.T) {
	handler := testHandlerWithAnalyzer(t, fakeAnalyzer{})
	performJSON[intake.Settings](t, handler, http.MethodPatch, "/api/v1/intake/settings", `{"auto_analyze":false}`, http.StatusOK)
	item := performJSON[inventory.Item](t, handler, http.MethodPost, "/api/v1/items", `{"name":"Dish soap","quantity":3,"unit":"bottle"}`, http.StatusCreated)
	batch := performIntakeBatch(t, handler, "Used one bottle of dish soap", nil, http.StatusCreated)
	if len(batch.Entries) != 1 || batch.Entries[0].WorkflowState != intake.WorkflowQueued {
		t.Fatalf("created text batch = %#v", batch)
	}
	performJSON[map[string]any](t, handler, http.MethodPost, "/api/v1/intake/batches/"+batch.BatchID+"/analyze", "", http.StatusAccepted)
	ready := waitForBatchWorkflow(t, handler, batch.BatchID, intake.WorkflowReady)
	if ready.Analysis == nil || ready.Analysis.NeedsReview {
		t.Fatalf("ready batch entry = %#v", ready)
	}
	unchanged := performJSON[inventory.Item](t, handler, http.MethodGet, "/api/v1/items/"+item.ID, "", http.StatusOK)
	if unchanged.Quantity != 3 {
		t.Fatalf("analysis changed inventory = %#v", unchanged)
	}
	firstApply := performJSON[intakeBulkResponse](t, handler, http.MethodPost, "/api/v1/intake/batches/"+batch.BatchID+"/apply-ready", "", http.StatusOK)
	if firstApply.Added != 1 || firstApply.Reviewed != 1 || len(firstApply.Failed) != 0 {
		t.Fatalf("first batch apply = %#v", firstApply)
	}
	secondApply := performJSON[intakeBulkResponse](t, handler, http.MethodPost, "/api/v1/intake/batches/"+batch.BatchID+"/apply-ready", "", http.StatusOK)
	if secondApply.Added != 0 || secondApply.Reviewed != 0 || len(secondApply.Failed) != 0 {
		t.Fatalf("second batch apply = %#v", secondApply)
	}
	updated := performJSON[inventory.Item](t, handler, http.MethodGet, "/api/v1/items/"+item.ID, "", http.StatusOK)
	if updated.Quantity != 2 {
		t.Fatalf("batch apply was not idempotent = %#v", updated)
	}
}

func TestBatchReviewCanCorrectLowConfidenceAndInventoryConflicts(t *testing.T) {
	lowConfidence := testHandlerWithAnalyzer(t, lowConfidenceAnalyzer{})
	performJSON[intake.Settings](t, lowConfidence, http.MethodPatch, "/api/v1/intake/settings", `{"auto_analyze":false}`, http.StatusOK)
	batch := performIntakeBatch(t, lowConfidence, "Maybe one bottle of dish soap", nil, http.StatusCreated)
	performJSON[map[string]any](t, lowConfidence, http.MethodPost, "/api/v1/intake/batches/"+batch.BatchID+"/analyze", "", http.StatusAccepted)
	needsReview := waitForBatchWorkflow(t, lowConfidence, batch.BatchID, intake.WorkflowNeedsReview)
	if needsReview.FailureReason == "" || needsReview.Analysis == nil || !needsReview.Analysis.NeedsReview {
		t.Fatalf("low-confidence review = %#v", needsReview)
	}
	resolved := performJSON[intakeResolveResponse](t, lowConfidence, http.MethodPost, "/api/v1/intake/"+needsReview.ID+"/resolve", `{"action":"apply","suggestions":[{"intent":"create","item_name":"Dish soap","amount":1,"unit":"bottle","category":"cleaning","confidence":1}]}`, http.StatusOK)
	if resolved.Added != 1 || resolved.Status != intake.WorkflowApplied {
		t.Fatalf("resolved review = %#v", resolved)
	}

	conflict := testHandlerWithAnalyzer(t, fakeAnalyzer{})
	performJSON[intake.Settings](t, conflict, http.MethodPatch, "/api/v1/intake/settings", `{"auto_analyze":false}`, http.StatusOK)
	item := performJSON[inventory.Item](t, conflict, http.MethodPost, "/api/v1/items", `{"name":"Dish soap","quantity":0,"unit":"bottle"}`, http.StatusCreated)
	conflictBatch := performIntakeBatch(t, conflict, "Used one bottle of dish soap", nil, http.StatusCreated)
	performJSON[map[string]any](t, conflict, http.MethodPost, "/api/v1/intake/batches/"+conflictBatch.BatchID+"/analyze", "", http.StatusAccepted)
	waitForBatchWorkflow(t, conflict, conflictBatch.BatchID, intake.WorkflowReady)
	failedApply := performJSON[intakeBulkResponse](t, conflict, http.MethodPost, "/api/v1/intake/batches/"+conflictBatch.BatchID+"/apply-ready", "", http.StatusOK)
	if len(failedApply.Failed) != 1 || failedApply.Added != 0 {
		t.Fatalf("conflicting apply = %#v", failedApply)
	}
	conflictedEntry := waitForBatchWorkflow(t, conflict, conflictBatch.BatchID, intake.WorkflowNeedsReview)
	if conflictedEntry.FailureReason == "" {
		t.Fatalf("conflicted entry = %#v", conflictedEntry)
	}
	performJSON[intakeResolveResponse](t, conflict, http.MethodPost, "/api/v1/intake/"+conflictedEntry.ID+"/resolve", `{"action":"apply","suggestions":[{"intent":"increase","item_name":"Dish soap","amount":1,"unit":"bottle","category":"cleaning","confidence":1}]}`, http.StatusOK)
	updated := performJSON[inventory.Item](t, conflict, http.MethodGet, "/api/v1/items/"+item.ID, "", http.StatusOK)
	if updated.Quantity != 1 {
		t.Fatalf("corrected conflict = %#v", updated)
	}
}

type testBatchFile struct {
	Name      string
	MediaType string
	Data      []byte
}

func performIntakeBatch(t *testing.T, handler http.Handler, text string, files []testBatchFile, expectedStatus int) intakeBatchResponse {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if text != "" {
		if err := writer.WriteField("text", text); err != nil {
			t.Fatalf("write batch text: %v", err)
		}
	}
	for _, file := range files {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="files"; filename="`+file.Name+`"`)
		header.Set("Content-Type", file.MediaType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("create batch file: %v", err)
		}
		if _, err := part.Write(file.Data); err != nil {
			t.Fatalf("write batch file: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close batch writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/intake/batches", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != expectedStatus {
		t.Fatalf("POST batch status = %d, want %d; body = %s", response.Code, expectedStatus, response.Body.String())
	}
	var result intakeBatchResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	return result
}

func waitForBatchWorkflow(t *testing.T, handler http.Handler, batchID, workflow string) intake.Entry {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries := performJSON[intake.ListResult](t, handler, http.MethodGet, "/api/v1/intake?status=pending&batch_id="+batchID+"&limit=20", "", http.StatusOK)
		for _, entry := range entries.Entries {
			if entry.WorkflowState == workflow {
				return entry
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("batch %s did not reach workflow state %s", batchID, workflow)
	return intake.Entry{}
}

func testHandler(t *testing.T) http.Handler {
	return testHandlerWithAnalyzer(t, nil)
}

func testHandlerWithAnalyzer(t *testing.T, analyzer ai.Analyzer) http.Handler {
	t.Helper()
	database, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	handler, err := New(database, "test", slog.New(slog.NewTextHandler(io.Discard, nil)), analyzer)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

type fakeAnalyzer struct{}

type lowConfidenceAnalyzer struct{}

type blockingAnalyzer struct {
	started chan struct{}
	release chan struct{}
}

func (fakeAnalyzer) Model() string {
	return "test-model"
}

func (fakeAnalyzer) AnalyzeInventoryCommand(_ context.Context, _ string) (ai.Analysis, error) {
	return fakeInventoryAnalysis(), nil
}

func (fakeAnalyzer) AnalyzeInventoryIntake(_ context.Context, _ ai.IntakeInput) (ai.Analysis, error) {
	return fakeInventoryAnalysis(), nil
}

func (lowConfidenceAnalyzer) Model() string { return "test-model" }

func (lowConfidenceAnalyzer) AnalyzeInventoryCommand(_ context.Context, _ string) (ai.Analysis, error) {
	return lowConfidenceAnalysis(), nil
}

func (lowConfidenceAnalyzer) AnalyzeInventoryIntake(_ context.Context, _ ai.IntakeInput) (ai.Analysis, error) {
	return lowConfidenceAnalysis(), nil
}

func (analyzer blockingAnalyzer) Model() string { return "test-model" }

func (analyzer blockingAnalyzer) AnalyzeInventoryCommand(_ context.Context, _ string) (ai.Analysis, error) {
	return analyzer.waitForRelease()
}

func (analyzer blockingAnalyzer) AnalyzeInventoryIntake(_ context.Context, _ ai.IntakeInput) (ai.Analysis, error) {
	return analyzer.waitForRelease()
}

func (analyzer blockingAnalyzer) waitForRelease() (ai.Analysis, error) {
	analyzer.started <- struct{}{}
	<-analyzer.release
	return fakeInventoryAnalysis(), nil
}

func fakeInventoryAnalysis() ai.Analysis {
	return ai.Analysis{
		Summary:     "Review one use of dish soap.",
		NeedsReview: true,
		Suggestions: []ai.Suggestion{{
			Intent: "decrease", ItemName: "Dish soap", Amount: 1, Unit: "bottle", Category: "cleaning", Confidence: 0.95,
		}},
	}
}

func lowConfidenceAnalysis() ai.Analysis {
	analysis := fakeInventoryAnalysis()
	analysis.Suggestions[0].Confidence = 0.5
	return analysis
}

func performJSON[T any](t *testing.T, handler http.Handler, method, path, body string, expectedStatus int) T {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != expectedStatus {
		t.Fatalf("%s %s status = %d, want %d; body = %s", method, path, response.Code, expectedStatus, response.Body.String())
	}
	var result T
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode %s %s response: %v", method, path, err)
	}
	return result
}
