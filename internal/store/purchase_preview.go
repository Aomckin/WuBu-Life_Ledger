package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
	"time"
)

// Validation uses a read-only snapshot: no ledger lock, mutations, IDs or keys.
func (s *Store) ValidatePurchase(ctx context.Context, input ledger.PurchaseInput) (ledger.Purchase, error) {
	var out ledger.Purchase
	p, e := ledger.ValidatePurchase(input)
	if e != nil {
		return out, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	out.PurchaseInput = p
	account, e := paymentAccountTx(ctx, tx, p.AccountID, p.PaymentMethod)
	if e != nil {
		return out, e
	}
	if account.CurrentBalance-ledger.SignedMoney(p.TotalAmount) < -1e12 {
		return out, fmt.Errorf("%w: balance out of range", ledger.ErrInvalid)
	}
	out.AccountID = account.ID
	out.Status = "PREVIEW"
	if p.ExternalID != "" {
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM purchase_events WHERE source=? AND external_id=?`, p.Source, p.ExternalID).Scan(&count); e != nil {
			return out, e
		}
		if count > 0 {
			return out, fmt.Errorf("%w: external_id already recorded", ledger.ErrConflict)
		}
	}
	projected := map[string]inventory.Item{}
	lineKeys := make([]string, len(out.Items))
	for i := range out.Items {
		l := &out.Items[i]
		out.ItemTotal += l.Subtotal
		if l.ItemType == ledger.Untracked {
			continue
		}
		// Durable lines without IDs represent new individual instances.
		if l.ItemType == ledger.Durable && l.ItemID == "" {
			after := l.Quantity
			l.InventoryAfter = &after
			l.QuantityAdded = l.Quantity
			continue
		}
		item, e := resolveTarget(ctx, tx, itemTarget{ID: l.ItemID, Name: l.Name, Spec: l.Spec})
		key := "new:" + inventory.NameKey(l.Name) + "#" + inventory.SpecKey(l.Spec)
		if errors.Is(e, ErrNotFound) && l.ItemID == "" {
			item = inventory.Item{Name: l.Name, ProductSpec: l.ProductSpec, Unit: l.Unit, ItemType: l.ItemType}
			e = nil
		}
		if e != nil {
			return out, e
		}
		if item.ID != "" {
			key = item.ID
		}
		if cached, ok := projected[key]; ok {
			item = cached
		} else if item.AutoConsumeAt != nil {
			if at, err := time.Parse(time.RFC3339Nano, *item.AutoConsumeAt); err == nil && !at.After(time.Now()) {
				item.Quantity = 0
			}
		}
		if inventory.UnitKey(item.Unit) != inventory.UnitKey(l.Unit) || item.ItemType != l.ItemType {
			return out, ledger.ErrConflict
		}
		if item.ItemType == ledger.Durable && item.ID != "" {
			var count int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM durable_details WHERE item_id=?`, item.ID).Scan(&count); e != nil {
				return out, e
			}
			if count > 0 || item.Quantity != 0 {
				return out, fmt.Errorf("%w: durable instance already has a purchase", ledger.ErrConflict)
			}
		}
		l.ItemID, l.Name, l.ProductSpec, l.Unit = item.ID, item.Name, item.ProductSpec, item.Unit
		item.Quantity += l.Quantity
		if math.IsInf(item.Quantity, 0) || item.Quantity > inventory.MaxQuantity {
			return out, fmt.Errorf("%w: quantity out of range", ledger.ErrInvalid)
		}
		projected[key] = item
		lineKeys[i] = key
		l.QuantityAdded = l.Quantity
	}
	for i, key := range lineKeys {
		if key != "" {
			after := projected[key].Quantity
			out.Items[i].InventoryAfter = &after
		}
	}
	ledger.DecoratePurchase(&out)
	return out, nil
}
