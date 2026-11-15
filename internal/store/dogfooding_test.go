package store

import (
	"context"
	"errors"
	"testing"

	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
)

func agentCola(spec string, quantity float64) ledger.AcquisitionInput {
	return ledger.AcquisitionInput{Source: "agent", Items: []ledger.AcquisitionLine{{Name: "无糖可乐", ProductSpec: inventory.ProductSpec{Spec: spec}, Quantity: quantity, Unit: "瓶"}}}
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
	if p.Items[0].Quantity != 1 || p.Items[0].QuantityAdded != 1 || p.Items[0].InventoryAfter == nil || *p.Items[0].InventoryAfter != 1 {
		t.Fatal(p)
	}
	countTable(t, s, "money_transactions", 0)
	countTable(t, s, "inventory_events", 1)
}

// Regressions #002 and #003: first purchase creates, repeats reuse atomically.
func TestFirstPurchaseAutoCreatesAndRepeatResolves(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	tea := ledger.AcquisitionInput{Items: []ledger.AcquisitionLine{{Name: "东方树叶", ProductSpec: inventory.ProductSpec{Spec: "900ml"}, Quantity: 1, Unit: "瓶"}}}
	p := mustPurchase(t, s, "tea", tea)
	if p.Items[0].ItemID == "" || p.Items[0].Spec != "900ml" {
		t.Fatal(p)
	}
	countTable(t, s, "items", 1)
	countTable(t, s, "purchase_events", 1)
	countTable(t, s, "purchase_items", 1)
	countTable(t, s, "money_transactions", 0)
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
func TestAcquisitionReplayKeepsItemSummary(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	input := agentCola("2L", 2)
	first := mustPurchase(t, s, "event-stable", input)
	again, replay, e := s.RecordAcquisition(ctx, "event-stable", input)
	if e != nil || !replay || again.ID != first.ID || *again.Items[0].InventoryAfter != 2 {
		t.Fatal(again, replay, e)
	}
	countTable(t, s, "inventory_events", 1)
	input.Items[0].Quantity = 3
	if _, _, e = s.RecordAcquisition(ctx, "event-stable", input); !errors.Is(e, ledger.ErrIdempotency) {
		t.Fatal(e)
	}
}
