package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"stockroom/internal/ledger"
)

func fundingVersion(t ledger.MoneyTransaction) ledger.FundingVersion {
	return ledger.FundingVersion{AccountID: t.AccountID, Amount: t.Amount, Direction: t.Type, RelatedTransactionID: t.RelatedTransactionID}
}

func (s FinanceRepository) CorrectFunding(ctx context.Context, key, id string, p ledger.FundingCorrection) (ledger.MoneyTransaction, bool, error) {
	var out ledger.MoneyTransaction
	replay, err := s.ledgerTx(ctx, key, "financial_correction:"+id, p, &out, func(tx *sql.Tx) error {
		old, err := scanMoney(tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE id=? AND voided_at=''`, id))
		if err != nil {
			return err
		}
		before := fundingVersion(old)
		if before != p.Expected {
			return fmt.Errorf("%w: 流水已修改，请刷新后重试", ledger.ErrConflict)
		}
		after := before
		if p.AccountID != nil {
			after.AccountID = *p.AccountID
		}
		if p.Amount != nil {
			after.Amount = *p.Amount
		}
		if p.Direction != nil {
			after.Direction = *p.Direction
		}
		if p.RelatedTransactionID != nil {
			after.RelatedTransactionID = *p.RelatedTransactionID
		}
		if after.Direction != "REFUND" {
			after.RelatedTransactionID = ""
		}
		if before == after {
			out = old
			return nil
		}
		allowed := func(kind string) bool { return kind == "EXPENSE" || kind == "INCOME" || kind == "REFUND" }
		if !allowed(before.Direction) || !allowed(after.Direction) || after.Amount <= 0 || after.Amount > 1e12 || after.AccountID == "" || after.AccountID == "unknown" || len([]rune(p.Note)) > 2000 {
			return ledger.ErrInvalid
		}
		destination, err := accountTx(ctx, tx, after.AccountID)
		if err != nil {
			return err
		}
		if destination.ID != after.AccountID {
			return ledger.ErrInvalid
		}
		if after.Direction == "EXPENSE" && (before.Direction != "EXPENSE" || before.AccountID != after.AccountID) {
			merchant := old.Merchant
			if err = accountMerchant(destination, &merchant); err != nil {
				return err
			}
		}
		var protected bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM living_claims WHERE source_transaction_id=? OR settlement_transaction_id=?)
			OR EXISTS(SELECT 1 FROM planned_cashflows WHERE linked_transaction_id=?)
			OR (? AND (EXISTS(SELECT 1 FROM domain_links WHERE transaction_id=?) OR EXISTS(SELECT 1 FROM residence_cost_links WHERE transaction_id=?)))`,
			id, id, id, before.Direction != after.Direction, id, id).Scan(&protected)
		if err != nil {
			return err
		}
		if protected || old.Reason == "DEPOSIT_LOSS" || old.Reason == "RECEIVABLE_LOSS" {
			return fmt.Errorf("%w: 此流水关联了其他记录，请先处理关联", ledger.ErrConflict)
		}
		var refunded ledger.Money
		const refunds = `SELECT COALESCE(SUM(amount_cents),0) FROM money_transactions WHERE type='REFUND' AND related_transaction_id=? AND id!=? AND voided_at=''`
		if err = tx.QueryRowContext(ctx, refunds, id, id).Scan(&refunded); err != nil {
			return err
		}
		if refunded > 0 && (after.Direction != "EXPENSE" || after.Amount < refunded) {
			return fmt.Errorf("%w: 原支出须覆盖已入账退款", ledger.ErrConflict)
		}
		if after.Direction == "REFUND" {
			if after.RelatedTransactionID == id {
				return ledger.ErrInvalid
			}
			original, err := refundOriginal(ctx, tx, ledger.MoneyInput{RelatedTransactionID: after.RelatedTransactionID})
			if err != nil {
				return err
			}
			if err = tx.QueryRowContext(ctx, refunds, original.ID, id).Scan(&refunded); err != nil {
				return err
			}
			if refunded+after.Amount > original.Amount {
				return fmt.Errorf("%w: 超出原支出可退款金额", ledger.ErrConflict)
			}
		}
		delta := ledger.SignedMoney(after.Amount)
		nature := old.ExpenseNature
		if after.Direction == "EXPENSE" {
			delta = -delta
		} else if after.Direction == "INCOME" {
			nature = ""
		}
		now := financialNow()
		if _, err = tx.ExecContext(ctx, `UPDATE money_transactions SET account_id=?,type=?,amount_cents=?,delta_cents=?,after_cents=before_cents+?,related_transaction_id=?,expense_nature=? WHERE id=?`, after.AccountID, after.Direction, after.Amount, delta, delta, after.RelatedTransactionID, nature, id); err != nil {
			return err
		}
		effects := map[string]ledger.SignedMoney{old.AccountID: -old.BalanceDelta}
		effects[after.AccountID] += delta
		for account, effect := range effects {
			result, err := tx.ExecContext(ctx, `UPDATE accounts SET balance_cents=balance_cents+?,updated_at=? WHERE id=? AND balance_cents+? BETWEEN -1000000000000 AND 1000000000000`, effect, now, account, effect)
			if err != nil {
				return err
			}
			if n, err := result.RowsAffected(); err != nil || n != 1 {
				return fmt.Errorf("%w: 余额超限", ledger.ErrInvalid)
			}
			if err = compensateCheckpoint(ctx, tx, ledger.MoneyTransaction{ID: id, AccountID: account, Type: after.Direction, OccurredAt: old.OccurredAt, BalanceDelta: effect}); err != nil {
				return err
			}
			// Running balances follow posting order. The current account balance also
			// accounts for previously posted checkpoints and any opening balance.
			if _, err = tx.ExecContext(ctx, `WITH running AS MATERIALIZED (
				SELECT m.rowid AS entry, a.balance_cents-COALESCE(SUM(m.delta_cents) OVER (ORDER BY m.rowid ROWS BETWEEN 1 FOLLOWING AND UNBOUNDED FOLLOWING),0) AS balance
				FROM money_transactions m JOIN accounts a ON a.id=m.account_id WHERE m.account_id=?
			) UPDATE money_transactions AS m SET after_cents=r.balance,before_cents=r.balance-m.delta_cents FROM running r WHERE m.rowid=r.entry`, account); err != nil {
				return err
			}
		}
		from, err := json.Marshal(before)
		if err != nil {
			return err
		}
		to, err := json.Marshal(after)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO financial_corrections VALUES(?,?,?,?,?,?)`, uuid.NewString(), id, string(from), string(to), p.Note, now); err != nil {
			return err
		}
		out, err = scanMoney(tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE id=?`, id))
		return err
	})
	return out, replay, err
}
