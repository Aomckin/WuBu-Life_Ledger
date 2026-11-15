package store

import (
	"context"
	"database/sql"
	"stockroom/internal/ledger"
)

func (s *Store) FinancialSettings(ctx context.Context) (ledger.FinancialSettings, error) {
	var out ledger.FinancialSettings
	e := s.db.QueryRowContext(ctx, `SELECT auto_commit_threshold FROM financial_settings WHERE id=1`).Scan(&out.AutoCommitThreshold)
	return out, e
}
func (s *Store) SetFinancialSettings(ctx context.Context, key string, p ledger.FinancialSettings) (ledger.FinancialSettings, bool, error) {
	out := p
	r, e := s.ledgerTx(ctx, key, "financial_settings", p, &out, func(tx *sql.Tx) error {
		if p.AutoCommitThreshold < 0.95 || p.AutoCommitThreshold > 1 {
			return ledger.ErrInvalid
		}
		_, e := tx.ExecContext(ctx, `UPDATE financial_settings SET auto_commit_threshold=? WHERE id=1`, p.AutoCommitThreshold)
		return e
	})
	return out, r, e
}
