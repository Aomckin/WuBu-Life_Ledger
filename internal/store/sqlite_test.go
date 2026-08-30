package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"stockroom/internal/inventory"
)

func TestStoreLifecycleAndSummary(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	threshold := 2.0
	price := int64(299)
	expiry := "2099-01-01"
	input, err := inventory.NormalizeAndValidate(inventory.ItemInput{
		Name: "Dish soap", Category: "cleaning", Icon: "soap", Quantity: 2,
		Unit: "bottles", Location: "Utility closet", UnitPriceCents: &price, LowStockThreshold: &threshold, ExpiryDate: &expiry,
	})
	if err != nil {
		t.Fatalf("normalize input: %v", err)
	}

	created, err := database.Create(ctx, "00000000-0000-4000-8000-000000000001", input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Version != 1 || created.Quantity != 2 {
		t.Fatalf("created item = %#v", created)
	}

	adjusted, err := database.Adjust(ctx, created.ID, -1)
	if err != nil {
		t.Fatalf("Adjust() error = %v", err)
	}
	if adjusted.Quantity != 1 || adjusted.Version != 2 {
		t.Fatalf("adjusted item = %#v", adjusted)
	}
	if _, err := database.Adjust(ctx, created.ID, -2); !errors.Is(err, ErrNegativeQuantity) {
		t.Fatalf("negative Adjust() error = %v, want ErrNegativeQuantity", err)
	}

	result, err := database.List(ctx, inventory.ListFilter{Search: "soap", Category: "cleaning", Status: "low", Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("List() = %#v", result)
	}

	summary, err := database.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.Tracked != 1 || summary.LowStock != 1 || summary.OutOfStock != 0 || summary.InventoryValueCents != 299 {
		t.Fatalf("Summary() = %#v", summary)
	}

	if err := database.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := database.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after delete error = %v, want ErrNotFound", err)
	}
}

func TestShortLivedItemAutoConsumesAndCanBeReactivated(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	input, err := inventory.NormalizeAndValidate(inventory.ItemInput{Name: "Pretzel", Quantity: 1, ShortLived: true})
	if err != nil {
		t.Fatalf("normalize input: %v", err)
	}
	created, err := database.Create(ctx, "short-lived-item", input)
	if err != nil || !created.ShortLived || created.AutoConsumeAt == nil {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
	filtered, err := database.List(ctx, inventory.ListFilter{Status: "short_lived", Limit: 10})
	if err != nil || filtered.Total != 1 {
		t.Fatalf("short-lived List() = %#v, %v", filtered, err)
	}
	if _, err := database.db.Exec(`UPDATE items SET auto_consume_at = '2000-01-01T00:00:00Z' WHERE id = 'short-lived-item'`); err != nil {
		t.Fatalf("expire item schedule: %v", err)
	}
	autoConsumed, err := database.Get(ctx, created.ID)
	if err != nil || autoConsumed.Quantity != 0 || autoConsumed.AutoConsumeAt != nil || !autoConsumed.ShortLived {
		t.Fatalf("auto-consumed item = %#v, %v", autoConsumed, err)
	}
	reactivated, err := database.Adjust(ctx, created.ID, 1)
	if err != nil || reactivated.Quantity != 1 || reactivated.AutoConsumeAt == nil {
		t.Fatalf("reactivated item = %#v, %v", reactivated, err)
	}
}
