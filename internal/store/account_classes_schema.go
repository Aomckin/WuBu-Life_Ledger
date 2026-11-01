package store

import (
	"context"
	"database/sql/driver"
	"fmt"
)

// Rebuild constrained tables within one transaction, preserving rowids, all
// event bytes, balance caches, links, and the read-only expenses compatibility view.
func (s *Store) migrateAccountClasses(ctx context.Context) (resultErr error) {
	// SQLite table replacement must suspend FK enforcement on this one startup
	// connection; a full foreign_key_check runs before commit, then enforcement
	// is restored before the connection returns to the pool.
	conn, e := s.db.Conn(ctx)
	if e != nil {
		return e
	}
	defer conn.Close()
	if _, e = conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); e != nil {
		return e
	}
	defer func() {
		if _, restoreErr := conn.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`); restoreErr != nil {
			// Never return a connection with suspended enforcement to the pool.
			conn.Raw(func(any) error { return driver.ErrBadConn })
			if resultErr == nil {
				resultErr = fmt.Errorf("restore foreign key enforcement: %w", restoreErr)
			}
		}
	}()
	tx, e := conn.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE ledger_lock SET revision=revision+1 WHERE id=1`); e != nil {
		return e
	}
	var done int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_migrations WHERE name='account-classes-v021'`).Scan(&done); e != nil {
		return e
	}
	if done == 0 {
		if _, e = tx.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON;`+accountClassesSchema); e != nil {
			return fmt.Errorf("account classes migration: %w", e)
		}
	}
	rows, e := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if e != nil {
		return e
	}
	bad := rows.Next()
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return rowErr
	}
	if bad {
		return fmt.Errorf("account classes foreign key check failed")
	}
	if _, e = tx.ExecContext(ctx, `PRAGMA user_version=11`); e != nil {
		return e
	}
	return tx.Commit()
}

const accountClassesSchema = `
CREATE TABLE accounts_v021 (
 id TEXT PRIMARY KEY,name TEXT NOT NULL UNIQUE,type TEXT NOT NULL CHECK(type IN ('WALLET','BANK','CASH','CAMPUS_CARD','CREDIT_CARD','MERCHANT_BALANCE','OTHER')),
 currency TEXT NOT NULL CHECK(currency='CNY'),balance_cents INTEGER NOT NULL CHECK(balance_cents BETWEEN -1000000000000 AND 1000000000000),
 credit_limit_cents INTEGER CHECK(credit_limit_cents IS NULL OR credit_limit_cents BETWEEN 0 AND 1000000000000),active INTEGER NOT NULL CHECK(active IN(0,1)),note TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,account_class TEXT NOT NULL DEFAULT 'STORAGE' CHECK(account_class IN ('STORAGE','CONSUMPTION')),merchant TEXT NOT NULL DEFAULT '');
INSERT INTO accounts_v021(rowid,id,name,type,currency,balance_cents,credit_limit_cents,active,note,created_at,updated_at,account_class,merchant) SELECT rowid,*, 'STORAGE', '' FROM accounts ORDER BY rowid;
DROP VIEW expenses;
CREATE TABLE money_v021 (
 id TEXT PRIMARY KEY,account_id TEXT NOT NULL REFERENCES accounts(id),type TEXT NOT NULL CHECK(type IN('EXPENSE','INCOME','TRANSFER_OUT','TRANSFER_IN','REFUND','ADJUSTMENT','BONUS')),
 amount_cents INTEGER NOT NULL CHECK(amount_cents BETWEEN 0 AND 1000000000000),delta_cents INTEGER NOT NULL,before_cents INTEGER NOT NULL,after_cents INTEGER NOT NULL,
 category TEXT NOT NULL DEFAULT 'other',merchant TEXT NOT NULL DEFAULT '',purchase_event_id TEXT NOT NULL DEFAULT '',maintenance_event_id TEXT NOT NULL DEFAULT '',
 related_transaction_id TEXT NOT NULL DEFAULT '',external_id TEXT NOT NULL DEFAULT '',occurred_at TEXT NOT NULL,source TEXT NOT NULL,note TEXT NOT NULL DEFAULT '',reason TEXT NOT NULL DEFAULT '',
 payment_method TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,voided_at TEXT NOT NULL DEFAULT '',
 CHECK(after_cents=before_cents+delta_cents),CHECK(amount_cents=abs(delta_cents)),
 CHECK((type IN ('EXPENSE','TRANSFER_OUT') AND delta_cents<=0) OR (type IN ('INCOME','TRANSFER_IN','REFUND','BONUS') AND delta_cents>=0) OR type='ADJUSTMENT'));

INSERT INTO money_v021(rowid,id,account_id,type,amount_cents,delta_cents,before_cents,after_cents,category,merchant,purchase_event_id,maintenance_event_id,related_transaction_id,external_id,occurred_at,source,note,reason,payment_method,created_at,voided_at) SELECT rowid,* FROM money_transactions ORDER BY rowid;
DROP TABLE money_transactions;
DROP TABLE accounts;
ALTER TABLE accounts_v021 RENAME TO accounts;
ALTER TABLE money_v021 RENAME TO money_transactions;
CREATE UNIQUE INDEX money_external ON money_transactions(source,account_id,external_id) WHERE external_id<>'';
CREATE UNIQUE INDEX money_purchase ON money_transactions(purchase_event_id) WHERE type='EXPENSE' AND voided_at='' AND purchase_event_id<>'';
CREATE UNIQUE INDEX money_maintenance ON money_transactions(maintenance_event_id) WHERE type='EXPENSE' AND voided_at='' AND maintenance_event_id<>'';
CREATE INDEX money_date ON money_transactions(occurred_at,account_id,type);
CREATE VIEW expenses AS SELECT id,NULLIF(purchase_event_id,'') AS purchase_event_id,NULLIF(maintenance_event_id,'') AS maintenance_event_id,amount_cents,'CNY' AS currency,category,merchant,payment_method,occurred_at,source,note FROM money_transactions WHERE type='EXPENSE' AND voided_at='';
INSERT INTO ledger_migrations VALUES('account-classes-v021');
`
