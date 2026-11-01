package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
)

func historyWhere(f ledger.HistoryFilter, date, category string) (string, []any) {
	where := []string{"1=1"}
	args := []any{}
	if f.From != "" {
		where = append(where, "julianday("+date+")>=julianday(?)")
		args = append(args, f.From)
	}
	if f.To != "" {
		where = append(where, "julianday("+date+")<julianday(?)")
		args = append(args, f.To)
	}
	if f.Category != "" && category != "" {
		where = append(where, category+"=?")
		args = append(args, f.Category)
	}
	return strings.Join(where, " AND "), args
}
func page(f ledger.HistoryFilter) (int, int) {
	if f.Limit < 1 || f.Limit > 500 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	return f.Limit, f.Offset
}

type purchaseReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *Store) GetPurchase(ctx context.Context, id string) (ledger.Purchase, error) {
	return getPurchase(ctx, s.db, id)
}
func getPurchase(ctx context.Context, db purchaseReader, id string) (ledger.Purchase, error) {
	var p ledger.Purchase
	e := db.QueryRowContext(ctx, `SELECT id,merchant,purchased_at,total_cents,item_total_cents,discount_cents,extra_fee_cents,currency,payment_method,source,external_id,category,note,created_at,updated_at FROM purchase_events WHERE id=? AND deleted_at IS NULL`, id).Scan(&p.ID, &p.Merchant, &p.PurchasedAt, &p.TotalAmount, &p.ItemTotal, &p.Discount, &p.ExtraFee, &p.Currency, &p.PaymentMethod, &p.Source, &p.ExternalID, &p.Category, &p.Note, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if e != nil {
		return p, e
	}
	if e = db.QueryRowContext(ctx, `SELECT expense_nature FROM money_transactions WHERE purchase_event_id=? AND type='EXPENSE' AND voided_at='' ORDER BY rowid DESC LIMIT 1`, id).Scan(&p.ExpenseNature); e != nil && !errors.Is(e, sql.ErrNoRows) {
		return p, e
	}
	var refunded ledger.Money
	if e = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_cents),0) FROM money_transactions WHERE purchase_event_id=? AND type='REFUND' AND voided_at=''`, id).Scan(&refunded); e != nil {
		return p, e
	}
	p.RefundedAmount = refunded
	p.Status = "COMPLETED"
	if refunded > 0 {
		p.Status = "PARTIAL_REFUND"
		if refunded == p.TotalAmount {
			p.Status = "REFUNDED"
		}
	}
	if e = db.QueryRowContext(ctx, `SELECT account_id FROM money_transactions WHERE purchase_event_id=? AND type='EXPENSE' AND voided_at=''`, id).Scan(&p.AccountID); e != nil && !errors.Is(e, sql.ErrNoRows) {
		return p, e
	}
	rows, e := db.QueryContext(ctx, `SELECT l.id,COALESCE(l.item_id,''),l.raw_name,l.quantity,l.unit,l.unit_price_cents,l.subtotal_cents,l.item_type,l.confidence,l.note,COALESCE(i.category,'other'),COALESCE(i.location,''),COALESCE(d.brand,''),COALESCE(d.model,''),COALESCE(d.serial_number,''),COALESCE(d.warranty_until,''),l.spec,l.package_size,l.package_unit FROM purchase_items l LEFT JOIN items i ON i.id=l.item_id LEFT JOIN durable_details d ON d.item_id=l.item_id WHERE l.purchase_event_id=? ORDER BY l.rowid`, id)
	if e != nil {
		return p, e
	}
	defer rows.Close()
	p.Items = []ledger.PurchaseLine{}
	for rows.Next() {
		var l ledger.PurchaseLine
		if e = rows.Scan(&l.ID, &l.ItemID, &l.Name, &l.Quantity, &l.Unit, &l.UnitPrice, &l.Subtotal, &l.ItemType, &l.Confidence, &l.Note, &l.Category, &l.Location, &l.Brand, &l.Model, &l.SerialNumber, &l.WarrantyUntil, &l.Spec, &l.PackageSize, &l.PackageUnit); e != nil {
			return p, e
		}
		p.Items = append(p.Items, l)
	}
	ledger.DecoratePurchase(&p)
	return p, rows.Err()
}
func (s *Store) Purchases(ctx context.Context, f ledger.HistoryFilter) ([]ledger.Purchase, error) {
	where, args := historyWhere(f, "p.purchased_at", "p.category")
	where += " AND p.deleted_at IS NULL"
	if f.ItemID != "" {
		where += ` AND EXISTS(SELECT 1 FROM purchase_items l WHERE l.purchase_event_id=p.id AND l.item_id=?)`
		args = append(args, f.ItemID)
	}
	limit, offset := page(f)
	args = append(args, limit, offset)
	rows, e := s.db.QueryContext(ctx, `SELECT p.id FROM purchase_events p WHERE `+where+` ORDER BY julianday(p.purchased_at) DESC,p.rowid DESC LIMIT ? OFFSET ?`, args...)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	result := []ledger.Purchase{}
	for _, id := range ids {
		p, e := s.GetPurchase(ctx, id)
		if e != nil {
			return nil, e
		}
		result = append(result, p)
	}
	return result, nil
}
func (s *Store) InventoryEvents(ctx context.Context, f ledger.HistoryFilter) ([]ledger.InventoryEvent, error) {
	where, args := historyWhere(f, "occurred_at", "")
	if f.ItemID != "" {
		where += " AND item_id=?"
		args = append(args, f.ItemID)
	}
	limit, offset := page(f)
	args = append(args, limit, offset)
	rows, e := s.db.QueryContext(ctx, `SELECT * FROM inventory_events WHERE `+where+` ORDER BY julianday(occurred_at) DESC,rowid DESC LIMIT ? OFFSET ?`, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.InventoryEvent{}
	for rows.Next() {
		var v ledger.InventoryEvent
		if e = scanEvent(rows, &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) PriceHistory(ctx context.Context, f ledger.HistoryFilter) ([]ledger.Price, error) {
	where, args := historyWhere(f, "p.purchased_at", "")
	where += " AND p.deleted_at IS NULL AND l.item_id=?"
	args = append(args, f.ItemID)
	limit, offset := page(f)
	args = append(args, limit, offset)
	rows, e := s.db.QueryContext(ctx, `SELECT p.id,p.merchant,p.purchased_at,p.currency,l.unit_price_cents,l.quantity,l.subtotal_cents FROM purchase_items l JOIN purchase_events p ON p.id=l.purchase_event_id WHERE `+where+` ORDER BY julianday(p.purchased_at) DESC,l.rowid DESC LIMIT ? OFFSET ?`, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.Price{}
	for rows.Next() {
		var v ledger.Price
		if e = rows.Scan(&v.PurchaseEventID, &v.Merchant, &v.PurchasedAt, &v.Currency, &v.UnitPrice, &v.Quantity, &v.Subtotal); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Expenses(ctx context.Context, f ledger.HistoryFilter) ([]ledger.Expense, error) {
	where, args := historyWhere(f, "occurred_at", "category")
	limit, offset := page(f)
	args = append(args, limit, offset)
	rows, e := s.db.QueryContext(ctx, `SELECT * FROM expenses WHERE `+where+` ORDER BY julianday(occurred_at) DESC,id DESC LIMIT ? OFFSET ?`, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.Expense{}
	for rows.Next() {
		var v ledger.Expense
		if e = rows.Scan(&v.ID, &v.PurchaseEventID, &v.MaintenanceEventID, &v.Amount, &v.Currency, &v.Category, &v.Merchant, &v.PaymentMethod, &v.OccurredAt, &v.Source, &v.Note); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) ExpenseTotal(ctx context.Context, f ledger.HistoryFilter) (ledger.Money, error) {
	where, args := historyWhere(f, "occurred_at", "category")
	var total ledger.Money
	e := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE type WHEN 'EXPENSE' THEN amount_cents WHEN 'REFUND' THEN -amount_cents ELSE 0 END),0) FROM money_transactions WHERE voided_at='' AND `+where, args...).Scan(&total)
	return total, e
}
func (s *Store) MaintenanceHistory(ctx context.Context, f ledger.HistoryFilter) ([]ledger.Maintenance, error) {
	where, args := historyWhere(f, "occurred_at", "")
	if f.ItemID != "" {
		where += " AND item_id=?"
		args = append(args, f.ItemID)
	}
	limit, offset := page(f)
	args = append(args, limit, offset)
	rows, e := s.db.QueryContext(ctx, `SELECT * FROM maintenance_events WHERE `+where+` ORDER BY julianday(occurred_at) DESC,rowid DESC LIMIT ? OFFSET ?`, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.Maintenance{}
	for rows.Next() {
		var v ledger.Maintenance
		if e = rows.Scan(&v.ID, &v.ItemID, &v.ExpenseID, &v.OccurredAt, &v.Title, &v.Description, &v.Cost, &v.Status, &v.Source, &v.Note); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) DurableDetails(ctx context.Context, id string) (ledger.DurableDetails, error) {
	return durableDetails(ctx, s.db, id)
}
func (s *Store) UpdateDurable(ctx context.Context, key, id string, p ledger.DurablePatch) (ledger.DurableDetails, bool, error) {
	var d ledger.DurableDetails
	replay, e := s.ledgerTx(ctx, key, "durable:"+id, p, &d, func(tx *sql.Tx) error {
		var e error
		d, e = durableDetails(ctx, tx, id)
		if e != nil {
			return e
		}
		for _, v := range []struct {
			in  *string
			out *string
		}{{p.Brand, &d.Brand}, {p.Model, &d.Model}, {p.SerialNumber, &d.SerialNumber}, {p.WarrantyUntil, &d.WarrantyUntil}, {p.Note, &d.Note}} {
			if v.in != nil {
				*v.out = strings.TrimSpace(*v.in)
			}
			if len([]rune(*v.out)) > 2000 {
				return ledger.ErrInvalid
			}
		}
		if d.WarrantyUntil != "" {
			if _, e = time.Parse(time.DateOnly, d.WarrantyUntil); e != nil {
				return ledger.ErrInvalid
			}
		}
		if p.Status != nil && *p.Status != d.Status {
			input := ledger.DurableActionInput{OccurredAt: "", Source: "manual", Note: d.Note}
			if p.SoldAt != nil {
				input.OccurredAt = *p.SoldAt
			}
			if p.SoldPrice != nil {
				input.Amount = *p.SoldPrice
			}
			if _, e = durableTransition(ctx, tx, id, *p.Status, input); e != nil {
				return e
			}
		} else if p.SoldAt != nil || p.SoldPrice != nil {
			return fmt.Errorf("%w: use sell_item for sale", ledger.ErrInvalid)
		}
		if p.HasOriginalPackaging != nil {
			d.HasOriginalPackaging = p.HasOriginalPackaging
		}
		if p.PurchaseProof != nil {
			if len([]rune(*p.PurchaseProof)) > 2000 {
				return ledger.ErrInvalid
			}
			d.PurchaseProof = *p.PurchaseProof
		}
		if p.EstimatedResaleValue != nil {
			if *p.EstimatedResaleValue < 0 || *p.EstimatedResaleValue > 1e12 {
				return ledger.ErrInvalid
			}
			d.EstimatedResaleValue = p.EstimatedResaleValue
		}
		if p.ResidenceID != nil {
			d.ResidenceID = *p.ResidenceID
		}
		var residence any
		if d.ResidenceID != "" {
			residence = d.ResidenceID
			if _, err := scanResidence(tx.QueryRowContext(ctx, `SELECT `+residenceColumns+` FROM residences WHERE id=?`, d.ResidenceID)); err != nil {
				return err
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE durable_details SET has_original_packaging=?,purchase_proof=?,estimated_resale_cents=?,residence_id=? WHERE item_id=?`, d.HasOriginalPackaging, d.PurchaseProof, d.EstimatedResaleValue, residence, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE durable_details SET brand=?,model=?,serial_number=?,warranty_until=?,note=? WHERE item_id=?`, d.Brand, d.Model, d.SerialNumber, d.WarrantyUntil, d.Note, id); e != nil {
			return e
		}
		d, e = durableDetails(ctx, tx, id)
		return e
	})
	return d, replay, e
}
func (s *Store) AddAlias(ctx context.Context, key, id, name string) (ledger.Alias, bool, error) {
	var a ledger.Alias
	replay, e := s.ledgerTx(ctx, key, "alias:"+id, name, &a, func(tx *sql.Tx) error {
		if _, e := getTxItem(ctx, tx, id); e != nil {
			return e
		}
		name = strings.TrimSpace(name)
		normalized := ledger.NormalizeName(name)
		if normalized == "" || len([]rune(name)) > 100 {
			return fmt.Errorf("%w: invalid alias", ledger.ErrInvalid)
		}
		var existingID, itemID string
		e := tx.QueryRowContext(ctx, `SELECT id,item_id FROM product_aliases WHERE normalized_alias=?`, normalized).Scan(&existingID, &itemID)
		if e == nil {
			if itemID != id {
				return fmt.Errorf("%w: alias belongs to another item", ledger.ErrConflict)
			}
			a = ledger.Alias{ID: existingID, ItemID: id, Alias: name, NormalizedAlias: normalized}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		matched, e := resolveTxItem(ctx, tx, name)
		if e == nil && matched.ID != id {
			return fmt.Errorf("%w: alias conflicts with an item name", ledger.ErrConflict)
		}
		if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		a = ledger.Alias{ID: uuid.NewString(), ItemID: id, Alias: name, NormalizedAlias: normalized}
		_, e = tx.ExecContext(ctx, `INSERT INTO product_aliases VALUES(?,?,?,?)`, a.ID, id, name, normalized)
		return e
	})
	return a, replay, e
}
func (s *Store) Aliases(ctx context.Context, id string) ([]ledger.Alias, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT * FROM product_aliases WHERE item_id=? ORDER BY rowid`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.Alias{}
	for rows.Next() {
		var a ledger.Alias
		if e = rows.Scan(&a.ID, &a.ItemID, &a.Alias, &a.NormalizedAlias); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) ResolveItem(ctx context.Context, name string, spec ...string) (inventory.Item, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return inventory.Item{}, e
	}
	defer tx.Rollback()
	target := itemTarget{Name: name}
	if len(spec) > 0 {
		target.Spec = spec[0]
	}
	return resolveTarget(ctx, tx, target)
}
func (s *Store) Dashboard(ctx context.Context, month string) (ledger.Dashboard, error) {
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	if month == "" {
		month = time.Now().In(zone).Format("2006-01")
	}
	start, e := time.ParseInLocation("2006-01", month, zone)
	if e != nil {
		return ledger.Dashboard{}, fmt.Errorf("%w: month must be YYYY-MM", ledger.ErrInvalid)
	}
	d := ledger.Dashboard{Month: month, Currency: "CNY"}
	balance, err := s.Balances(ctx)
	if err != nil {
		return d, err
	}
	d.LiquidBalance = balance.LiquidBalance
	d.ConsumptionBalance = balance.ConsumptionBalance
	d.TotalValue = balance.TotalValue
	d.Accounts = balance.Accounts
	d.TotalBalance = balance.TotalBalance
	d.AvailableBalance = balance.AvailableBalance
	from, to, err := ledger.PeriodRange("today", "", "", time.Now())
	if err != nil {
		return d, err
	}
	if d.TodayExpense, err = s.ExpenseTotal(ctx, ledger.HistoryFilter{From: from, To: to}); err != nil {
		return d, err
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM durable_details WHERE status IN ('ACTIVE','STORED')`).Scan(&d.DurableCount); err != nil {
		return d, err
	}
	if d.RecentTransactions, err = s.Transactions(ctx, ledger.MoneyFilter{HistoryFilter: ledger.HistoryFilter{Limit: 5}}); err != nil {
		return d, err
	}
	f := ledger.HistoryFilter{From: start.Format(time.RFC3339), To: start.AddDate(0, 1, 0).Format(time.RFC3339)}
	if d.MonthExpense, e = s.ExpenseTotal(ctx, f); e != nil {
		return d, e
	}
	if d.Inventory, e = s.Summary(ctx); e != nil {
		return d, e
	}
	if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE item_type='CONSUMABLE' AND quantity>0`).Scan(&d.ConsumableCount); e != nil {
		return d, e
	}

	if d.RecentPurchases, e = s.Purchases(ctx, ledger.HistoryFilter{Limit: 5}); e != nil {
		return d, e
	}
	// Filter before LIMIT so recent consumption cannot be displaced by purchases.
	rows, e := s.db.QueryContext(ctx, `SELECT * FROM inventory_events WHERE type IN ('CONSUME','FINISH') ORDER BY julianday(occurred_at) DESC,rowid DESC LIMIT 5`)
	if e != nil {
		return d, e
	}
	d.RecentConsumption = []ledger.InventoryEvent{}
	for rows.Next() {
		var v ledger.InventoryEvent
		if e = scanEvent(rows, &v); e != nil {
			rows.Close()
			return d, e
		}
		d.RecentConsumption = append(d.RecentConsumption, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return d, e
	}
	d.RecentMaintenance, e = s.MaintenanceHistory(ctx, ledger.HistoryFilter{Limit: 5})
	return d, e
}
