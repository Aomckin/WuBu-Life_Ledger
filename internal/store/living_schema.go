package store

import "context"

func (s *Store) migrateLiving(ctx context.Context) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var done int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_migrations WHERE name='living-domain-v03'`).Scan(&done); e != nil {
		return e
	}
	if done > 0 {
		return nil
	}
	_, e = tx.ExecContext(ctx, `
 CREATE TABLE residences(id TEXT PRIMARY KEY,name TEXT NOT NULL,address_text TEXT NOT NULL,move_in_date TEXT NOT NULL,move_out_date TEXT NOT NULL,monthly_rent_cents INTEGER NOT NULL,payment_cycle TEXT NOT NULL,contract_end_date TEXT NOT NULL,network_included INTEGER NOT NULL,management_fee_included INTEGER NOT NULL,status TEXT NOT NULL,note TEXT NOT NULL);
 CREATE TABLE residence_cost_links(transaction_id TEXT PRIMARY KEY REFERENCES money_transactions(id),residence_id TEXT NOT NULL REFERENCES residences(id),role TEXT NOT NULL);
 CREATE INDEX residence_cost_lookup ON residence_cost_links(residence_id);
 CREATE TABLE living_claims(id TEXT PRIMARY KEY,kind TEXT NOT NULL,amount_cents INTEGER NOT NULL,account_id TEXT NOT NULL REFERENCES accounts(id),residence_id TEXT REFERENCES residences(id),counterparty TEXT NOT NULL,reason TEXT NOT NULL,due_at TEXT NOT NULL,occurred_at TEXT NOT NULL,returned_cents INTEGER NOT NULL DEFAULT 0,forfeited_cents INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL,source_transaction_id TEXT NOT NULL REFERENCES money_transactions(id),settlement_transaction_id TEXT NOT NULL DEFAULT '',returned_at TEXT NOT NULL DEFAULT '');
 CREATE INDEX living_claims_status ON living_claims(kind,status,due_at);
 ALTER TABLE durable_details ADD COLUMN has_original_packaging INTEGER;
 ALTER TABLE durable_details ADD COLUMN purchase_proof TEXT NOT NULL DEFAULT '';
 ALTER TABLE durable_details ADD COLUMN estimated_resale_cents INTEGER;
 ALTER TABLE durable_details ADD COLUMN residence_id TEXT REFERENCES residences(id);
 CREATE INDEX durable_residence ON durable_details(residence_id);
 INSERT INTO ledger_migrations VALUES('living-domain-v03');`)
	if e != nil {
		return e
	}
	return tx.Commit()
}
