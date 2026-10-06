package store

import (
	"context"
	"stockroom/internal/inventory"
)

// Version 9 extracts legacy specifications once without changing stock or money.
func (s *Store) migrateProducts(ctx context.Context) error {
	for _, table := range []string{"items", "purchase_items"} {
		for _, col := range []struct{ name, definition string }{{"spec", "TEXT NOT NULL DEFAULT ''"}, {"package_size", "REAL CHECK(package_size IS NULL OR package_size>0)"}, {"package_unit", "TEXT NOT NULL DEFAULT ''"}} {
			if e := s.ensureColumn(ctx, table, col.name, "ALTER TABLE "+table+" ADD COLUMN "+col.name+" "+col.definition); e != nil {
				return e
			}
		}
	}
	var done int
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_migrations WHERE name='products-v9'`).Scan(&done); e != nil {
		return e
	}
	if done != 0 {
		return nil
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, table := range []struct{ table, name string }{{"items", "name"}, {"purchase_items", "raw_name"}} {
		rows, e := tx.QueryContext(ctx, "SELECT id,"+table.name+" FROM "+table.table+" WHERE spec=''")
		if e != nil {
			return e
		}
		type legacy struct{ id, name string }
		entries := []legacy{}
		for rows.Next() {
			var l legacy
			if e = rows.Scan(&l.id, &l.name); e != nil {
				rows.Close()
				return e
			}
			entries = append(entries, l)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, l := range entries {
			name, p, e := inventory.NormalizeProduct(l.name, inventory.ProductSpec{})
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "UPDATE "+table.table+" SET "+table.name+"=?,spec=?,package_size=?,package_unit=? WHERE id=?", name, p.Spec, p.PackageSize, p.PackageUnit, l.id); e != nil {
				return e
			}
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO ledger_migrations VALUES('products-v9'); PRAGMA user_version=9;`); e != nil {
		return e
	}
	return tx.Commit()
}
