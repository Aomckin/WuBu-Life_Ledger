package store

import (
	"context"

	"stockroom/internal/ledger"
)

// FinanceRepository is the independent Finance write surface for the SQLite repository.
type FinanceRepository struct{ repository }

func (s *Store) Finance() FinanceRepository { return s.FinanceRepository }

func (f FinanceRepository) Transaction(ctx context.Context, id string) (ledger.MoneyTransaction, error) {
	return scanMoney(f.db.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE id=?`, id))
}
