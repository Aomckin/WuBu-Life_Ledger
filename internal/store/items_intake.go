package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"stockroom/internal/intake"
	"stockroom/internal/inventory"
)

// ApplyIntakeSuggestions validates proposed inventory changes on the caller's
// transaction, which also records that the user applied this Intake entry.
func (s ItemsRepository) ApplyIntakeSuggestions(ctx context.Context, tx *sql.Tx, entryID string, nowTime time.Time, suggestions []intake.Suggestion) (int, error) {
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
		if found && inventory.UnitKey(item.Unit) != inventory.UnitKey(unit) {
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
short_lived, auto_consume_at, notes, created_at, updated_at, version,spec,package_size,package_unit
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1,?,?,?)`,
				itemID, input.Name, input.Category, input.Icon, input.Quantity, input.Unit, input.Location,
				input.LowStockThreshold, input.ExpiryDate, input.UnitPriceCents, input.ShortLived, input.AutoConsumeAt, input.Notes, now, now, input.Spec, input.PackageSize, input.PackageUnit); err != nil {
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

	return applied, nil
}

type inboxItemMatch struct {
	ID   string
	Unit string
}

func lookupInboxItem(tx *sql.Tx, ctx context.Context, name string) (inboxItemMatch, bool, error) {
	item, err := resolveTarget(ctx, tx, itemTarget{Name: name})
	if errors.Is(err, ErrNotFound) {
		return inboxItemMatch{}, false, nil
	}
	if err != nil {
		return inboxItemMatch{}, false, err
	}
	return inboxItemMatch{item.ID, item.Unit}, true, nil
}
