package store

import "context"

func (s *Store) migrateLedger(ctx context.Context) error {
	if err := s.ensureColumn(ctx, "items", "item_type", `ALTER TABLE items ADD COLUMN item_type TEXT NOT NULL DEFAULT 'CONSUMABLE' CHECK (item_type IN ('CONSUMABLE','DURABLE'))`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "items", "opened_at", `ALTER TABLE items ADD COLUMN opened_at TEXT`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, ledgerSchema)
	return err
}

const ledgerSchema = `
CREATE TABLE IF NOT EXISTS ledger_lock (id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL);
INSERT OR IGNORE INTO ledger_lock VALUES(1,0);
CREATE TABLE IF NOT EXISTS purchase_events (
 id TEXT PRIMARY KEY, merchant TEXT NOT NULL, purchased_at TEXT NOT NULL,
 total_cents INTEGER NOT NULL CHECK(total_cents>=0), item_total_cents INTEGER NOT NULL CHECK(item_total_cents>=0),
 discount_cents INTEGER NOT NULL CHECK(discount_cents>=0), extra_fee_cents INTEGER NOT NULL CHECK(extra_fee_cents>=0),
 currency TEXT NOT NULL CHECK(currency='CNY'), payment_method TEXT NOT NULL, source TEXT NOT NULL,
 external_id TEXT NOT NULL DEFAULT '', category TEXT NOT NULL, note TEXT NOT NULL,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS purchase_external ON purchase_events(source,external_id) WHERE external_id<>'';
CREATE INDEX IF NOT EXISTS purchase_date ON purchase_events(purchased_at);
CREATE TABLE IF NOT EXISTS purchase_items (
 id TEXT PRIMARY KEY, purchase_event_id TEXT NOT NULL REFERENCES purchase_events(id),
 item_id TEXT REFERENCES items(id) ON DELETE RESTRICT, raw_name TEXT NOT NULL,
 quantity REAL NOT NULL CHECK(quantity>0), unit TEXT NOT NULL, unit_price_cents INTEGER NOT NULL CHECK(unit_price_cents>=0),
 subtotal_cents INTEGER NOT NULL CHECK(subtotal_cents>=0), item_type TEXT NOT NULL CHECK(item_type IN ('CONSUMABLE','DURABLE','UNTRACKED')),
 confidence REAL CHECK(confidence IS NULL OR confidence BETWEEN 0 AND 1), note TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS purchase_item_lookup ON purchase_items(item_id,purchase_event_id);
CREATE TABLE IF NOT EXISTS expenses (
 id TEXT PRIMARY KEY, purchase_event_id TEXT UNIQUE REFERENCES purchase_events(id), maintenance_event_id TEXT UNIQUE REFERENCES maintenance_events(id),
 amount_cents INTEGER NOT NULL CHECK(amount_cents>=0), currency TEXT NOT NULL CHECK(currency='CNY'), category TEXT NOT NULL,
 merchant TEXT NOT NULL, payment_method TEXT NOT NULL, occurred_at TEXT NOT NULL, source TEXT NOT NULL, note TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS expense_date ON expenses(occurred_at,category);
-- item_id deliberately retains the historical identity when a legacy-only item is deleted.
CREATE TABLE IF NOT EXISTS inventory_events (
 id TEXT PRIMARY KEY, item_id TEXT NOT NULL, type TEXT NOT NULL CHECK(type IN ('PURCHASE','CONSUME','ADJUST','OPEN','FINISH','DISCARD')),
 quantity_delta REAL NOT NULL, quantity_before REAL NOT NULL, quantity_after REAL NOT NULL,
 purchase_event_id TEXT REFERENCES purchase_events(id), occurred_at TEXT NOT NULL, source TEXT NOT NULL, note TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS inventory_event_item ON inventory_events(item_id,occurred_at);
CREATE TABLE IF NOT EXISTS durable_details (
 item_id TEXT PRIMARY KEY REFERENCES items(id) ON DELETE RESTRICT, brand TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', serial_number TEXT NOT NULL DEFAULT '',
 purchase_date TEXT NOT NULL, warranty_until TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK(status IN ('ACTIVE','SOLD','LOST','DISCARDED','ARCHIVED')),
 sold_at TEXT NOT NULL DEFAULT '', sold_price_cents INTEGER CHECK(sold_price_cents IS NULL OR sold_price_cents>=0), note TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS maintenance_events (
 id TEXT PRIMARY KEY, item_id TEXT NOT NULL REFERENCES items(id) ON DELETE RESTRICT,
 expense_id TEXT UNIQUE REFERENCES expenses(id), occurred_at TEXT NOT NULL, title TEXT NOT NULL, description TEXT NOT NULL,
 cost_cents INTEGER NOT NULL CHECK(cost_cents>=0), status TEXT NOT NULL, source TEXT NOT NULL, note TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS maintenance_item ON maintenance_events(item_id,occurred_at);
CREATE TABLE IF NOT EXISTS product_aliases (
 id TEXT PRIMARY KEY, item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
 alias TEXT NOT NULL, normalized_alias TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS idempotency_keys (
 key TEXT PRIMARY KEY, operation TEXT NOT NULL, request_hash TEXT NOT NULL, response_json TEXT NOT NULL, created_at TEXT NOT NULL
);
-- Baseline is inserted only for pre-existing rows with no history (safe across restarts).
INSERT INTO inventory_events
SELECT lower(hex(randomblob(16))),i.id,'ADJUST',i.quantity,0,i.quantity,NULL,i.created_at,'import','Stockroom migration baseline'
FROM items i WHERE i.item_type='CONSUMABLE' AND i.quantity>0 AND NOT EXISTS(SELECT 1 FROM inventory_events e WHERE e.item_id=i.id);
CREATE TRIGGER IF NOT EXISTS wubu_item_create AFTER INSERT ON items WHEN NEW.item_type='CONSUMABLE' AND NEW.quantity>0 BEGIN
 INSERT INTO inventory_events VALUES(lower(hex(randomblob(16))),NEW.id,'ADJUST',NEW.quantity,0,NEW.quantity,NULL,NEW.updated_at,'manual','Initial inventory');
END;
CREATE TRIGGER IF NOT EXISTS wubu_item_quantity AFTER UPDATE OF quantity ON items WHEN OLD.quantity<>NEW.quantity BEGIN
 INSERT INTO inventory_events VALUES(lower(hex(randomblob(16))),NEW.id,
 CASE WHEN OLD.auto_consume_at IS NOT NULL AND julianday(OLD.auto_consume_at)<=julianday(NEW.updated_at) AND NEW.quantity=0 THEN 'FINISH' ELSE 'ADJUST' END,
 ROUND(NEW.quantity-OLD.quantity,3),OLD.quantity,NEW.quantity,NULL,NEW.updated_at,
 CASE WHEN OLD.auto_consume_at IS NOT NULL AND julianday(OLD.auto_consume_at)<=julianday(NEW.updated_at) AND NEW.quantity=0 THEN 'system' ELSE 'manual' END,'');
END;
CREATE TRIGGER IF NOT EXISTS wubu_item_delete BEFORE DELETE ON items WHEN OLD.quantity>0 BEGIN
 INSERT INTO inventory_events VALUES(lower(hex(randomblob(16))),OLD.id,'DISCARD',-OLD.quantity,OLD.quantity,0,NULL,strftime('%Y-%m-%dT%H:%M:%fZ','now'),'manual','Deleted legacy inventory item');
END;
PRAGMA user_version = 7;
`
