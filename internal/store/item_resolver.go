package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
)

type ItemCandidate struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Spec            string  `json:"spec"`
	Unit            string  `json:"unit"`
	CurrentQuantity float64 `json:"current_quantity"`
}

type ResolutionError struct {
	Code       string
	Message    string
	Candidates []ItemCandidate
}

func (e *ResolutionError) Error() string { return e.Message }
func (e *ResolutionError) Unwrap() error {
	if e.Code == "ITEM_NOT_FOUND" {
		return ErrNotFound
	}
	return ledger.ErrConflict
}

type itemTarget struct{ ID, Name, Spec string }

func chooseItem(items []inventory.Item, name string) (inventory.Item, error) {
	if len(items) == 1 {
		return items[0], nil
	}
	if len(items) == 0 {
		return inventory.Item{}, &ResolutionError{Code: "ITEM_NOT_FOUND", Message: "未找到物品，请补充名称或规格；购买时可自动创建。"}
	}
	candidates := make([]ItemCandidate, 0, len(items))
	for _, i := range items {
		candidates = append(candidates, ItemCandidate{i.ID, i.Name, i.Spec, i.Unit, i.Quantity})
	}
	return inventory.Item{}, &ResolutionError{Code: "ITEM_AMBIGUOUS", Message: fmt.Sprintf("%s 匹配到多个物品，请指定规格或 item_id。", name), Candidates: candidates}
}

// ItemResolver is shared by reads and every event operation, inside the caller's
// transaction. Only purchase callers may create after ITEM_NOT_FOUND.
func resolveTarget(ctx context.Context, db purchaseReader, target itemTarget) (inventory.Item, error) {
	if target.ID != "" {
		i, e := scanItem(db.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items WHERE id=?`, target.ID))
		if errors.Is(e, sql.ErrNoRows) {
			return chooseItem(nil, target.ID)
		}
		if e == nil && target.Spec != "" && inventory.SpecKey(i.Spec) != inventory.SpecKey(target.Spec) {
			return i, fmt.Errorf("%w: item_id 与规格不一致", ledger.ErrConflict)
		}
		return i, e
	}
	name, p, e := inventory.NormalizeProduct(target.Name, inventory.ProductSpec{Spec: target.Spec})
	if e != nil || name == "" || len([]rune(name)) > inventory.MaxNameLength {
		return inventory.Item{}, fmt.Errorf("%w: 需要有效的商品名称和规格", ledger.ErrInvalid)
	}
	rows, e := db.QueryContext(ctx, `SELECT `+itemColumns+` FROM items ORDER BY name,spec,id`)
	if e != nil {
		return inventory.Item{}, e
	}
	all := []inventory.Item{}
	for rows.Next() {
		i, err := scanItem(rows)
		if err != nil {
			rows.Close()
			return i, err
		}
		all = append(all, i)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return inventory.Item{}, e
	}
	compatible := func(i inventory.Item) bool {
		return p.Spec == "" || inventory.SpecKey(i.Spec) == inventory.SpecKey(p.Spec)
	}
	filter := func(match func(inventory.Item) bool) []inventory.Item {
		out := []inventory.Item{}
		for _, i := range all {
			if compatible(i) && match(i) {
				out = append(out, i)
			}
		}
		return out
	}
	if exact := filter(func(i inventory.Item) bool { return i.Name == name && (p.Spec == "" || i.Spec == p.Spec) }); len(exact) > 0 {
		return chooseItem(exact, name)
	}
	aliases, e := db.QueryContext(ctx, `SELECT item_id,alias FROM product_aliases ORDER BY id`)
	if e != nil {
		return inventory.Item{}, e
	}
	ids := map[string]bool{}
	queryKey := ledger.NormalizeName(name + p.Spec)
	for aliases.Next() {
		var id, alias string
		if e = aliases.Scan(&id, &alias); e != nil {
			aliases.Close()
			return inventory.Item{}, e
		}
		if ledger.NormalizeName(alias) == queryKey || (p.Spec != "" && ledger.NormalizeName(alias) == ledger.NormalizeName(name)) {
			ids[id] = true
		}
	}
	e = aliases.Err()
	aliases.Close()
	if e != nil {
		return inventory.Item{}, e
	}
	if matches := filter(func(i inventory.Item) bool { return ids[i.ID] }); len(matches) > 0 {
		return chooseItem(matches, name)
	}
	if normalized := filter(func(i inventory.Item) bool { return inventory.NameKey(i.Name) == inventory.NameKey(name) }); len(normalized) > 0 {
		return chooseItem(normalized, name)
	}
	key := inventory.NameKey(name)
	if utf8.RuneCountInString(key) >= 2 {
		matches := filter(func(i inventory.Item) bool { return strings.Contains(inventory.NameKey(i.Name), key) })
		if len(matches) > 1 {
			return chooseItem(matches, name)
		}
		if len(matches) == 1 && float64(utf8.RuneCountInString(key))/float64(utf8.RuneCountInString(inventory.NameKey(matches[0].Name))) >= 0.6 {
			return matches[0], nil
		}
		if len(matches) == 1 {
			i := matches[0]
			return inventory.Item{}, &ResolutionError{Code: "ITEM_AMBIGUOUS", Message: "名称不够明确，请确认候选物品或补充完整名称。", Candidates: []ItemCandidate{{i.ID, i.Name, i.Spec, i.Unit, i.Quantity}}}
		}
	}
	return chooseItem(nil, name)
}

func resolveTxItem(ctx context.Context, tx *sql.Tx, name string) (inventory.Item, error) {
	return resolveTarget(ctx, tx, itemTarget{Name: name})
}
