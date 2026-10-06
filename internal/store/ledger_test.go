package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
)

func ledgerDB(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "物簿.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func samplePurchase() ledger.PurchaseInput {
	return ledger.PurchaseInput{Merchant: "乐尔乐", PurchasedAt: "2026-10-05T20:30:00+08:00", TotalAmount: 22900, Currency: "CNY", Source: "agent", Category: "food", Items: []ledger.PurchaseLine{
		{Name: "无糖可乐 2L", Quantity: 4, Unit: "瓶", UnitPrice: 550, Subtotal: 2200, ItemType: ledger.Consumable},
		{Name: "泡面", Quantity: 2, Unit: "包", UnitPrice: 400, Subtotal: 800, ItemType: ledger.Consumable},
		{Name: "耳机", Quantity: 1, Unit: "个", UnitPrice: 19900, Subtotal: 19900, ItemType: ledger.Durable},
	}}
}
func mustPurchase(t *testing.T, s *Store, key string, p ledger.PurchaseInput) ledger.Purchase {
	t.Helper()
	v, _, e := s.RecordPurchase(context.Background(), key, p)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func countTable(t *testing.T, s *Store, table string, want int) {
	t.Helper()
	var n int
	if e := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); e != nil || n != want {
		t.Fatalf("%s count %d, want %d: %v", table, n, want, e)
	}
}

func TestPurchaseAcceptanceAndLifecycle(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	p := mustPurchase(t, s, "purchase-A", samplePurchase())
	countTable(t, s, "purchase_events", 1)
	countTable(t, s, "purchase_items", 3)
	countTable(t, s, "expenses", 1)
	countTable(t, s, "items", 3)
	countTable(t, s, "inventory_events", 3)
	if p.ItemTotal != 22900 || p.Items[0].ItemID == "" {
		t.Fatalf("purchase %+v", p)
	}
	item, e := s.Get(ctx, p.Items[0].ItemID)
	if e != nil || item.Quantity != 4 {
		t.Fatalf("stock %+v %v", item, e)
	}
	d, e := s.DurableDetails(ctx, p.Items[2].ItemID)
	if e != nil || d.Status != "ACTIVE" {
		t.Fatal(d, e)
	}
	one := 1.0
	consume, replay, e := s.InventoryAction(ctx, "consume-B", item.ID, "CONSUME", ledger.InventoryInput{Quantity: &one, Source: "agent"})
	if e != nil || replay || consume.Item.Quantity != 3 || consume.Event.QuantityDelta != -1 || consume.Event.QuantityBefore != 4 {
		t.Fatal(consume, e)
	}
	if v, replay, e := s.InventoryAction(ctx, "consume-B", item.ID, "CONSUME", ledger.InventoryInput{Quantity: &one, Source: "agent"}); e != nil || !replay || v.Item.Quantity != 3 {
		t.Fatal(v, e)
	}
	five := 5.0
	adjust, _, e := s.InventoryAction(ctx, "adjust-C", item.ID, "ADJUST", ledger.InventoryInput{Quantity: &five})
	if e != nil || adjust.Event.QuantityDelta != 2 || adjust.Item.Quantity != 5 {
		t.Fatal(adjust, e)
	}
	opened, _, e := s.InventoryAction(ctx, "open", item.ID, "OPEN", ledger.InventoryInput{})
	if e != nil || opened.Item.OpenedAt == nil || opened.Event.QuantityDelta != 0 {
		t.Fatal(opened, e)
	}
	finished, _, e := s.InventoryAction(ctx, "finish", item.ID, "FINISH", ledger.InventoryInput{})
	if e != nil || finished.Item.Quantity != 0 || finished.Event.QuantityDelta != -5 {
		t.Fatal(finished, e)
	}
	if _, _, e = s.InventoryAction(ctx, "bad-consume", item.ID, "CONSUME", ledger.InventoryInput{Quantity: &one}); !errors.Is(e, ErrNegativeQuantity) {
		t.Fatal(e)
	}
	m, _, e := s.AddMaintenance(ctx, "maintenance-D", p.Items[2].ItemID, ledger.MaintenanceInput{Title: "换后胎", Cost: 6500, OccurredAt: "2026-10-05T21:00:00+08:00"})
	if e != nil || m.ExpenseID == nil {
		t.Fatal(m, e)
	}
	if _, replay, e = s.AddMaintenance(ctx, "maintenance-D", p.Items[2].ItemID, ledger.MaintenanceInput{Title: "换后胎", Cost: 6500, OccurredAt: "2026-10-05T21:00:00+08:00"}); e != nil || !replay {
		t.Fatal(e)
	}
	countTable(t, s, "maintenance_events", 1)
	countTable(t, s, "expenses", 2)
	summary, e := s.Dashboard(ctx, "2026-10")
	if e != nil || summary.MonthExpense != 29400 {
		t.Fatal(summary, e)
	}
}
func TestPurchaseIdempotencyConflictAndUntracked(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	input := samplePurchase()
	p := mustPurchase(t, s, "same", input)
	again, replay, e := s.RecordPurchase(ctx, "same", input)
	if e != nil || !replay || again.ID != p.ID {
		t.Fatal(again, e)
	}
	input.TotalAmount = 22800
	if _, _, e = s.RecordPurchase(ctx, "same", input); !errors.Is(e, ledger.ErrIdempotency) {
		t.Fatal(e)
	}
	input = ledger.PurchaseInput{TotalAmount: 3000, Items: []ledger.PurchaseLine{{Name: "理发", Quantity: 1, Unit: "次", UnitPrice: 3500, Subtotal: 3500, ItemType: ledger.Untracked}}}
	// Discounts do not require subtotal sum == actual payment.
	p = mustPurchase(t, s, "haircut", input)
	if p.Items[0].ItemID != "" || p.ItemTotal != 3500 {
		t.Fatal(p)
	}
	countTable(t, s, "items", 3)
	countTable(t, s, "inventory_events", 3)
	countTable(t, s, "expenses", 2)
	input.ExternalID = "bill-001"
	mustPurchase(t, s, "ext", input)
	if _, _, e = s.RecordPurchase(ctx, "new-key", input); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
}
func TestPurchaseRollbackAfterExpenseAndStockFailure(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	if _, e := s.db.Exec(`CREATE TRIGGER force_inventory_failure BEFORE INSERT ON inventory_events BEGIN SELECT RAISE(ABORT,'test inventory failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.RecordPurchase(ctx, "retryable", samplePurchase()); e == nil {
		t.Fatal("expected injected failure")
	}
	for _, table := range []string{"purchase_events", "purchase_items", "expenses", "items", "inventory_events", "durable_details", "idempotency_keys"} {
		countTable(t, s, table, 0)
	}
	s.db.Exec(`DROP TRIGGER force_inventory_failure`)
	mustPurchase(t, s, "retryable", samplePurchase())
	// A failure on the last line rolls back earlier successful quantity increments.
	input := samplePurchase()
	input.Items[2].ItemID = "missing"
	if _, _, e := s.RecordPurchase(ctx, "last-line", input); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	countTable(t, s, "purchase_events", 1)
	item, _ := s.ResolveItem(ctx, "无糖可乐 2L")
	if item.Quantity != 4 {
		t.Fatal(item)
	}
}
func TestAliasPriceHistoryAndDurableInstances(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	p := mustPurchase(t, s, "1", samplePurchase())
	id := p.Items[0].ItemID
	if _, _, e := s.AddAlias(ctx, "alias", id, "零度可乐大瓶"); e != nil {
		t.Fatal(e)
	}
	v, e := s.ResolveItem(ctx, "零度 可乐 大瓶")
	if e != nil || v.ID != id {
		t.Fatal(v, e)
	}
	list, e := s.List(ctx, inventory.ListFilter{Search: "零度", Limit: 100})
	if e != nil || list.Total != 1 {
		t.Fatal(list, e)
	}
	input := samplePurchase()
	input.Items = input.Items[:1]
	input.Items[0].Name = "零度可乐大瓶"
	input.Items[0].Quantity = 1
	input.Items[0].UnitPrice = 600
	input.Items[0].Subtotal = 600
	input.TotalAmount = 600
	input.PurchasedAt = "2026-10-06T10:00:00+08:00"
	second := mustPurchase(t, s, "2", input)
	if second.Items[0].ItemID != id {
		t.Fatal(second)
	}
	prices, e := s.PriceHistory(ctx, ledger.HistoryFilter{ItemID: id})
	if e != nil || len(prices) != 2 || prices[0].UnitPrice != 600 || prices[1].UnitPrice != 550 {
		t.Fatal(prices, e)
	}
	if _, _, e = s.AddAlias(ctx, "wrong-alias", p.Items[1].ItemID, "零度可乐大瓶"); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	input = samplePurchase()
	input.Items = input.Items[2:]
	input.TotalAmount = 19900
	newDurable := mustPurchase(t, s, "3", input)
	if newDurable.Items[0].ItemID == p.Items[2].ItemID {
		t.Fatal("durable instances merged")
	}
	if _, e = s.ResolveItem(ctx, "耳机"); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal("ambiguous durable", e)
	}
	if e = s.Delete(ctx, id); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal("historical item deletion", e)
	}
	status := "SOLD"
	soldAt := "2026-10-07T10:00:00+08:00"
	price := ledger.Money(9900)
	detail, _, e := s.UpdateDurable(ctx, "sold", newDurable.Items[0].ItemID, ledger.DurablePatch{Status: &status, SoldAt: &soldAt, SoldPrice: &price})
	if e != nil || detail.Status != "SOLD" {
		t.Fatal(detail, e)
	}
}
func TestConcurrentIdempotentPurchaseAndConsume(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	ids := make(chan string, 16)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, _, e := s.RecordPurchase(ctx, "concurrent", samplePurchase())
			if e != nil {
				errs <- e
			} else {
				ids <- p.ID
			}
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for e := range errs {
		t.Error(e)
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Error("different results")
		}
	}
	countTable(t, s, "purchase_events", 1)
	item, _ := s.ResolveItem(ctx, "无糖可乐 2L")
	errs = make(chan error, 8)
	one := 1.0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, e := s.InventoryAction(ctx, "", item.ID, "CONSUME", ledger.InventoryInput{Quantity: &one})
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	success, negative := 0, 0
	for e := range errs {
		if e == nil {
			success++
		} else if errors.Is(e, ErrNegativeQuantity) {
			negative++
		} else {
			t.Error(e)
		}
	}
	if success != 4 || negative != 4 {
		t.Fatal(success, negative)
	}
	item, _ = s.Get(ctx, item.ID)
	if item.Quantity != 0 {
		t.Fatal(item)
	}
}
func TestLegacyWritesAndAutomaticConsumptionHaveHistory(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	input, _ := inventory.NormalizeAndValidate(inventory.ItemInput{Name: "牛奶", Quantity: 2, ShortLived: true})
	v, e := s.Create(ctx, "milk", input)
	if e != nil {
		t.Fatal(e)
	}
	v, e = s.Adjust(ctx, v.ID, -1)
	if e != nil {
		t.Fatal(e)
	}
	events, _ := s.InventoryEvents(ctx, ledger.HistoryFilter{ItemID: v.ID})
	if len(events) != 2 || events[0].Type != "CONSUME" {
		t.Fatal(events)
	}
	input.Quantity = 3
	if _, e = s.Update(ctx, v.ID, input); e != nil {
		t.Fatal(e)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if _, e = s.db.Exec(`UPDATE items SET auto_consume_at=? WHERE id=?`, past, v.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyDueAutoConsumption(ctx); e != nil {
		t.Fatal(e)
	}
	events, _ = s.InventoryEvents(ctx, ledger.HistoryFilter{ItemID: v.ID})
	if len(events) != 4 || events[0].Type != "FINISH" || events[0].QuantityDelta != -3 {
		t.Fatal(events)
	}
	// Re-running migration cannot duplicate baseline/events.
	if e = s.migrate(ctx); e != nil {
		t.Fatal(e)
	}
	countTable(t, s, "inventory_events", 4)
}
func TestInvalidPurchaseAndMoney(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	for _, change := range []func(*ledger.PurchaseInput){func(p *ledger.PurchaseInput) { p.Items = nil }, func(p *ledger.PurchaseInput) { p.Items[0].Quantity = -1 }, func(p *ledger.PurchaseInput) { p.Items[0].ItemType = "BOGUS" }, func(p *ledger.PurchaseInput) { p.PurchasedAt = "yesterday" }, func(p *ledger.PurchaseInput) { p.Currency = "USD" }, func(p *ledger.PurchaseInput) { p.Items[2].Quantity = 2 }} {
		p := samplePurchase()
		change(&p)
		if _, _, e := s.RecordPurchase(ctx, "", p); !errors.Is(e, ledger.ErrInvalid) {
			t.Fatal(e)
		}
	}
	countTable(t, s, "purchase_events", 0)
	for _, raw := range []string{"-1", "0.001", "null", "\"5\""} {
		var m ledger.Money
		if json.Unmarshal([]byte(raw), &m) == nil {
			t.Fatal("accepted invalid money", raw)
		}
	}
	var m ledger.Money
	if e := json.Unmarshal([]byte("0.29"), &m); e != nil || m != 29 {
		t.Fatal(m, e)
	}
}

func TestIdempotencyPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	input := samplePurchase()
	first := mustPurchase(t, s, "persistent", input)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	second, replay, err := s.RecordPurchase(context.Background(), "persistent", input)
	if err != nil || !replay || first.ID != second.ID {
		t.Fatal(second, replay, err)
	}
	countTable(t, s, "purchase_events", 1)
	countTable(t, s, "inventory_events", 3)
}

func TestMaintenanceRollbackAndDurableRestore(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	p := mustPurchase(t, s, "p", samplePurchase())
	id := p.Items[2].ItemID
	if _, err := s.db.Exec(`CREATE TRIGGER expense_failure BEFORE INSERT ON expenses WHEN NEW.maintenance_event_id IS NOT NULL BEGIN SELECT RAISE(ABORT,'maintenance expense failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddMaintenance(ctx, "repair", id, ledger.MaintenanceInput{Title: "维修", Cost: 6500}); err == nil {
		t.Fatal("expected failure")
	}
	countTable(t, s, "maintenance_events", 0)
	countTable(t, s, "expenses", 1)
	s.db.Exec(`DROP TRIGGER expense_failure`)
	if _, _, err := s.AddMaintenance(ctx, "repair", id, ledger.MaintenanceInput{Title: "维修", Cost: 6500}); err != nil {
		t.Fatal(err)
	}
	status := "ARCHIVED"
	if _, _, err := s.UpdateDurable(ctx, "archive", id, ledger.DurablePatch{Status: &status}); err != nil {
		t.Fatal(err)
	}
	item, _ := s.Get(ctx, id)
	if item.Quantity != 0 {
		t.Fatal(item)
	}
	status = "ACTIVE"
	if _, _, err := s.UpdateDurable(ctx, "restore", id, ledger.DurablePatch{Status: &status}); err != nil {
		t.Fatal(err)
	}
	item, _ = s.Get(ctx, id)
	if item.Quantity != 1 {
		t.Fatal(item)
	}
}

func TestNewPurchaseDoesNotInheritExpiredStock(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	input, _ := inventory.NormalizeAndValidate(inventory.ItemInput{Name: "无糖可乐 2L", Quantity: 2, ShortLived: true, Unit: "瓶"})
	if _, err := s.Create(ctx, "short-cola", input); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Exec(`UPDATE items SET auto_consume_at=? WHERE id='short-cola'`, past); err != nil {
		t.Fatal(err)
	}
	p := samplePurchase()
	p.Items = p.Items[:1]
	p.TotalAmount = 2200
	mustPurchase(t, s, "replacement", p)
	item, err := s.Get(ctx, "short-cola")
	if err != nil || item.Quantity != 4 || item.AutoConsumeAt == nil || *item.AutoConsumeAt == past {
		t.Fatal(item, err)
	}
	events, err := s.InventoryEvents(ctx, ledger.HistoryFilter{ItemID: item.ID})
	if err != nil || len(events) != 3 {
		t.Fatal(events, err)
	}
	// Explicit purchase time may predate the automatic event; assert types without chronological assumptions.
	counts := map[string]int{}
	for _, e := range events {
		counts[e.Type]++
	}
	if counts["PURCHASE"] != 1 || counts["FINISH"] != 1 || counts["ADJUST"] != 1 {
		t.Fatal(events)
	}
}
