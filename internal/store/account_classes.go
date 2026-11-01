package store

import (
	"context"
	"database/sql"
	"fmt"
	"stockroom/internal/ledger"
	"strings"
)

func validAccountClass(class string) bool {
	return class == ledger.StorageAccount || class == ledger.ConsumptionAccount
}
func accountMerchant(a ledger.Account, merchant *string) error {
	if a.AccountClass != ledger.ConsumptionAccount || a.Merchant == "" {
		return nil
	}
	if strings.TrimSpace(*merchant) == "" {
		*merchant = a.Merchant
		return nil
	}
	if ledger.NormalizeName(*merchant) != ledger.NormalizeName(a.Merchant) {
		return fmt.Errorf("%w: 账户「%s」绑定「%s」，本次商家为「%s」，请核对付款账户或商家。", ledger.ErrAccountMerchantMismatch, a.Name, a.Merchant, *merchant)
	}
	return nil
}
func bindPurchaseAccount(ctx context.Context, tx *sql.Tx, p *ledger.PurchaseInput) error {
	a, e := paymentAccountTx(ctx, tx, p.AccountID, p.PaymentMethod)
	if e != nil {
		return e
	}
	p.AccountID = a.ID
	return accountMerchant(a, &p.Merchant)
}

// A top-up converts liquid funds to consumption value. The principal is paired
// transfer legs; a positive difference is restricted bonus, a loss an adjustment.
func (s *Store) TopUpAccount(ctx context.Context, key string, input ledger.TopUpInput) (ledger.MoneyResult, bool, error) {
	out := ledger.MoneyResult{Transactions: []ledger.MoneyTransaction{}, Warnings: []ledger.Warning{}}
	replay, e := s.ledgerTx(ctx, key, "top_up_account", input, &out, func(tx *sql.Tx) error {
		if input.FromAccount == "" || input.ToAccount == "" || input.PaidAmount <= 0 || input.CreditedAmount <= 0 || input.PaidAmount > 1e12 || input.CreditedAmount > 1e12 {
			return ledger.ErrInvalid
		}
		a, e := accountTx(ctx, tx, input.FromAccount)
		if e != nil {
			return e
		}
		b, e := accountTx(ctx, tx, input.ToAccount)
		if e != nil {
			return e
		}
		if a.ID == b.ID || a.AccountClass != ledger.StorageAccount || b.AccountClass != ledger.ConsumptionAccount {
			return fmt.Errorf("%w: top up requires different STORAGE source and CONSUMPTION destination", ledger.ErrInvalid)
		}
		at, e := ledger.Timestamp(input.OccurredAt)
		if e != nil {
			return e
		}
		p := ledger.MoneyInput{AccountID: a.ID, Amount: input.PaidAmount, OccurredAt: at, Source: input.Source, Note: input.Note, Reason: "TOP_UP", Merchant: b.Merchant}
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
		out.Transactions = append(out.Transactions, x, y)
		difference := input.CreditedAmount - input.PaidAmount
		if difference != 0 {
			kind := "BONUS"
			p.Amount = difference
			p.RelatedTransactionID = y.ID
			if difference < 0 {
				kind = "ADJUSTMENT"
				p.Amount = -difference
				p.Reason = "TOP_UP_VALUE_LOSS"
				out.Warnings = append(out.Warnings, ledger.Warning{Code: "TOP_UP_VALUE_LOSS", Message: "到账少于实付，差额已记录为储值损失调整，不计普通消费。"})
			}
			z, e := postMoney(ctx, tx, kind, p, "", ledger.SignedMoney(difference))
			if e != nil {
				return e
			}
			out.Transactions = append(out.Transactions, z)
		}
		return nil
	})
	return out, replay, e
}
func (s *Store) AccountStatistics(ctx context.Context, id string) (ledger.AccountStatistics, error) {
	// Keep balance and lifetime aggregates in the same read snapshot.
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return ledger.AccountStatistics{}, e
	}
	defer tx.Rollback()
	a, e := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=? OR name=?`, id, id))
	if e != nil {
		return ledger.AccountStatistics{}, e
	}
	out := ledger.AccountStatistics{AccountID: a.ID, CurrentBalance: a.CurrentBalance}
	e = tx.QueryRowContext(ctx, `SELECT
 COALESCE(SUM(CASE WHEN type='TRANSFER_IN' AND reason='TOP_UP' AND account_id=? THEN amount_cents ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type='BONUS' AND account_id=? THEN amount_cents ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type='EXPENSE' AND account_id=? THEN amount_cents ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN type='REFUND' AND EXISTS(SELECT 1 FROM money_transactions original WHERE original.id=m.related_transaction_id AND original.account_id=?) THEN amount_cents ELSE 0 END),0)
 FROM money_transactions m WHERE voided_at=''`, a.ID, a.ID, a.ID, a.ID).Scan(&out.TopUpPrincipal, &out.Bonus, &out.Expense, &out.Refund)
	out.NetExpense = ledger.SignedMoney(out.Expense - out.Refund)
	if out.TopUpPrincipal > 0 {
		out.BonusRatio = float64(out.Bonus) / float64(out.TopUpPrincipal)
	}
	return out, e
}
