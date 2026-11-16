package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"stockroom/internal/ledger"
)

func (s FinanceRepository) FinancialCandidates(ctx context.Context, id string) ([]ledger.MoneyTransaction, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions
		WHERE voided_at='' AND id IN (SELECT j.value FROM raw_financial_events r,json_each(r.candidates) j WHERE r.id=?)
		ORDER BY julianday(occurred_at) DESC,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ledger.MoneyTransaction{}
	for rows.Next() {
		t, err := scanMoney(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

func validFinancialTags(tags []string) bool {
	if len(tags) > 50 {
		return false
	}
	for _, tag := range tags {
		if len([]rune(tag)) > 100 {
			return false
		}
	}
	return true
}

func (s FinanceRepository) EditTransactionDetails(ctx context.Context, key, id string, p ledger.TransactionDetailsPatch) (ledger.AnnotatedTransaction, bool, error) {
	var out ledger.AnnotatedTransaction
	replay, err := s.ledgerTx(ctx, key, "financial_details:"+id, p, &out, func(tx *sql.Tx) error {
		for _, field := range []struct {
			value *string
			max   int
		}{{p.Category, 100}, {p.Merchant, 200}, {p.ItemDescription, 2000}, {p.Note, 2000}} {
			if field.value != nil && len([]rune(*field.value)) > field.max {
				return ledger.ErrInvalid
			}
		}
		if p.Tags != nil && !validFinancialTags(*p.Tags) {
			return ledger.ErrInvalid
		}
		t, err := scanMoney(tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE id=? AND voided_at=''`, id))
		if err != nil {
			return err
		}
		a := ledger.TransactionAnnotation{Tags: []string{}, AnnotationStatus: "UNANNOTATED"}
		var payload string
		err = tx.QueryRowContext(ctx, `SELECT payload FROM transaction_annotations WHERE transaction_id=?`, id).Scan(&payload)
		if err == nil {
			if err = json.Unmarshal([]byte(payload), &a); err != nil {
				return err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if p.Category != nil {
			t.Category = *p.Category
			a.Category = p.Category
		}
		if p.Merchant != nil {
			t.Merchant = *p.Merchant
		}
		if p.ItemDescription != nil {
			a.ItemDescription = p.ItemDescription
		}
		if p.Note != nil {
			t.Note = *p.Note
			a.Note = *p.Note
		}
		if p.Tags != nil {
			a.Tags = *p.Tags
		}
		out = ledger.AnnotatedTransaction{MoneyTransaction: t, Annotation: a}
		if p.Category == nil && p.Merchant == nil && p.ItemDescription == nil && p.Note == nil && p.Tags == nil {
			return nil
		}
		a.Confidence, a.AnnotatedBy, a.AnnotationStatus = 1, "manual", "CONFIRMED"
		out.Annotation = a
		b, err := json.Marshal(a)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE money_transactions SET category=?,merchant=?,note=? WHERE id=?`, t.Category, t.Merchant, t.Note, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO transaction_annotations VALUES(?,?,?) ON CONFLICT(transaction_id) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`, id, string(b), financialNow())
		return err
	})
	return out, replay, err
}
