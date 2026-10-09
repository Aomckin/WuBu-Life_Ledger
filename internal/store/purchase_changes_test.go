package store

import (
	"context"
	"errors"
	"stockroom/internal/ledger"
	"testing"
)

func colaPurchase(unit string, quantity float64) ledger.PurchaseInput {
	return ledger.PurchaseInput{TotalAmount: 550, Source: "agent", Category: "food", Items: []ledger.PurchaseLine{{Name: "无糖可乐 2L", Quantity: quantity, Unit: unit, UnitPrice: 550, Subtotal: 550, ItemType: ledger.Consumable}}}
}
func amendment(p ledger.Purchase) ledger.PurchaseAmendment {
	in := p.PurchaseInput
	in.Items = append([]ledger.PurchaseLine(nil), p.Items...)
	return ledger.PurchaseAmendment{PurchaseInput: in, ExpectedUpdatedAt: p.UpdatedAt}
}
func TestPurchaseCorrectUnitAndDeleteAtomic(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	old := mustPurchase(t, s, "create", colaPurchase("L", 2))
	change := amendment(old)
	change.Items[0].Name = "无糖可乐2L"
	change.Items[0].Quantity = 1
	change.Items[0].Unit = "瓶"
	updated, replayed, e := s.AmendPurchase(ctx, "edit", old.ID, change)
	if e != nil || replayed {
		t.Fatal(updated, e)
	}
	item, e := s.Get(ctx, old.Items[0].ItemID)
	if e != nil || item.Quantity != 1 || item.Unit != "瓶" || item.Name != "无糖可乐" || item.Spec != "2L" {
		t.Fatal(item, e)
	}
	if updated.ID != old.ID || updated.Items[0].ID != old.Items[0].ID {
		t.Fatal("lost stable identity")
	}
	if _, replayed, e = s.AmendPurchase(ctx, "edit", old.ID, change); e != nil || !replayed {
		t.Fatal(replayed, e)
	}
	countTable(t, s, "purchase_revisions", 1)
	events, e := s.InventoryEvents(ctx, ledger.HistoryFilter{ItemID: item.ID})
	if e != nil || len(events) != 2 || events[0].QuantityDelta != -1 || events[0].PurchaseEventID == nil || *events[0].PurchaseEventID != old.ID {
		t.Fatal(events, e)
	}
	if _, _, e = s.AmendPurchase(ctx, "stale", old.ID, change); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	deletion := ledger.PurchaseDeletion{ExpectedUpdatedAt: updated.UpdatedAt}
	if _, replayed, e = s.DeletePurchase(ctx, "delete", old.ID, deletion); e != nil || replayed {
		t.Fatal(e)
	}
	if _, replayed, e = s.DeletePurchase(ctx, "delete", old.ID, deletion); e != nil || !replayed {
		t.Fatal(e)
	}
	if _, e = s.GetPurchase(ctx, old.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	countTable(t, s, "purchase_events", 1)
	countTable(t, s, "purchase_revisions", 2)
	countTable(t, s, "expenses", 0)
	item, _ = s.Get(ctx, item.ID)
	if item.Quantity != 0 || item.UnitPriceCents != nil {
		t.Fatal(item)
	}
	purchases, e := s.Purchases(ctx, ledger.HistoryFilter{})
	if e != nil || len(purchases) != 0 {
		t.Fatal(purchases, e)
	}
	prices, e := s.PriceHistory(ctx, ledger.HistoryFilter{ItemID: item.ID})
	if e != nil || len(prices) != 0 {
		t.Fatal(prices, e)
	}
	total, e := s.ExpenseTotal(ctx, ledger.HistoryFilter{})
	if e != nil || total != 0 {
		t.Fatal(total, e)
	}
}
func TestPurchaseAmendPreservesConsumptionAndRollsBack(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	p := mustPurchase(t, s, "create", colaPurchase("瓶", 4))
	one := 1.0
	if _, _, e := s.InventoryAction(ctx, "consume", p.Items[0].ItemID, "CONSUME", ledger.InventoryInput{Quantity: &one}); e != nil {
		t.Fatal(e)
	}
	change := amendment(p)
	change.Items[0].Quantity = 3
	change.TotalAmount = 1200
	change.Items[0].UnitPrice = 400
	change.Items[0].Subtotal = 1200
	updated, _, e := s.AmendPurchase(ctx, "edit", p.ID, change)
	if e != nil {
		t.Fatal(e)
	}
	item, _ := s.Get(ctx, p.Items[0].ItemID)
	if item.Quantity != 2 || item.UnitPriceCents == nil || *item.UnitPriceCents != 400 {
		t.Fatal(item)
	}
	total, _ := s.ExpenseTotal(ctx, ledger.HistoryFilter{})
	if total != 1200 {
		t.Fatal(total)
	}
	if _, _, e = s.DeletePurchase(ctx, "bad-delete", p.ID, ledger.PurchaseDeletion{ExpectedUpdatedAt: updated.UpdatedAt}); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	fail := amendment(updated)
	fail.Items[0].Quantity = 0.5
	fail.TotalAmount = 100
	if _, _, e = s.AmendPurchase(ctx, "bad-edit", p.ID, fail); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	fail = amendment(updated)
	fail.Items[0].Unit = "升"
	if _, _, e = s.AmendPurchase(ctx, "bad-unit", p.ID, fail); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	current, _ := s.GetPurchase(ctx, p.ID)
	if current.UpdatedAt != updated.UpdatedAt || current.TotalAmount != 1200 {
		t.Fatal(current)
	}
	item, _ = s.Get(ctx, item.ID)
	if item.Quantity != 2 || item.Unit != "瓶" {
		t.Fatal(item)
	}
	countTable(t, s, "purchase_revisions", 1)
	total, _ = s.ExpenseTotal(ctx, ledger.HistoryFilter{})
	if total != 1200 {
		t.Fatal(total)
	}
	fail = amendment(updated)
	fail.TotalAmount = 1100
	if _, _, e = s.AmendPurchase(ctx, "money", p.ID, fail); e != nil {
		t.Fatal(e)
	}
	countTable(t, s, "inventory_events", 3)
}
func TestPurchaseSharedUnitsAndPriceRestoration(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	first := mustPurchase(t, s, "first", colaPurchase("瓶", 2))
	in := colaPurchase("瓶", 1)
	in.PurchasedAt = "2099-01-01T00:00:00Z"
	in.Items[0].UnitPrice = 700
	second := mustPurchase(t, s, "second", in)
	change := amendment(first)
	change.Items[0].Unit = "升"
	if _, _, e := s.AmendPurchase(ctx, "units", first.ID, change); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	if _, _, e := s.DeletePurchase(ctx, "delete", second.ID, ledger.PurchaseDeletion{ExpectedUpdatedAt: second.UpdatedAt}); e != nil {
		t.Fatal(e)
	}
	item, _ := s.Get(ctx, first.Items[0].ItemID)
	if item.Quantity != 2 || item.UnitPriceCents == nil || *item.UnitPriceCents != 550 {
		t.Fatal(item)
	}
}
func TestPurchaseLineReplacementAndLateFailureRollback(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	p := mustPurchase(t, s, "first", samplePurchase())
	change := amendment(p)
	change.Items = change.Items[:1]
	change.Items = append(change.Items, ledger.PurchaseLine{Name: "苏打水", Quantity: 2, Unit: "罐", UnitPrice: 300, Subtotal: 600, ItemType: ledger.Consumable})
	updated, _, e := s.AmendPurchase(ctx, "edit", p.ID, change)
	if e != nil {
		t.Fatal(e)
	}
	if len(updated.Items) != 2 || updated.Items[1].ItemID == "" {
		t.Fatal(updated)
	}
	durable, _ := s.DurableDetails(ctx, p.Items[2].ItemID)
	if durable.Status != "RETIRED" {
		t.Fatal(durable)
	}
	fail := amendment(updated)
	fail.TotalAmount = 100
	fail.Items[0].Quantity = 1
	fail.Items = append(fail.Items, ledger.PurchaseLine{Name: "苏打水", Quantity: 1, Unit: "升", UnitPrice: 1, Subtotal: 1, ItemType: ledger.Consumable})
	if _, _, e = s.AmendPurchase(ctx, "late-failure", p.ID, fail); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	current, _ := s.GetPurchase(ctx, p.ID)
	if current.TotalAmount != updated.TotalAmount || len(current.Items) != 2 {
		t.Fatal(current)
	}
	item, _ := s.Get(ctx, p.Items[0].ItemID)
	if item.Quantity != 4 {
		t.Fatal(item)
	}
	countTable(t, s, "purchase_revisions", 1)
}
