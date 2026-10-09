package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"stockroom/internal/ledger"
)

const durableColumns = `item_id,brand,model,serial_number,purchase_date,warranty_until,status,sold_at,sold_price_cents,note`

func durableDetails(ctx context.Context, db purchaseReader, id string) (ledger.DurableDetails, error) {
	var d ledger.DurableDetails
	e := db.QueryRowContext(ctx, `SELECT `+durableColumns+` FROM durable_details WHERE item_id=?`, id).Scan(&d.ItemID, &d.Brand, &d.Model, &d.SerialNumber, &d.PurchaseDate, &d.WarrantyUntil, &d.Status, &d.SoldAt, &d.SoldPrice, &d.Note)
	if errors.Is(e, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if e != nil {
		return d, e
	}
	e = db.QueryRowContext(ctx, `SELECT location FROM items WHERE id=?`, id).Scan(&d.Location)
	if e != nil {
		return d, e
	}
	// Preserve purchase identity/price even after retirement; cancelled purchases are excluded.
	e = db.QueryRowContext(ctx, `SELECT COALESCE((SELECT l.purchase_event_id FROM purchase_items l JOIN purchase_events p ON p.id=l.purchase_event_id WHERE l.item_id=? AND p.deleted_at IS NULL ORDER BY julianday(p.purchased_at) LIMIT 1),''),COALESCE((SELECT CAST(ROUND(SUM(CASE WHEN p.item_total_cents>0 THEN 1.0*l.subtotal_cents*p.total_cents/p.item_total_cents ELSE 1.0*p.total_cents/(SELECT COUNT(*) FROM purchase_items WHERE purchase_event_id=p.id) END)) AS INTEGER) FROM purchase_items l JOIN purchase_events p ON p.id=l.purchase_event_id WHERE l.item_id=? AND p.deleted_at IS NULL),0)`, id, id).Scan(&d.PurchaseEventID, &d.PurchasePrice)
	if e != nil {
		return d, e
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(zone)
	if purchased, e := time.Parse(time.RFC3339Nano, d.PurchaseDate); e == nil {
		end := now
		if d.SoldAt != "" {
			if sold, e := time.Parse(time.RFC3339Nano, d.SoldAt); e == nil {
				end = sold
			}
		}
		d.UseDays = int(end.Sub(purchased).Hours() / 24)
		if d.UseDays < 0 {
			d.UseDays = 0
		}
	}
	if d.WarrantyUntil != "" {
		if until, e := time.ParseInLocation(time.DateOnly, d.WarrantyUntil, zone); e == nil {
			days := int(until.AddDate(0, 0, 1).Sub(now).Hours() / 24)
			d.WarrantyDays = &days
		}
	}
	return d, nil
}
func addDurableEvent(ctx context.Context, tx *sql.Tx, id, kind, from, to, location, at, source, note string) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO durable_events VALUES(?,?,?,?,?,?,?,?,?)`, uuid.NewString(), id, kind, at, from, to, location, note, source)
	return e
}
func durableTransition(ctx context.Context, tx *sql.Tx, id, status string, p ledger.DurableActionInput) (ledger.DurableDetails, error) {
	d, e := durableDetails(ctx, tx, id)
	if e != nil {
		return d, e
	}
	status = strings.ToUpper(status)
	if status == "ARCHIVED" {
		status = "STORED"
	}
	if status == "DISCARDED" {
		status = "RETIRED"
	}
	switch status {
	case "ACTIVE", "STORED", "LOST", "SOLD", "RETIRED":
	default:
		return d, ledger.ErrInvalid
	}
	if d.Status == "SOLD" || d.Status == "RETIRED" {
		return d, fmt.Errorf("%w: durable lifecycle has ended", ledger.ErrConflict)
	}
	if status == d.Status {
		return d, fmt.Errorf("%w: item already has this status", ledger.ErrConflict)
	}
	at, e := ledger.Timestamp(p.OccurredAt)
	if e != nil {
		return d, e
	}
	source, e := ledger.Source(p.Source)
	if e != nil {
		return d, e
	}
	if len([]rune(p.Note)) > 2000 {
		return d, ledger.ErrInvalid
	}
	quantity := 0.0
	if status == "ACTIVE" || status == "STORED" {
		quantity = 1
	}
	if _, e = mutateInventory(ctx, tx, id, "ADJUST", ledger.InventoryInput{Quantity: &quantity, OccurredAt: at, Source: source, Note: "耐用品状态: " + status + " " + p.Note}, nil); e != nil {
		return d, e
	}
	kind := status
	if status == "ACTIVE" {
		kind = "ACTIVATED"
		if d.Status == "LOST" {
			kind = "FOUND"
		}
	}
	if e = addDurableEvent(ctx, tx, id, kind, d.Status, status, d.Location, at, source, p.Note); e != nil {
		return d, e
	}
	if status == "SOLD" {
		if p.Amount <= 0 || p.Amount > 1e12 {
			return d, fmt.Errorf("%w: sale requires positive amount", ledger.ErrInvalid)
		}
		if _, e = postMoney(ctx, tx, "INCOME", ledger.MoneyInput{AccountID: p.AccountID, Amount: p.Amount, Category: "sale", OccurredAt: at, Source: source, Note: p.Note, Reason: "DURABLE_SALE:" + id}, "", ledger.SignedMoney(p.Amount)); e != nil {
			return d, e
		}
		d.SoldAt = at
		price := p.Amount
		d.SoldPrice = &price
	}
	_, e = tx.ExecContext(ctx, `UPDATE durable_details SET status=?,sold_at=?,sold_price_cents=? WHERE item_id=?`, status, d.SoldAt, d.SoldPrice, id)
	if e != nil {
		return d, e
	}
	return durableDetails(ctx, tx, id)
}
func (s *Store) DurableAction(ctx context.Context, key, id, action string, p ledger.DurableActionInput) (ledger.DurableDetails, bool, error) {
	var out ledger.DurableDetails
	r, e := s.ledgerTx(ctx, key, "durable_action:"+action+":"+id, p, &out, func(tx *sql.Tx) error {
		if action == "move" {
			d, e := durableDetails(ctx, tx, id)
			if e != nil {
				return e
			}
			if d.Status == "SOLD" || d.Status == "RETIRED" {
				return ledger.ErrConflict
			}
			location := strings.TrimSpace(p.Location)
			if location == "" || len([]rune(location)) > 100 || len([]rune(p.Note)) > 2000 {
				return ledger.ErrInvalid
			}
			at, e := ledger.Timestamp(p.OccurredAt)
			if e != nil {
				return e
			}
			source, e := ledger.Source(p.Source)
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE items SET location=?,updated_at=?,version=version+1 WHERE id=?`, location, time.Now().UTC().Format(time.RFC3339Nano), id); e != nil {
				return e
			}
			if e = addDurableEvent(ctx, tx, id, "MOVED", d.Status, d.Status, location, at, source, p.Note); e != nil {
				return e
			}
			out, e = durableDetails(ctx, tx, id)
			return e
		}
		status := p.Status
		switch action {
		case "sell":
			status = "SOLD"
		case "retire":
			status = "RETIRED"
		case "status":
		default:
			return ledger.ErrInvalid
		}
		var e error
		out, e = durableTransition(ctx, tx, id, status, p)
		return e
	})
	return out, r, e
}
func (s *Store) DurableEvents(ctx context.Context, f ledger.HistoryFilter) ([]ledger.DurableEvent, error) {
	where, args := historyWhere(f, "occurred_at", "")
	where += " AND item_id=?"
	args = append(args, f.ItemID)
	limit, offset := page(f)
	args = append(args, limit, offset)
	rows, e := s.db.QueryContext(ctx, `SELECT * FROM durable_events WHERE `+where+` ORDER BY julianday(occurred_at) DESC,rowid DESC LIMIT ? OFFSET ?`, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.DurableEvent{}
	for rows.Next() {
		var d ledger.DurableEvent
		if e = rows.Scan(&d.ID, &d.ItemID, &d.Type, &d.OccurredAt, &d.FromStatus, &d.ToStatus, &d.Location, &d.Note, &d.Source); e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

type DurableRow struct {
	Name    string                `json:"name"`
	Details ledger.DurableDetails `json:"details"`
}

func (s *Store) Durables(ctx context.Context, status, location, search string, f ledger.HistoryFilter) ([]DurableRow, error) {
	where, args := historyWhere(f, "d.purchase_date", "")
	where += " AND i.item_type='DURABLE'"
	if status != "" {
		switch status {
		case "ACTIVE", "STORED", "LOST", "SOLD", "RETIRED":
		default:
			return nil, ledger.ErrInvalid
		}
		where += " AND d.status=?"
		args = append(args, status)
	}
	if location != "" {
		where += " AND i.location=?"
		args = append(args, location)
	}
	if search != "" {
		where += " AND instr(lower(i.name),lower(?))>0"
		args = append(args, search)
	}
	limit, offset := page(f)
	args = append(args, limit, offset)
	rows, e := s.db.QueryContext(ctx, `SELECT i.id,i.name FROM items i JOIN durable_details d ON d.item_id=i.id WHERE `+where+` ORDER BY julianday(d.purchase_date) DESC,i.rowid DESC LIMIT ? OFFSET ?`, args...)
	if e != nil {
		return nil, e
	}
	ids := [][2]string{}
	for rows.Next() {
		var v [2]string
		if e = rows.Scan(&v[0], &v[1]); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []DurableRow{}
	for _, v := range ids {
		d, e := s.DurableDetails(ctx, v[0])
		if e != nil {
			return nil, e
		}
		out = append(out, DurableRow{Name: v[1], Details: d})
	}
	return out, nil
}
func (s *Store) ItemCost(ctx context.Context, id string, f ledger.HistoryFilter) (ledger.ItemCost, error) {
	out := ledger.ItemCost{ItemID: id}
	if _, e := s.Get(ctx, id); e != nil {
		return out, e
	}
	where, args := historyWhere(f, "p.purchased_at", "")
	where += " AND l.item_id=? AND p.deleted_at IS NULL"
	args = append(args, id)
	e := s.db.QueryRowContext(ctx, `SELECT COALESCE(CAST(ROUND(SUM(CASE WHEN p.item_total_cents>0 THEN 1.0*l.subtotal_cents*p.total_cents/p.item_total_cents ELSE 1.0*p.total_cents/(SELECT COUNT(*) FROM purchase_items WHERE purchase_event_id=p.id) END)) AS INTEGER),0),COUNT(DISTINCT p.id),COALESCE(SUM(l.quantity),0) FROM purchase_items l JOIN purchase_events p ON p.id=l.purchase_event_id WHERE `+where, args...).Scan(&out.PurchaseCost, &out.PurchaseCount, &out.PurchaseQuantity)
	if e != nil {
		return out, e
	}
	where, args = historyWhere(f, "occurred_at", "")
	where += " AND item_id=? AND status='DONE'"
	args = append(args, id)
	if e = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(cost_cents),0),COUNT(*) FROM maintenance_events WHERE `+where, args...).Scan(&out.MaintenanceCost, &out.MaintenanceCount); e != nil {
		return out, e
	}
	where, args = historyWhere(f, "m.occurred_at", "")
	where += ` AND m.type='REFUND' AND m.voided_at='' AND (EXISTS(SELECT 1 FROM purchase_items l WHERE l.purchase_event_id=m.purchase_event_id AND l.item_id=?) OR EXISTS(SELECT 1 FROM money_transactions original JOIN maintenance_events me ON me.id=original.maintenance_event_id WHERE original.id=m.related_transaction_id AND me.item_id=?))`
	args = append(args, id, id)
	// Purchase refunds are allocated by original item share, maintenance refunds wholly to the item.
	e = s.db.QueryRowContext(ctx, `SELECT COALESCE(CAST(ROUND(SUM(CASE WHEN m.purchase_event_id<>'' THEN m.amount_cents*COALESCE((SELECT SUM(CASE WHEN p.item_total_cents>0 THEN 1.0*l.subtotal_cents/p.item_total_cents ELSE 1.0/(SELECT COUNT(*) FROM purchase_items WHERE purchase_event_id=p.id) END) FROM purchase_items l JOIN purchase_events p ON p.id=l.purchase_event_id WHERE l.purchase_event_id=m.purchase_event_id AND l.item_id=?),0) ELSE m.amount_cents END)) AS INTEGER),0) FROM money_transactions m WHERE `+where, append([]any{id}, args...)...).Scan(&out.Refund)
	if e != nil {
		return out, e
	}
	prices, e := s.PriceHistory(ctx, ledger.HistoryFilter{ItemID: id, Limit: 1, From: f.From, To: f.To})
	if e != nil {
		return out, e
	}
	if len(prices) > 0 {
		price := prices[0].UnitPrice
		out.LatestPrice = &price
	}
	out.TotalCost = out.PurchaseCost + out.MaintenanceCost
	out.NetCost = ledger.SignedMoney(out.TotalCost - out.Refund)
	return out, nil
}
