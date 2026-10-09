package store

import (
	"context"
	"fmt"
)

func (s *Store) migrateMoney(ctx context.Context) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE ledger_lock SET revision=revision+1 WHERE id=1`); e != nil {
		return e
	}
	var done int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_migrations WHERE name='money-v02'`).Scan(&done); e != nil {
		return e
	}
	if done > 0 {
		_, e = tx.ExecContext(ctx, `PRAGMA user_version=10`)
		if e != nil {
			return e
		}
		return tx.Commit()
	}
	if _, e = tx.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, moneySchema); e != nil {
		return fmt.Errorf("money schema: %w", e)
	}
	// Unknown preserves historical payments without guessing a real wallet.
	if _, e = tx.ExecContext(ctx, `INSERT INTO accounts VALUES('unknown','未指定账户','OTHER','CNY',0,NULL,1,'旧记录或未配置默认账户',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'));
 INSERT INTO money_settings VALUES(1,'');`); e != nil {
		return e
	}
	// Fresh databases have no Expense table; upgrades copy every original ID/link.
	var old int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='expenses'`).Scan(&old); e != nil {
		return e
	}
	if old > 0 {
		if _, e = tx.ExecContext(ctx, `INSERT INTO money_transactions(id,account_id,type,amount_cents,delta_cents,before_cents,after_cents,category,merchant,purchase_event_id,maintenance_event_id,occurred_at,source,note,payment_method,created_at)
 SELECT id,'unknown','EXPENSE',amount_cents,-amount_cents,
 -COALESCE(SUM(amount_cents) OVER(ORDER BY julianday(occurred_at),rowid ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING),0),
 -SUM(amount_cents) OVER(ORDER BY julianday(occurred_at),rowid),category,merchant,COALESCE(purchase_event_id,''),COALESCE(maintenance_event_id,''),occurred_at,source,note,payment_method,occurred_at FROM expenses ORDER BY julianday(occurred_at),rowid;
 UPDATE accounts SET balance_cents=(SELECT COALESCE(SUM(delta_cents),0) FROM money_transactions WHERE account_id='unknown') WHERE id='unknown';`); e != nil {
			return e
		}
	}
	// Remove the legacy circular expense FK; maintenance now points to the single ledger.
	if _, e = tx.ExecContext(ctx, `CREATE TABLE maintenance_v02 (
 id TEXT PRIMARY KEY,item_id TEXT NOT NULL REFERENCES items(id) ON DELETE RESTRICT,
 expense_id TEXT UNIQUE REFERENCES money_transactions(id),occurred_at TEXT NOT NULL,title TEXT NOT NULL,description TEXT NOT NULL,
 cost_cents INTEGER NOT NULL CHECK(cost_cents>=0),status TEXT NOT NULL,source TEXT NOT NULL,note TEXT NOT NULL);
 INSERT INTO maintenance_v02 SELECT * FROM maintenance_events;`); e != nil {
		return e
	}
	// Break both sides of the old circular FK only after both records were copied.
	if old > 0 {
		if _, e = tx.ExecContext(ctx, `UPDATE maintenance_events SET expense_id=NULL;DELETE FROM expenses;`); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `DROP TABLE maintenance_events;
 ALTER TABLE maintenance_v02 RENAME TO maintenance_events;
 CREATE INDEX maintenance_item ON maintenance_events(item_id,occurred_at);`); e != nil {
		return e
	}
	if old > 0 {
		if _, e = tx.ExecContext(ctx, `DROP TABLE expenses`); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `CREATE VIEW expenses AS SELECT id,NULLIF(purchase_event_id,'') AS purchase_event_id,NULLIF(maintenance_event_id,'') AS maintenance_event_id,amount_cents,'CNY' AS currency,category,merchant,payment_method,occurred_at,source,note FROM money_transactions WHERE type='EXPENSE' AND voided_at='';
 CREATE TABLE durable_v02 (
 item_id TEXT PRIMARY KEY REFERENCES items(id) ON DELETE RESTRICT,brand TEXT NOT NULL DEFAULT '',model TEXT NOT NULL DEFAULT '',serial_number TEXT NOT NULL DEFAULT '',
 purchase_date TEXT NOT NULL,warranty_until TEXT NOT NULL DEFAULT '',status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK(status IN ('ACTIVE','STORED','LOST','SOLD','RETIRED')),
 sold_at TEXT NOT NULL DEFAULT '',sold_price_cents INTEGER CHECK(sold_price_cents IS NULL OR sold_price_cents>=0),note TEXT NOT NULL DEFAULT '');
 INSERT INTO durable_v02 SELECT item_id,brand,model,serial_number,purchase_date,warranty_until,CASE status WHEN 'DISCARDED' THEN 'RETIRED' WHEN 'ARCHIVED' THEN 'RETIRED' ELSE status END,sold_at,sold_price_cents,note FROM durable_details;
 DROP TABLE durable_details;
 ALTER TABLE durable_v02 RENAME TO durable_details;
 INSERT OR IGNORE INTO durable_details(item_id,purchase_date) SELECT id,created_at FROM items WHERE item_type='DURABLE';
 INSERT INTO durable_events SELECT lower(hex(randomblob(16))),item_id,'PURCHASED',purchase_date,'','ACTIVE',COALESCE((SELECT location FROM items WHERE id=item_id),''),'迁移保留原购买','import' FROM durable_details;
 INSERT INTO durable_events SELECT lower(hex(randomblob(16))),item_id,CASE status WHEN 'RETIRED' THEN 'RETIRED' WHEN 'SOLD' THEN 'SOLD' ELSE status END,CASE WHEN sold_at<>'' THEN sold_at ELSE purchase_date END,'ACTIVE',status,COALESCE((SELECT location FROM items WHERE id=item_id),''),'迁移保留当前生命周期状态','import' FROM durable_details WHERE status<>'ACTIVE';
 INSERT INTO ledger_migrations VALUES('money-v02');
 PRAGMA user_version=10;`); e != nil {
		return e
	}
	rows, e := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if e != nil {
		return e
	}
	bad := rows.Next()
	rows.Close()
	if bad {
		return fmt.Errorf("money migration foreign key check failed")
	}
	return tx.Commit()
}

const moneySchema = `
CREATE TABLE accounts (
 id TEXT PRIMARY KEY,name TEXT NOT NULL UNIQUE,type TEXT NOT NULL CHECK(type IN ('WALLET','BANK','CASH','CAMPUS_CARD','CREDIT_CARD','OTHER')),
 currency TEXT NOT NULL CHECK(currency='CNY'),balance_cents INTEGER NOT NULL CHECK(balance_cents BETWEEN -1000000000000 AND 1000000000000),
 credit_limit_cents INTEGER CHECK(credit_limit_cents IS NULL OR credit_limit_cents BETWEEN 0 AND 1000000000000),active INTEGER NOT NULL CHECK(active IN(0,1)),note TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE money_settings(id INTEGER PRIMARY KEY CHECK(id=1),default_payment_account TEXT NOT NULL);
CREATE TABLE money_transactions (
 id TEXT PRIMARY KEY,account_id TEXT NOT NULL REFERENCES accounts(id),type TEXT NOT NULL CHECK(type IN('EXPENSE','INCOME','TRANSFER_OUT','TRANSFER_IN','REFUND','ADJUSTMENT')),
 amount_cents INTEGER NOT NULL CHECK(amount_cents BETWEEN 0 AND 1000000000000),delta_cents INTEGER NOT NULL,before_cents INTEGER NOT NULL,after_cents INTEGER NOT NULL,
 category TEXT NOT NULL DEFAULT 'other',merchant TEXT NOT NULL DEFAULT '',purchase_event_id TEXT NOT NULL DEFAULT '',maintenance_event_id TEXT NOT NULL DEFAULT '',
 related_transaction_id TEXT NOT NULL DEFAULT '',external_id TEXT NOT NULL DEFAULT '',occurred_at TEXT NOT NULL,source TEXT NOT NULL,note TEXT NOT NULL DEFAULT '',reason TEXT NOT NULL DEFAULT '',
 payment_method TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,voided_at TEXT NOT NULL DEFAULT '',
 CHECK(after_cents=before_cents+delta_cents),CHECK(amount_cents=abs(delta_cents)),
 CHECK((type IN ('EXPENSE','TRANSFER_OUT') AND delta_cents<=0) OR (type IN ('INCOME','TRANSFER_IN','REFUND') AND delta_cents>=0) OR type='ADJUSTMENT'));
CREATE UNIQUE INDEX money_external ON money_transactions(source,account_id,external_id) WHERE external_id<>'';
CREATE UNIQUE INDEX money_purchase ON money_transactions(purchase_event_id) WHERE type='EXPENSE' AND voided_at='' AND purchase_event_id<>'';
CREATE UNIQUE INDEX money_maintenance ON money_transactions(maintenance_event_id) WHERE type='EXPENSE' AND voided_at='' AND maintenance_event_id<>'';
CREATE INDEX money_date ON money_transactions(occurred_at,account_id,type);
CREATE TABLE durable_events(id TEXT PRIMARY KEY,item_id TEXT NOT NULL REFERENCES items(id),type TEXT NOT NULL,occurred_at TEXT NOT NULL,from_status TEXT NOT NULL,to_status TEXT NOT NULL,location TEXT NOT NULL,note TEXT NOT NULL,source TEXT NOT NULL);
CREATE INDEX durable_event_item ON durable_events(item_id,occurred_at);
CREATE TABLE money_categories(name TEXT PRIMARY KEY);
INSERT INTO money_categories VALUES('餐饮'),('食品饮料'),('交通'),('娱乐'),('数码'),('生活用品'),('学习'),('住房'),('医疗'),('服务'),('其他');
CREATE TABLE refund_returns(transaction_id TEXT NOT NULL REFERENCES money_transactions(id),purchase_event_id TEXT NOT NULL,item_id TEXT NOT NULL REFERENCES items(id),quantity REAL NOT NULL CHECK(quantity>0),PRIMARY KEY(transaction_id,item_id));
`
