package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"stockroom/internal/ai"
	"stockroom/internal/intake"
	"stockroom/internal/media"
	"stockroom/internal/store"
)

const automaticReviewConfidence = 0.90

type intakeAnalysisResponse struct {
	IntakeID string      `json:"intake_id"`
	Model    string      `json:"model"`
	Analysis ai.Analysis `json:"analysis"`
}

type intakeBulkResponse struct {
	Analyzed int      `json:"analyzed"`
	Added    int      `json:"added"`
	Reviewed int      `json:"reviewed"`
	Failed   []string `json:"failed,omitempty"`
}

var (
	errIntakeNotPending = errors.New("only pending inbox entries can be analyzed")
	errIntakeAnalyzing  = errors.New("inbox entry is already being analyzed")
)

func (a *API) analyzeIntake(w http.ResponseWriter, r *http.Request) {
	if a.analyzer == nil {
		a.errorResponse(w, r, http.StatusServiceUnavailable, "ai_unavailable", "Local AI is not configured.", nil)
		return
	}
	result, err := a.analyzePendingIntake(r.Context(), r.PathValue("id"))
	if err != nil {
		a.handleIntakeAnalysisFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) analyzeAllIntakes(w http.ResponseWriter, r *http.Request) {
	if a.analyzer == nil {
		a.errorResponse(w, r, http.StatusServiceUnavailable, "ai_unavailable", "Local AI is not configured.", nil)
		return
	}
	entries, err := a.pendingIntakes(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	response := intakeBulkResponse{}
	for _, entry := range entries {
		if entry.Analysis != nil && !needsCategoryAnalysis(*entry.Analysis) {
			continue
		}
		if _, err := a.analyzePendingIntake(r.Context(), entry.ID); err != nil {
			response.Failed = append(response.Failed, entry.ID)
			continue
		}
		response.Analyzed++
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *API) analyzeIntakeBatch(w http.ResponseWriter, r *http.Request) {
	if a.analyzer == nil {
		a.errorResponse(w, r, http.StatusServiceUnavailable, "ai_unavailable", "Local AI is not configured.", nil)
		return
	}
	batchID := r.PathValue("id")
	entries, err := a.pendingBatchIntakes(r.Context(), batchID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if len(entries) == 0 {
		a.errorResponse(w, r, http.StatusNotFound, "not_found", "No pending entries were found for this batch.", nil)
		return
	}
	a.scheduleBatchAnalysis(batchID)
	writeJSON(w, http.StatusAccepted, map[string]any{"batch_id": batchID, "status": "queued"})
}

func (a *API) applyReadyIntakeBatch(w http.ResponseWriter, r *http.Request) {
	batchID := r.PathValue("id")
	entries, err := a.readyBatchIntakes(r.Context(), batchID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a.applyReadyIntakes(r.Context(), entries))
}

func (a *API) applyAllReadyIntakes(w http.ResponseWriter, r *http.Request) {
	entries, err := a.readyIntakes(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a.applyReadyIntakes(r.Context(), entries))
}

type intakeResolveResponse struct {
	IntakeID string `json:"intake_id"`
	Added    int    `json:"added"`
	Status   string `json:"status"`
}

func (a *API) resolveIntake(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Action      string               `json:"action"`
		Suggestions *[]intake.Suggestion `json:"suggestions"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		a.badJSON(w, r, err)
		return
	}
	payload.Action = strings.ToLower(strings.TrimSpace(payload.Action))
	entry, err := a.store.GetIntake(r.Context(), r.PathValue("id"))
	if err != nil {
		a.handleIntakeStoreError(w, r, err)
		return
	}
	if entry.Status != intake.StatusPending || entry.WorkflowState != intake.WorkflowNeedsReview {
		a.validationError(w, r, map[string]string{"status": "Only entries that need review can be resolved."})
		return
	}
	switch payload.Action {
	case "discard":
		if _, err := a.store.UpdateIntakeStatus(r.Context(), entry.ID, intake.StatusReviewed); err != nil {
			a.handleIntakeStoreError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, intakeResolveResponse{IntakeID: entry.ID, Status: intake.WorkflowDiscarded})
	case "apply":
		if payload.Suggestions == nil {
			a.validationError(w, r, map[string]string{"suggestions": "Provide the corrected changes before applying."})
			return
		}
		if fields := a.validateReviewSuggestions(*payload.Suggestions); len(fields) > 0 {
			a.validationError(w, r, fields)
			return
		}
		if _, err := a.store.ReplaceIntakeSuggestions(r.Context(), entry.ID, *payload.Suggestions); err != nil {
			a.handleIntakeStoreError(w, r, err)
			return
		}
		added, err := a.store.ApplyIntakeAnalysis(r.Context(), entry.ID)
		if err != nil {
			a.markIntakeNeedsReview(r.Context(), entry.ID, applyFailureReason(err))
			a.errorResponse(w, r, http.StatusConflict, "review_required", applyFailureReason(err), nil)
			return
		}
		writeJSON(w, http.StatusOK, intakeResolveResponse{IntakeID: entry.ID, Added: added, Status: intake.WorkflowApplied})
	default:
		a.validationError(w, r, map[string]string{"action": "Action must be apply or discard."})
	}
}

func (a *API) addAllIntakes(w http.ResponseWriter, r *http.Request) {
	entries, err := a.pendingIntakes(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a.applyAnalyzedIntakes(r.Context(), entries))
}

func (a *API) analyzeAndAddAllIntakes(w http.ResponseWriter, r *http.Request) {
	if a.analyzer == nil {
		a.errorResponse(w, r, http.StatusServiceUnavailable, "ai_unavailable", "Local AI is not configured.", nil)
		return
	}
	entries, err := a.pendingIntakes(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	response := intakeBulkResponse{}
	for index := range entries {
		if entries[index].Analysis != nil && !needsCategoryAnalysis(*entries[index].Analysis) {
			continue
		}
		result, err := a.analyzePendingIntake(r.Context(), entries[index].ID)
		if err != nil {
			response.Failed = append(response.Failed, entries[index].ID)
			continue
		}
		entries[index].Analysis = &intake.Analysis{Model: result.Model, Summary: result.Analysis.Summary, Suggestions: toIntakeSuggestions(result.Analysis.Suggestions), NeedsReview: result.Analysis.NeedsReview}
		response.Analyzed++
	}
	applied := a.applyAnalyzedIntakes(r.Context(), entries)
	response.Added += applied.Added
	response.Reviewed += applied.Reviewed
	response.Failed = append(response.Failed, applied.Failed...)
	writeJSON(w, http.StatusOK, response)
}

func (a *API) scheduleBatchAnalysis(batchID string) {
	go func() {
		a.analysisMu.Lock()
		defer a.analysisMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		entries, err := a.pendingBatchIntakes(ctx, batchID)
		if err != nil {
			a.logger.Error("load inbox batch for analysis", "batch_id", batchID, "error", err)
			return
		}
		for _, entry := range entries {
			if entry.WorkflowState != intake.WorkflowQueued && entry.WorkflowState != intake.WorkflowFailed {
				continue
			}
			if _, err := a.analyzePendingIntake(ctx, entry.ID); err != nil {
				a.logger.Warn("analyze inbox batch entry", "batch_id", batchID, "entry_id", entry.ID, "error", err)
			}
		}
	}()
}

func needsCategoryAnalysis(analysis intake.Analysis) bool {
	for _, suggestion := range analysis.Suggestions {
		if suggestion.Intent != "unknown" && suggestion.Intent != "" && suggestion.Category == "" {
			return true
		}
	}
	return false
}

func (a *API) pendingIntakes(ctx context.Context) ([]intake.Entry, error) {
	first, err := a.store.ListIntakes(ctx, intake.ListFilter{Status: intake.StatusPending, Limit: 1})
	if err != nil || first.Total == 0 {
		return first.Entries, err
	}
	all, err := a.store.ListIntakes(ctx, intake.ListFilter{Status: intake.StatusPending, Limit: first.Total})
	if err != nil {
		return nil, err
	}
	return all.Entries, nil
}

func (a *API) pendingBatchIntakes(ctx context.Context, batchID string) ([]intake.Entry, error) {
	first, err := a.store.ListIntakes(ctx, intake.ListFilter{Status: intake.StatusPending, BatchID: batchID, Limit: 1})
	if err != nil || first.Total == 0 {
		return first.Entries, err
	}
	all, err := a.store.ListIntakes(ctx, intake.ListFilter{Status: intake.StatusPending, BatchID: batchID, Limit: first.Total})
	if err != nil {
		return nil, err
	}
	return all.Entries, nil
}

func (a *API) readyBatchIntakes(ctx context.Context, batchID string) ([]intake.Entry, error) {
	first, err := a.store.ListIntakes(ctx, intake.ListFilter{Status: intake.StatusPending, WorkflowState: intake.WorkflowReady, BatchID: batchID, Limit: 1})
	if err != nil || first.Total == 0 {
		return first.Entries, err
	}
	all, err := a.store.ListIntakes(ctx, intake.ListFilter{Status: intake.StatusPending, WorkflowState: intake.WorkflowReady, BatchID: batchID, Limit: first.Total})
	if err != nil {
		return nil, err
	}
	return all.Entries, nil
}

func (a *API) readyIntakes(ctx context.Context) ([]intake.Entry, error) {
	first, err := a.store.ListIntakes(ctx, intake.ListFilter{Status: intake.StatusPending, WorkflowState: intake.WorkflowReady, Limit: 1})
	if err != nil || first.Total == 0 {
		return first.Entries, err
	}
	all, err := a.store.ListIntakes(ctx, intake.ListFilter{Status: intake.StatusPending, WorkflowState: intake.WorkflowReady, Limit: first.Total})
	if err != nil {
		return nil, err
	}
	return all.Entries, nil
}

func (a *API) applyAnalyzedIntakes(ctx context.Context, entries []intake.Entry) intakeBulkResponse {
	response := intakeBulkResponse{}
	for _, entry := range entries {
		if entry.Analysis == nil {
			continue
		}
		added, err := a.store.ApplyIntakeAnalysis(ctx, entry.ID)
		if err != nil {
			a.markIntakeNeedsReview(ctx, entry.ID, applyFailureReason(err))
			response.Failed = append(response.Failed, entry.ID)
			continue
		}
		response.Added += added
		response.Reviewed++
	}
	return response
}

func (a *API) applyReadyIntakes(ctx context.Context, entries []intake.Entry) intakeBulkResponse {
	response := intakeBulkResponse{}
	for _, entry := range entries {
		added, err := a.store.ApplyIntakeAnalysis(ctx, entry.ID)
		if err != nil {
			a.markIntakeNeedsReview(ctx, entry.ID, applyFailureReason(err))
			response.Failed = append(response.Failed, entry.ID)
			continue
		}
		response.Added += added
		response.Reviewed++
	}
	return response
}

func (a *API) analyzePendingIntake(ctx context.Context, id string) (response intakeAnalysisResponse, resultErr error) {
	entry, err := a.store.GetIntake(ctx, id)
	if err != nil {
		return intakeAnalysisResponse{}, err
	}
	if entry.Status != intake.StatusPending {
		return intakeAnalysisResponse{}, errIntakeNotPending
	}
	if entry.WorkflowState == intake.WorkflowAnalyzing {
		return intakeAnalysisResponse{}, errIntakeAnalyzing
	}
	if entry.Analysis != nil && (entry.WorkflowState == intake.WorkflowReady || entry.WorkflowState == intake.WorkflowNeedsReview) {
		return responseFromStoredAnalysis(entry.ID, *entry.Analysis), nil
	}
	if entry.WorkflowState != intake.WorkflowQueued && entry.WorkflowState != intake.WorkflowFailed && entry.WorkflowState != "" {
		return intakeAnalysisResponse{}, errIntakeNotPending
	}
	entry, err = a.store.BeginIntakeAnalysis(ctx, entry.ID)
	if err != nil {
		if errors.Is(err, store.ErrIntakeWorkflowConflict) {
			return intakeAnalysisResponse{}, errIntakeAnalyzing
		}
		return intakeAnalysisResponse{}, err
	}
	fail := func(err error) (intakeAnalysisResponse, error) {
		a.markIntakeFailed(ctx, entry.ID, analysisFailureReason(err))
		return intakeAnalysisResponse{}, err
	}
	input := ai.IntakeInput{Source: entry.Source, Text: entry.Text}
	if len(entry.Attachments) > 0 {
		attachment, err := a.store.GetIntakeAttachment(ctx, entry.ID, entry.Attachments[0].ID)
		if err != nil {
			return fail(err)
		}
		prepared, err := a.media.Prepare(ctx, attachment)
		if err != nil {
			return fail(err)
		}
		input.FileName, input.MediaType = attachment.FileName, attachment.MediaType
		input.Images, input.Transcript = prepared.Images, prepared.Transcript
	}
	analysis, err := a.analyzer.AnalyzeInventoryIntake(ctx, input)
	if err != nil {
		return fail(err)
	}
	needsReview, reason := a.analysisNeedsReview(analysis)
	persisted, err := a.store.SaveIntakeAnalysis(ctx, entry.ID, intake.Analysis{Model: a.analyzer.Model(), Summary: analysis.Summary, Suggestions: toIntakeSuggestions(analysis.Suggestions), NeedsReview: needsReview})
	if err != nil {
		return fail(err)
	}
	if persisted.Analysis == nil {
		return fail(errors.New("persisted intake analysis is missing"))
	}
	workflowState := intake.WorkflowReady
	if needsReview {
		workflowState = intake.WorkflowNeedsReview
	}
	persisted, err = a.store.SetIntakeWorkflow(ctx, entry.ID, workflowState, reason)
	if err != nil {
		return intakeAnalysisResponse{}, err
	}
	if persisted.Analysis == nil {
		return intakeAnalysisResponse{}, errors.New("persisted intake analysis is missing")
	}
	return responseFromStoredAnalysis(entry.ID, *persisted.Analysis), nil
}

func (a *API) analysisNeedsReview(analysis ai.Analysis) (bool, string) {
	if len(analysis.Suggestions) == 0 {
		return true, "No actionable change was found."
	}
	seenItems := map[string]bool{}
	for _, suggestion := range analysis.Suggestions {
		intent := strings.ToLower(strings.TrimSpace(suggestion.Intent))
		if intent != "increase" && intent != "decrease" && intent != "create" {
			return true, "The item or intended change is ambiguous."
		}
		if suggestion.Confidence < automaticReviewConfidence {
			return true, fmt.Sprintf("AI confidence is below %.0f%%.", automaticReviewConfidence*100)
		}
		if strings.TrimSpace(suggestion.ItemName) == "" || strings.TrimSpace(suggestion.Unit) == "" || strings.TrimSpace(suggestion.Category) == "" ||
			math.IsNaN(suggestion.Amount) || math.IsInf(suggestion.Amount, 0) || suggestion.Amount <= 0 {
			return true, "The suggested change is incomplete."
		}
		if !a.categories[strings.ToLower(strings.TrimSpace(suggestion.Category))] {
			return true, "The suggested category needs confirmation."
		}
		itemKey := strings.ToLower(strings.TrimSpace(suggestion.ItemName))
		if seenItems[itemKey] {
			return true, "Multiple suggestions refer to the same item."
		}
		seenItems[itemKey] = true
	}
	return false, ""
}

func (a *API) validateReviewSuggestions(suggestions []intake.Suggestion) map[string]string {
	if len(suggestions) == 0 {
		return map[string]string{"suggestions": "Add at least one corrected change before applying."}
	}
	for index, suggestion := range suggestions {
		prefix := fmt.Sprintf("suggestions[%d]", index)
		intent := strings.ToLower(strings.TrimSpace(suggestion.Intent))
		if intent != "increase" && intent != "decrease" && intent != "create" {
			return map[string]string{prefix + ".intent": "Choose increase, decrease, or create."}
		}
		if strings.TrimSpace(suggestion.ItemName) == "" {
			return map[string]string{prefix + ".item_name": "Item name is required."}
		}
		if strings.TrimSpace(suggestion.Unit) == "" {
			return map[string]string{prefix + ".unit": "Unit is required."}
		}
		if math.IsNaN(suggestion.Amount) || math.IsInf(suggestion.Amount, 0) || suggestion.Amount <= 0 {
			return map[string]string{prefix + ".amount": "Amount must be greater than zero."}
		}
		if !a.categories[strings.ToLower(strings.TrimSpace(suggestion.Category))] {
			return map[string]string{prefix + ".category": "Choose a supported category."}
		}
	}
	return nil
}

func (a *API) markIntakeFailed(ctx context.Context, entryID, reason string) {
	if _, err := a.store.SetIntakeWorkflow(ctx, entryID, intake.WorkflowFailed, reason); err != nil {
		a.logger.Error("mark inbox entry failed", "entry_id", entryID, "error", err)
	}
}

func (a *API) markIntakeNeedsReview(ctx context.Context, entryID, reason string) {
	if _, err := a.store.SetIntakeWorkflow(ctx, entryID, intake.WorkflowNeedsReview, reason); err != nil {
		a.logger.Error("mark inbox entry for review", "entry_id", entryID, "error", err)
	}
}

func analysisFailureReason(err error) string {
	switch {
	case errors.Is(err, ai.ErrTranscriptionUnavailable):
		return "Voice transcription is unavailable. Try again when local transcription is running."
	case errors.Is(err, media.ErrProcessing):
		return "The uploaded file could not be prepared for local AI."
	case errors.Is(err, ai.ErrUnavailable):
		return "Local AI is unavailable. Try the batch again later."
	case errors.Is(err, ai.ErrInvalidResponse):
		return "Local AI returned an unusable result. Try again or review manually."
	default:
		return "Analysis could not be completed. Try again or discard this entry."
	}
}

func applyFailureReason(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, store.ErrNegativeQuantity):
		return "The change would reduce inventory below zero. Correct the amount before applying."
	case strings.Contains(message, "unit mismatch"):
		return "The suggested unit conflicts with the existing inventory item. Correct it before applying."
	case strings.Contains(message, "not in inventory"):
		return "This decrease refers to an item that is not in inventory. Review the item and intent."
	case strings.Contains(message, "no category"):
		return "The suggested category is missing. Choose a category before applying."
	default:
		return "The suggested changes could not be applied. Correct them and try again."
	}
}

func (a *API) handleIntakeAnalysisFailure(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrIntakeNotFound), errors.Is(err, store.ErrAttachmentNotFound):
		a.handleIntakeStoreError(w, r, err)
	case errors.Is(err, errIntakeNotPending):
		a.validationError(w, r, map[string]string{"status": "Only pending inbox entries can be analyzed."})
	case errors.Is(err, errIntakeAnalyzing):
		a.errorResponse(w, r, http.StatusConflict, "analysis_in_progress", "This inbox entry is already being analyzed.", nil)
	case errors.Is(err, ai.ErrTranscriptionUnavailable), errors.Is(err, media.ErrProcessing):
		a.handleMediaAnalysisError(w, r, err)
	case errors.Is(err, ai.ErrUnavailable), errors.Is(err, ai.ErrInvalidResponse):
		a.handleAIAnalysisError(w, r, err)
	default:
		a.internalError(w, r, err)
	}
}

func (a *API) handleMediaAnalysisError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ai.ErrTranscriptionUnavailable):
		a.logger.Warn("local transcription unavailable", "error", err, "request_id", requestID(r.Context()))
		a.errorResponse(w, r, http.StatusServiceUnavailable, "transcription_unavailable", "Local transcription did not respond.", nil)
	case errors.Is(err, media.ErrProcessing):
		a.logger.Warn("inbox media preparation failed", "error", err, "request_id", requestID(r.Context()))
		a.errorResponse(w, r, http.StatusUnprocessableEntity, "media_processing_failed", "The uploaded file could not be prepared for local AI.", nil)
	default:
		a.logger.Error("inbox media analysis failed", "error", err, "request_id", requestID(r.Context()))
		a.errorResponse(w, r, http.StatusBadGateway, "media_analysis_failed", "Local media processing failed.", nil)
	}
}

func (a *API) handleAIAnalysisError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ai.ErrUnavailable):
		a.logger.Warn("local AI unavailable", "error", err, "request_id", requestID(r.Context()))
		a.errorResponse(w, r, http.StatusServiceUnavailable, "ai_unavailable", "Local AI did not respond.", nil)
	case errors.Is(err, ai.ErrInvalidResponse):
		a.logger.Error("local AI returned invalid analysis", "error", err, "request_id", requestID(r.Context()))
		a.errorResponse(w, r, http.StatusBadGateway, "ai_invalid_response", "Local AI returned an invalid analysis.", nil)
	default:
		a.logger.Error("local AI analysis failed", "error", err, "request_id", requestID(r.Context()))
		a.errorResponse(w, r, http.StatusBadGateway, "ai_analysis_failed", "Local AI analysis failed.", nil)
	}
}

func responseFromStoredAnalysis(entryID string, stored intake.Analysis) intakeAnalysisResponse {
	return intakeAnalysisResponse{
		IntakeID: entryID,
		Model:    stored.Model,
		Analysis: ai.Analysis{
			Summary:     stored.Summary,
			Suggestions: fromIntakeSuggestions(stored.Suggestions),
			NeedsReview: stored.NeedsReview,
		},
	}
}

func toIntakeSuggestions(suggestions []ai.Suggestion) []intake.Suggestion {
	result := make([]intake.Suggestion, 0, len(suggestions))
	for _, suggestion := range suggestions {
		result = append(result, intake.Suggestion{
			Intent: suggestion.Intent, ItemName: suggestion.ItemName, Amount: suggestion.Amount,
			Unit: suggestion.Unit, Category: suggestion.Category, Confidence: suggestion.Confidence,
		})
	}
	return result
}

func fromIntakeSuggestions(suggestions []intake.Suggestion) []ai.Suggestion {
	result := make([]ai.Suggestion, 0, len(suggestions))
	for _, suggestion := range suggestions {
		result = append(result, ai.Suggestion{
			Intent: suggestion.Intent, ItemName: suggestion.ItemName, Amount: suggestion.Amount,
			Unit: suggestion.Unit, Category: suggestion.Category, Confidence: suggestion.Confidence,
		})
	}
	return result
}
