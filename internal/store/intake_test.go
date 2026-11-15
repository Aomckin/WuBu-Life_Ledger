package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"stockroom/internal/intake"
	"stockroom/internal/inventory"
)

func TestIntakeLifecycle(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	input, err := intake.NormalizeAndValidate(intake.Input{
		Source: intake.SourceReceipt, Text: "Weekly shop",
		Attachments: []intake.AttachmentInput{{
			ID: "attachment-1", FileName: "receipt.png", MediaType: "image/png", Data: []byte("image-content"),
		}},
	})
	if err != nil {
		t.Fatalf("normalize intake: %v", err)
	}

	created, err := database.CreateIntake(ctx, "intake-1", input)
	if err != nil {
		t.Fatalf("CreateIntake() error = %v", err)
	}
	if created.Status != intake.StatusPending || len(created.Attachments) != 1 {
		t.Fatalf("created intake = %#v", created)
	}
	storagePath := database.intakePath(created.Attachments[0].ID)
	if _, err := os.Stat(storagePath); err != nil {
		t.Fatalf("temporary attachment file: %v", err)
	}
	listed, err := database.ListIntakes(ctx, intake.ListFilter{Status: intake.StatusPending, Limit: 10})
	if err != nil || listed.Total != 1 || len(listed.Entries) != 1 {
		t.Fatalf("ListIntakes() = %#v, %v", listed, err)
	}
	attachment, err := database.GetIntakeAttachment(ctx, created.ID, created.Attachments[0].ID)
	if err != nil || !bytes.Equal(attachment.Data, []byte("image-content")) {
		t.Fatalf("GetIntakeAttachment() = %#v, %v", attachment, err)
	}
	withAnalysis, err := database.SaveIntakeAnalysis(ctx, created.ID, intake.Analysis{
		Model: "test-model", Summary: "Found one bottle of dish soap.", NeedsReview: true,
		Suggestions: []intake.Suggestion{{Intent: "increase", ItemName: "Dish soap", Amount: 1, Unit: "bottle", Category: "cleaning", Confidence: 0.9}},
	})
	if err != nil || withAnalysis.Analysis == nil || withAnalysis.Analysis.Model != "test-model" || len(withAnalysis.Analysis.Suggestions) != 1 {
		t.Fatalf("SaveIntakeAnalysis() = %#v, %v", withAnalysis, err)
	}
	reviewed, err := database.UpdateIntakeStatus(ctx, created.ID, intake.StatusReviewed)
	if err != nil || reviewed.Status != intake.StatusReviewed || len(reviewed.Attachments) != 0 || reviewed.Analysis != nil {
		t.Fatalf("UpdateIntakeStatus() = %#v, %v", reviewed, err)
	}
	if _, err := os.Stat(storagePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reviewed attachment still exists: %v", err)
	}
	if _, err := database.GetIntakeAttachment(ctx, created.ID, created.Attachments[0].ID); !errors.Is(err, ErrAttachmentNotFound) {
		t.Fatalf("GetIntakeAttachment() after review error = %v", err)
	}
}

func TestExpiredIntakeFileIsPurged(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	input, err := intake.NormalizeAndValidate(intake.Input{
		Source: intake.SourcePurchasePhoto,
		Attachments: []intake.AttachmentInput{{
			ID: "expired-file", FileName: "purchase.mp4", MediaType: "video/mp4", Data: []byte("video"),
		}},
	})
	if err != nil {
		t.Fatalf("normalize intake: %v", err)
	}
	if _, err := database.CreateIntake(ctx, "expired-entry", input); err != nil {
		t.Fatalf("CreateIntake() error = %v", err)
	}
	storagePath := database.intakePath("expired-file")
	if _, err := database.db.Exec(`UPDATE intake_attachments SET expires_at = '2000-01-01T00:00:00Z' WHERE id = 'expired-file'`); err != nil {
		t.Fatalf("expire attachment: %v", err)
	}
	listed, err := database.ListIntakes(ctx, intake.ListFilter{Status: intake.StatusPending, Limit: 10})
	if err != nil || listed.Total != 0 {
		t.Fatalf("ListIntakes() = %#v, %v", listed, err)
	}
	if _, err := os.Stat(storagePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired attachment still exists: %v", err)
	}
}

func TestApplyIntakeAnalysisCreatesAICategory(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	input, err := intake.NormalizeAndValidate(intake.Input{Source: intake.SourceCommand, Text: "Bought dish soap"})
	if err != nil {
		t.Fatalf("normalize intake: %v", err)
	}
	if _, err := database.CreateIntake(ctx, "category-entry", input); err != nil {
		t.Fatalf("CreateIntake() error = %v", err)
	}
	if _, err := database.SaveIntakeAnalysis(ctx, "category-entry", intake.Analysis{Model: "test-model", Summary: "Dish soap purchased.", NeedsReview: true, Suggestions: []intake.Suggestion{{Intent: "increase", ItemName: "Dish soap", Amount: 1, Unit: "bottle", Category: "cleaning", Confidence: 0.9}}}); err != nil {
		t.Fatalf("SaveIntakeAnalysis() error = %v", err)
	}
	if _, err := database.ApplyIntakeAnalysis(ctx, "category-entry"); err != nil {
		t.Fatalf("ApplyIntakeAnalysis() error = %v", err)
	}
	items, err := database.List(ctx, inventory.ListFilter{Limit: 10})
	if err != nil || len(items.Items) != 1 || items.Items[0].Category != "cleaning" || items.Items[0].Icon != "cleaning_services" {
		t.Fatalf("created category item = %#v, %v", items, err)
	}
}

func TestOpenRejectsSchema14WithoutMigrating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema14.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err = raw.Exec(`CREATE TABLE items(id TEXT PRIMARY KEY); INSERT INTO items VALUES ('historical'); PRAGMA user_version=14;`); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path); err == nil {
		t.Fatal("Open accepted schema 14")
	}
	var version int
	if err = raw.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 14 {
		t.Fatal(version, err)
	}
	var id string
	if err = raw.QueryRow(`SELECT id FROM items`).Scan(&id); err != nil || id != "historical" {
		t.Fatal(id, err)
	}
}
