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
	"stockroom/internal/ledger"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("item not found")
var ErrNegativeQuantity = errors.New("adjustment would make quantity negative")
var ErrIntakeNotFound = errors.New("intake entry not found")
var ErrAttachmentNotFound = errors.New("intake attachment not found")
var ErrIntakeWorkflowConflict = errors.New("intake workflow state changed")

const itemColumns = `id, name, category, icon, quantity, unit, location,
	low_stock_threshold, expiry_date, unit_price_cents, short_lived, auto_consume_at,
	notes, created_at, updated_at, version, item_type, opened_at, spec, package_size, package_unit`

type repository struct{ db *sql.DB }

// Store assembles the domain repositories and shared persistence.
type Store struct {
	repository
	ItemsRepository
	FinanceRepository
	LinksRepository
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
	uriPath := filepath.ToSlash(absolutePath)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	databaseURL := (&url.URL{Scheme: "file", Path: uriPath}).String()
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
	shared := repository{db: db}
	store := &Store{
		repository: shared, ItemsRepository: ItemsRepository{shared},
		FinanceRepository: FinanceRepository{shared}, LinksRepository: LinksRepository{shared},
		intakeDir: intakeDir,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.initializeSchema(ctx); err != nil {
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

func (s ItemsRepository) Create(ctx context.Context, id string, input inventory.ItemInput) (inventory.Item, error) {
	var err error
	input.Name, input.ProductSpec, err = inventory.NormalizeProduct(input.Name, input.ProductSpec)
	if err != nil {
		return inventory.Item{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO items (
id, name, category, icon, quantity, unit, location, low_stock_threshold, expiry_date, unit_price_cents,
short_lived, auto_consume_at, notes, created_at, updated_at, version,spec,package_size,package_unit
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1,?,?,?)`,
		id, input.Name, input.Category, input.Icon, input.Quantity, input.Unit, input.Location,
		input.LowStockThreshold, input.ExpiryDate, input.UnitPriceCents, input.ShortLived, input.AutoConsumeAt, input.Notes, now, now, input.Spec, input.PackageSize, input.PackageUnit)
	if err != nil {
		return inventory.Item{}, fmt.Errorf("create item: %w", err)
	}
	return s.Get(ctx, id)
}

func (s ItemsRepository) Get(ctx context.Context, id string) (inventory.Item, error) {
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

func (s ItemsRepository) Update(ctx context.Context, id string, input inventory.ItemInput) (inventory.Item, error) {
	var out inventory.Item
	_, err := s.ledgerTx(ctx, "", "item-update:"+id, input, &out, func(tx *sql.Tx) error {
		current, e := getTxItem(ctx, tx, id)
		if e != nil {
			return e
		}
		if current.ItemType == ledger.Durable && (input.Quantity != current.Quantity || input.Location != current.Location || input.ShortLived || input.AutoConsumeAt != nil) {
			return fmt.Errorf("%w: use durable lifecycle/move actions", ledger.ErrConflict)
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		result, err := tx.ExecContext(ctx, `UPDATE items SET
name = ?, category = ?, icon = ?, quantity = ?, unit = ?, location = ?,
low_stock_threshold = ?, expiry_date = ?, unit_price_cents = ?,
short_lived = ?, auto_consume_at = ?, notes = ?, updated_at = ?, version = version + 1,spec=?,package_size=?,package_unit=?
WHERE id = ?`, input.Name, input.Category, input.Icon, input.Quantity, input.Unit, input.Location,
			input.LowStockThreshold, input.ExpiryDate, input.UnitPriceCents, input.ShortLived, input.AutoConsumeAt, input.Notes, now, input.Spec, input.PackageSize, input.PackageUnit, id)
		if err != nil {
			return fmt.Errorf("update item: %w", err)
		}
		if err := requireAffected(result); err != nil {
			return err
		}
		out, e = getTxItem(ctx, tx, id)
		return e
	})
	return out, err
}

func (s ItemsRepository) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id)
	if err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			return fmt.Errorf("%w: preserve purchase/lifecycle history; use finish, discard or durable status", ledger.ErrConflict)
		}
		return fmt.Errorf("delete item: %w", err)
	}
	return requireAffected(result)
}

func (s ItemsRepository) Adjust(ctx context.Context, id string, delta float64) (inventory.Item, error) {
	if _, err := s.ApplyDueAutoConsumption(ctx); err != nil {
		return inventory.Item{}, err
	}
	input := ledger.InventoryInput{Delta: &delta}
	action := "ADJUST"
	if delta < 0 {
		amount := -delta
		input.Delta = nil
		input.Quantity = &amount
		action = "CONSUME"
	}
	out, _, err := s.InventoryAction(ctx, "", id, action, input)
	return out.Item, err
}

func (s ItemsRepository) List(ctx context.Context, filter inventory.ListFilter) (inventory.ListResult, error) {
	if _, err := s.ApplyDueAutoConsumption(ctx); err != nil {
		return inventory.ListResult{}, err
	}
	where := []string{"1 = 1"}
	args := []any{}
	if filter.Search != "" {
		where = append(where, `(name LIKE ? ESCAPE '\' OR notes LIKE ? ESCAPE '\' OR EXISTS (SELECT 1 FROM product_aliases a WHERE a.item_id=items.id AND (a.alias LIKE ? ESCAPE '\' OR a.normalized_alias LIKE ? ESCAPE '\')))`)
		search := "%" + escapeLike(filter.Search) + "%"
		args = append(args, search, search, search, "%"+escapeLike(ledger.NormalizeName(filter.Search))+"%")
		// Listing can show every ambiguous candidate instead of choosing one.
		matched, resolveErr := resolveTarget(ctx, s.db, itemTarget{Name: filter.Search})
		ids := []string{}
		if resolveErr == nil {
			ids = append(ids, matched.ID)
		} else {
			var resolution *ResolutionError
			if errors.As(resolveErr, &resolution) {
				for _, c := range resolution.Candidates {
					ids = append(ids, c.ID)
				}
			} else if !errors.Is(resolveErr, ledger.ErrInvalid) {
				return inventory.ListResult{}, resolveErr
			}
		}
		if len(ids) > 0 {
			marks := make([]string, len(ids))
			for i, id := range ids {
				marks[i] = "?"
				args = append(args, id)
			}
			where[len(where)-1] = "(" + where[len(where)-1] + " OR id IN (" + strings.Join(marks, ",") + "))"
		}
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

func (s ItemsRepository) Summary(ctx context.Context) (inventory.Summary, error) {
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
	var opened sql.NullString
	err := row.Scan(&item.ID, &item.Name, &item.Category, &item.Icon, &item.Quantity, &item.Unit,
		&item.Location, &threshold, &expiry, &unitPrice, &shortLived, &autoConsume,
		&item.Notes, &item.CreatedAt, &item.UpdatedAt, &item.Version, &item.ItemType, &opened, &item.Spec, &item.PackageSize, &item.PackageUnit)
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
	item.InventoryUnit = item.Unit
	item.ShortLived = shortLived == 1
	if opened.Valid {
		item.OpenedAt = &opened.String
	}
	if autoConsume.Valid {
		item.AutoConsumeAt = &autoConsume.String
	}
	return item, nil
}

func (s ItemsRepository) ApplyDueAutoConsumption(ctx context.Context) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE items SET
quantity = 0, auto_consume_at = NULL, updated_at = ?, version = version + 1
WHERE item_type='CONSUMABLE' AND short_lived = 1 AND auto_consume_at IS NOT NULL
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
