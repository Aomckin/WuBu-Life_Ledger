package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stockroom/internal/inventory"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("item not found")
var ErrNegativeQuantity = errors.New("adjustment would make quantity negative")
var ErrIntakeNotFound = errors.New("intake entry not found")
var ErrAttachmentNotFound = errors.New("intake attachment not found")
var ErrIntakeWorkflowConflict = errors.New("intake workflow state changed")

const itemColumns = `id, name, category, icon, quantity, unit, location,
	low_stock_threshold, expiry_date, unit_price_cents, short_lived, auto_consume_at,
	notes, created_at, updated_at, version`

type Store struct {
	db        *sql.DB
	intakeDir string
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	databaseURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolutePath)}).String()
	dsn := databaseURL +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)

	intakeDir := filepath.Join(filepath.Dir(absolutePath), "intake-tmp")
	if err := os.MkdirAll(intakeDir, 0o750); err != nil {
		db.Close()
		return nil, fmt.Errorf("create intake directory: %w", err)
	}
	store := &Store{db: db, intakeDir: intakeDir}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS items (
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
);
CREATE INDEX IF NOT EXISTS idx_items_name ON items(name COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_items_category ON items(category);
CREATE INDEX IF NOT EXISTS idx_items_location ON items(location COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_items_expiry ON items(expiry_date) WHERE expiry_date IS NOT NULL;
CREATE TABLE IF NOT EXISTS intake_entries (
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
CREATE TABLE IF NOT EXISTS intake_attachments (
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
CREATE INDEX IF NOT EXISTS idx_intake_status_created ON intake_entries(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_intake_attachments_entry ON intake_attachments(entry_id);
CREATE TABLE IF NOT EXISTS intake_analyses (
    entry_id TEXT PRIMARY KEY REFERENCES intake_entries(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    summary TEXT NOT NULL,
    suggestions_json TEXT NOT NULL,
    needs_review INTEGER NOT NULL DEFAULT 1 CHECK (needs_review IN (0, 1)),
    processed_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS intake_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    auto_analyze INTEGER NOT NULL DEFAULT 1 CHECK (auto_analyze IN (0, 1))
);`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply database schema: %w", err)
	}
	if err := s.ensureColumn(ctx, "items", "unit_price_cents", `ALTER TABLE items ADD COLUMN unit_price_cents INTEGER CHECK (unit_price_cents IS NULL OR (unit_price_cents >= 0 AND unit_price_cents <= 1000000000))`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "intake_attachments", "storage_key", `ALTER TABLE intake_attachments ADD COLUMN storage_key TEXT`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "intake_attachments", "expires_at", `ALTER TABLE intake_attachments ADD COLUMN expires_at TEXT`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "intake_entries", "batch_id", `ALTER TABLE intake_entries ADD COLUMN batch_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "intake_entries", "workflow_state", `ALTER TABLE intake_entries ADD COLUMN workflow_state TEXT NOT NULL DEFAULT 'queued'`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "intake_entries", "failure_reason", `ALTER TABLE intake_entries ADD COLUMN failure_reason TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_intake_batch_workflow_created
ON intake_entries(batch_id, workflow_state, created_at DESC)`); err != nil {
		return fmt.Errorf("create intake batch workflow index: %w", err)
	}
	if err := s.ensureColumn(ctx, "items", "short_lived", `ALTER TABLE items ADD COLUMN short_lived INTEGER NOT NULL DEFAULT 0 CHECK (short_lived IN (0, 1))`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "items", "auto_consume_at", `ALTER TABLE items ADD COLUMN auto_consume_at TEXT`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE intake_attachments
SET expires_at = strftime('%Y-%m-%dT%H:%M:%SZ', created_at, '+24 hours')
WHERE expires_at IS NULL`); err != nil {
		return fmt.Errorf("backfill intake expiry: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE intake_entries
SET workflow_state = 'applied'
WHERE status = 'reviewed' AND workflow_state = 'queued'`); err != nil {
		return fmt.Errorf("backfill reviewed intake workflow: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE intake_entries
SET workflow_state = 'needs_review'
WHERE status = 'pending' AND workflow_state = 'queued'
AND EXISTS (SELECT 1 FROM intake_analyses WHERE intake_analyses.entry_id = intake_entries.id)`); err != nil {
		return fmt.Errorf("backfill analyzed intake workflow: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO intake_settings (id, auto_analyze) VALUES (1, 1)`); err != nil {
		return fmt.Errorf("create intake settings: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA user_version = 6`); err != nil {
		return fmt.Errorf("set database schema version: %w", err)
	}
	return nil
}

func (s *Store) ensureColumn(ctx context.Context, table, column, statement string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return fmt.Errorf("inspect %s schema: %w", table, err)
	}
	found := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan %s schema: %w", table, err)
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s schema rows: %w", table, err)
	}
	if found {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *Store) Create(ctx context.Context, id string, input inventory.ItemInput) (inventory.Item, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO items (
id, name, category, icon, quantity, unit, location, low_stock_threshold, expiry_date, unit_price_cents,
short_lived, auto_consume_at, notes, created_at, updated_at, version
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		id, input.Name, input.Category, input.Icon, input.Quantity, input.Unit, input.Location,
		input.LowStockThreshold, input.ExpiryDate, input.UnitPriceCents, input.ShortLived, input.AutoConsumeAt, input.Notes, now, now)
	if err != nil {
		return inventory.Item{}, fmt.Errorf("create item: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id string) (inventory.Item, error) {
	if _, err := s.ApplyDueAutoConsumption(ctx); err != nil {
		return inventory.Item{}, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items WHERE id = ?`, id)
	item, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return inventory.Item{}, ErrNotFound
	}
	if err != nil {
		return inventory.Item{}, fmt.Errorf("get item: %w", err)
	}
	return item, nil
}

func (s *Store) Update(ctx context.Context, id string, input inventory.ItemInput) (inventory.Item, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE items SET
name = ?, category = ?, icon = ?, quantity = ?, unit = ?, location = ?,
low_stock_threshold = ?, expiry_date = ?, unit_price_cents = ?,
short_lived = ?, auto_consume_at = ?, notes = ?, updated_at = ?, version = version + 1
WHERE id = ?`, input.Name, input.Category, input.Icon, input.Quantity, input.Unit, input.Location,
		input.LowStockThreshold, input.ExpiryDate, input.UnitPriceCents, input.ShortLived, input.AutoConsumeAt, input.Notes, now, id)
	if err != nil {
		return inventory.Item{}, fmt.Errorf("update item: %w", err)
	}
	if err := requireAffected(result); err != nil {
		return inventory.Item{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) Adjust(ctx context.Context, id string, delta float64) (inventory.Item, error) {
	if _, err := s.ApplyDueAutoConsumption(ctx); err != nil {
		return inventory.Item{}, err
	}
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	newSchedule := nowTime.Add(inventory.ShortLivedTTL).Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE items SET
quantity = ROUND(quantity + ?, 3),
auto_consume_at = CASE
  WHEN ROUND(quantity + ?, 3) <= 0 THEN NULL
  WHEN short_lived = 1 AND auto_consume_at IS NULL THEN ?
  ELSE auto_consume_at
END,
updated_at = ?, version = version + 1
WHERE id = ? AND quantity + ? >= 0`, delta, delta, newSchedule, now, id, delta)
	if err != nil {
		return inventory.Item{}, fmt.Errorf("adjust quantity: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return inventory.Item{}, fmt.Errorf("read adjustment result: %w", err)
	}
	if affected == 0 {
		if _, err := s.Get(ctx, id); errors.Is(err, ErrNotFound) {
			return inventory.Item{}, ErrNotFound
		}
		return inventory.Item{}, ErrNegativeQuantity
	}
	return s.Get(ctx, id)
}

func (s *Store) List(ctx context.Context, filter inventory.ListFilter) (inventory.ListResult, error) {
	if _, err := s.ApplyDueAutoConsumption(ctx); err != nil {
		return inventory.ListResult{}, err
	}
	where := []string{"1 = 1"}
	args := []any{}
	if filter.Search != "" {
		where = append(where, `(name LIKE ? ESCAPE '\' OR notes LIKE ? ESCAPE '\')`)
		search := "%" + escapeLike(filter.Search) + "%"
		args = append(args, search, search)
	}
	if filter.Category != "" {
		where = append(where, "category = ?")
		args = append(args, filter.Category)
	}
	if filter.Location != "" {
		where = append(where, "location = ? COLLATE NOCASE")
		args = append(args, filter.Location)
	}
	if filter.ExcludeOut {
		where = append(where, "quantity > 0")
	}

	today := time.Now().In(time.Local).Format(time.DateOnly)
	soon := time.Now().In(time.Local).AddDate(0, 0, 30).Format(time.DateOnly)
	switch filter.Status {
	case "low":
		where = append(where, "low_stock_threshold IS NOT NULL AND quantity <= low_stock_threshold")
	case "out":
		where = append(where, "quantity <= 0")
	case "expired":
		where = append(where, "expiry_date IS NOT NULL AND expiry_date < ?")
		args = append(args, today)
	case "expiring":
		where = append(where, "expiry_date IS NOT NULL AND expiry_date >= ? AND expiry_date <= ?")
		args = append(args, today, soon)
	case "short_lived":
		where = append(where, "short_lived = 1")
	}

	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE `+clause, args...).Scan(&total); err != nil {
		return inventory.ListResult{}, fmt.Errorf("count items: %w", err)
	}

	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColumns+` FROM items WHERE `+clause+`
ORDER BY name COLLATE NOCASE, created_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return inventory.ListResult{}, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()

	items := make([]inventory.Item, 0)
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return inventory.ListResult{}, fmt.Errorf("scan item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return inventory.ListResult{}, fmt.Errorf("iterate items: %w", err)
	}
	return inventory.ListResult{Items: items, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (s *Store) Summary(ctx context.Context) (inventory.Summary, error) {
	if _, err := s.ApplyDueAutoConsumption(ctx); err != nil {
		return inventory.Summary{}, err
	}
	today := time.Now().In(time.Local).Format(time.DateOnly)
	soon := time.Now().In(time.Local).AddDate(0, 0, 30).Format(time.DateOnly)
	var summary inventory.Summary
	err := s.db.QueryRowContext(ctx, `SELECT
COUNT(*),
COALESCE(SUM(CASE WHEN low_stock_threshold IS NOT NULL AND quantity <= low_stock_threshold THEN 1 ELSE 0 END), 0),
COALESCE(SUM(CASE WHEN expiry_date IS NOT NULL AND expiry_date >= ? AND expiry_date <= ? THEN 1 ELSE 0 END), 0),
COALESCE(SUM(CASE WHEN quantity <= 0 THEN 1 ELSE 0 END), 0),
CAST(ROUND(COALESCE(SUM(quantity * unit_price_cents), 0)) AS INTEGER)
FROM items`, today, soon).Scan(&summary.Tracked, &summary.LowStock, &summary.ExpiringSoon, &summary.OutOfStock, &summary.InventoryValueCents)
	if err != nil {
		return inventory.Summary{}, fmt.Errorf("summarize items: %w", err)
	}
	return summary, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanItem(row scanner) (inventory.Item, error) {
	var item inventory.Item
	var threshold sql.NullFloat64
	var expiry sql.NullString
	var unitPrice sql.NullInt64
	var shortLived int
	var autoConsume sql.NullString
	err := row.Scan(&item.ID, &item.Name, &item.Category, &item.Icon, &item.Quantity, &item.Unit,
		&item.Location, &threshold, &expiry, &unitPrice, &shortLived, &autoConsume,
		&item.Notes, &item.CreatedAt, &item.UpdatedAt, &item.Version)
	if err != nil {
		return inventory.Item{}, err
	}
	if threshold.Valid {
		item.LowStockThreshold = &threshold.Float64
	}
	if expiry.Valid {
		item.ExpiryDate = &expiry.String
	}
	if unitPrice.Valid {
		item.UnitPriceCents = &unitPrice.Int64
	}
	item.ShortLived = shortLived == 1
	if autoConsume.Valid {
		item.AutoConsumeAt = &autoConsume.String
	}
	return item, nil
}

func (s *Store) ApplyDueAutoConsumption(ctx context.Context) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE items SET
quantity = 0, auto_consume_at = NULL, updated_at = ?, version = version + 1
WHERE short_lived = 1 AND auto_consume_at IS NOT NULL
AND julianday(auto_consume_at) <= julianday(?)`, now, now)
	if err != nil {
		return 0, fmt.Errorf("apply due auto-consumption: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read auto-consumption result: %w", err)
	}
	return affected, nil
}

func requireAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read write result: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(strings.TrimSpace(value))
}
