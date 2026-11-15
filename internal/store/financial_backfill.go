package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/google/uuid"
	"stockroom/internal/ledger"
	"strings"
)

func (s FinanceRepository) AttachSemanticEvidence(ctx context.Context, key string, p ledger.SemanticEvidence) (ledger.SemanticEvidence, bool, error) {
	out := p
	r, e := s.ledgerTx(ctx, key, "semantic_evidence", p, &out, func(tx *sql.Tx) error {
		switch p.Source {
		case "image", "chat", "manual", "receipt":
		default:
			return ledger.ErrInvalid
		}
		if len(p.Text) > 16000 || len(p.AttachmentRef) > 2000 || p.Text == "" && p.AttachmentRef == "" {
			return ledger.ErrInvalid
		}
		if p.OccurredAt != "" {
			var e error
			out.OccurredAt, e = ledger.Timestamp(p.OccurredAt)
			if e != nil {
				return e
			}
		}
		out.Status = "UNMATCHED"
		if p.MatchedTransactionID != "" {
			if _, e := scanMoney(tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE id=?`, p.MatchedTransactionID)); e != nil {
				return e
			}
			out.Status = "MATCHED"
		}
		if p.ID == "" {
			out.ID = uuid.NewString()
		} else {
			var existing string
			if e := tx.QueryRowContext(ctx, `SELECT id FROM semantic_evidence WHERE id=?`, p.ID).Scan(&existing); e != nil {
				return e
			}
			out.ID = existing
		}
		b, e := json.Marshal(out)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO semantic_evidence VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, out.ID, string(b))
		return e
	})
	return out, r, e
}
func (s FinanceRepository) SaveMerchantRule(ctx context.Context, key, id string, p ledger.MerchantRule) (ledger.MerchantRule, bool, error) {
	out := p
	r, e := s.ledgerTx(ctx, key, "merchant_rule:"+id, p, &out, func(tx *sql.Tx) error {
		if p.Annotation.ExpenseNature != nil && !validNature(*p.Annotation.ExpenseNature) {
			return ledger.ErrInvalid
		}
		if p.Annotation.Category != nil && len(*p.Annotation.Category) > 100 {
			return ledger.ErrInvalid
		}
		if !p.UserConfirmed || strings.TrimSpace(p.MerchantRaw) == "" || len([]rune(p.MerchantRaw)) > 200 || len([]rune(p.MerchantNormalized)) > 200 {
			return ledger.ErrInvalid
		}
		if id == "" {
			out.ID = uuid.NewString()
		} else {
			var found string
			if e := tx.QueryRowContext(ctx, `SELECT id FROM merchant_rules WHERE id=?`, id).Scan(&found); e != nil {
				return e
			}
			out.ID = id
		}
		b, e := json.Marshal(out)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO merchant_rules VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET merchant_raw=excluded.merchant_raw,payload=excluded.payload`, out.ID, p.MerchantRaw, string(b))
		return e
	})
	return out, r, e
}
func (s FinanceRepository) FinancialAuxiliary(ctx context.Context, kind string) ([]json.RawMessage, error) {
	table := map[string]string{"semantic-evidence": "semantic_evidence", "merchant-rules": "merchant_rules"}[kind]
	if table == "" {
		return nil, ledger.ErrInvalid
	}
	rows, e := s.db.QueryContext(ctx, `SELECT payload FROM `+table+` ORDER BY rowid DESC LIMIT 500`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b string
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}
