package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
)

func agentCola(spec string, quantity float64) ledger.PurchaseInput {
	price := ledger.Money(550)
	return (ledger.AgentPurchaseInput{Items: []ledger.AgentPurchaseLine{{Name: "无糖可乐", Spec: spec, Quantity: quantity, Unit: "瓶", Price: &price}}}).PurchaseInput()
}

// Regression #001: package volume never becomes the stock count.
func TestPurchase2LBottleMustNotBecomeQuantity2Liters(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	p := mustPurchase(t, s, "one-bottle", agentCola("2L", 1))
	i, e := s.Get(ctx, p.Items[0].ItemID)
	if e != nil || i.Name != "无糖可乐" || i.PackageSize == nil || *i.PackageSize != 2 || i.PackageUnit != "L" || i.InventoryUnit != "瓶" || i.Quantity != 1 {
		t.Fatal(i, e)
	}
	if p.ExpenseAmount != 550 || p.Items[0].Quantity != 1 || p.Items[0].UnitPrice != 550 || p.Items[0].QuantityAdded != 1 || p.Items[0].InventoryAfter == nil || *p.Items[0].InventoryAfter != 1 || p.PurchaseID != p.ID || p.ValidationStatus != "VALID" || len(p.Warnings) != 0 {
		t.Fatal(p)
	}
	countTable(t, s, "expenses", 1)
	countTable(t, s, "inventory_events", 1)
}

// Regressions #002 and #003: first purchase creates, repeats reuse atomically.
func TestFirstPurchaseAutoCreatesAndRepeatResolves(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	price := ledger.Money(500)
	tea := ledger.AgentPurchaseInput{Items: []ledger.AgentPurchaseLine{{Name: "东方树叶", Spec: "900ml", Quantity: 1, Unit: "瓶", Price: &price}}}
	p := mustPurchase(t, s, "tea", tea.PurchaseInput())
	if p.Items[0].ItemID == "" || p.Items[0].Spec != "900ml" {
		t.Fatal(p)
	}
	countTable(t, s, "items", 1)
	countTable(t, s, "purchase_events", 1)
	countTable(t, s, "purchase_items", 1)
	countTable(t, s, "expenses", 1)
	countTable(t, s, "inventory_events", 1)
	p = mustPurchase(t, s, "cola", agentCola("2L", 1))
	second := agentCola("2000 ml", 2)
	second.Items[0].Name = "  无糖 可乐  "
	next := mustPurchase(t, s, "cola-repeat", second)
	i, e := s.Get(ctx, p.Items[0].ItemID)
	if e != nil || i.Quantity != 3 || next.Items[0].ItemID != i.ID || *next.Items[0].InventoryAfter != 3 {
		t.Fatal(i, next, e)
	}
	countTable(t, s, "items", 2)
	for _, name := range []string{"无糖可乐2L", " 无糖可乐 2 L ", "无糖可乐２Ｌ", "无糖可乐 2000ml"} {
		v, e := s.ResolveItem(ctx, name)
		if e != nil || v.ID != i.ID {
			t.Fatal(name, v, e)
		}
	}
}

// Regressions #004/#005: the same resolver is used for queries and mutations.
func TestAliasEveryEventAndAmbiguousCandidates(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	p := mustPurchase(t, s, "first", agentCola("2L", 4))
	id := p.Items[0].ItemID
	if _, _, e := s.AddAlias(ctx, "alias", id, "零度可乐大瓶"); e != nil {
		t.Fatal(e)
	}
	input := agentCola("", 1)
	input.Items[0].Name = "零度 可乐 大瓶"
	if v := mustPurchase(t, s, "alias-purchase", input); v.Items[0].ItemID != id || v.Items[0].Spec != "2L" {
		t.Fatal(v)
	}
	v, e := s.ResolveItem(ctx, "零度可乐大瓶")
	if e != nil || v.ID != id {
		t.Fatal(v, e)
	}
	for _, action := range []string{"CONSUME", "ADJUST", "OPEN", "FINISH"} {
		in := ledger.InventoryInput{Name: "零度 可乐大瓶", Source: "agent"}
		qty := 1.0
		if action == "CONSUME" || action == "ADJUST" {
			in.Quantity = &qty
		}
		v, replay, e := s.InventoryAction(ctx, action, "", action, in)
		if e != nil || replay || v.Item.ID != id || v.Event.Source != "agent" {
			t.Fatal(v, e)
		}
		if _, replay, e = s.InventoryAction(ctx, action, "", action, in); e != nil || !replay {
			t.Fatal(e)
		}
	}
	mustPurchase(t, s, "small", agentCola("500ml", 1))
	one := 1.0
	_, _, e = s.InventoryAction(ctx, "ambiguous", "", "CONSUME", ledger.InventoryInput{Name: "无糖可乐", Quantity: &one})
	var resolution *ResolutionError
	if !errors.As(e, &resolution) || resolution.Code != "ITEM_AMBIGUOUS" || len(resolution.Candidates) != 2 {
		t.Fatal(e)
	}
	for _, c := range resolution.Candidates {
		if c.ID == "" || c.Spec == "" || c.Unit != "瓶" {
			t.Fatal(c)
		}
	}
	countTable(t, s, "items", 2)
	for _, action := range []string{"CONSUME", "OPEN", "FINISH", "ADJUST"} {
		_, _, e = s.InventoryAction(ctx, "missing-"+action, "", action, ledger.InventoryInput{Name: "不存在的物品", Quantity: &one})
		if !errors.As(e, &resolution) || resolution.Code != "ITEM_NOT_FOUND" {
			t.Fatal(e)
		}
	}
	countTable(t, s, "items", 2)
}

// Regression #006 also verifies the returned summary survives replay/restart.
func TestDogfoodingRetryAndDryRunDoNotDuplicate(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	input := agentCola("2L", 1)
	preview, e := s.ValidatePurchase(ctx, input)
	if e != nil || preview.ID != "" || preview.Items[0].ItemID != "" || *preview.Items[0].InventoryAfter != 1 {
		t.Fatal(preview, e)
	}
	for _, table := range []string{"items", "purchase_events", "expenses", "purchase_items", "inventory_events", "idempotency_keys"} {
		countTable(t, s, table, 0)
	}
	p := mustPurchase(t, s, "event-stable", input)
	again, replay, e := s.RecordPurchase(ctx, "event-stable", input)
	if e != nil || !replay || p.ID != again.ID || *again.Items[0].InventoryAfter != 1 {
		t.Fatal(again, e)
	}
	for _, table := range []string{"items", "purchase_events", "expenses", "purchase_items", "inventory_events", "idempotency_keys"} {
		countTable(t, s, table, 1)
	}
	input.Items[0].Quantity = 2
	if _, _, e = s.RecordPurchase(ctx, "event-stable", input); !errors.Is(e, ledger.ErrIdempotency) {
		t.Fatal(e)
	}
}

func TestAmountWarningsAndInvalidRollback(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	bad := colaPurchase("L", 2)
	p := mustPurchase(t, s, "suspect", bad)
	codes := map[string]bool{}
	for _, w := range p.Warnings {
		codes[w.Code] = true
	}
	if p.ValidationStatus != "WARNING" || !codes["SPEC_QUANTITY_CONFLICT"] || !codes["PURCHASE_AMOUNT_INCONSISTENT"] {
		t.Fatal(p)
	}
	// A discount and fee reconcile, while an unexplained mismatch warns.
	input := agentCola("500ml", 2)
	input.TotalAmount = 1000
	input.Discount = 200
	input.ExtraFee = 100
	p = mustPurchase(t, s, "discount", input)
	if len(p.Warnings) != 0 {
		t.Fatal(p)
	}
	input.TotalAmount = 990
	p = mustPurchase(t, s, "rounding", input)
	if p.ValidationStatus != "WARNING" {
		t.Fatal(p)
	}
	input.Items[0].Quantity = -1
	if _, _, e := s.RecordPurchase(ctx, "invalid", input); !errors.Is(e, ledger.ErrInvalid) {
		t.Fatal(e)
	}
	countTable(t, s, "purchase_events", 3)
}

func TestStructuredSpecMigrationPreservesQuantitiesAndMoney(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	p := mustPurchase(t, s, "legacy", agentCola("2L", 1))
	if _, e = s.db.Exec(`UPDATE items SET name='无糖可乐2L',spec='',package_size=NULL,package_unit=''; UPDATE purchase_items SET raw_name='无糖可乐 2 L',spec='',package_size=NULL,package_unit=''; DELETE FROM ledger_migrations WHERE name='products-v9'`); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	i, e := s.Get(ctx, p.Items[0].ItemID)
	if e != nil || i.Name != "无糖可乐" || i.Spec != "2L" || i.Quantity != 1 {
		t.Fatal(i, e)
	}
	p, e = s.GetPurchase(ctx, p.ID)
	if e != nil || p.Items[0].Name != "无糖可乐" || p.Items[0].Spec != "2L" || p.TotalAmount != 550 {
		t.Fatal(p, e)
	}
	countTable(t, s, "inventory_events", 1)
}

func TestAgentTotalOnlyAndSpecificationValidation(t *testing.T) {
	total := ledger.Money(550)
	p := (ledger.AgentPurchaseInput{Total: &total, Items: []ledger.AgentPurchaseLine{{Name: "无糖可乐", Spec: "2L", Quantity: 1, Unit: "瓶"}}}).PurchaseInput()
	if p.Items[0].Subtotal != 550 || p.Items[0].UnitPrice != 550 {
		t.Fatal(p)
	}
	p.Items[0].ProductSpec = inventory.ProductSpec{Spec: "500ml"}
	p.Items[0].Name = "无糖可乐2L"
	if _, e := ledger.ValidatePurchase(p); !errors.Is(e, ledger.ErrInvalid) {
		t.Fatal(e)
	}
}

func TestLegacyIdempotencyKeySurvivesAddedSpecFields(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	input := colaPurchase("L", 2)
	p := mustPurchase(t, s, "legacy-key", input)
	// Fingerprint generated by the v0.1 DTO, before product fields existed.
	oldJSON := `{"merchant":"","purchased_at":"","total_amount":5.50,"currency":"","payment_method":"","source":"agent","external_id":"","category":"food","discount":0.00,"extra_fee":0.00,"note":"","items":[{"name":"无糖可乐 2L","quantity":2,"unit":"L","unit_price":5.50,"subtotal":5.50,"item_type":"CONSUMABLE"}]}`
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(oldJSON)))
	if _, e := s.db.Exec(`UPDATE idempotency_keys SET request_hash=? WHERE key='legacy-key'`, fingerprint); e != nil {
		t.Fatal(e)
	}
	again, replay, e := s.RecordPurchase(ctx, "legacy-key", input)
	if e != nil || !replay || again.ID != p.ID {
		t.Fatal(again, replay, e)
	}
	countTable(t, s, "purchase_events", 1)
	countTable(t, s, "expenses", 1)
	countTable(t, s, "inventory_events", 1)
}

func TestDryRunAggregatesRepeatedLinesWithoutWrites(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	input := agentCola("2L", 1)
	input.Items = append(input.Items, input.Items[0])
	input.TotalAmount = 1100
	p, e := s.ValidatePurchase(ctx, input)
	if e != nil || *p.Items[0].InventoryAfter != 2 || *p.Items[1].InventoryAfter != 2 {
		t.Fatal(p, e)
	}
	countTable(t, s, "items", 0)
	created := mustPurchase(t, s, "same-lines", input)
	if *created.Items[0].InventoryAfter != 2 || *created.Items[1].InventoryAfter != 2 || created.Items[0].ItemID != created.Items[1].ItemID {
		t.Fatal(created)
	}
}
