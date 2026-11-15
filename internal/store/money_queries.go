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
		where += ` AND EXISTS(SELECT 1 FROM transaction_items i WHERE i.transaction_id=m.id AND i.item_key=?)`
		args = append(args, f.ItemID)
	}
	if f.Type != "" {
		switch f.Type {
		case "EXPENSE", "INCOME", "TRANSFER_OUT", "TRANSFER_IN", "REFUND", "ADJUSTMENT", "BONUS":
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
	rows, e := s.db.QueryContext(ctx, `SELECT `+strings.Join(cols, ",")+`,CASE WHEN m.type='EXPENSE' AND m.voided_at='' THEN m.amount_cents-COALESCE((SELECT SUM(x.amount_cents) FROM money_transactions x WHERE x.type='REFUND' AND x.voided_at='' AND x.related_transaction_id=m.id),0) END FROM money_transactions m WHERE `+where+` ORDER BY julianday(m.occurred_at) DESC,m.rowid DESC LIMIT ? OFFSET ?`, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.MoneyTransaction{}
	for rows.Next() {
		var remaining *ledger.Money
		t, e := scanMoney(rows, &remaining)
		if e != nil {
			return nil, e
		}
		t.RefundableAmount = remaining
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
	e = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN m.type='EXPENSE' THEN amount_cents ELSE 0 END),0),COALESCE(SUM(CASE WHEN m.type='REFUND' THEN amount_cents ELSE 0 END),0),COALESCE(SUM(CASE WHEN m.type='INCOME' THEN amount_cents ELSE 0 END),0),COALESCE(SUM(CASE WHEN m.type='TRANSFER_OUT' THEN amount_cents ELSE 0 END),0),COALESCE(SUM(CASE WHEN m.type='BONUS' THEN amount_cents ELSE 0 END),0) FROM money_transactions m WHERE `+where, args...).Scan(&out.Expense, &out.Refund, &out.Income, &out.TransferOut, &out.Bonus)
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
		if f.ItemID != "" {
			where += " AND i.item_key=?"
			args = append(args, f.ItemID)
		}
		rows, e = s.db.QueryContext(ctx, `SELECT COALESCE(i.item_key,'未关联物品'),
 CAST(ROUND(SUM(CASE m.type WHEN 'REFUND' THEN -m.amount_cents ELSE m.amount_cents END * COALESCE(i.share,1))) AS INTEGER),
 COUNT(DISTINCT m.id) FROM money_transactions m LEFT JOIN transaction_items i ON i.transaction_id=m.id
 WHERE m.type IN ('EXPENSE','REFUND') AND `+where+` GROUP BY 1 ORDER BY 2 DESC`, args...)
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
func (s FinanceRepository) MoneyCategories(ctx context.Context) ([]string, error) {
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

func (s FinanceRepository) SaveMoneyCategory(ctx context.Context, key string, p CategoryInput) (CategoryInput, bool, error) {
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
