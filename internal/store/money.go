package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"stockroom/internal/ledger"
)

const accountColumns = `id,name,type,currency,balance_cents,credit_limit_cents,active,note,created_at,updated_at,account_class,merchant`
const transactionColumns = `id,account_id,type,amount_cents,delta_cents,before_cents,after_cents,category,merchant,related_transaction_id,external_id,occurred_at,source,note,reason,created_at,voided_at,expense_nature`

func scanAccount(r scanner) (ledger.Account, error) {
	var a ledger.Account
	e := r.Scan(&a.ID, &a.Name, &a.Type, &a.Currency, &a.CurrentBalance, &a.CreditLimit, &a.Active, &a.Note, &a.CreatedAt, &a.UpdatedAt, &a.AccountClass, &a.Merchant)
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	a.Warnings = []ledger.Warning{}
	if !a.Active && a.AccountClass == ledger.ConsumptionAccount && a.CurrentBalance > 0 {
		a.Warnings = append(a.Warnings, ledger.Warning{Code: "ACCOUNT_HAS_REMAINING_BALANCE", Message: "消费账户仍有余额，停用后不会计入消费储值汇总；记录与余额仍保留。"})
	}
	return a, e
}
func scanMoney(r scanner, extra ...any) (ledger.MoneyTransaction, error) {
	var t ledger.MoneyTransaction
	e := r.Scan(append([]any{&t.ID, &t.AccountID, &t.Type, &t.Amount, &t.BalanceDelta, &t.BalanceBefore, &t.BalanceAfter, &t.Category, &t.Merchant, &t.RelatedTransactionID, &t.ExternalID, &t.OccurredAt, &t.Source, &t.Note, &t.Reason, &t.CreatedAt, &t.VoidedAt, &t.ExpenseNature}, extra...)...)
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
func (s FinanceRepository) GetAccount(ctx context.Context, id string) (ledger.Account, error) {
	return scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=? OR name=?`, id, id))
}
func (s FinanceRepository) Accounts(ctx context.Context, filters ...ledger.AccountFilter) ([]ledger.Account, error) {
	where, args := "1=1", []any{}
	if len(filters) > 0 {
		f := filters[0]
		if f.AccountClass != "" {
			if !validAccountClass(f.AccountClass) {
				return nil, ledger.ErrInvalid
			}
			where += " AND account_class=?"
			args = append(args, f.AccountClass)
		}
		if len([]rune(f.Merchant)) > 200 {
			return nil, ledger.ErrInvalid
		}
		if f.Merchant != "" {
			where += " AND merchant=?"
			args = append(args, f.Merchant)
		}
		if f.Active != nil {
			where += " AND active=?"
			args = append(args, *f.Active)
		}
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE `+where+` ORDER BY rowid`, args...)
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
func (s FinanceRepository) Balances(ctx context.Context) (ledger.BalanceSummary, error) {
	a, e := s.Accounts(ctx)
	out := ledger.BalanceSummary{Currency: "CNY", Accounts: a}
	for _, v := range a {
		if v.ID == "unknown" {
			continue
		}
		out.TotalBalance += v.CurrentBalance
		if v.Active && v.Type != "CREDIT_CARD" {
			if v.AccountClass == ledger.ConsumptionAccount {
				out.ConsumptionBalance += v.CurrentBalance
			} else {
				out.LiquidBalance += v.CurrentBalance
			}
		}
	}
	out.TotalValue = out.LiquidBalance + out.ConsumptionBalance
	return out, e
}
func (s FinanceRepository) CreateAccount(ctx context.Context, key string, p ledger.AccountInput) (ledger.Account, bool, error) {
	var out ledger.Account
	replay, e := s.ledgerTx(ctx, key, "create_account", p, &out, func(tx *sql.Tx) error {
		p.Name = strings.TrimSpace(p.Name)
		p.Type = strings.ToUpper(p.Type)
		p.AccountClass = strings.ToUpper(p.AccountClass)
		if p.AccountClass == "" {
			p.AccountClass = ledger.StorageAccount
		}
		p.Merchant = strings.TrimSpace(p.Merchant)
		if !validAccountClass(p.AccountClass) || len([]rune(p.Merchant)) > 200 {
			return ledger.ErrInvalid
		}
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
		case "WALLET", "BANK", "CASH", "CAMPUS_CARD", "CREDIT_CARD", "MERCHANT_BALANCE", "OTHER":
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
		if _, e := tx.ExecContext(ctx, `INSERT INTO accounts(`+accountColumns+`) VALUES(?,?,?,?,0,?,1,?,?,?,?,?)`, id, p.Name, p.Type, p.Currency, p.CreditLimit, p.Note, now, now, p.AccountClass, p.Merchant); e != nil {
			return e
		}
		amount := ledger.Money(p.InitialBalance)
		if amount < 0 {
			amount = -amount
		}
		_, e := postMoney(ctx, tx, "ADJUSTMENT", ledger.MoneyInput{AccountID: id, Amount: amount, Source: "system", Reason: "INITIAL_BALANCE"}, p.InitialBalance)
		if e != nil {
			return e
		}
		out, e = accountTx(ctx, tx, id)
		return e
	})
	return out, replay, e
}
func (s FinanceRepository) PatchAccount(ctx context.Context, key, id string, p ledger.AccountPatch) (ledger.Account, bool, error) {
	var out ledger.Account
	r, e := s.ledgerTx(ctx, key, "patch_account:"+id, p, &out, func(tx *sql.Tx) error {
		a, e := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=?`, id))
		if e != nil {
			return e
		}
		if id == "unknown" {
			return fmt.Errorf("%w: migration account is reserved", ledger.ErrConflict)
		}
		if p.AccountClass != nil {
			a.AccountClass = strings.ToUpper(*p.AccountClass)
		}
		if p.Merchant != nil {
			a.Merchant = strings.TrimSpace(*p.Merchant)
		}
		if !validAccountClass(a.AccountClass) || len([]rune(a.Merchant)) > 200 {
			return ledger.ErrInvalid
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
		_, e = tx.ExecContext(ctx, `UPDATE accounts SET name=?,active=?,note=?,credit_limit_cents=?,updated_at=?,account_class=?,merchant=? WHERE id=?`, a.Name, a.Active, a.Note, a.CreditLimit, a.UpdatedAt, a.AccountClass, a.Merchant, id)
		if e != nil {
			return e
		}
		out, e = scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=?`, id))
		return e
	})
	return out, r, e
}
func (s FinanceRepository) MoneySettings(ctx context.Context) (ledger.MoneySettings, error) {
	var p ledger.MoneySettings
	e := s.db.QueryRowContext(ctx, `SELECT default_payment_account FROM money_settings WHERE id=1`).Scan(&p.DefaultPaymentAccount)
	return p, e
}
func (s FinanceRepository) SetMoneySettings(ctx context.Context, key string, p ledger.MoneySettings) (ledger.MoneySettings, bool, error) {
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
func postMoney(ctx context.Context, tx *sql.Tx, kind string, p ledger.MoneyInput, delta ledger.SignedMoney) (ledger.MoneyTransaction, error) {
	var out ledger.MoneyTransaction
	if !validNature(p.ExpenseNature) || !validFinancialTags(p.Tags) || (len(p.Tags) > 0 && kind != "EXPENSE" && kind != "INCOME") {
		return out, ledger.ErrInvalid
	}
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
	if kind == "BONUS" && a.AccountClass != ledger.ConsumptionAccount {
		return out, fmt.Errorf("%w: BONUS requires CONSUMPTION account", ledger.ErrInvalid)
	}
	if kind == "EXPENSE" {
		if e = accountMerchant(a, &p.Merchant); e != nil {
			return out, e
		}
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
	out = ledger.MoneyTransaction{ID: uuid.NewString(), AccountID: a.ID, Type: kind, Amount: p.Amount, BalanceDelta: delta, BalanceBefore: a.CurrentBalance, BalanceAfter: a.CurrentBalance + delta, Category: p.Category, Merchant: p.Merchant, RelatedTransactionID: p.RelatedTransactionID, ExternalID: p.ExternalID, OccurredAt: at, Source: source, Note: p.Note, Reason: p.Reason, CreatedAt: now, ExpenseNature: p.ExpenseNature}
	_, e = tx.ExecContext(ctx, `INSERT INTO money_transactions(`+transactionColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, out.ID, out.AccountID, out.Type, out.Amount, out.BalanceDelta, out.BalanceBefore, out.BalanceAfter, out.Category, out.Merchant, out.RelatedTransactionID, out.ExternalID, out.OccurredAt, out.Source, out.Note, out.Reason, out.CreatedAt, out.VoidedAt, out.ExpenseNature)
	if e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, `UPDATE accounts SET balance_cents=?,updated_at=? WHERE id=?`, out.BalanceAfter, now, a.ID)
	if e != nil {
		return out, e
	}
	if len(p.Tags) > 0 {
		annotation := ledger.TransactionAnnotation{Tags: p.Tags, Note: out.Note, Confidence: 1, AnnotatedBy: source, AnnotationStatus: "CONFIRMED"}
		payload, e := json.Marshal(annotation)
		if e != nil {
			return out, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO transaction_annotations VALUES(?,?,?)`, out.ID, string(payload), now); e != nil {
			return out, e
		}
	}
	e = compensateCheckpoint(ctx, tx, out)
	return out, e
}
func (s FinanceRepository) Record(ctx context.Context, key, kind string, p ledger.MoneyInput) (ledger.MoneyResult, bool, error) {
	out := ledger.MoneyResult{Transactions: []ledger.MoneyTransaction{}}
	operation := "money:" + kind
	if kind == "REFUND" {
		operation = "finance:refund"
	}
	r, e := s.ledgerTx(ctx, key, operation, p, &out, func(tx *sql.Tx) error {
		if kind != "ADJUSTMENT" && p.Amount <= 0 {
			return fmt.Errorf("%w: amount must be positive", ledger.ErrInvalid)
		}
		if kind != "REFUND" && p.RelatedTransactionID != "" {
			return fmt.Errorf("%w: related_transaction_id is only accepted for refunds", ledger.ErrInvalid)
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
			x, e := postMoney(ctx, tx, "TRANSFER_OUT", p, -ledger.SignedMoney(p.Amount))
			if e != nil {
				return e
			}
			p.AccountID = b.ID
			p.RelatedTransactionID = x.ID
			y, e := postMoney(ctx, tx, "TRANSFER_IN", p, ledger.SignedMoney(p.Amount))
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE money_transactions SET related_transaction_id=? WHERE id=?`, y.ID, x.ID); e != nil {
				return e
			}
			x.RelatedTransactionID = y.ID
			out.Transactions = []ledger.MoneyTransaction{x, y}
			return nil
		case "EXPENSE", "INCOME", "ADJUSTMENT", "REFUND", "BONUS":
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
			var evidence *ledger.RawFinancialEvent
			if kind == "EXPENSE" || kind == "INCOME" || kind == "REFUND" {
				payload, e := json.Marshal(p)
				if e != nil {
					return e
				}
				capture := "MANUAL"
				if p.Source == "agent" {
					capture = "AGENT"
				} else if p.Source == "system" {
					capture = "SYSTEM"
				} else if p.Source == "import" {
					capture = "CSV_IMPORT"
				}
				raw := ledger.RawFinancialEvent{ID: uuid.NewString(), Status: "NORMALIZED", Decision: "EXPLICIT_MONEY_EVENT", Fingerprint: fmt.Sprintf("%x", sha256.Sum256(payload)), Candidates: []string{}, Confidence: 1, RawFinancialInput: ledger.RawFinancialInput{Provider: "OTHER", CaptureSource: capture, ExternalID: p.ExternalID, Amount: &p.Amount, Direction: kind, OccurredAt: p.OccurredAt, CapturedAt: financialNow(), AccountID: p.AccountID, MerchantRaw: p.Merchant, RawText: p.Note, RawPayload: string(payload)}}
				if e = saveRawTx(ctx, tx, raw); e != nil {
					return e
				}
				evidence = &raw
			}
			t, e := postMoney(ctx, tx, kind, p, delta)
			if e != nil {
				return e
			}
			if evidence != nil {
				if e = linkRawTx(ctx, tx, evidence, t.ID, "PRIMARY"); e != nil {
					return e
				}
			}
			if kind == "ADJUSTMENT" {
				var unresolved ledger.SignedMoney
				e = tx.QueryRowContext(ctx, `SELECT unexplained_cents FROM balance_checkpoints WHERE account_id=? ORDER BY julianday(observed_at) DESC,rowid DESC LIMIT 1`, t.AccountID).Scan(&unresolved)
				if e != nil && !errors.Is(e, sql.ErrNoRows) {
					return e
				}
				_, e = tx.ExecContext(ctx, `INSERT INTO balance_checkpoints VALUES(?,?,?,?,?,?,?)`, uuid.NewString(), t.AccountID, t.BalanceAfter, t.BalanceBefore, t.BalanceDelta, unresolved+t.BalanceDelta, t.OccurredAt)
				if e != nil {
					return e
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
	if p.RelatedTransactionID == "" {
		return ledger.MoneyTransaction{}, fmt.Errorf("%w: refund needs related_transaction_id", ledger.ErrInvalid)
	}
	t, e := scanMoney(tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE id=?`, p.RelatedTransactionID))
	if e != nil {
		return t, e
	}
	if t.Reason == "DEPOSIT_LOSS" || t.Reason == "RECEIVABLE_LOSS" {
		return t, ledger.ErrConflict
	}
	if t.Type != "EXPENSE" || t.VoidedAt != "" {
		return t, ledger.ErrConflict
	}
	return t, nil
}
