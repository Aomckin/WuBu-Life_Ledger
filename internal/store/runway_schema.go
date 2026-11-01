package store

import "context"

// Additive migration: historical expenses remain explicitly unclassified.
func (s *Store) migrateRunway(ctx context.Context) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var done int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_migrations WHERE name='living-runway-v03'`).Scan(&done); e != nil {
		return e
	}
	if done > 0 {
		return nil
	}
	_, e = tx.ExecContext(ctx, `
 ALTER TABLE money_transactions ADD COLUMN expense_nature TEXT NOT NULL DEFAULT '';
 CREATE INDEX money_nature_time ON money_transactions(expense_nature,occurred_at);
 CREATE TABLE runway_settings(id INTEGER PRIMARY KEY CHECK(id=1), safety_buffer_cents INTEGER NOT NULL, horizon_days INTEGER NOT NULL);
 INSERT INTO runway_settings VALUES(1,0,30);
 CREATE TABLE planned_cashflows(
 id TEXT PRIMARY KEY,name TEXT NOT NULL,direction TEXT NOT NULL CHECK(direction IN ('OUTFLOW','INFLOW')),
 amount_cents INTEGER NOT NULL CHECK(amount_cents>0),due_at TEXT NOT NULL,account_id TEXT NOT NULL,
 category TEXT NOT NULL,expense_nature TEXT NOT NULL,recurrence TEXT NOT NULL,interval_days INTEGER NOT NULL,
 mandatory INTEGER NOT NULL,status TEXT NOT NULL,linked_transaction_id TEXT NOT NULL,
 note TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
 CREATE INDEX planned_due_status ON planned_cashflows(status,due_at);
 INSERT INTO ledger_migrations VALUES('living-runway-v03');`)
	if e != nil {
		return e
	}
	return tx.Commit()
}
