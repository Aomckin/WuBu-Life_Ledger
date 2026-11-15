"""Prepare and verify a new schema 16 database from a backed-up v0.4 schema 14 database."""
import argparse
from contextlib import closing
import json
import os
from pathlib import Path
import sqlite3
import tempfile
import uuid

ROOT = Path(__file__).resolve().parents[1]
SCHEMA = ROOT / "internal/store/schema.sql"
ARCHIVED_TABLES = {"collector_status", "refund_returns", "ledger_migrations"}
REMOVED_COLUMNS = {
    "money_transactions": {"purchase_event_id", "maintenance_event_id", "payment_method"},
    "maintenance_events": {"expense_id"},
    "purchase_events": {"payment_method"},
}


def connect_readonly(path):
    return sqlite3.connect(path.as_uri() + "?mode=ro", uri=True)


def table_names(db):
    return {name for name, in db.execute(
        "SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'"
    )}


def columns(db, table):
    return [name for _, name, *_ in db.execute(f'PRAGMA table_info("{table}")')]


def validate(db, version):
    if db.execute("PRAGMA user_version").fetchone()[0] != version:
        raise ValueError(f"expected schema {version}")
    if db.execute("PRAGMA integrity_check").fetchall() != [("ok",)]:
        raise ValueError("SQLite integrity check failed")
    if db.execute("PRAGMA foreign_key_check").fetchone():
        raise ValueError("SQLite foreign key check failed")


def copy_facts(source, target):
    old_tables = table_names(source)
    new_tables = table_names(target) - {"migration_archive", "domain_links"}
    if old_tables - ARCHIVED_TABLES != new_tables:
        raise ValueError("unexpected v0.4 tables")
    triggers = target.execute("SELECT name,sql FROM sqlite_schema WHERE type='trigger'").fetchall()
    for name, _ in triggers:
        target.execute(f'DROP TRIGGER "{name}"')
    for table in sorted(old_tables):
        original = columns(source, table)
        retained = set(columns(target, table))
        removed = set(original) - retained
        expected = set(original) if table in ARCHIVED_TABLES else REMOVED_COLUMNS.get(table, set())
        if removed != expected or retained - set(original):
            raise ValueError(f"unexpected fields in {table}")
        source_rows = source.execute(f'SELECT rowid,* FROM "{table}" ORDER BY rowid').fetchall()
        if retained:
            fields = ",".join(f'"{name}"' for name in original if name in retained)
            rows = [tuple([rowid] + [value for name, value in zip(original, values) if name in retained])
                    for rowid, *values in source_rows]
            marks = ",".join("?" for _ in range(len(retained) + 1))
            target.executemany(f'INSERT INTO "{table}" (rowid,{fields}) VALUES ({marks})', rows)
            if target.execute(f'SELECT rowid,{fields} FROM "{table}" ORDER BY rowid').fetchall() != rows:
                raise ValueError(f"copy changed source facts in {table}")
        if removed:
            archive = [(table, rowid, json.dumps(
                {name: value for name, value in zip(original, values)
                 if name in removed or name in {"id", "item_id"}},
                ensure_ascii=False, sort_keys=True,
            )) for rowid, *values in source_rows]
            target.executemany("INSERT INTO migration_archive VALUES(?,?,?)", archive)
    for _, sql in triggers:
        target.execute(sql)
    return len(old_tables)


def migrate_links(source, target):
    # Preserve historical associations, including cancelled acquisitions and voided transactions.
    associations = source.execute("""
        SELECT 'ITEM_ACQUISITION',purchase_event_id,id,
            CASE type WHEN 'REFUND' THEN 'REFUND' WHEN 'EXPENSE' THEN 'PURCHASE' ELSE 'CORRECTION' END,created_at
        FROM money_transactions WHERE purchase_event_id<>''
        UNION ALL
        SELECT 'MAINTENANCE',maintenance_event_id,id,'MAINTENANCE',created_at
        FROM money_transactions WHERE maintenance_event_id<>''
        UNION ALL
        SELECT 'MAINTENANCE',e.id,m.id,'MAINTENANCE',m.created_at
        FROM maintenance_events e JOIN money_transactions m ON m.id=e.expense_id
        UNION ALL
        SELECT 'MAINTENANCE',m.maintenance_event_id,r.id,'REFUND',r.created_at
        FROM money_transactions r JOIN money_transactions m ON m.id=r.related_transaction_id
        WHERE r.type='REFUND' AND m.maintenance_event_id<>''
        UNION ALL
        SELECT 'ITEM',substr(reason,14),id,'SALE',created_at
        FROM money_transactions WHERE type='INCOME' AND reason LIKE 'DURABLE_SALE:%'
        UNION ALL
        SELECT 'ITEM',r.item_id,m.id,'RETURN',m.created_at
        FROM refund_returns r JOIN money_transactions m ON m.id=r.transaction_id
        UNION ALL
        SELECT 'ITEM_ACQUISITION',r.purchase_event_id,m.id,'REFUND',m.created_at
        FROM refund_returns r JOIN money_transactions m ON m.id=r.transaction_id
        """).fetchall()
    origins = {
        "ITEM_ACQUISITION": ("purchase_events", "id"),
        "MAINTENANCE": ("maintenance_events", "id"),
        "ITEM": ("items", "id"),
    }
    seen = set()
    for source_type, source_id, transaction_id, relation, created_at in sorted(associations):
        key = source_type, source_id, transaction_id, relation
        if key in seen:
            continue
        seen.add(key)
        table, column = origins[source_type]
        if (not target.execute(f"SELECT 1 FROM {table} WHERE {column}=?", (source_id,)).fetchone()
                or not target.execute("SELECT 1 FROM money_transactions WHERE id=?", (transaction_id,)).fetchone()):
            raise ValueError(f"unresolved historical {source_type} / {relation} reference")
        link_id = str(uuid.uuid5(uuid.NAMESPACE_URL, json.dumps(key)))
        target.execute("INSERT INTO domain_links VALUES(?,?,?,?,?,?)", (link_id, *key, created_at))
    if target.execute("SELECT COUNT(*) FROM domain_links").fetchone()[0] != len(seen):
        raise ValueError("historical link count differs")
    return len(seen)


def verify_existing(expected, path):
    with closing(connect_readonly(path)) as existing:
        validate(existing, 16)
        objects = "SELECT type,name,tbl_name,sql FROM sqlite_schema ORDER BY 1,2"
        if existing.execute(objects).fetchall() != expected.execute(objects).fetchall():
            raise ValueError("target schema differs; refusing to overwrite")
        for table in table_names(expected):
            sql = f'SELECT rowid,* FROM "{table}" ORDER BY rowid'
            if expected.execute(sql).fetchall() != existing.execute(sql).fetchall():
                raise ValueError(f"target {table} differs; refusing to overwrite")


def copy_attachments(db, source, output):
    for key, in db.execute("SELECT DISTINCT storage_key FROM intake_attachments WHERE storage_key<>''"):
        if Path(key).name != key or key in {".", ".."}:
            raise ValueError("invalid intake storage key")
        src = source.parent / "intake-tmp" / key
        dest = output.parent / "intake-tmp" / key
        data = src.read_bytes()
        if dest.exists():
            if dest.read_bytes() != data:
                raise ValueError("destination attachment differs")
            continue
        dest.parent.mkdir(parents=True, exist_ok=True)
        try:
            with dest.open("xb") as file:
                file.write(data)
        except FileExistsError:
            if dest.read_bytes() != data:
                raise ValueError("destination attachment differs")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("backup", type=Path, help="isolated v0.4 SQLite backup")
    parser.add_argument("output", type=Path, help="new SQLite path in an isolated directory")
    args = parser.parse_args()
    source, output = args.backup.resolve(strict=True), args.output.resolve()
    production = ROOT / "data/wubu.db"
    if source == output or any(
        p == production or (p.exists() and production.exists() and p.samefile(production))
        for p in (source, output)
    ):
        parser.error("use separate paths outside the production database")
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="wubu-migrate-", dir=output.parent) as folder:
        candidate = Path(folder) / "candidate.db"
        with closing(connect_readonly(source)) as old, closing(sqlite3.connect(candidate)) as new:
            old.execute("BEGIN")
            validate(old, 14)
            new.executescript(SCHEMA.read_text(encoding="utf-8"))
            with new:
                tables = copy_facts(old, new)
                links = migrate_links(old, new)
                validate(new, 16)
            if output.exists():
                verify_existing(new, output)
                copy_attachments(old, source, output)
            else:
                copy_attachments(old, source, output)
                try:
                    os.link(candidate, output)
                except FileExistsError:
                    verify_existing(new, output)
    print(f"schema 16 verified: {tables} source tables, {links} links")


if __name__ == "__main__":
    main()
