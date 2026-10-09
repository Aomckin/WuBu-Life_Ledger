package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"stockroom/internal/ledger"
)

func moneyWhere(f ledger.MoneyFilter, includeVoided bool) (string, []any, error) {
	where, args := historyWhere(f.HistoryFilter, "m.occurred_at", "m.category")
	if !includeVoided {
		where += " AND m.voided_at=''"
	}
	for _, v := range []struct{ key, value string }{{"m.account_id", f.AccountID}, {"m.merchant", f.Merchant}, {"m.type", f.Type}} {
		if v.value != "" {
			where += " AND " + v.key + "=?"
			args = append(args, v.value)
		}
	}
	if f.ToAccount != "" {
		where += ` AND m.type='TRANSFER_OUT' AND EXISTS(SELECT 1 FROM money_transactions destination WHERE destination.id=m.related_transaction_id AND destination.account_id=?)`
		args = append(args, f.ToAccount)
	}
	if f.ItemID != "" {
		where += ` AND (EXISTS(SELECT 1 FROM purchase_items l WHERE l.purchase_event_id=m.purchase_event_id AND l.item_id=?) OR EXISTS(SELECT 1 FROM maintenance_events me WHERE me.id=m.maintenance_event_id AND me.item_id=?) OR (m.type='REFUND' AND EXISTS(SELECT 1 FROM money_transactions original JOIN maintenance_events me ON me.id=original.maintenance_event_id WHERE original.id=m.related_transaction_id AND me.item_id=?)))`
		args = append(args, f.ItemID, f.ItemID, f.ItemID)
	}
	if f.Type != "" {
		switch f.Type {
		case "EXPENSE", "INCOME", "TRANSFER_OUT", "TRANSFER_IN", "REFUND", "ADJUSTMENT":
		default:
			return "", nil, ledger.ErrInvalid
		}
	}
	return where, args, nil
}
func (s *Store) Transactions(ctx context.Context, f ledger.MoneyFilter) ([]ledger.MoneyTransaction, error) {
	where, args, e := moneyWhere(f, true)
	if e != nil {
		return nil, e
	}
	limit, offset := page(f.HistoryFilter)
	args = append(args, limit, offset)
	cols := strings.Split(transactionColumns, ",")
	for i := range cols {
		cols[i] = "m." + cols[i]
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+strings.Join(cols, ",")+` FROM money_transactions m WHERE `+where+` ORDER BY julianday(m.occurred_at) DESC,m.rowid DESC LIMIT ? OFFSET ?`, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.MoneyTransaction{}
	for rows.Next() {
		t, e := scanMoney(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) MoneySummary(ctx context.Context, f ledger.MoneyFilter) (ledger.MoneySummary, error) {
	out := ledger.MoneySummary{Currency: "CNY", Groups: []ledger.MoneyGroup{}}
	where, args, e := moneyWhere(f, false)
	if e != nil {
		return out, e
	}
	e = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN m.type='EXPENSE' THEN amount_cents ELSE 0 END),0),COALESCE(SUM(CASE WHEN m.type='REFUND' THEN amount_cents ELSE 0 END),0),COALESCE(SUM(CASE WHEN m.type='INCOME' THEN amount_cents ELSE 0 END),0),COALESCE(SUM(CASE WHEN m.type='TRANSFER_OUT' THEN amount_cents ELSE 0 END),0) FROM money_transactions m WHERE `+where, args...).Scan(&out.Expense, &out.Refund, &out.Income, &out.TransferOut)
	if e != nil {
		return out, e
	}
	out.NetExpense = ledger.SignedMoney(out.Expense - out.Refund)
	if f.GroupBy == "" {
		return out, nil
	}
	var expr string
	switch f.GroupBy {
	case "category":
		expr = "m.category"
	case "merchant":
		expr = "m.merchant"
	case "account":
		expr = "m.account_id"
	case "item":
		expr = "item"
	default:
		return out, fmt.Errorf("%w: group_by must be category/merchant/account/item", ledger.ErrInvalid)
	}
	var rows *sql.Rows
	if expr == "item" {
		itemWhere := where
		itemArgs := append([]any{}, args...)
		if f.ItemID != "" {
			itemWhere += " AND l.item_id=?"
			itemArgs = append(itemArgs, f.ItemID)
		}
		query := `WITH allocation AS (
        SELECT COALESCE(NULLIF(l.item_id,''),l.raw_name) AS item_key,
        (CASE m.type WHEN 'REFUND' THEN -1 ELSE 1 END)*m.amount_cents*CASE WHEN p.item_total_cents>0 THEN 1.0*l.subtotal_cents/p.item_total_cents ELSE 1.0/(SELECT COUNT(*) FROM purchase_items WHERE purchase_event_id=p.id) END AS amount,
        m.id AS event_id FROM money_transactions m JOIN purchase_events p ON p.id=m.purchase_event_id JOIN purchase_items l ON l.purchase_event_id=p.id WHERE m.type IN ('EXPENSE','REFUND') AND p.deleted_at IS NULL AND ` + itemWhere + `
        UNION ALL
        SELECT me.item_id,(CASE m.type WHEN 'REFUND' THEN -m.amount_cents ELSE m.amount_cents END),m.id
        FROM money_transactions m JOIN maintenance_events me ON me.id=CASE WHEN m.type='REFUND' THEN (SELECT maintenance_event_id FROM money_transactions original WHERE original.id=m.related_transaction_id) ELSE m.maintenance_event_id END
        WHERE m.type IN ('EXPENSE','REFUND') AND ` + where + `
        UNION ALL
        SELECT '未关联物品',(CASE m.type WHEN 'REFUND' THEN -m.amount_cents ELSE m.amount_cents END),m.id FROM money_transactions m
        WHERE m.purchase_event_id='' AND m.maintenance_event_id='' AND NOT EXISTS(SELECT 1 FROM money_transactions original WHERE original.id=m.related_transaction_id AND original.maintenance_event_id<>'') AND m.type IN ('EXPENSE','REFUND') AND ` + where + `
        ) SELECT item_key,CAST(ROUND(SUM(amount)) AS INTEGER),COUNT(DISTINCT event_id) FROM allocation GROUP BY item_key ORDER BY 2 DESC`
		allArgs := append(itemArgs, args...)
		allArgs = append(allArgs, args...)
		rows, e = s.db.QueryContext(ctx, query, allArgs...)
	} else {
		rows, e = s.db.QueryContext(ctx, `SELECT `+expr+`,SUM(CASE m.type WHEN 'REFUND' THEN -m.amount_cents ELSE m.amount_cents END),COUNT(*) FROM money_transactions m WHERE m.type IN ('EXPENSE','REFUND') AND `+where+` GROUP BY `+expr+` ORDER BY 2 DESC`, args...)
	}
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var g ledger.MoneyGroup
		if e = rows.Scan(&g.Key, &g.Amount, &g.Count); e != nil {
			return out, e
		}
		out.Groups = append(out.Groups, g)
	}
	return out, rows.Err()
}
func (s *Store) MoneyCategories(ctx context.Context) ([]string, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT name FROM money_categories UNION SELECT category FROM money_transactions ORDER BY 1`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if e = rows.Scan(&v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type CategoryInput struct {
	Name         string `json:"name"`
	PreviousName string `json:"previous_name,omitempty"`
}

func (s *Store) SaveMoneyCategory(ctx context.Context, key string, p CategoryInput) (CategoryInput, bool, error) {
	var out CategoryInput
	r, e := s.ledgerTx(ctx, key, "money_category", p, &out, func(tx *sql.Tx) error {
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" || len(p.Name) > 100 || len(p.PreviousName) > 100 {
			return ledger.ErrInvalid
		}
		if p.PreviousName != "" && p.PreviousName != p.Name {
			if _, e := tx.ExecContext(ctx, `UPDATE money_transactions SET category=? WHERE category=?`, p.Name, p.PreviousName); e != nil {
				return e
			}
			if _, e := tx.ExecContext(ctx, `UPDATE purchase_events SET category=? WHERE category=?`, p.Name, p.PreviousName); e != nil {
				return e
			}
			if _, e := tx.ExecContext(ctx, `DELETE FROM money_categories WHERE name=?`, p.PreviousName); e != nil {
				return e
			}
		}
		if _, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO money_categories VALUES(?)`, p.Name); e != nil {
			return e
		}
		out = p
		return nil
	})
	return out, r, e
}
