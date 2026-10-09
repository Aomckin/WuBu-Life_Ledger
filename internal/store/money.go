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
	"stockroom/internal/ledger"
)

const accountColumns = `id,name,type,currency,balance_cents,credit_limit_cents,active,note,created_at,updated_at`
const transactionColumns = `id,account_id,type,amount_cents,delta_cents,before_cents,after_cents,category,merchant,purchase_event_id,maintenance_event_id,related_transaction_id,external_id,occurred_at,source,note,reason,created_at,voided_at`

func scanAccount(r scanner) (ledger.Account, error) {
	var a ledger.Account
	e := r.Scan(&a.ID, &a.Name, &a.Type, &a.Currency, &a.CurrentBalance, &a.CreditLimit, &a.Active, &a.Note, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return a, e
}
func scanMoney(r scanner) (ledger.MoneyTransaction, error) {
	var t ledger.MoneyTransaction
	e := r.Scan(&t.ID, &t.AccountID, &t.Type, &t.Amount, &t.BalanceDelta, &t.BalanceBefore, &t.BalanceAfter, &t.Category, &t.Merchant, &t.PurchaseEventID, &t.MaintenanceEventID, &t.RelatedTransactionID, &t.ExternalID, &t.OccurredAt, &t.Source, &t.Note, &t.Reason, &t.CreatedAt, &t.VoidedAt)
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return t, e
}
func accountTx(ctx context.Context, tx *sql.Tx, id string) (ledger.Account, error) {
	if id == "" {
		if e := tx.QueryRowContext(ctx, `SELECT default_payment_account FROM money_settings WHERE id=1`).Scan(&id); e != nil {
			return ledger.Account{}, e
		}
		if id == "" {
			id = "unknown"
		}
	}
	a, e := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=? OR name=?`, id, id))
	if e != nil {
		return a, e
	}
	if !a.Active {
		return a, fmt.Errorf("%w: account is inactive", ledger.ErrConflict)
	}
	return a, nil
}
func (s *Store) GetAccount(ctx context.Context, id string) (ledger.Account, error) {
	return scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=? OR name=?`, id, id))
}
func (s *Store) Accounts(ctx context.Context) ([]ledger.Account, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM accounts ORDER BY rowid`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.Account{}
	for rows.Next() {
		a, e := scanAccount(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) Balances(ctx context.Context) (ledger.BalanceSummary, error) {
	a, e := s.Accounts(ctx)
	out := ledger.BalanceSummary{Currency: "CNY", Accounts: a}
	for _, v := range a {
		if v.ID == "unknown" {
			continue
		}
		out.TotalBalance += v.CurrentBalance
		if v.Active && v.Type != "CREDIT_CARD" {
			out.AvailableBalance += v.CurrentBalance
		}
	}
	return out, e
}
func (s *Store) CreateAccount(ctx context.Context, key string, p ledger.AccountInput) (ledger.Account, bool, error) {
	var out ledger.Account
	replay, e := s.ledgerTx(ctx, key, "create_account", p, &out, func(tx *sql.Tx) error {
		p.Name = strings.TrimSpace(p.Name)
		p.Type = strings.ToUpper(p.Type)
		if p.Type == "" {
			p.Type = "OTHER"
		}
		if p.Currency == "" {
			p.Currency = "CNY"
		}
		if p.Name == "" || len([]rune(p.Name)) > 100 || len([]rune(p.Note)) > 2000 || p.Currency != "CNY" || p.InitialBalance < -1e12 || p.InitialBalance > 1e12 {
			return ledger.ErrInvalid
		}
		switch p.Type {
		case "WALLET", "BANK", "CASH", "CAMPUS_CARD", "CREDIT_CARD", "OTHER":
		default:
			return ledger.ErrInvalid
		}
		if p.CreditLimit != nil && (*p.CreditLimit < 0 || *p.CreditLimit > 1e12 || p.Type != "CREDIT_CARD") {
			return fmt.Errorf("%w: credit limit requires CREDIT_CARD", ledger.ErrInvalid)
		}
		var count int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE name=?`, p.Name).Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			return fmt.Errorf("%w: account name exists", ledger.ErrConflict)
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		id := uuid.NewString()
		if _, e := tx.ExecContext(ctx, `INSERT INTO accounts VALUES(?,?,?,?,0,?,1,?,?,?)`, id, p.Name, p.Type, p.Currency, p.CreditLimit, p.Note, now, now); e != nil {
			return e
		}
		amount := ledger.Money(p.InitialBalance)
		if amount < 0 {
			amount = -amount
		}
		_, e := postMoney(ctx, tx, "ADJUSTMENT", ledger.MoneyInput{AccountID: id, Amount: amount, Source: "system", Reason: "INITIAL_BALANCE"}, "", p.InitialBalance)
		if e != nil {
			return e
		}
		out, e = accountTx(ctx, tx, id)
		return e
	})
	return out, replay, e
}
func (s *Store) PatchAccount(ctx context.Context, key, id string, p ledger.AccountPatch) (ledger.Account, bool, error) {
	var out ledger.Account
	r, e := s.ledgerTx(ctx, key, "patch_account:"+id, p, &out, func(tx *sql.Tx) error {
		a, e := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=?`, id))
		if e != nil {
			return e
		}
		if id == "unknown" {
			return fmt.Errorf("%w: migration account is reserved", ledger.ErrConflict)
		}
		if p.Name != nil {
			a.Name = strings.TrimSpace(*p.Name)
		}
		if p.Note != nil {
			a.Note = *p.Note
		}
		if p.Active != nil {
			a.Active = *p.Active
		}
		if p.CreditLimit != nil {
			a.CreditLimit = p.CreditLimit
		}
		if a.Name == "" || len([]rune(a.Name)) > 100 || len([]rune(a.Note)) > 2000 {
			return ledger.ErrInvalid
		}
		if a.CreditLimit != nil && (*a.CreditLimit < 0 || *a.CreditLimit > 1e12 || a.Type != "CREDIT_CARD") {
			return ledger.ErrInvalid
		}
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE name=? AND id<>?`, a.Name, id).Scan(&n); e != nil {
			return e
		}
		if n > 0 {
			return ledger.ErrConflict
		}
		if !a.Active {
			var def string
			if e = tx.QueryRowContext(ctx, `SELECT default_payment_account FROM money_settings WHERE id=1`).Scan(&def); e != nil {
				return e
			}
			if def == id {
				return fmt.Errorf("%w: choose another default account before deactivation", ledger.ErrConflict)
			}
		}
		a.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		_, e = tx.ExecContext(ctx, `UPDATE accounts SET name=?,active=?,note=?,credit_limit_cents=?,updated_at=? WHERE id=?`, a.Name, a.Active, a.Note, a.CreditLimit, a.UpdatedAt, id)
		out = a
		return e
	})
	return out, r, e
}
func (s *Store) MoneySettings(ctx context.Context) (ledger.MoneySettings, error) {
	var p ledger.MoneySettings
	e := s.db.QueryRowContext(ctx, `SELECT default_payment_account FROM money_settings WHERE id=1`).Scan(&p.DefaultPaymentAccount)
	return p, e
}
func (s *Store) SetMoneySettings(ctx context.Context, key string, p ledger.MoneySettings) (ledger.MoneySettings, bool, error) {
	var out ledger.MoneySettings
	r, e := s.ledgerTx(ctx, key, "money_settings", p, &out, func(tx *sql.Tx) error {
		out = p
		if p.DefaultPaymentAccount != "" {
			a, e := accountTx(ctx, tx, p.DefaultPaymentAccount)
			if e != nil {
				return e
			}
			out.DefaultPaymentAccount = a.ID
		}
		_, e := tx.ExecContext(ctx, `UPDATE money_settings SET default_payment_account=? WHERE id=1`, out.DefaultPaymentAccount)
		return e
	})
	return out, r, e
}

// postMoney records before/after and updates the balance within the caller's reality event transaction.
func postMoney(ctx context.Context, tx *sql.Tx, kind string, p ledger.MoneyInput, maintenance string, delta ledger.SignedMoney) (ledger.MoneyTransaction, error) {
	var out ledger.MoneyTransaction
	if p.Amount < 0 || p.Amount > 1e12 || len(p.Category) > 100 || len([]rune(p.Merchant)) > 200 || len(p.ExternalID) > 200 || len([]rune(p.Note)) > 2000 || len([]rune(p.Reason)) > 2000 {
		return out, ledger.ErrInvalid
	}
	at, e := ledger.Timestamp(p.OccurredAt)
	if e != nil {
		return out, e
	}
	source, e := ledger.Source(p.Source)
	if e != nil {
		return out, e
	}
	a, e := accountTx(ctx, tx, p.AccountID)
	if e != nil {
		return out, e
	}
	if p.ExternalID != "" {
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM money_transactions WHERE source=? AND account_id=? AND external_id=?`, source, a.ID, p.ExternalID).Scan(&n); e != nil {
			return out, e
		}
		if n > 0 {
			return out, fmt.Errorf("%w: external_id already recorded", ledger.ErrConflict)
		}
	}
	if delta > 1e12 || delta < -1e12 || a.CurrentBalance+delta > 1e12 || a.CurrentBalance+delta < -1e12 {
		return out, fmt.Errorf("%w: balance out of range", ledger.ErrInvalid)
	}
	if p.Category == "" {
		p.Category = "other"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	out = ledger.MoneyTransaction{ID: uuid.NewString(), AccountID: a.ID, Type: kind, Amount: p.Amount, BalanceDelta: delta, BalanceBefore: a.CurrentBalance, BalanceAfter: a.CurrentBalance + delta, Category: p.Category, Merchant: p.Merchant, PurchaseEventID: p.PurchaseEventID, MaintenanceEventID: maintenance, RelatedTransactionID: p.RelatedTransactionID, ExternalID: p.ExternalID, OccurredAt: at, Source: source, Note: p.Note, Reason: p.Reason, CreatedAt: now}
	_, e = tx.ExecContext(ctx, `INSERT INTO money_transactions(`+transactionColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, out.ID, out.AccountID, out.Type, out.Amount, out.BalanceDelta, out.BalanceBefore, out.BalanceAfter, out.Category, out.Merchant, out.PurchaseEventID, out.MaintenanceEventID, out.RelatedTransactionID, out.ExternalID, out.OccurredAt, out.Source, out.Note, out.Reason, out.CreatedAt, out.VoidedAt)
	if e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, `UPDATE accounts SET balance_cents=?,updated_at=? WHERE id=?`, out.BalanceAfter, now, a.ID)
	return out, e
}
func paymentAccountTx(ctx context.Context, tx *sql.Tx, id, method string) (ledger.Account, error) {
	if id == "" && method != "" {
		var found string
		e := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE name=?`, method).Scan(&found)
		if e == nil {
			id = found
		} else if !errors.Is(e, sql.ErrNoRows) {
			return ledger.Account{}, e
		}
	}
	return accountTx(ctx, tx, id)
}
func purchasePayment(ctx context.Context, tx *sql.Tx, p *ledger.Purchase) (ledger.MoneyTransaction, error) {
	account, e := paymentAccountTx(ctx, tx, p.AccountID, p.PaymentMethod)
	if e != nil {
		return ledger.MoneyTransaction{}, e
	}
	p.AccountID = account.ID
	t, e := postMoney(ctx, tx, "EXPENSE", ledger.MoneyInput{AccountID: p.AccountID, Amount: p.TotalAmount, Category: p.Category, Merchant: p.Merchant, PurchaseEventID: p.ID, OccurredAt: p.PurchasedAt, Source: p.Source, Note: p.Note}, "", -ledger.SignedMoney(p.TotalAmount))
	if e != nil {
		return t, e
	}
	p.AccountID = t.AccountID
	p.Status = "COMPLETED"
	_, e = tx.ExecContext(ctx, `UPDATE money_transactions SET payment_method=? WHERE id=?`, p.PaymentMethod, t.ID)
	return t, e
}
func (s *Store) MoneyAction(ctx context.Context, key, kind string, p ledger.MoneyInput) (ledger.MoneyResult, bool, error) {
	out := ledger.MoneyResult{Transactions: []ledger.MoneyTransaction{}}
	r, e := s.ledgerTx(ctx, key, "money:"+kind, p, &out, func(tx *sql.Tx) error {
		if kind != "ADJUSTMENT" && p.Amount <= 0 {
			return fmt.Errorf("%w: amount must be positive", ledger.ErrInvalid)
		}
		if kind != "REFUND" && (p.PurchaseEventID != "" || p.RelatedTransactionID != "" || len(p.ReturnItems) > 0) {
			return fmt.Errorf("%w: links/returns are only accepted for refunds", ledger.ErrInvalid)
		}
		if kind != "ADJUSTMENT" && p.ActualBalance != nil {
			return ledger.ErrInvalid
		}
		switch kind {
		case "TRANSFER":
			if p.FromAccount == "" || p.ToAccount == "" || p.AccountID != "" {
				return fmt.Errorf("%w: transfer requires from_account/to_account", ledger.ErrInvalid)
			}
			a, e := accountTx(ctx, tx, p.FromAccount)
			if e != nil {
				return e
			}
			b, e := accountTx(ctx, tx, p.ToAccount)
			if e != nil {
				return e
			}
			if a.ID == b.ID {
				return fmt.Errorf("%w: accounts must differ", ledger.ErrInvalid)
			}
			p.AccountID = a.ID
			x, e := postMoney(ctx, tx, "TRANSFER_OUT", p, "", -ledger.SignedMoney(p.Amount))
			if e != nil {
				return e
			}
			p.AccountID = b.ID
			p.RelatedTransactionID = x.ID
			y, e := postMoney(ctx, tx, "TRANSFER_IN", p, "", ledger.SignedMoney(p.Amount))
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE money_transactions SET related_transaction_id=? WHERE id=?`, y.ID, x.ID); e != nil {
				return e
			}
			x.RelatedTransactionID = y.ID
			out.Transactions = []ledger.MoneyTransaction{x, y}
			return nil
		case "EXPENSE", "INCOME", "ADJUSTMENT", "REFUND":
			if p.FromAccount != "" || p.ToAccount != "" {
				return ledger.ErrInvalid
			}
			delta := ledger.SignedMoney(p.Amount)
			if kind == "EXPENSE" {
				delta = -delta
			}
			if kind == "ADJUSTMENT" {
				if p.ActualBalance == nil || p.Amount != 0 {
					return fmt.Errorf("%w: adjustment requires actual_balance, no amount", ledger.ErrInvalid)
				}
				a, e := accountTx(ctx, tx, p.AccountID)
				if e != nil {
					return e
				}
				delta = *p.ActualBalance - a.CurrentBalance
				p.Amount = ledger.Money(delta)
				if p.Amount < 0 {
					p.Amount = -p.Amount
				}
			}
			if kind == "REFUND" {
				original, e := refundOriginal(ctx, tx, p)
				if e != nil {
					return e
				}
				p.RelatedTransactionID = original.ID
				p.PurchaseEventID = original.PurchaseEventID
				p.Category = original.Category
				p.Merchant = original.Merchant
				if p.AccountID == "" {
					p.AccountID = original.AccountID
				}
				var refunded ledger.Money
				if e = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_cents),0) FROM money_transactions WHERE type='REFUND' AND related_transaction_id=? AND voided_at=''`, original.ID).Scan(&refunded); e != nil {
					return e
				}
				if refunded+p.Amount > original.Amount {
					return fmt.Errorf("%w: refund exceeds remaining expense", ledger.ErrConflict)
				}
			}
			t, e := postMoney(ctx, tx, kind, p, "", delta)
			if e != nil {
				return e
			}
			if kind == "REFUND" {
				if e = returnStock(ctx, tx, t, p.ReturnItems); e != nil {
					return e
				}
				if t.PurchaseEventID != "" {
					if _, e = tx.ExecContext(ctx, `UPDATE purchase_events SET updated_at=? WHERE id=?`, nextPurchaseVersion(t.CreatedAt), t.PurchaseEventID); e != nil {
						return e
					}
				}
			}
			out.Transactions = append(out.Transactions, t)
			return nil
		default:
			return ledger.ErrInvalid
		}
	})
	return out, r, e
}
func refundOriginal(ctx context.Context, tx *sql.Tx, p ledger.MoneyInput) (ledger.MoneyTransaction, error) {
	var t ledger.MoneyTransaction
	var e error
	if p.RelatedTransactionID != "" {
		t, e = scanMoney(tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE id=?`, p.RelatedTransactionID))
	} else if p.PurchaseEventID != "" {
		t, e = scanMoney(tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE purchase_event_id=? AND type='EXPENSE' AND voided_at=''`, p.PurchaseEventID))
	} else {
		return t, fmt.Errorf("%w: refund needs purchase_event_id or related_transaction_id", ledger.ErrInvalid)
	}
	if e != nil {
		return t, e
	}
	if t.Type != "EXPENSE" || t.VoidedAt != "" || p.PurchaseEventID != "" && t.PurchaseEventID != p.PurchaseEventID {
		return t, ledger.ErrConflict
	}
	return t, nil
}
func returnStock(ctx context.Context, tx *sql.Tx, t ledger.MoneyTransaction, lines []ledger.ReturnItem) error {
	seen := map[string]bool{}
	for _, l := range lines {
		if t.PurchaseEventID == "" || l.ItemID == "" || seen[l.ItemID] || l.Quantity <= 0 || math.IsNaN(l.Quantity) || math.IsInf(l.Quantity, 0) || math.Abs(l.Quantity*1000-math.Round(l.Quantity*1000)) > 0.0001 {
			return ledger.ErrInvalid
		}
		seen[l.ItemID] = true
		var purchased, returned float64
		if e := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(quantity),0) FROM purchase_items WHERE purchase_event_id=? AND item_id=?`, t.PurchaseEventID, l.ItemID).Scan(&purchased); e != nil {
			return e
		}
		if e := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(quantity),0) FROM refund_returns WHERE purchase_event_id=? AND item_id=?`, t.PurchaseEventID, l.ItemID).Scan(&returned); e != nil {
			return e
		}
		if l.Quantity+returned > purchased+0.00001 {
			return fmt.Errorf("%w: return exceeds purchased quantity", ledger.ErrConflict)
		}
		item, e := getTxItem(ctx, tx, l.ItemID)
		if e != nil {
			return e
		}
		if item.ItemType == ledger.Durable {
			if l.Quantity != 1 {
				return ledger.ErrInvalid
			}
			if _, e = durableTransition(ctx, tx, l.ItemID, "RETIRED", ledger.DurableActionInput{OccurredAt: t.OccurredAt, Source: t.Source, Note: "退款退回物品"}); e != nil {
				return e
			}
		} else {
			delta := -l.Quantity
			if _, e = mutateInventory(ctx, tx, l.ItemID, "ADJUST", ledger.InventoryInput{Delta: &delta, OccurredAt: t.OccurredAt, Source: t.Source, Note: "退款退回库存"}, &t.PurchaseEventID); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO refund_returns VALUES(?,?,?,?)`, t.ID, t.PurchaseEventID, l.ItemID, l.Quantity); e != nil {
			return e
		}
	}
	return nil
}

// Corrections compensate the old balance delta and replace only the statistical entry.
func correctPurchaseMoney(ctx context.Context, tx *sql.Tx, before ledger.Purchase, after *ledger.Purchase) error {
	t, e := scanMoney(tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE purchase_event_id=? AND type='EXPENSE' AND voided_at=''`, before.ID))
	if e != nil {
		return e
	}
	var refunds int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM money_transactions WHERE type='REFUND' AND related_transaction_id=?`, t.ID).Scan(&refunds); e != nil {
		return e
	}
	if refunds > 0 {
		return fmt.Errorf("%w: refunded purchase cannot be amended or cancelled", ledger.ErrConflict)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, e = tx.ExecContext(ctx, `UPDATE money_transactions SET voided_at=? WHERE id=?`, now, t.ID); e != nil {
		return e
	}
	if _, e = postMoney(ctx, tx, "ADJUSTMENT", ledger.MoneyInput{AccountID: t.AccountID, Amount: t.Amount, RelatedTransactionID: t.ID, PurchaseEventID: t.PurchaseEventID, Source: "system", Reason: "PURCHASE_CORRECTION"}, "", -t.BalanceDelta); e != nil {
		return e
	}
	if after != nil {
		if after.AccountID == "" {
			after.AccountID = before.AccountID
		}
		_, e = purchasePayment(ctx, tx, after)
	}
	return e
}
