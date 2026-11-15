package store

import (
	"context"
	_ "embed"
	"fmt"
)

//go:embed schema.sql
var schema string

func (s *Store) initializeSchema(ctx context.Context) error {
	const currentSchema = 16
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version == currentSchema {
		return nil
	}
	if version != 0 {
		return fmt.Errorf("unsupported schema %d; prepare a schema %d database with scripts/migrate-v10.py", version, currentSchema)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&existing); err != nil {
		return err
	}
	if existing != 0 {
		return fmt.Errorf("unversioned database contains tables; use a fresh database")
	}
	if _, err = tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
	INSERT INTO ledger_lock VALUES(1,0);
	INSERT INTO intake_settings VALUES(1,1);
	INSERT INTO money_settings VALUES(1,'');
	INSERT INTO runway_settings VALUES(1,0,30);
	INSERT INTO financial_settings VALUES(1,0.95);
	INSERT INTO accounts (id,name,type,currency,balance_cents,credit_limit_cents,active,note,created_at,updated_at,account_class,merchant)
	VALUES('unknown','未指定账户','OTHER','CNY',0,NULL,1,'未配置默认账户',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'),'STORAGE','');
	INSERT INTO money_categories VALUES('餐饮'),('食品饮料'),('交通'),('娱乐'),('数码'),('生活用品'),('学习'),('住房'),('医疗'),('服务'),('其他');`); err != nil {
		return err
	}
	return tx.Commit()
}
