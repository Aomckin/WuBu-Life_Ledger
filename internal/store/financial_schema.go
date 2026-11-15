package store

import "context"

func (s *Store) migrateFinancial(ctx context.Context) error {
	_, e := s.db.ExecContext(ctx, `
 CREATE TABLE IF NOT EXISTS raw_financial_events(id TEXT PRIMARY KEY,payload TEXT NOT NULL,status TEXT NOT NULL,fingerprint TEXT NOT NULL,transaction_id TEXT NOT NULL DEFAULT '',decision TEXT NOT NULL,candidates TEXT NOT NULL,confidence REAL NOT NULL);
 CREATE TABLE IF NOT EXISTS financial_event_links(raw_event_id TEXT PRIMARY KEY REFERENCES raw_financial_events(id),transaction_id TEXT NOT NULL REFERENCES money_transactions(id),relation_type TEXT NOT NULL,confidence REAL NOT NULL,created_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS transaction_annotations(transaction_id TEXT PRIMARY KEY REFERENCES money_transactions(id),payload TEXT NOT NULL,updated_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS balance_checkpoints(id TEXT PRIMARY KEY,account_id TEXT NOT NULL REFERENCES accounts(id),observed_cents INTEGER NOT NULL,expected_cents INTEGER NOT NULL,difference_cents INTEGER NOT NULL,unexplained_cents INTEGER NOT NULL,observed_at TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS checkpoint_account_time ON balance_checkpoints(account_id,observed_at);
 CREATE TABLE IF NOT EXISTS financial_settings(id INTEGER PRIMARY KEY CHECK(id=1),auto_commit_threshold REAL NOT NULL);
 INSERT OR IGNORE INTO financial_settings VALUES(1,0.95);
 CREATE TABLE IF NOT EXISTS semantic_evidence(id TEXT PRIMARY KEY,payload TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS merchant_rules(id TEXT PRIMARY KEY,merchant_raw TEXT UNIQUE NOT NULL,payload TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS financial_reviews(id TEXT PRIMARY KEY,raw_event_id TEXT NOT NULL REFERENCES raw_financial_events(id),payload TEXT NOT NULL,created_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS import_batches(id TEXT PRIMARY KEY,provider_code TEXT NOT NULL,file_hash TEXT NOT NULL,account_binding TEXT NOT NULL,sync_id TEXT NOT NULL,filename TEXT NOT NULL,original_file BLOB NOT NULL,payload TEXT NOT NULL,imported_at TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS import_batch_file ON import_batches(provider_code,file_hash,account_binding);
 CREATE INDEX IF NOT EXISTS import_batch_sync ON import_batches(sync_id,imported_at);
 CREATE TABLE IF NOT EXISTS import_batch_rows(batch_id TEXT NOT NULL REFERENCES import_batches(id),row_number INTEGER NOT NULL,raw_event_id TEXT NOT NULL REFERENCES raw_financial_events(id),PRIMARY KEY(batch_id,row_number));
 CREATE INDEX IF NOT EXISTS raw_provider_external ON raw_financial_events(json_extract(payload,'$.provider'),COALESCE(json_extract(payload,'$.provider_code'),''),json_extract(payload,'$.external_id'));
 CREATE INDEX IF NOT EXISTS raw_occurred_at ON raw_financial_events(json_extract(payload,'$.occurred_at'));
 CREATE INDEX IF NOT EXISTS raw_fingerprint ON raw_financial_events(fingerprint);
 CREATE UNIQUE INDEX IF NOT EXISTS raw_evidence_key ON raw_financial_events(json_extract(payload,'$.evidence_key')) WHERE COALESCE(json_extract(payload,'$.evidence_key'),'')!='';
 CREATE INDEX IF NOT EXISTS financial_link_transaction ON financial_event_links(transaction_id);
 CREATE INDEX IF NOT EXISTS financial_money_time_amount ON money_transactions(occurred_at,amount_cents);
 PRAGMA user_version=14;`)
	return e
}
