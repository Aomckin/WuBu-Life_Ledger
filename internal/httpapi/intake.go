package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"stockroom/internal/intake"
	"stockroom/internal/store"
)

func (a *API) listIntakes(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := intake.ListFilter{
		Status: strings.TrimSpace(query.Get("status")), WorkflowState: strings.TrimSpace(query.Get("workflow_state")),
		BatchID: strings.TrimSpace(query.Get("batch_id")), Limit: 20,
	}
	fields := map[string]string{}
	if filter.Status != "" && !intake.ValidStatus(filter.Status) {
		fields["status"] = "Status must be pending or reviewed."
	}
	if filter.WorkflowState != "" && !intake.ValidWorkflowState(filter.WorkflowState) {
		fields["workflow_state"] = "Choose a supported inbox workflow state."
	}
	if value := query.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			fields["limit"] = "Limit must be an integer between 1 and 100."
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
	if len(fields) > 0 {
		a.validationError(w, r, fields)
		return
	}
	result, err := a.store.ListIntakes(r.Context(), filter)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	for index := range result.Entries {
		addAttachmentURLs(&result.Entries[index])
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) createIntake(w http.ResponseWriter, r *http.Request) {
	input, err := a.parseIntakeInput(w, r)
	if err != nil {
		a.handleIntakeInputError(w, r, err)
		return
	}
	defer a.discardIntakeTempFiles(input.Attachments)
	for index := range input.Attachments {
		input.Attachments[index].ID, err = newID()
		if err != nil {
			a.internalError(w, r, err)
			return
		}
	}
	input, err = intake.NormalizeAndValidate(input)
	if err != nil {
		a.handleIntakeInputError(w, r, err)
		return
	}
	id, err := newID()
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	entry, err := a.store.CreateIntake(r.Context(), id, input)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	addAttachmentURLs(&entry)
	w.Header().Set("Location", "/api/v1/intake/"+entry.ID)
	writeJSON(w, http.StatusCreated, entry)
}

type intakeBatchResponse struct {
	BatchID           string         `json:"batch_id"`
	Entries           []intake.Entry `json:"entries"`
	AutoAnalyze       bool           `json:"auto_analyze"`
	AnalysisScheduled bool           `json:"analysis_scheduled"`
}

type intakeBatchDetail struct {
	BatchID string         `json:"batch_id"`
	Entries []intake.Entry `json:"entries"`
}

// createIntakeBatch creates one independent inbox entry for each uploaded file.
// That keeps source previews, retry state, and exception reviews precise while
// still letting the UI treat all files selected together as one batch.
func (a *API) createIntakeBatch(w http.ResponseWriter, r *http.Request) {
	inputs, tempFiles, err := a.parseIntakeBatchInput(w, r)
	if err != nil {
		a.handleIntakeInputError(w, r, err)
		return
	}
	defer a.discardIntakeTempFiles(tempFiles)
	batchID, err := newID()
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	entries := make([]store.IntakeBatchEntry, 0, len(inputs))
	for _, input := range inputs {
		for index := range input.Attachments {
			input.Attachments[index].ID, err = newID()
			if err != nil {
				a.internalError(w, r, err)
				return
			}
		}
		input, err = intake.NormalizeAndValidate(input)
		if err != nil {
			a.handleIntakeInputError(w, r, err)
			return
		}
		id, err := newID()
		if err != nil {
			a.internalError(w, r, err)
			return
		}
		entries = append(entries, store.IntakeBatchEntry{ID: id, Input: input})
	}
	stored, err := a.store.CreateIntakeBatch(r.Context(), batchID, entries)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	for index := range stored {
		addAttachmentURLs(&stored[index])
	}
	settings, err := a.store.IntakeSettings(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	analysisScheduled := settings.AutoAnalyze && a.analyzer != nil
	if analysisScheduled {
		a.scheduleBatchAnalysis(batchID)
	}
	w.Header().Set("Location", "/api/v1/intake/batches/"+batchID)
	writeJSON(w, http.StatusCreated, intakeBatchResponse{BatchID: batchID, Entries: stored, AutoAnalyze: settings.AutoAnalyze, AnalysisScheduled: analysisScheduled})
}

func (a *API) intakeSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := a.store.IntakeSettings(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (a *API) getIntakeBatch(w http.ResponseWriter, r *http.Request) {
	batchID := r.PathValue("id")
	first, err := a.store.ListIntakes(r.Context(), intake.ListFilter{BatchID: batchID, Limit: 1})
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if first.Total == 0 {
		a.errorResponse(w, r, http.StatusNotFound, "not_found", "Inbox batch not found.", nil)
		return
	}
	result, err := a.store.ListIntakes(r.Context(), intake.ListFilter{BatchID: batchID, Limit: first.Total})
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	for index := range result.Entries {
		addAttachmentURLs(&result.Entries[index])
	}
	writeJSON(w, http.StatusOK, intakeBatchDetail{BatchID: batchID, Entries: result.Entries})
}

func (a *API) updateIntakeSettings(w http.ResponseWriter, r *http.Request) {
	var patch struct {
		AutoAnalyze *bool `json:"auto_analyze"`
	}
	if err := decodeJSON(w, r, &patch); err != nil {
		a.badJSON(w, r, err)
		return
	}
	if patch.AutoAnalyze == nil {
		a.validationError(w, r, map[string]string{"auto_analyze": "Choose whether new uploads should be analyzed automatically."})
		return
	}
	settings, err := a.store.UpdateIntakeSettings(r.Context(), intake.Settings{AutoAnalyze: *patch.AutoAnalyze})
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (a *API) getIntake(w http.ResponseWriter, r *http.Request) {
	entry, err := a.store.GetIntake(r.Context(), r.PathValue("id"))
	if err != nil {
		a.handleIntakeStoreError(w, r, err)
		return
	}
	addAttachmentURLs(&entry)
	writeJSON(w, http.StatusOK, entry)
}

func (a *API) updateIntake(w http.ResponseWriter, r *http.Request) {
	var patch struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(w, r, &patch); err != nil {
		a.badJSON(w, r, err)
		return
	}
	patch.Status = strings.ToLower(strings.TrimSpace(patch.Status))
	if !intake.ValidStatus(patch.Status) {
		a.validationError(w, r, map[string]string{"status": "Status must be pending or reviewed."})
		return
	}
	entry, err := a.store.UpdateIntakeStatus(r.Context(), r.PathValue("id"), patch.Status)
	if err != nil {
		a.handleIntakeStoreError(w, r, err)
		return
	}
	addAttachmentURLs(&entry)
	writeJSON(w, http.StatusOK, entry)
}

func (a *API) getIntakeAttachment(w http.ResponseWriter, r *http.Request) {
	attachment, err := a.store.GetIntakeAttachment(r.Context(), r.PathValue("id"), r.PathValue("attachment_id"))
	if err != nil {
		a.handleIntakeStoreError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", attachment.MediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(attachment.SizeBytes, 10))
	dispositionType := "attachment"
	if strings.HasPrefix(attachment.MediaType, "image/") || strings.HasPrefix(attachment.MediaType, "audio/") || strings.HasPrefix(attachment.MediaType, "video/") {
		dispositionType = "inline"
	}
	disposition := mime.FormatMediaType(dispositionType, map[string]string{"filename": attachment.FileName})
	if disposition != "" {
		w.Header().Set("Content-Disposition", disposition)
	}
	http.ServeContent(w, r, attachment.FileName, time.Time{}, bytes.NewReader(attachment.Data))
}

func (a *API) parseIntakeInput(w http.ResponseWriter, r *http.Request) (intake.Input, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return intake.Input{}, errors.New("Content-Type must be application/json or multipart/form-data")
	}
	if mediaType == "application/json" {
		var payload struct {
			Source string `json:"source"`
			Text   string `json:"text"`
		}
		if err := decodeJSON(w, r, &payload); err != nil {
			return intake.Input{}, err
		}
		return intake.Input{Source: payload.Source, Text: payload.Text}, nil
	}
	if mediaType != "multipart/form-data" {
		return intake.Input{}, errors.New("Content-Type must be application/json or multipart/form-data")
	}
	r.Body = http.MaxBytesReader(w, r.Body, intake.MaxRequestBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		return intake.Input{}, fmt.Errorf("invalid multipart body: %w", err)
	}
	return a.readIntakeParts(reader)
}

func (a *API) parseIntakeBatchInput(w http.ResponseWriter, r *http.Request) ([]intake.Input, []intake.AttachmentInput, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return nil, nil, errors.New("Content-Type must be multipart/form-data")
	}
	r.Body = http.MaxBytesReader(w, r.Body, intake.MaxBatchRequestBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid multipart body: %w", err)
	}
	text, attachments, err := a.readIntakeBatchParts(reader)
	if err != nil {
		a.discardIntakeTempFiles(attachments)
		return nil, nil, err
	}
	if len(attachments) == 0 {
		if strings.TrimSpace(text) == "" {
			return nil, nil, &intake.ValidationError{Fields: map[string]string{"input": "Add a description or at least one file."}}
		}
		return []intake.Input{{Source: intake.SourceCommand, Text: text}}, attachments, nil
	}
	inputs := make([]intake.Input, 0, len(attachments))
	for _, attachment := range attachments {
		inputs = append(inputs, intake.Input{
			Source: sourceForBatchAttachment(attachment.MediaType), Text: text, Attachments: []intake.AttachmentInput{attachment},
		})
	}
	return inputs, attachments, nil
}

func (a *API) readIntakeParts(reader *multipart.Reader) (input intake.Input, resultErr error) {
	defer func() {
		if resultErr != nil {
			a.discardIntakeTempFiles(input.Attachments)
		}
	}()
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return intake.Input{}, err
		}
		name := part.FormName()
		if name == "source" || name == "text" {
			value, readErr := io.ReadAll(io.LimitReader(part, intake.MaxTextLength*4+1))
			_ = part.Close()
			if readErr != nil {
				return intake.Input{}, readErr
			}
			if len(value) > intake.MaxTextLength*4 {
				return intake.Input{}, &intake.ValidationError{Fields: map[string]string{name: "Field is too long."}}
			}
			if name == "source" {
				input.Source = string(value)
			} else {
				input.Text = string(value)
			}
			continue
		}
		if name != "files" || part.FileName() == "" {
			_ = part.Close()
			return intake.Input{}, fmt.Errorf("unsupported multipart field %q", name)
		}
		if len(input.Attachments) >= intake.MaxAttachments {
			_ = part.Close()
			return intake.Input{}, &intake.ValidationError{Fields: map[string]string{
				"files": fmt.Sprintf("Upload at most %d files.", intake.MaxAttachments),
			}}
		}
		tempFile, tempErr := a.store.NewIntakeTempFile()
		if tempErr != nil {
			_ = part.Close()
			return input, tempErr
		}
		tempPath := tempFile.Name()
		written, readErr := io.Copy(tempFile, io.LimitReader(part, intake.MaxFileBytes+1))
		closeErr := tempFile.Close()
		_ = part.Close()
		if readErr != nil || closeErr != nil {
			a.store.RemoveIntakeTempFile(tempPath)
			if readErr != nil {
				return input, readErr
			}
			return input, closeErr
		}
		if written > intake.MaxFileBytes {
			a.store.RemoveIntakeTempFile(tempPath)
			return intake.Input{}, &intake.ValidationError{Fields: map[string]string{
				"files": fmt.Sprintf("The file must be at most %d MB.", intake.MaxFileBytes>>20),
			}}
		}
		mediaType := part.Header.Get("Content-Type")
		input.Attachments = append(input.Attachments, intake.AttachmentInput{
			FileName: part.FileName(), MediaType: mediaType, TempPath: tempPath, SizeBytes: written,
		})
	}
	return input, nil
}

func (a *API) readIntakeBatchParts(reader *multipart.Reader) (text string, attachments []intake.AttachmentInput, resultErr error) {
	defer func() {
		if resultErr != nil {
			a.discardIntakeTempFiles(attachments)
		}
	}()
	var totalBytes int64
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, err
		}
		name := part.FormName()
		if name == "text" {
			value, readErr := io.ReadAll(io.LimitReader(part, intake.MaxTextLength*4+1))
			_ = part.Close()
			if readErr != nil {
				return "", attachments, readErr
			}
			if len(value) > intake.MaxTextLength*4 {
				return "", attachments, &intake.ValidationError{Fields: map[string]string{"text": "Field is too long."}}
			}
			text = string(value)
			continue
		}
		if name != "files" || part.FileName() == "" {
			_ = part.Close()
			return "", attachments, fmt.Errorf("unsupported multipart field %q", name)
		}
		if len(attachments) >= intake.MaxBatchFiles {
			_ = part.Close()
			return "", attachments, &intake.ValidationError{Fields: map[string]string{
				"files": fmt.Sprintf("Upload at most %d files at once.", intake.MaxBatchFiles),
			}}
		}
		tempFile, tempErr := a.store.NewIntakeTempFile()
		if tempErr != nil {
			_ = part.Close()
			return "", attachments, tempErr
		}
		tempPath := tempFile.Name()
		written, readErr := io.Copy(tempFile, io.LimitReader(part, intake.MaxFileBytes+1))
		closeErr := tempFile.Close()
		_ = part.Close()
		if readErr != nil || closeErr != nil {
			a.store.RemoveIntakeTempFile(tempPath)
			if readErr != nil {
				return "", attachments, readErr
			}
			return "", attachments, closeErr
		}
		if written > intake.MaxFileBytes {
			a.store.RemoveIntakeTempFile(tempPath)
			return "", attachments, &intake.ValidationError{Fields: map[string]string{
				"files": fmt.Sprintf("Each file must be at most %d MB.", intake.MaxFileBytes>>20),
			}}
		}
		totalBytes += written
		if totalBytes > intake.MaxBatchBytes {
			a.store.RemoveIntakeTempFile(tempPath)
			return "", attachments, &intake.ValidationError{Fields: map[string]string{
				"files": fmt.Sprintf("A batch must be at most %d MB.", intake.MaxBatchBytes>>20),
			}}
		}
		attachments = append(attachments, intake.AttachmentInput{
			FileName: part.FileName(), MediaType: part.Header.Get("Content-Type"), TempPath: tempPath, SizeBytes: written,
		})
	}
	return text, attachments, nil
}

func sourceForBatchAttachment(mediaType string) string {
	parsed, _, err := mime.ParseMediaType(strings.TrimSpace(mediaType))
	if err == nil {
		mediaType = parsed
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if strings.HasPrefix(mediaType, "audio/") {
		return intake.SourceVoice
	}
	if mediaType == "application/pdf" {
		return intake.SourceReceipt
	}
	return intake.SourcePurchasePhoto
}

func (a *API) discardIntakeTempFiles(attachments []intake.AttachmentInput) {
	for _, attachment := range attachments {
		if attachment.TempPath != "" {
			a.store.RemoveIntakeTempFile(attachment.TempPath)
		}
	}
}

func (a *API) handleIntakeInputError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *intake.ValidationError
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &validation):
		a.validationError(w, r, validation.Fields)
	case errors.As(err, &tooLarge):
		a.errorResponse(w, r, http.StatusRequestEntityTooLarge, "request_too_large", "Intake request is too large.", nil)
	default:
		a.errorResponse(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
	}
}

func (a *API) handleIntakeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrIntakeNotFound) || errors.Is(err, store.ErrAttachmentNotFound) {
		a.errorResponse(w, r, http.StatusNotFound, "not_found", "Intake entry or attachment not found.", nil)
		return
	}
	a.internalError(w, r, err)
}

func addAttachmentURLs(entry *intake.Entry) {
	for index := range entry.Attachments {
		entry.Attachments[index].URL = "/api/v1/intake/" + url.PathEscape(entry.ID) +
			"/attachments/" + url.PathEscape(entry.Attachments[index].ID)
	}
}
