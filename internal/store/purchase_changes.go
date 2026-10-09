package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
)

func purchaseVersion(p ledger.Purchase, expected string) error {
	if expected == "" {
		return fmt.Errorf("%w: 请提供购买记录的当前版本。", ledger.ErrInvalid)
	}
	if expected != p.UpdatedAt {
		return fmt.Errorf("%w: 这笔购买已被修改，请重新打开明细后再操作。", ledger.ErrConflict)
	}
	return nil
}

func nextPurchaseVersion(previous string) string {
	now := time.Now().UTC()
	if old, err := time.Parse(time.RFC3339Nano, previous); err == nil && !now.After(old) {
		now = old.Add(time.Nanosecond)
	}
	return now.Format(time.RFC3339Nano)
}

// AmendPurchase applies net stock differences, never rolls the stock back to its
// original purchase-time quantity. Later consumption therefore remains intact.
func (s *Store) AmendPurchase(ctx context.Context, key, id string, input ledger.PurchaseAmendment) (ledger.Purchase, bool, error) {
	var out ledger.Purchase
	replay, err := s.ledgerTx(ctx, key, "amend_purchase:"+id, input, &out, func(tx *sql.Tx) error {
		before, e := getPurchase(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = purchaseVersion(before, input.ExpectedUpdatedAt); e != nil {
			return e
		}
		p, e := ledger.ValidatePurchase(input.PurchaseInput)
		if e != nil {
			return e
		}
		if p.Source != before.Source || p.ExternalID != before.ExternalID {
			return fmt.Errorf("%w: 改单不能更换原始来源或外部编号。", ledger.ErrInvalid)
		}
		oldLines := map[string]ledger.PurchaseLine{}
		for _, line := range before.Items {
			oldLines[line.ID] = line
		}
		seen := map[string]bool{}
		for i := range p.Items {
			line := &p.Items[i]
			line.ID = input.Items[i].ID
			if line.ID == "" {
				continue
			}
			old, ok := oldLines[line.ID]
			if !ok || seen[line.ID] {
				return fmt.Errorf("%w: 商品行编号无效或重复。", ledger.ErrInvalid)
			}
			seen[line.ID] = true
			if line.ItemType != old.ItemType || (line.ItemID != "" && line.ItemID != old.ItemID) {
				return fmt.Errorf("%w: 已关联库存的商品不能直接更换物品或类型，请移除该行后新增。", ledger.ErrConflict)
			}
			line.ItemID = old.ItemID
		}
		if e = applyDueTx(ctx, tx); e != nil {
			return e
		}
		now := nextPurchaseVersion(before.UpdatedAt)
		out = ledger.Purchase{ID: id, PurchaseInput: p, CreatedAt: before.CreatedAt, UpdatedAt: now}
		for _, line := range p.Items {
			out.ItemTotal += line.Subtotal
		}
		if e = reconcilePurchaseStock(ctx, tx, before, out.Items, out.PurchasedAt, now); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE purchase_events SET merchant=?,purchased_at=?,total_cents=?,item_total_cents=?,discount_cents=?,extra_fee_cents=?,currency=?,payment_method=?,category=?,note=?,updated_at=? WHERE id=?`, p.Merchant, p.PurchasedAt, p.TotalAmount, out.ItemTotal, p.Discount, p.ExtraFee, p.Currency, p.PaymentMethod, p.Category, p.Note, now, id); e != nil {
			return e
		}
		if e = correctPurchaseMoney(ctx, tx, before, &out); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM purchase_items WHERE purchase_event_id=?`, id); e != nil {
			return e
		}
		for i := range out.Items {
			line := &out.Items[i]
			if line.ID == "" {
				if e = insertPurchaseLine(ctx, tx, &out, line, now); e != nil {
					return e
				}
			} else if e = savePurchaseLine(ctx, tx, &out, line); e != nil {
				return e
			}
		}
		if e = refreshPurchasePrices(ctx, tx, before.Items, out.Items); e != nil {
			return e
		}
		if e = completePurchase(ctx, tx, &out); e != nil {
			return e
		}
		return recordPurchaseRevision(ctx, tx, before, out, "EDIT", now)
	})
	return out, replay, err
}

// DeletePurchase cancels the purchase in the ledger while retaining its audit
// trail and original identity. It does not purge inventory history.
func (s *Store) DeletePurchase(ctx context.Context, key, id string, input ledger.PurchaseDeletion) (ledger.PurchaseDeletionResult, bool, error) {
	out := ledger.PurchaseDeletionResult{ID: id, Deleted: true}
	replay, err := s.ledgerTx(ctx, key, "delete_purchase:"+id, input, &out, func(tx *sql.Tx) error {
		before, e := getPurchase(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = purchaseVersion(before, input.ExpectedUpdatedAt); e != nil {
			return e
		}
		if e = applyDueTx(ctx, tx); e != nil {
			return e
		}
		now := nextPurchaseVersion(before.UpdatedAt)
		if e = reconcilePurchaseStock(ctx, tx, before, nil, before.PurchasedAt, now); e != nil {
			return e
		}
		if e = correctPurchaseMoney(ctx, tx, before, nil); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE purchase_events SET deleted_at=?,updated_at=? WHERE id=?`, now, now, id); e != nil {
			return e
		}
		if e = refreshPurchasePrices(ctx, tx, before.Items, nil); e != nil {
			return e
		}
		return recordPurchaseRevision(ctx, tx, before, out, "DELETE", now)
	})
	return out, replay, err
}

func recordPurchaseRevision(ctx context.Context, tx *sql.Tx, before ledger.Purchase, after any, action, now string) error {
	a, e := json.Marshal(before)
	if e != nil {
		return e
	}
	b, e := json.Marshal(after)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO purchase_revisions VALUES(?,?,?,?,?,?)`, uuid.NewString(), before.ID, action, string(a), string(b), now)
	return e
}

func reconcilePurchaseStock(ctx context.Context, tx *sql.Tx, before ledger.Purchase, lines []ledger.PurchaseLine, purchasedAt, now string) error {
	oldQty, newQty := map[string]float64{}, map[string]float64{}
	oldLine, newLine := map[string]ledger.PurchaseLine{}, map[string]ledger.PurchaseLine{}
	for _, line := range before.Items {
		if line.ItemID == "" {
			continue
		}
		oldQty[line.ItemID] += line.Quantity
		oldLine[line.ItemID] = line
	}
	for _, line := range lines {
		// New rows are resolved and applied by insertPurchaseLine later in the tx.
		if line.ID == "" || line.ItemID == "" {
			continue
		}
		if previous, ok := newLine[line.ItemID]; ok && (previous.Unit != line.Unit || previous.Name != line.Name || inventory.SpecKey(previous.Spec) != inventory.SpecKey(line.Spec)) {
			return fmt.Errorf("%w: 同一物品的名称和单位必须一致。", ledger.ErrInvalid)
		}
		newQty[line.ItemID] += line.Quantity
		newLine[line.ItemID] = line
	}
	for id, quantity := range oldQty {
		item, e := getTxItem(ctx, tx, id)
		if e != nil {
			return e
		}
		if inventory.UnitKey(item.Unit) != inventory.UnitKey(oldLine[id].Unit) {
			return fmt.Errorf("%w: %s 的库存单位与购买记录不一致，请先核对。", ledger.ErrConflict, item.Name)
		}
		next, retained := newLine[id]
		var otherPurchases int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM purchase_items l JOIN purchase_events p ON p.id=l.purchase_event_id WHERE l.item_id=? AND p.id<>? AND p.deleted_at IS NULL`, id, before.ID).Scan(&otherPurchases); e != nil {
			return e
		}
		if retained && otherPurchases > 0 && inventory.SpecKey(next.Spec) != inventory.SpecKey(item.Spec) {
			return fmt.Errorf("%w: %s 已被其他购买引用，不能直接更换包装规格。", ledger.ErrConflict, item.Name)
		}
		if retained && inventory.UnitKey(item.Unit) != inventory.UnitKey(next.Unit) {
			var laterEvents int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_events WHERE item_id=? AND (purchase_event_id IS NULL OR purchase_event_id<>?)`, id, before.ID).Scan(&laterEvents); e != nil {
				return e
			}
			if otherPurchases != 0 || laterEvents != 0 || math.Abs(item.Quantity-quantity) > 0.0001 {
				return fmt.Errorf("%w: %s 已有其他购买或库存变化，不能直接更换单位；请先核对库存。", ledger.ErrConflict, item.Name)
			}
		}
		if item.ItemType == ledger.Durable && !retained {
			var count int
			if e = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM maintenance_events WHERE item_id=?) + (SELECT COUNT(*) FROM inventory_events WHERE item_id=? AND (purchase_event_id IS NULL OR purchase_event_id<>?))`, id, id, before.ID).Scan(&count); e != nil {
				return e
			}
			var status string
			if e = tx.QueryRowContext(ctx, `SELECT status FROM durable_details WHERE item_id=?`, id).Scan(&status); e != nil {
				return e
			}
			var lifecycle int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM durable_events WHERE item_id=? AND type<>'PURCHASED'`, id).Scan(&lifecycle); e != nil {
				return e
			}
			if count != 0 || lifecycle != 0 || status != "ACTIVE" {
				return fmt.Errorf("%w: %s 已有维护或生命周期记录，不能删除其购买。", ledger.ErrConflict, item.Name)
			}
		}
		delta := math.Round((newQty[id]-quantity)*1000) / 1000
		if item.Quantity+delta < -0.0001 {
			return fmt.Errorf("%w: %s 已有消耗，删除或减少购买数量会使库存小于零，请先核对库存。", ledger.ErrConflict, item.Name)
		}
		// A unit correction is only allowed for a pristine, exclusively owned item.
		// The adjustment note records both units because historical events keep the
		// original numeric quantities rather than fabricating a conversion factor.
		note := "购买改单：调整本笔购买的库存贡献"
		if !retained {
			note = "删除购买：撤销本笔购买的库存贡献"
		}
		if retained && item.Unit != next.Unit {
			note += fmt.Sprintf("；单位 %s → %s", item.Unit, next.Unit)
		}
		if delta != 0 || (retained && item.Unit != next.Unit) {
			if _, e = mutateInventory(ctx, tx, id, "ADJUST", ledger.InventoryInput{Delta: &delta, OccurredAt: now, Source: "manual", Note: note}, &before.ID); e != nil {
				return e
			}
		}
		if retained {
			name := item.Name
			spec := item.ProductSpec
			if otherPurchases == 0 {
				found, err := resolveTarget(ctx, tx, itemTarget{Name: next.Name, Spec: next.Spec})
				if err == nil && found.ID != id {
					return fmt.Errorf("%w: 修改后的名称已属于其他物品。", ledger.ErrConflict)
				}
				if err != nil && !errors.Is(err, ErrNotFound) {
					return err
				}
				name = next.Name
				spec = next.ProductSpec
			}
			if _, e = tx.ExecContext(ctx, `UPDATE items SET name=?,unit=?,updated_at=?,version=version+1,spec=?,package_size=?,package_unit=? WHERE id=?`, name, next.Unit, now, spec.Spec, spec.PackageSize, spec.PackageUnit, id); e != nil {
				return e
			}
			if item.ItemType == ledger.Durable {
				if _, e = tx.ExecContext(ctx, `UPDATE durable_details SET brand=?,model=?,serial_number=?,purchase_date=?,warranty_until=?,note=? WHERE item_id=?`, next.Brand, next.Model, next.SerialNumber, purchasedAt, next.WarrantyUntil, next.Note, id); e != nil {
					return e
				}
			}
		} else if item.ItemType == ledger.Durable {
			if _, e = tx.ExecContext(ctx, `UPDATE durable_details SET status='RETIRED' WHERE item_id=?`, id); e != nil {
				return e
			}
			if e = addDurableEvent(ctx, tx, id, "RETIRED", "ACTIVE", "RETIRED", item.Location, now, "manual", "撤销购买"); e != nil {
				return e
			}
		}
	}
	return nil
}

func refreshPurchasePrices(ctx context.Context, tx *sql.Tx, before, after []ledger.PurchaseLine) error {
	ids := map[string]bool{}
	for _, lines := range [][]ledger.PurchaseLine{before, after} {
		for _, line := range lines {
			if line.ItemID != "" {
				ids[line.ItemID] = true
			}
		}
	}
	for id := range ids {
		if _, e := tx.ExecContext(ctx, `UPDATE items SET unit_price_cents=(SELECT l.unit_price_cents FROM purchase_items l JOIN purchase_events p ON p.id=l.purchase_event_id WHERE l.item_id=? AND p.deleted_at IS NULL ORDER BY julianday(p.purchased_at) DESC,p.rowid DESC,l.rowid DESC LIMIT 1) WHERE id=?`, id, id); e != nil {
			return e
		}
	}
	return nil
}

func savePurchaseLine(ctx context.Context, tx *sql.Tx, purchase *ledger.Purchase, line *ledger.PurchaseLine) error {
	var itemID any
	if line.ItemID != "" {
		itemID = line.ItemID
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO purchase_items(id,purchase_event_id,item_id,raw_name,quantity,unit,unit_price_cents,subtotal_cents,item_type,confidence,note,spec,package_size,package_unit) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, line.ID, purchase.ID, itemID, line.Name, line.Quantity, line.Unit, line.UnitPrice, line.Subtotal, line.ItemType, line.Confidence, line.Note, line.Spec, line.PackageSize, line.PackageUnit)
	return e
}
