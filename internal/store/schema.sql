CREATE TABLE items (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL COLLATE NOCASE,
    category TEXT NOT NULL,
    icon TEXT NOT NULL,
    quantity REAL NOT NULL CHECK (quantity >= 0),
    unit TEXT NOT NULL,
    location TEXT NOT NULL DEFAULT '',
    low_stock_threshold REAL CHECK (low_stock_threshold IS NULL OR low_stock_threshold >= 0),
    expiry_date TEXT,
    unit_price_cents INTEGER CHECK (unit_price_cents IS NULL OR (unit_price_cents >= 0 AND unit_price_cents <= 1000000000)),
    short_lived INTEGER NOT NULL DEFAULT 0 CHECK (short_lived IN (0, 1)),
    auto_consume_at TEXT,
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1
, item_type TEXT NOT NULL DEFAULT 'CONSUMABLE' CHECK (item_type IN ('CONSUMABLE','DURABLE')), opened_at TEXT, spec TEXT NOT NULL DEFAULT '', package_size REAL CHECK(package_size IS NULL OR package_size>0), package_unit TEXT NOT NULL DEFAULT '');

CREATE TABLE intake_entries (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL CHECK (source IN ('command', 'receipt', 'purchase_photo', 'voice')),
    text TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'reviewed')),
    batch_id TEXT NOT NULL DEFAULT '',
    workflow_state TEXT NOT NULL DEFAULT 'queued',
    failure_reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE intake_attachments (
    id TEXT PRIMARY KEY,
    entry_id TEXT NOT NULL REFERENCES intake_entries(id) ON DELETE CASCADE,
    file_name TEXT NOT NULL,
    media_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),
    content BLOB NOT NULL,
    storage_key TEXT,
    expires_at TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE intake_analyses (
    entry_id TEXT PRIMARY KEY REFERENCES intake_entries(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    summary TEXT NOT NULL,
    suggestions_json TEXT NOT NULL,
    needs_review INTEGER NOT NULL DEFAULT 1 CHECK (needs_review IN (0, 1)),
    processed_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE intake_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    auto_analyze INTEGER NOT NULL DEFAULT 1 CHECK (auto_analyze IN (0, 1))
);

CREATE TABLE ledger_lock (id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL);

CREATE TABLE purchase_events (
 id TEXT PRIMARY KEY, merchant TEXT NOT NULL, purchased_at TEXT NOT NULL,
 total_cents INTEGER NOT NULL CHECK(total_cents>=0), item_total_cents INTEGER NOT NULL CHECK(item_total_cents>=0),
 discount_cents INTEGER NOT NULL CHECK(discount_cents>=0), extra_fee_cents INTEGER NOT NULL CHECK(extra_fee_cents>=0),
 currency TEXT NOT NULL CHECK(currency='CNY'), source TEXT NOT NULL,
 external_id TEXT NOT NULL DEFAULT '', category TEXT NOT NULL, note TEXT NOT NULL,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
, deleted_at TEXT);

CREATE TABLE purchase_items (
 id TEXT PRIMARY KEY, purchase_event_id TEXT NOT NULL REFERENCES purchase_events(id),
 item_id TEXT REFERENCES items(id) ON DELETE RESTRICT, raw_name TEXT NOT NULL,
 quantity REAL NOT NULL CHECK(quantity>0), unit TEXT NOT NULL, unit_price_cents INTEGER NOT NULL CHECK(unit_price_cents>=0),
 subtotal_cents INTEGER NOT NULL CHECK(subtotal_cents>=0), item_type TEXT NOT NULL CHECK(item_type IN ('CONSUMABLE','DURABLE','UNTRACKED')),
 confidence REAL CHECK(confidence IS NULL OR confidence BETWEEN 0 AND 1), note TEXT NOT NULL
, spec TEXT NOT NULL DEFAULT '', package_size REAL CHECK(package_size IS NULL OR package_size>0), package_unit TEXT NOT NULL DEFAULT '');

CREATE TABLE inventory_events (
 id TEXT PRIMARY KEY, item_id TEXT NOT NULL, type TEXT NOT NULL CHECK(type IN ('PURCHASE','CONSUME','ADJUST','OPEN','FINISH','DISCARD','RETURN')),
 quantity_delta REAL NOT NULL, quantity_before REAL NOT NULL, quantity_after REAL NOT NULL,
 purchase_event_id TEXT REFERENCES purchase_events(id), occurred_at TEXT NOT NULL, source TEXT NOT NULL, note TEXT NOT NULL
);

CREATE TABLE product_aliases (
 id TEXT PRIMARY KEY, item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
 alias TEXT NOT NULL, normalized_alias TEXT NOT NULL UNIQUE
);

CREATE TABLE idempotency_keys (
 key TEXT PRIMARY KEY, operation TEXT NOT NULL, request_hash TEXT NOT NULL, response_json TEXT NOT NULL, created_at TEXT NOT NULL
);

CREATE TABLE purchase_revisions (
 id TEXT PRIMARY KEY, purchase_event_id TEXT NOT NULL REFERENCES purchase_events(id),
 action TEXT NOT NULL CHECK(action IN ('EDIT','DELETE')), before_json TEXT NOT NULL,
 after_json TEXT NOT NULL, occurred_at TEXT NOT NULL
);

CREATE TABLE money_settings(id INTEGER PRIMARY KEY CHECK(id=1),default_payment_account TEXT NOT NULL);

CREATE TABLE durable_events(id TEXT PRIMARY KEY,item_id TEXT NOT NULL REFERENCES items(id),type TEXT NOT NULL,occurred_at TEXT NOT NULL,from_status TEXT NOT NULL,to_status TEXT NOT NULL,location TEXT NOT NULL,note TEXT NOT NULL,source TEXT NOT NULL);

CREATE TABLE money_categories(name TEXT PRIMARY KEY);

CREATE TABLE "maintenance_events" (
 id TEXT PRIMARY KEY,item_id TEXT NOT NULL REFERENCES items(id) ON DELETE RESTRICT,
 occurred_at TEXT NOT NULL,title TEXT NOT NULL,description TEXT NOT NULL,
 cost_cents INTEGER NOT NULL CHECK(cost_cents>=0),status TEXT NOT NULL,source TEXT NOT NULL,note TEXT NOT NULL);

CREATE TABLE "durable_details" (
 item_id TEXT PRIMARY KEY REFERENCES items(id) ON DELETE RESTRICT,brand TEXT NOT NULL DEFAULT '',model TEXT NOT NULL DEFAULT '',serial_number TEXT NOT NULL DEFAULT '',
 purchase_date TEXT NOT NULL,warranty_until TEXT NOT NULL DEFAULT '',status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK(status IN ('ACTIVE','STORED','LOST','SOLD','RETIRED')),
 sold_at TEXT NOT NULL DEFAULT '',sold_price_cents INTEGER CHECK(sold_price_cents IS NULL OR sold_price_cents>=0),note TEXT NOT NULL DEFAULT '', has_original_packaging INTEGER, purchase_proof TEXT NOT NULL DEFAULT '', estimated_resale_cents INTEGER, residence_id TEXT);

CREATE TABLE "accounts" (
 id TEXT PRIMARY KEY,name TEXT NOT NULL UNIQUE,type TEXT NOT NULL CHECK(type IN ('WALLET','BANK','CASH','CAMPUS_CARD','CREDIT_CARD','MERCHANT_BALANCE','OTHER')),
 currency TEXT NOT NULL CHECK(currency='CNY'),balance_cents INTEGER NOT NULL CHECK(balance_cents BETWEEN -1000000000000 AND 1000000000000),
 credit_limit_cents INTEGER CHECK(credit_limit_cents IS NULL OR credit_limit_cents BETWEEN 0 AND 1000000000000),active INTEGER NOT NULL CHECK(active IN(0,1)),note TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,account_class TEXT NOT NULL DEFAULT 'STORAGE' CHECK(account_class IN ('STORAGE','CONSUMPTION')),merchant TEXT NOT NULL DEFAULT '');

CREATE TABLE "money_transactions" (
 id TEXT PRIMARY KEY,account_id TEXT NOT NULL REFERENCES accounts(id),type TEXT NOT NULL CHECK(type IN('EXPENSE','INCOME','TRANSFER_OUT','TRANSFER_IN','REFUND','ADJUSTMENT','BONUS')),
 amount_cents INTEGER NOT NULL CHECK(amount_cents BETWEEN 0 AND 1000000000000),delta_cents INTEGER NOT NULL,before_cents INTEGER NOT NULL,after_cents INTEGER NOT NULL,
 category TEXT NOT NULL DEFAULT 'other',merchant TEXT NOT NULL DEFAULT '',related_transaction_id TEXT NOT NULL DEFAULT '',external_id TEXT NOT NULL DEFAULT '',occurred_at TEXT NOT NULL,source TEXT NOT NULL,note TEXT NOT NULL DEFAULT '',reason TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,voided_at TEXT NOT NULL DEFAULT '', expense_nature TEXT NOT NULL DEFAULT '',
 CHECK(after_cents=before_cents+delta_cents),CHECK(amount_cents=abs(delta_cents)),
 CHECK((type IN ('EXPENSE','TRANSFER_OUT') AND delta_cents<=0) OR (type IN ('INCOME','TRANSFER_IN','REFUND','BONUS') AND delta_cents>=0) OR type='ADJUSTMENT'));

CREATE TABLE runway_settings(id INTEGER PRIMARY KEY CHECK(id=1), safety_buffer_cents INTEGER NOT NULL, horizon_days INTEGER NOT NULL);

CREATE TABLE planned_cashflows(
 id TEXT PRIMARY KEY,name TEXT NOT NULL,direction TEXT NOT NULL CHECK(direction IN ('OUTFLOW','INFLOW')),
 amount_cents INTEGER NOT NULL CHECK(amount_cents>0),due_at TEXT NOT NULL,account_id TEXT NOT NULL,
 category TEXT NOT NULL,expense_nature TEXT NOT NULL,recurrence TEXT NOT NULL,interval_days INTEGER NOT NULL,
 mandatory INTEGER NOT NULL,status TEXT NOT NULL,linked_transaction_id TEXT NOT NULL,
 note TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);

CREATE TABLE residences(id TEXT PRIMARY KEY,name TEXT NOT NULL,address_text TEXT NOT NULL,move_in_date TEXT NOT NULL,move_out_date TEXT NOT NULL,monthly_rent_cents INTEGER NOT NULL,payment_cycle TEXT NOT NULL,contract_end_date TEXT NOT NULL,network_included INTEGER NOT NULL,management_fee_included INTEGER NOT NULL,status TEXT NOT NULL,note TEXT NOT NULL);

CREATE TABLE residence_cost_links(transaction_id TEXT PRIMARY KEY REFERENCES money_transactions(id),residence_id TEXT NOT NULL REFERENCES residences(id),role TEXT NOT NULL);

CREATE TABLE living_claims(id TEXT PRIMARY KEY,kind TEXT NOT NULL,amount_cents INTEGER NOT NULL,account_id TEXT NOT NULL REFERENCES accounts(id),residence_id TEXT REFERENCES residences(id),counterparty TEXT NOT NULL,reason TEXT NOT NULL,due_at TEXT NOT NULL,occurred_at TEXT NOT NULL,returned_cents INTEGER NOT NULL DEFAULT 0,forfeited_cents INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL,source_transaction_id TEXT NOT NULL REFERENCES money_transactions(id),settlement_transaction_id TEXT NOT NULL DEFAULT '',returned_at TEXT NOT NULL DEFAULT '');

CREATE TABLE raw_financial_events(id TEXT PRIMARY KEY,payload TEXT NOT NULL,status TEXT NOT NULL,fingerprint TEXT NOT NULL,transaction_id TEXT NOT NULL DEFAULT '',decision TEXT NOT NULL,candidates TEXT NOT NULL,confidence REAL NOT NULL);

CREATE TABLE financial_event_links(raw_event_id TEXT PRIMARY KEY REFERENCES raw_financial_events(id),transaction_id TEXT NOT NULL REFERENCES money_transactions(id),relation_type TEXT NOT NULL,confidence REAL NOT NULL,created_at TEXT NOT NULL);

CREATE TABLE transaction_annotations(transaction_id TEXT PRIMARY KEY REFERENCES money_transactions(id),payload TEXT NOT NULL,updated_at TEXT NOT NULL);

CREATE TABLE balance_checkpoints(id TEXT PRIMARY KEY,account_id TEXT NOT NULL REFERENCES accounts(id),observed_cents INTEGER NOT NULL,expected_cents INTEGER NOT NULL,difference_cents INTEGER NOT NULL,unexplained_cents INTEGER NOT NULL,observed_at TEXT NOT NULL);

CREATE TABLE financial_settings(id INTEGER PRIMARY KEY CHECK(id=1),auto_commit_threshold REAL NOT NULL);

CREATE TABLE semantic_evidence(id TEXT PRIMARY KEY,payload TEXT NOT NULL);

CREATE TABLE merchant_rules(id TEXT PRIMARY KEY,merchant_raw TEXT UNIQUE NOT NULL,payload TEXT NOT NULL);

CREATE TABLE financial_reviews(id TEXT PRIMARY KEY,raw_event_id TEXT NOT NULL REFERENCES raw_financial_events(id),payload TEXT NOT NULL,created_at TEXT NOT NULL);

CREATE TABLE import_batches(id TEXT PRIMARY KEY,provider_code TEXT NOT NULL,file_hash TEXT NOT NULL,account_binding TEXT NOT NULL,sync_id TEXT NOT NULL,filename TEXT NOT NULL,original_file BLOB NOT NULL,payload TEXT NOT NULL,imported_at TEXT NOT NULL);

CREATE TABLE import_batch_rows(batch_id TEXT NOT NULL REFERENCES import_batches(id),row_number INTEGER NOT NULL,raw_event_id TEXT NOT NULL REFERENCES raw_financial_events(id),PRIMARY KEY(batch_id,row_number));

CREATE INDEX idx_items_name ON items(name COLLATE NOCASE);

CREATE INDEX idx_items_category ON items(category);

CREATE INDEX idx_items_location ON items(location COLLATE NOCASE);

CREATE INDEX idx_items_expiry ON items(expiry_date) WHERE expiry_date IS NOT NULL;

CREATE INDEX idx_intake_status_created ON intake_entries(status, created_at DESC);

CREATE INDEX idx_intake_attachments_entry ON intake_attachments(entry_id);

CREATE INDEX idx_intake_batch_workflow_created
ON intake_entries(batch_id, workflow_state, created_at DESC);

CREATE UNIQUE INDEX purchase_external ON purchase_events(source,external_id) WHERE external_id<>'';

CREATE INDEX purchase_date ON purchase_events(purchased_at);

CREATE INDEX purchase_item_lookup ON purchase_items(item_id,purchase_event_id);

CREATE INDEX inventory_event_item ON inventory_events(item_id,occurred_at);

CREATE INDEX durable_event_item ON durable_events(item_id,occurred_at);

CREATE INDEX maintenance_item ON maintenance_events(item_id,occurred_at);

CREATE UNIQUE INDEX money_external ON money_transactions(source,account_id,external_id) WHERE external_id<>'';

CREATE INDEX money_date ON money_transactions(occurred_at,account_id,type);

CREATE INDEX money_nature_time ON money_transactions(expense_nature,occurred_at);

CREATE INDEX planned_due_status ON planned_cashflows(status,due_at);

CREATE INDEX residence_cost_lookup ON residence_cost_links(residence_id);

CREATE INDEX living_claims_status ON living_claims(kind,status,due_at);

CREATE INDEX durable_residence ON durable_details(residence_id);

CREATE INDEX checkpoint_account_time ON balance_checkpoints(account_id,observed_at);

CREATE INDEX import_batch_file ON import_batches(provider_code,file_hash,account_binding);

CREATE INDEX import_batch_sync ON import_batches(sync_id,imported_at);

CREATE INDEX raw_provider_external ON raw_financial_events(json_extract(payload,'$.provider'),COALESCE(json_extract(payload,'$.provider_code'),''),json_extract(payload,'$.external_id'));

CREATE INDEX raw_occurred_at ON raw_financial_events(json_extract(payload,'$.occurred_at'));

CREATE INDEX raw_fingerprint ON raw_financial_events(fingerprint);

CREATE UNIQUE INDEX raw_evidence_key ON raw_financial_events(json_extract(payload,'$.evidence_key')) WHERE COALESCE(json_extract(payload,'$.evidence_key'),'')!='';

CREATE INDEX financial_link_transaction ON financial_event_links(transaction_id);

CREATE INDEX financial_money_time_amount ON money_transactions(occurred_at,amount_cents);

CREATE TRIGGER wubu_item_create AFTER INSERT ON items WHEN NEW.item_type='CONSUMABLE' AND NEW.quantity>0 BEGIN
 INSERT INTO inventory_events VALUES(lower(hex(randomblob(16))),NEW.id,'ADJUST',NEW.quantity,0,NEW.quantity,NULL,NEW.updated_at,'manual','Initial inventory');
END;

CREATE TRIGGER wubu_item_quantity AFTER UPDATE OF quantity ON items WHEN OLD.quantity<>NEW.quantity BEGIN
 INSERT INTO inventory_events VALUES(lower(hex(randomblob(16))),NEW.id,
 CASE WHEN OLD.auto_consume_at IS NOT NULL AND julianday(OLD.auto_consume_at)<=julianday(NEW.updated_at) AND NEW.quantity=0 THEN 'FINISH' ELSE 'ADJUST' END,
 ROUND(NEW.quantity-OLD.quantity,3),OLD.quantity,NEW.quantity,NULL,NEW.updated_at,
 CASE WHEN OLD.auto_consume_at IS NOT NULL AND julianday(OLD.auto_consume_at)<=julianday(NEW.updated_at) AND NEW.quantity=0 THEN 'system' ELSE 'manual' END,'');
END;

CREATE TRIGGER wubu_item_delete BEFORE DELETE ON items WHEN OLD.quantity>0 BEGIN
 INSERT INTO inventory_events VALUES(lower(hex(randomblob(16))),OLD.id,'DISCARD',-OLD.quantity,OLD.quantity,0,NULL,strftime('%Y-%m-%dT%H:%M:%fZ','now'),'manual','Deleted legacy inventory item');
END;

CREATE TABLE domain_links (
 id TEXT PRIMARY KEY, source_type TEXT NOT NULL, source_id TEXT NOT NULL,
 transaction_id TEXT NOT NULL, relation TEXT NOT NULL, created_at TEXT NOT NULL,
 UNIQUE(source_type,source_id,transaction_id,relation)
);
CREATE INDEX domain_links_transaction ON domain_links(transaction_id);
CREATE TABLE migration_archive (
 source_table TEXT NOT NULL, source_rowid INTEGER NOT NULL, payload TEXT NOT NULL,
 PRIMARY KEY(source_table,source_rowid)
);
-- Read model for item-scoped financial reports. Allocate once per transaction.
CREATE VIEW transaction_items AS
WITH weights AS (
 SELECT d.transaction_id,COALESCE(l.item_id,l.raw_name) AS item_key,'PURCHASE' AS kind,
   CASE WHEN p.item_total_cents>0 THEN CAST(l.subtotal_cents AS REAL)/p.item_total_cents
        ELSE 1.0/COUNT(*) OVER(PARTITION BY d.id) END AS weight
 FROM domain_links d JOIN purchase_events p ON p.id=d.source_id
 JOIN purchase_items l ON l.purchase_event_id=p.id
 WHERE d.source_type='ITEM_ACQUISITION' AND d.relation IN ('PURCHASE','REFUND')
 UNION ALL
 SELECT d.transaction_id,e.item_id,'MAINTENANCE',1.0 FROM domain_links d JOIN maintenance_events e ON e.id=d.source_id
 WHERE d.source_type='MAINTENANCE'
 UNION ALL
 SELECT d.transaction_id,d.source_id,d.relation,1.0 FROM domain_links d
 WHERE d.source_type='ITEM' AND (d.relation='SALE' OR (d.relation='RETURN' AND NOT EXISTS(
     SELECT 1 FROM domain_links acquisition WHERE acquisition.transaction_id=d.transaction_id
     AND acquisition.source_type='ITEM_ACQUISITION' AND acquisition.relation='REFUND')))
), totals AS (
 SELECT transaction_id,item_key,kind,SUM(weight) AS weight FROM weights GROUP BY transaction_id,item_key,kind
)
SELECT transaction_id,item_key,kind,
 COALESCE(weight/NULLIF(SUM(weight) OVER(PARTITION BY transaction_id),0),1.0/COUNT(*) OVER(PARTITION BY transaction_id)) AS share
FROM totals;
PRAGMA user_version = 16;
