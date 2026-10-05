package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stockroom/internal/intake"
	"stockroom/internal/inventory"

	"github.com/google/uuid"
)

const intakeColumns = `id, batch_id, source, text, status, workflow_state, failure_reason, created_at, updated_at`

type IntakeBatchEntry struct {
	ID    string
	Input intake.Input
}

func (s *Store) NewIntakeTempFile() (*os.File, error) {
	file, err := os.CreateTemp(s.intakeDir, ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("create intake upload: %w", err)
	}
	return file, nil
}

func (s *Store) RemoveIntakeTempFile(path string) {
	if s.isIntakeTempPath(path) {
		_ = os.Remove(path)
	}
}

func (s *Store) CreateIntake(ctx context.Context, id string, input intake.Input) (intake.Entry, error) {
	if err := s.purgeExpiredIntakeAttachments(ctx); err != nil {
		return intake.Entry{}, err
	}
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	expiresAt := nowTime.Add(intake.AttachmentTTL).Format(time.RFC3339Nano)
	storageKeys := make([]string, 0, len(input.Attachments))
	for _, attachment := range input.Attachments {
		if err := s.persistIntakeAttachment(attachment); err != nil {
			s.removeStorageKeys(storageKeys)
			return intake.Entry{}, err
		}
		storageKeys = append(storageKeys, attachment.ID)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.removeStorageKeys(storageKeys)
		return intake.Entry{}, fmt.Errorf("begin intake transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `INSERT INTO intake_entries
(id, batch_id, source, text, status, workflow_state, failure_reason, created_at, updated_at)
VALUES (?, '', ?, ?, ?, ?, '', ?, ?)`,
		id, input.Source, input.Text, intake.StatusPending, intake.WorkflowQueued, now, now); err != nil {
		s.removeStorageKeys(storageKeys)
		return intake.Entry{}, fmt.Errorf("create intake entry: %w", err)
	}
	for _, attachment := range input.Attachments {
		if _, err := tx.ExecContext(ctx, `INSERT INTO intake_attachments
(id, entry_id, file_name, media_type, size_bytes, content, storage_key, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, attachment.ID, id, attachment.FileName, attachment.MediaType,
			attachment.SizeBytes, []byte{}, attachment.ID, expiresAt, now); err != nil {
			s.removeStorageKeys(storageKeys)
			return intake.Entry{}, fmt.Errorf("create intake attachment: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		s.removeStorageKeys(storageKeys)
		return intake.Entry{}, fmt.Errorf("commit intake transaction: %w", err)
	}
	return s.GetIntake(ctx, id)
}

// CreateIntakeBatch persists a group of independent inbox entries atomically.
// Each entry keeps one attachment so the existing analysis pipeline and its
// temporary-file lifecycle remain unchanged.
func (s *Store) CreateIntakeBatch(ctx context.Context, batchID string, entries []IntakeBatchEntry) ([]intake.Entry, error) {
	if len(entries) == 0 {
		return nil, errors.New("intake batch must contain at least one entry")
	}
	if err := s.purgeExpiredIntakeAttachments(ctx); err != nil {
		return nil, err
	}
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	expiresAt := nowTime.Add(intake.AttachmentTTL).Format(time.RFC3339Nano)
	storageKeys := make([]string, 0, len(entries))
	for _, entry := range entries {
		for _, attachment := range entry.Input.Attachments {
			if err := s.persistIntakeAttachment(attachment); err != nil {
				s.removeStorageKeys(storageKeys)
				return nil, err
			}
			storageKeys = append(storageKeys, attachment.ID)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.removeStorageKeys(storageKeys)
		return nil, fmt.Errorf("begin intake batch transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, entry := range entries {
		if _, err := tx.ExecContext(ctx, `INSERT INTO intake_entries
(id, batch_id, source, text, status, workflow_state, failure_reason, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, '', ?, ?)`,
			entry.ID, batchID, entry.Input.Source, entry.Input.Text, intake.StatusPending, intake.WorkflowQueued, now, now); err != nil {
			s.removeStorageKeys(storageKeys)
			return nil, fmt.Errorf("create intake batch entry: %w", err)
		}
		for _, attachment := range entry.Input.Attachments {
			if _, err := tx.ExecContext(ctx, `INSERT INTO intake_attachments
(id, entry_id, file_name, media_type, size_bytes, content, storage_key, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, attachment.ID, entry.ID, attachment.FileName, attachment.MediaType,
				attachment.SizeBytes, []byte{}, attachment.ID, expiresAt, now); err != nil {
				s.removeStorageKeys(storageKeys)
				return nil, fmt.Errorf("create intake batch attachment: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		s.removeStorageKeys(storageKeys)
		return nil, fmt.Errorf("commit intake batch transaction: %w", err)
	}
	result := make([]intake.Entry, 0, len(entries))
	for _, entry := range entries {
		stored, err := s.GetIntake(ctx, entry.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, stored)
	}
	return result, nil
}

func (s *Store) GetIntake(ctx context.Context, id string) (intake.Entry, error) {
	if err := s.purgeExpiredIntakeAttachments(ctx); err != nil {
		return intake.Entry{}, err
	}
	entry, err := scanIntake(s.db.QueryRowContext(ctx, `SELECT `+intakeColumns+` FROM intake_entries WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return intake.Entry{}, ErrIntakeNotFound
	}
	if err != nil {
		return intake.Entry{}, fmt.Errorf("get intake entry: %w", err)
	}
	entry.Attachments, err = s.listIntakeAttachments(ctx, id)
	if err != nil {
		return intake.Entry{}, err
	}
	entry.Analysis, err = s.getIntakeAnalysis(ctx, id)
	if err != nil {
		return intake.Entry{}, err
	}
	return entry, nil
}

func (s *Store) ListIntakes(ctx context.Context, filter intake.ListFilter) (intake.ListResult, error) {
	if err := s.purgeExpiredIntakeAttachments(ctx); err != nil {
		return intake.ListResult{}, err
	}
	whereParts := []string{"1 = 1"}
	args := []any{}
	if filter.Status != "" {
		whereParts = append(whereParts, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.WorkflowState != "" {
		whereParts = append(whereParts, "workflow_state = ?")
		args = append(args, filter.WorkflowState)
	}
	if filter.BatchID != "" {
		whereParts = append(whereParts, "batch_id = ?")
		args = append(args, filter.BatchID)
	}
	where := strings.Join(whereParts, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intake_entries WHERE `+where, args...).Scan(&total); err != nil {
		return intake.ListResult{}, fmt.Errorf("count intake entries: %w", err)
	}

	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT `+intakeColumns+` FROM intake_entries WHERE `+where+`
ORDER BY created_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return intake.ListResult{}, fmt.Errorf("list intake entries: %w", err)
	}
	entries := make([]intake.Entry, 0)
	for rows.Next() {
		entry, err := scanIntake(rows)
		if err != nil {
			_ = rows.Close()
			return intake.ListResult{}, fmt.Errorf("scan intake entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return intake.ListResult{}, fmt.Errorf("iterate intake entries: %w", err)
	}
	if err := rows.Close(); err != nil {
		return intake.ListResult{}, fmt.Errorf("close intake rows: %w", err)
	}
	for index := range entries {
		entries[index].Attachments, err = s.listIntakeAttachments(ctx, entries[index].ID)
		if err != nil {
			return intake.ListResult{}, err
		}
		entries[index].Analysis, err = s.getIntakeAnalysis(ctx, entries[index].ID)
		if err != nil {
			return intake.ListResult{}, err
		}
	}
	return intake.ListResult{Entries: entries, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (s *Store) UpdateIntakeStatus(ctx context.Context, id, status string) (intake.Entry, error) {
	if err := s.purgeExpiredIntakeAttachments(ctx); err != nil {
		return intake.Entry{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	storageKeys := []string{}
	if status == intake.StatusReviewed {
		var err error
		storageKeys, err = s.intakeStorageKeys(ctx, id)
		if err != nil {
			return intake.Entry{}, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return intake.Entry{}, fmt.Errorf("begin intake status transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	workflowState := intake.WorkflowQueued
	if status == intake.StatusReviewed {
		workflowState = intake.WorkflowDiscarded
	}
	result, err := tx.ExecContext(ctx, `UPDATE intake_entries
SET status = ?, workflow_state = ?, failure_reason = '', updated_at = ? WHERE id = ?`, status, workflowState, now, id)
	if err != nil {
		return intake.Entry{}, fmt.Errorf("update intake status: %w", err)
	}
	if err := requireIntakeAffected(result); err != nil {
		return intake.Entry{}, err
	}
	if status == intake.StatusReviewed {
		if _, err := tx.ExecContext(ctx, `DELETE FROM intake_attachments WHERE entry_id = ?`, id); err != nil {
			return intake.Entry{}, fmt.Errorf("delete reviewed intake attachment: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM intake_analyses WHERE entry_id = ?`, id); err != nil {
			return intake.Entry{}, fmt.Errorf("delete reviewed intake analysis: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return intake.Entry{}, fmt.Errorf("commit intake status transaction: %w", err)
	}
	s.removeStorageKeys(storageKeys)
	return s.GetIntake(ctx, id)
}

func (s *Store) SetIntakeWorkflow(ctx context.Context, id, workflowState, failureReason string) (intake.Entry, error) {
	if !intake.ValidWorkflowState(workflowState) {
		return intake.Entry{}, fmt.Errorf("unsupported intake workflow state %q", workflowState)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE intake_entries
SET workflow_state = ?, failure_reason = ?, updated_at = ?
WHERE id = ? AND status = ?`, workflowState, strings.TrimSpace(failureReason), now, id, intake.StatusPending)
	if err != nil {
		return intake.Entry{}, fmt.Errorf("update intake workflow: %w", err)
	}
	if err := requireIntakeAffected(result); err != nil {
		return intake.Entry{}, err
	}
	return s.GetIntake(ctx, id)
}

// BeginIntakeAnalysis atomically claims a queued or failed entry for analysis.
// It prevents a manual retry and the batch worker from analysing the same
// source concurrently.
func (s *Store) BeginIntakeAnalysis(ctx context.Context, id string) (intake.Entry, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE intake_entries
SET workflow_state = ?, failure_reason = '', updated_at = ?
WHERE id = ? AND status = ? AND workflow_state IN (?, ?)`, intake.WorkflowAnalyzing, now, id,
		intake.StatusPending, intake.WorkflowQueued, intake.WorkflowFailed)
	if err != nil {
		return intake.Entry{}, fmt.Errorf("claim intake analysis: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return intake.Entry{}, fmt.Errorf("read intake analysis claim result: %w", err)
	}
	if affected == 0 {
		if _, err := s.GetIntake(ctx, id); err != nil {
			return intake.Entry{}, err
		}
		return intake.Entry{}, ErrIntakeWorkflowConflict
	}
	return s.GetIntake(ctx, id)
}

func (s *Store) IntakeSettings(ctx context.Context) (intake.Settings, error) {
	var autoAnalyze int
	if err := s.db.QueryRowContext(ctx, `SELECT auto_analyze FROM intake_settings WHERE id = 1`).Scan(&autoAnalyze); err != nil {
		return intake.Settings{}, fmt.Errorf("read intake settings: %w", err)
	}
	return intake.Settings{AutoAnalyze: autoAnalyze == 1}, nil
}

func (s *Store) UpdateIntakeSettings(ctx context.Context, settings intake.Settings) (intake.Settings, error) {
	value := 0
	if settings.AutoAnalyze {
		value = 1
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE intake_settings SET auto_analyze = ? WHERE id = 1`, value); err != nil {
		return intake.Settings{}, fmt.Errorf("update intake settings: %w", err)
	}
	return s.IntakeSettings(ctx)
}

func (s *Store) GetIntakeAttachment(ctx context.Context, entryID, attachmentID string) (intake.AttachmentContent, error) {
	if err := s.purgeExpiredIntakeAttachments(ctx); err != nil {
		return intake.AttachmentContent{}, err
	}
	var attachment intake.AttachmentContent
	var legacyData []byte
	var storageKey sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, file_name, media_type, size_bytes, expires_at, content, storage_key
FROM intake_attachments WHERE entry_id = ? AND id = ?`, entryID, attachmentID).Scan(
		&attachment.ID, &attachment.FileName, &attachment.MediaType, &attachment.SizeBytes,
		&attachment.ExpiresAt, &legacyData, &storageKey)
	if errors.Is(err, sql.ErrNoRows) {
		return intake.AttachmentContent{}, ErrAttachmentNotFound
	}
	if err != nil {
		return intake.AttachmentContent{}, fmt.Errorf("get intake attachment: %w", err)
	}
	if storageKey.Valid && storageKey.String != "" {
		attachment.Data, err = os.ReadFile(s.intakePath(storageKey.String))
		if errors.Is(err, os.ErrNotExist) {
			return intake.AttachmentContent{}, ErrAttachmentNotFound
		}
		if err != nil {
			return intake.AttachmentContent{}, fmt.Errorf("read intake attachment: %w", err)
		}
	} else {
		attachment.Data = legacyData
	}
	return attachment, nil
}

func (s *Store) SaveIntakeAnalysis(ctx context.Context, entryID string, analysis intake.Analysis) (intake.Entry, error) {
	analysis.Model = strings.TrimSpace(analysis.Model)
	analysis.Summary = strings.TrimSpace(analysis.Summary)
	if analysis.Model == "" || analysis.Summary == "" {
		return intake.Entry{}, errors.New("intake analysis requires model and summary")
	}
	suggestions, err := json.Marshal(analysis.Suggestions)
	if err != nil {
		return intake.Entry{}, fmt.Errorf("encode intake analysis suggestions: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	analysis.ProcessedAt = now
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return intake.Entry{}, fmt.Errorf("begin intake analysis transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	updated, err := tx.ExecContext(ctx, `UPDATE intake_entries SET updated_at = ? WHERE id = ?`, now, entryID)
	if err != nil {
		return intake.Entry{}, fmt.Errorf("touch intake entry for analysis: %w", err)
	}
	if err := requireIntakeAffected(updated); err != nil {
		return intake.Entry{}, err
	}
	needsReview := 0
	if analysis.NeedsReview {
		needsReview = 1
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO intake_analyses
(entry_id, model, summary, suggestions_json, needs_review, processed_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(entry_id) DO UPDATE SET model = excluded.model, summary = excluded.summary,
suggestions_json = excluded.suggestions_json, needs_review = excluded.needs_review,
processed_at = excluded.processed_at, updated_at = excluded.updated_at`,
		entryID, analysis.Model, analysis.Summary, string(suggestions), needsReview, now, now, now); err != nil {
		return intake.Entry{}, fmt.Errorf("save intake analysis: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return intake.Entry{}, fmt.Errorf("commit intake analysis transaction: %w", err)
	}
	return s.GetIntake(ctx, entryID)
}

func (s *Store) ReplaceIntakeSuggestions(ctx context.Context, entryID string, suggestions []intake.Suggestion) (intake.Entry, error) {
	encoded, err := json.Marshal(suggestions)
	if err != nil {
		return intake.Entry{}, fmt.Errorf("encode corrected intake suggestions: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return intake.Entry{}, fmt.Errorf("begin corrected intake suggestions transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE intake_analyses
SET suggestions_json = ?, needs_review = 1, updated_at = ?
WHERE entry_id = ?`, string(encoded), now, entryID)
	if err != nil {
		return intake.Entry{}, fmt.Errorf("update corrected intake suggestions: %w", err)
	}
	if err := requireIntakeAffected(result); err != nil {
		return intake.Entry{}, errors.New("inbox entry has no saved analysis")
	}
	result, err = tx.ExecContext(ctx, `UPDATE intake_entries
SET workflow_state = ?, failure_reason = '', updated_at = ?
WHERE id = ? AND status = ?`, intake.WorkflowNeedsReview, now, entryID, intake.StatusPending)
	if err != nil {
		return intake.Entry{}, fmt.Errorf("touch corrected intake entry: %w", err)
	}
	if err := requireIntakeAffected(result); err != nil {
		return intake.Entry{}, err
	}
	if err := tx.Commit(); err != nil {
		return intake.Entry{}, fmt.Errorf("commit corrected intake suggestions: %w", err)
	}
	return s.GetIntake(ctx, entryID)
}

// ApplyIntakeAnalysis applies one persisted inbox analysis and then removes it
// from the pending inbox. The item writes and the reviewed status commit as one
// transaction, so repeating a bulk action cannot apply the same entry twice.
func (s *Store) ApplyIntakeAnalysis(ctx context.Context, entryID string) (int, error) {
	if _, err := s.ApplyDueAutoConsumption(ctx); err != nil {
		return 0, err
	}
	storageKeys, err := s.intakeStorageKeys(ctx, entryID)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin inbox apply transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var status, suggestionsJSON string
	err = tx.QueryRowContext(ctx, `SELECT e.status, a.suggestions_json
FROM intake_entries e JOIN intake_analyses a ON a.entry_id = e.id WHERE e.id = ?`, entryID).Scan(&status, &suggestionsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errors.New("inbox entry has no saved analysis")
	}
	if err != nil {
		return 0, fmt.Errorf("read inbox analysis: %w", err)
	}
	if status != intake.StatusPending {
		return 0, errors.New("only pending inbox entries can be added")
	}
	var suggestions []intake.Suggestion
	if err := json.Unmarshal([]byte(suggestionsJSON), &suggestions); err != nil {
		return 0, fmt.Errorf("decode inbox suggestions: %w", err)
	}

	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	newSchedule := nowTime.Add(inventory.ShortLivedTTL).Format(time.RFC3339Nano)
	applied := 0
	for _, suggestion := range suggestions {
		intent := strings.ToLower(strings.TrimSpace(suggestion.Intent))
		if intent == "unknown" || intent == "" {
			continue
		}
		if intent != "increase" && intent != "decrease" && intent != "create" {
			return 0, fmt.Errorf("unsupported suggestion intent %q", suggestion.Intent)
		}
		if math.IsNaN(suggestion.Amount) || math.IsInf(suggestion.Amount, 0) || suggestion.Amount <= 0 {
			return 0, fmt.Errorf("invalid amount for %q", suggestion.ItemName)
		}
		name := strings.TrimSpace(suggestion.ItemName)
		unit := strings.TrimSpace(suggestion.Unit)
		if name == "" {
			return 0, errors.New("suggestion has no item name")
		}
		if strings.TrimSpace(suggestion.Category) == "" {
			return 0, fmt.Errorf("suggestion for %q has no category; analyze the inbox again", name)
		}
		if unit == "" {
			unit = "pcs"
		}

		item, found, err := lookupInboxItem(tx, ctx, name)
		if err != nil {
			return 0, err
		}
		if found && !strings.EqualFold(item.Unit, unit) {
			return 0, fmt.Errorf("unit mismatch for %q: inbox says %s, inventory has %s", name, unit, item.Unit)
		}
		if !found {
			if intent == "decrease" {
				return 0, fmt.Errorf("cannot reduce %q because it is not in inventory", name)
			}
			input, err := inventory.NormalizeAndValidate(inventory.ItemInput{Name: name, Category: suggestion.Category, Quantity: suggestion.Amount, Unit: unit})
			if err != nil {
				return 0, fmt.Errorf("normalize inbox item %q: %w", name, err)
			}
			for _, category := range inventory.Categories {
				if category.Value == input.Category {
					input.Icon = category.Icon
					break
				}
			}
			itemID := uuid.NewString()
			if _, err := tx.ExecContext(ctx, `INSERT INTO items (
id, name, category, icon, quantity, unit, location, low_stock_threshold, expiry_date, unit_price_cents,
short_lived, auto_consume_at, notes, created_at, updated_at, version
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
				itemID, input.Name, input.Category, input.Icon, input.Quantity, input.Unit, input.Location,
				input.LowStockThreshold, input.ExpiryDate, input.UnitPriceCents, input.ShortLived, input.AutoConsumeAt, input.Notes, now, now); err != nil {
				return 0, fmt.Errorf("create inbox item: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE inventory_events SET source='intake',note=? WHERE rowid=(SELECT MAX(rowid) FROM inventory_events WHERE item_id=?)`, "Intake entry "+entryID, itemID); err != nil {
				return 0, err
			}
			applied++
			continue
		}

		delta := suggestion.Amount
		if intent == "decrease" {
			delta = -delta
		}
		result, err := tx.ExecContext(ctx, `UPDATE items SET
quantity = ROUND(quantity + ?, 3),
auto_consume_at = CASE
  WHEN ROUND(quantity + ?, 3) <= 0 THEN NULL
  WHEN short_lived = 1 AND auto_consume_at IS NULL THEN ?
  ELSE auto_consume_at
END,
updated_at = ?, version = version + 1
WHERE id = ? AND quantity + ? >= 0`, delta, delta, newSchedule, now, item.ID, delta)
		if err != nil {
			return 0, fmt.Errorf("apply inbox quantity: %w", err)
		}
		if err := requireAffected(result); err != nil {
			return 0, ErrNegativeQuantity
		}
		eventType := "ADJUST"
		if delta < 0 {
			eventType = "CONSUME"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE inventory_events SET type=?,source='intake',note=? WHERE rowid=(SELECT MAX(rowid) FROM inventory_events WHERE item_id=?)`, eventType, "Intake entry "+entryID, item.ID); err != nil {
			return 0, err
		}
		applied++
	}
	if _, err := tx.ExecContext(ctx, `UPDATE intake_entries
SET status = ?, workflow_state = ?, failure_reason = '', updated_at = ? WHERE id = ?`, intake.StatusReviewed, intake.WorkflowApplied, now, entryID); err != nil {
		return 0, fmt.Errorf("mark applied inbox entry reviewed: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM intake_attachments WHERE entry_id = ?`, entryID); err != nil {
		return 0, fmt.Errorf("delete applied inbox attachment: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM intake_analyses WHERE entry_id = ?`, entryID); err != nil {
		return 0, fmt.Errorf("delete applied inbox analysis: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit inbox apply transaction: %w", err)
	}
	s.removeStorageKeys(storageKeys)
	return applied, nil
}

type inboxItemMatch struct {
	ID   string
	Unit string
}

func lookupInboxItem(tx *sql.Tx, ctx context.Context, name string) (inboxItemMatch, bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, unit FROM items WHERE name = ? COLLATE NOCASE ORDER BY created_at LIMIT 2`, name)
	if err != nil {
		return inboxItemMatch{}, false, fmt.Errorf("find inbox item: %w", err)
	}
	defer rows.Close()
	matches := []inboxItemMatch{}
	for rows.Next() {
		var match inboxItemMatch
		if err := rows.Scan(&match.ID, &match.Unit); err != nil {
			return inboxItemMatch{}, false, fmt.Errorf("scan inbox item: %w", err)
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return inboxItemMatch{}, false, fmt.Errorf("iterate inbox items: %w", err)
	}
	if len(matches) > 1 {
		return inboxItemMatch{}, false, fmt.Errorf("multiple inventory items match %q", name)
	}
	if len(matches) == 0 {
		return inboxItemMatch{}, false, nil
	}
	return matches[0], true, nil
}

func (s *Store) listIntakeAttachments(ctx context.Context, entryID string) ([]intake.Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, file_name, media_type, size_bytes, expires_at
FROM intake_attachments WHERE entry_id = ? ORDER BY created_at, id`, entryID)
	if err != nil {
		return nil, fmt.Errorf("list intake attachments: %w", err)
	}
	defer rows.Close()
	attachments := make([]intake.Attachment, 0)
	for rows.Next() {
		var attachment intake.Attachment
		if err := rows.Scan(&attachment.ID, &attachment.FileName, &attachment.MediaType, &attachment.SizeBytes, &attachment.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan intake attachment: %w", err)
		}
		attachments = append(attachments, attachment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate intake attachments: %w", err)
	}
	return attachments, nil
}

func (s *Store) getIntakeAnalysis(ctx context.Context, entryID string) (*intake.Analysis, error) {
	var analysis intake.Analysis
	var suggestions string
	err := s.db.QueryRowContext(ctx, `SELECT model, summary, suggestions_json, needs_review, processed_at
FROM intake_analyses WHERE entry_id = ?`, entryID).Scan(
		&analysis.Model, &analysis.Summary, &suggestions, &analysis.NeedsReview, &analysis.ProcessedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get intake analysis: %w", err)
	}
	if err := json.Unmarshal([]byte(suggestions), &analysis.Suggestions); err != nil {
		return nil, fmt.Errorf("decode intake analysis suggestions: %w", err)
	}
	return &analysis, nil
}

func (s *Store) persistIntakeAttachment(attachment intake.AttachmentInput) error {
	destination := s.intakePath(attachment.ID)
	if attachment.TempPath != "" {
		if !s.isIntakeTempPath(attachment.TempPath) {
			return errors.New("intake upload is outside the managed temporary directory")
		}
		if err := os.Rename(attachment.TempPath, destination); err != nil {
			return fmt.Errorf("stage intake attachment: %w", err)
		}
		return nil
	}
	part, err := os.CreateTemp(s.intakeDir, ".write-*")
	if err != nil {
		return fmt.Errorf("create intake attachment file: %w", err)
	}
	partName := part.Name()
	defer func() { _ = os.Remove(partName) }()
	if _, err := part.Write(attachment.Data); err != nil {
		_ = part.Close()
		return fmt.Errorf("write intake attachment file: %w", err)
	}
	if err := part.Close(); err != nil {
		return fmt.Errorf("close intake attachment file: %w", err)
	}
	if err := os.Rename(partName, destination); err != nil {
		return fmt.Errorf("stage intake attachment: %w", err)
	}
	return nil
}

func (s *Store) purgeExpiredIntakeAttachments(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `SELECT storage_key FROM intake_attachments
WHERE expires_at IS NOT NULL AND julianday(expires_at) <= julianday(?) AND storage_key IS NOT NULL`, now)
	if err != nil {
		return fmt.Errorf("list expired intake attachments: %w", err)
	}
	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan expired intake attachment: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close expired intake rows: %w", err)
	}
	s.removeStorageKeys(keys)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM intake_attachments
WHERE expires_at IS NOT NULL AND julianday(expires_at) <= julianday(?)`, now); err != nil {
		return fmt.Errorf("purge expired intake attachments: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM intake_entries
WHERE status = 'pending' AND source <> 'command'
AND NOT EXISTS (SELECT 1 FROM intake_attachments WHERE entry_id = intake_entries.id)
AND NOT EXISTS (SELECT 1 FROM intake_analyses WHERE entry_id = intake_entries.id)`); err != nil {
		return fmt.Errorf("purge empty expired intake entries: %w", err)
	}
	return nil
}

func (s *Store) intakeStorageKeys(ctx context.Context, entryID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT storage_key FROM intake_attachments
WHERE entry_id = ? AND storage_key IS NOT NULL`, entryID)
	if err != nil {
		return nil, fmt.Errorf("list intake storage keys: %w", err)
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan intake storage key: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) removeStorageKeys(keys []string) {
	for _, key := range keys {
		if key != "" && filepath.Base(key) == key {
			_ = os.Remove(s.intakePath(key))
		}
	}
}

func (s *Store) intakePath(key string) string {
	return filepath.Join(s.intakeDir, filepath.Base(key))
}

func (s *Store) isIntakeTempPath(path string) bool {
	clean := filepath.Clean(path)
	return filepath.Dir(clean) == filepath.Clean(s.intakeDir) && strings.HasPrefix(filepath.Base(clean), ".upload-")
}

func scanIntake(row scanner) (intake.Entry, error) {
	var entry intake.Entry
	err := row.Scan(&entry.ID, &entry.BatchID, &entry.Source, &entry.Text, &entry.Status,
		&entry.WorkflowState, &entry.FailureReason, &entry.CreatedAt, &entry.UpdatedAt)
	return entry, err
}

func requireIntakeAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read intake write result: %w", err)
	}
	if affected == 0 {
		return ErrIntakeNotFound
	}
	return nil
}
