package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
)

// ledgerTx obtains the SQLite writer lock before any reads. Idempotency and the
// event mutation therefore have a single serializable commit boundary.
func (s *Store) ledgerTx(ctx context.Context, key, operation string, request any, result any, fn func(*sql.Tx) error) (bool, error) {
	if len(key) > 200 || strings.TrimSpace(key) != key {
		return false, fmt.Errorf("%w: invalid Idempotency-Key", ledger.ErrInvalid)
	}
	data, err := ledger.CanonicalRequest(request)
	if err != nil {
		return false, err
	}
	hash := sha256.Sum256(data)
	fingerprint := hex.EncodeToString(hash[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE ledger_lock SET revision=revision+1 WHERE id=1`); err != nil {
		return false, err
	}
	if key != "" {
		var op, fp, response string
		err = tx.QueryRowContext(ctx, `SELECT operation,request_hash,response_json FROM idempotency_keys WHERE key=?`, key).Scan(&op, &fp, &response)
		if err == nil {
			if op != operation || fp != fingerprint {
				return false, ledger.ErrIdempotency
			}
			if err = json.Unmarshal([]byte(response), result); err != nil {
				return false, err
			}
			return true, tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	if err = fn(tx); err != nil {
		return false, err
	}
	if key != "" {
		b, e := json.Marshal(result)
		if e != nil {
			return false, e
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys VALUES(?,?,?,?,?)`, key, operation, fingerprint, string(b), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return false, err
		}
	}
	return false, tx.Commit()
}

func (s *Store) RecordPurchase(ctx context.Context, key string, input ledger.PurchaseInput) (ledger.Purchase, bool, error) {
	var out ledger.Purchase
	replay, err := s.ledgerTx(ctx, key, "record_purchase_event", input, &out, func(tx *sql.Tx) error {
		p, e := ledger.ValidatePurchase(input)
		if e != nil {
			return e
		}
		if e = applyDueTx(ctx, tx); e != nil {
			return e
		}
		if p.ExternalID != "" {
			var count int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM purchase_events WHERE source=? AND external_id=?`, p.Source, p.ExternalID).Scan(&count); e != nil {
				return e
			}
			if count > 0 {
				return fmt.Errorf("%w: external_id already recorded", ledger.ErrConflict)
			}
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		out = ledger.Purchase{ID: uuid.NewString(), PurchaseInput: p, CreatedAt: now, UpdatedAt: now}
		for _, l := range p.Items {
			out.ItemTotal += l.Subtotal
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO purchase_events(id,merchant,purchased_at,total_cents,item_total_cents,discount_cents,extra_fee_cents,currency,payment_method,source,external_id,category,note,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, out.ID, p.Merchant, p.PurchasedAt, p.TotalAmount, out.ItemTotal, p.Discount, p.ExtraFee, p.Currency, p.PaymentMethod, p.Source, p.ExternalID, p.Category, p.Note, now, now)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO expenses VALUES(?,?,NULL,?,?,?,?,?,?,?,?)`, uuid.NewString(), out.ID, p.TotalAmount, p.Currency, p.Category, p.Merchant, p.PaymentMethod, p.PurchasedAt, p.Source, p.Note); e != nil {
			return e
		}
		for i := range out.Items {
			if e = insertPurchaseLine(ctx, tx, &out, &out.Items[i], now); e != nil {
				return e
			}
		}
		return completePurchase(ctx, tx, &out)
	})
	return out, replay, err
}

func getTxItem(ctx context.Context, tx *sql.Tx, id string) (inventory.Item, error) {
	item, e := scanItem(tx.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items WHERE id=?`, id))
	if errors.Is(e, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	return item, e
}

func (s *Store) InventoryAction(ctx context.Context, key, id, action string, input ledger.InventoryInput) (ledger.InventoryResult, bool, error) {
	var out ledger.InventoryResult
	replay, e := s.ledgerTx(ctx, key, action+":"+id, input, &out, func(tx *sql.Tx) error {
		if err := applyDueTx(ctx, tx); err != nil {
			return err
		}
		item, e := resolveTarget(ctx, tx, itemTarget{ID: id, Name: input.Name, Spec: input.Spec})
		if e != nil {
			return e
		}
		out, e = mutateInventory(ctx, tx, item.ID, action, input, nil)
		return e
	})
	return out, replay, e
}

func mutateInventory(ctx context.Context, tx *sql.Tx, id, action string, input ledger.InventoryInput, purchaseID *string) (ledger.InventoryResult, error) {
	var out ledger.InventoryResult
	item, e := getTxItem(ctx, tx, id)
	if e != nil {
		return out, e
	}
	at, e := ledger.Timestamp(input.OccurredAt)
	if e != nil {
		return out, e
	}
	source, e := ledger.Source(input.Source)
	if e != nil {
		return out, e
	}
	if len([]rune(input.Note)) > 2000 {
		return out, fmt.Errorf("%w: note too long", ledger.ErrInvalid)
	}
	after := item.Quantity
	switch action {
	case "CONSUME":
		if input.Quantity == nil || *input.Quantity <= 0 || input.Delta != nil {
			return out, fmt.Errorf("%w: consume needs positive quantity", ledger.ErrInvalid)
		}
		after -= *input.Quantity
	case "ADJUST":
		if (input.Quantity == nil) == (input.Delta == nil) {
			return out, fmt.Errorf("%w: adjust needs either quantity or delta", ledger.ErrInvalid)
		}
		if input.Quantity != nil {
			after = *input.Quantity
		} else {
			after += *input.Delta
		}
	case "PURCHASE":
		if input.Delta == nil {
			return out, ledger.ErrInvalid
		}
		after += *input.Delta
	case "FINISH", "DISCARD":
		if input.Quantity != nil || input.Delta != nil {
			return out, fmt.Errorf("%w: finish/discard take no quantity", ledger.ErrInvalid)
		}
		after = 0
	case "OPEN":
		if input.Quantity != nil || input.Delta != nil {
			return out, ledger.ErrInvalid
		}
	default:
		return out, fmt.Errorf("%w: unknown action", ledger.ErrInvalid)
	}
	if after < 0 {
		return out, ErrNegativeQuantity
	}
	if math.IsNaN(after) || math.IsInf(after, 0) || after > inventory.MaxQuantity {
		return out, fmt.Errorf("%w: quantity out of range", ledger.ErrInvalid)
	}
	if math.Abs(after*1000-math.Round(after*1000)) > 0.0001 {
		return out, fmt.Errorf("%w: quantity supports three decimals", ledger.ErrInvalid)
	}
	after = math.Round(after*1000) / 1000
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	schedule := any(nil)
	if item.ShortLived && after > 0 {
		if item.AutoConsumeAt != nil {
			schedule = *item.AutoConsumeAt
		} else {
			schedule = nowTime.Add(inventory.ShortLivedTTL).Format(time.RFC3339Nano)
		}
	}
	_, e = tx.ExecContext(ctx, `UPDATE items SET quantity=?,auto_consume_at=?,opened_at=CASE WHEN ?='OPEN' THEN ? ELSE opened_at END,updated_at=?,version=version+1 WHERE id=?`, after, schedule, action, at, now, id)
	if e != nil {
		return out, e
	}
	if item.Quantity != after {
		_, e = tx.ExecContext(ctx, `UPDATE inventory_events SET type=?,purchase_event_id=?,occurred_at=?,source=?,note=? WHERE rowid=(SELECT MAX(rowid) FROM inventory_events WHERE item_id=?)`, action, purchaseID, at, source, input.Note, id)
	} else {
		_, e = tx.ExecContext(ctx, `INSERT INTO inventory_events VALUES(?,?,?,?,?,?,?,?,?,?)`, uuid.NewString(), id, action, 0, after, after, purchaseID, at, source, input.Note)
	}
	if e != nil {
		return out, e
	}
	out.Item, e = getTxItem(ctx, tx, id)
	if e != nil {
		return out, e
	}
	e = scanEvent(tx.QueryRowContext(ctx, `SELECT * FROM inventory_events WHERE item_id=? ORDER BY rowid DESC LIMIT 1`, id), &out.Event)
	return out, e
}

func (s *Store) AddMaintenance(ctx context.Context, key, id string, input ledger.MaintenanceInput) (ledger.Maintenance, bool, error) {
	var out ledger.Maintenance
	replay, e := s.ledgerTx(ctx, key, "maintenance:"+id, input, &out, func(tx *sql.Tx) error {
		item, e := getTxItem(ctx, tx, id)
		if e != nil {
			return e
		}
		if item.ItemType != ledger.Durable {
			return fmt.Errorf("%w: maintenance needs a durable item", ledger.ErrConflict)
		}
		p := input
		p.Title = strings.TrimSpace(p.Title)
		if p.Title == "" || len([]rune(p.Title)) > 200 || len([]rune(p.Description)) > 2000 || len([]rune(p.Note)) > 2000 || p.Cost < 0 || p.Cost > 1e12 {
			return fmt.Errorf("%w: invalid maintenance", ledger.ErrInvalid)
		}
		if p.OccurredAt, e = ledger.Timestamp(p.OccurredAt); e != nil {
			return e
		}
		if p.Source, e = ledger.Source(p.Source); e != nil {
			return e
		}
		if p.Status == "" {
			p.Status = "DONE"
		}
		if p.Status != "DONE" && p.Status != "PLANNED" {
			return fmt.Errorf("%w: status must be DONE or PLANNED", ledger.ErrInvalid)
		}
		if p.Status == "PLANNED" && p.Cost > 0 {
			return fmt.Errorf("%w: planned maintenance cannot record an expense", ledger.ErrInvalid)
		}
		out = ledger.Maintenance{ID: uuid.NewString(), ItemID: id, MaintenanceInput: p}
		if _, e = tx.ExecContext(ctx, `INSERT INTO maintenance_events VALUES(?,?,NULL,?,?,?,?,?,?,?)`, out.ID, id, p.OccurredAt, p.Title, p.Description, p.Cost, p.Status, p.Source, p.Note); e != nil {
			return e
		}
		if p.Cost > 0 {
			expenseID := uuid.NewString()
			out.ExpenseID = &expenseID
			if _, e = tx.ExecContext(ctx, `INSERT INTO expenses VALUES(?,NULL,?,?,'CNY','maintenance','','',?,?,?)`, expenseID, out.ID, p.Cost, p.OccurredAt, p.Source, p.Note); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE maintenance_events SET expense_id=? WHERE id=?`, expenseID, out.ID); e != nil {
				return e
			}
		}
		return nil
	})
	return out, replay, e
}

func scanEvent(r scanner, e *ledger.InventoryEvent) error {
	return r.Scan(&e.ID, &e.ItemID, &e.Type, &e.QuantityDelta, &e.QuantityBefore, &e.QuantityAfter, &e.PurchaseEventID, &e.OccurredAt, &e.Source, &e.Note)
}

func applyDueTx(ctx context.Context, tx *sql.Tx) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `UPDATE items SET quantity=0,auto_consume_at=NULL,updated_at=?,version=version+1 WHERE short_lived=1 AND auto_consume_at IS NOT NULL AND julianday(auto_consume_at)<=julianday(?)`, now, now)
	return err
}

func insertPurchaseLine(ctx context.Context, tx *sql.Tx, purchase *ledger.Purchase, l *ledger.PurchaseLine, now string) error {
	var e error

	if l.ID == "" {
		l.ID = uuid.NewString()
	}
	if l.ItemType != ledger.Untracked {
		var item inventory.Item
		if l.ItemID != "" {
			item, e = resolveTarget(ctx, tx, itemTarget{ID: l.ItemID, Spec: l.Spec})
		} else if l.ItemType == ledger.Consumable {
			item, e = resolveTarget(ctx, tx, itemTarget{Name: l.Name, Spec: l.Spec})
		} else {
			e = ErrNotFound
		}
		if errors.Is(e, ErrNotFound) && l.ItemID == "" {
			item.ID = uuid.NewString()
			qty := 0.0
			_, e = tx.ExecContext(ctx, `INSERT INTO items(id,name,category,icon,quantity,unit,location,unit_price_cents,notes,created_at,updated_at,item_type,spec,package_size,package_unit) VALUES(?,?,?,'inventory_2',?,?,?,?,?,?,?,?,?,?,?)`, item.ID, l.Name, l.Category, qty, l.Unit, l.Location, l.UnitPrice, l.Note, now, now, l.ItemType, l.Spec, l.PackageSize, l.PackageUnit)
			if e != nil {
				return e
			}
			item, e = getTxItem(ctx, tx, item.ID)
		}
		if e != nil {
			return e
		}
		if item.ItemType != l.ItemType || inventory.UnitKey(item.Unit) != inventory.UnitKey(l.Unit) {
			return fmt.Errorf("%w: item type or unit mismatch for %s", ledger.ErrConflict, l.Name)
		}
		l.ItemID = item.ID
		l.Name, l.ProductSpec, l.Unit = item.Name, item.ProductSpec, item.Unit
		if l.ItemType == ledger.Durable && item.Quantity != 0 {
			return fmt.Errorf("%w: 耐用品实例已有库存，不能重复入库", ledger.ErrConflict)
		}
		{
			d := l.Quantity
			eventInput := ledger.InventoryInput{Delta: &d, OccurredAt: purchase.PurchasedAt, Source: purchase.Source, Note: l.Note}
			result, err := mutateInventory(ctx, tx, item.ID, "PURCHASE", eventInput, &purchase.ID)
			e = err
			if e != nil {
				return e
			}
			l.QuantityAdded = l.Quantity
			l.InventoryAfter = &result.Item.Quantity
			if _, e = tx.ExecContext(ctx, `UPDATE items SET unit_price_cents=? WHERE id=?`, l.UnitPrice, item.ID); e != nil {
				return e
			}
		}
		if l.ItemType == ledger.Durable {
			var count int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM durable_details WHERE item_id=?`, item.ID).Scan(&count); e != nil {
				return e
			}
			if count > 0 {
				return fmt.Errorf("%w: durable instance already has a purchase", ledger.ErrConflict)
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO durable_details(item_id,brand,model,serial_number,purchase_date,warranty_until,note) VALUES(?,?,?,?,?,?,?)`, item.ID, l.Brand, l.Model, l.SerialNumber, purchase.PurchasedAt, l.WarrantyUntil, l.Note); e != nil {
				return e
			}
		}
	}
	var itemID any
	if l.ItemID != "" {
		itemID = l.ItemID
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO purchase_items(id,purchase_event_id,item_id,raw_name,quantity,unit,unit_price_cents,subtotal_cents,item_type,confidence,note,spec,package_size,package_unit) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, l.ID, purchase.ID, itemID, l.Name, l.Quantity, l.Unit, l.UnitPrice, l.Subtotal, l.ItemType, l.Confidence, l.Note, l.Spec, l.PackageSize, l.PackageUnit); e != nil {
		return e
	}
	return nil
}

func completePurchase(ctx context.Context, tx *sql.Tx, p *ledger.Purchase) error {
	for i := range p.Items {
		l := &p.Items[i]
		if l.ItemID == "" {
			continue
		}
		item, e := getTxItem(ctx, tx, l.ItemID)
		if e != nil {
			return e
		}
		l.InventoryAfter = &item.Quantity
	}
	ledger.DecoratePurchase(p)
	return nil
}
