package store

import (
	"context"
	"testing"

	"stockroom/internal/ledger"
	"stockroom/internal/links"
)

func TestIndependentItemsWithoutFinance(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`DELETE FROM accounts;
	CREATE TRIGGER no_finance BEFORE INSERT ON money_transactions BEGIN SELECT RAISE(ABORT,'finance disabled'); END;`); err != nil {
		t.Fatal(err)
	}
	items := s.Items()
	acquired, _, err := items.RecordAcquisition(ctx, "independent", ledger.AcquisitionInput{Items: []ledger.AcquisitionLine{
		{Name: "测试可乐", Quantity: 6, Unit: "瓶", Location: "冰箱"},
		{Name: "测试键盘", Quantity: 1, Unit: "件", ItemType: ledger.Durable},
		{Name: "测试显示器", Quantity: 1, Unit: "台", ItemType: ledger.Durable},
	}})
	if err != nil {
		t.Fatal(acquired, err)
	}
	drink, keyboard, monitor := acquired.Items[0].ItemID, acquired.Items[1].ItemID, acquired.Items[2].ItemID
	one := 1.0
	if _, _, err = items.InventoryAction(ctx, "consume", drink, "CONSUME", ledger.InventoryInput{Quantity: &one}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = items.InventoryAction(ctx, "open", drink, "OPEN", ledger.InventoryInput{}); err != nil {
		t.Fatal(err)
	}
	if returned, _, err := items.Return(ctx, "return-drink", drink, ledger.InventoryInput{Quantity: &one}); err != nil || returned.Item.Quantity != 4 || returned.Event.Type != "RETURN" {
		t.Fatal(returned, err)
	}
	if _, _, err = items.RecordMaintenance(ctx, "maintenance", keyboard, ledger.MaintenanceRecord{Title: "换轴"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = items.durableAction(ctx, "move", "items:durable", keyboard, "move", ledger.DurableActionInput{Location: "办公室"}); err != nil {
		t.Fatal(err)
	}
	if sold, _, err := items.Sell(ctx, "sold", keyboard, ledger.SaleRecord{}); err != nil || sold.Status != "SOLD" || sold.SoldPrice != nil {
		t.Fatal(sold, err)
	}
	if _, _, err = items.durableAction(ctx, "retire", "items:durable", monitor, "retire", ledger.DurableActionInput{}); err != nil {
		t.Fatal(err)
	}
	countTable(t, s, "money_transactions", 0)
}

func TestBackfillRetriesOnlyIncompleteLinkAndKeepsFacts(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "bank", "BANK", 10000)
	f := s.Finance()
	p := ledger.MoneyInput{AccountID: a.ID, Amount: 6900}
	m, _, err := f.Record(ctx, "original", "EXPENSE", p)
	if err != nil {
		t.Fatal(err)
	}
	tx := m.Transactions[0].ID
	input := ledger.AcquisitionInput{Items: []ledger.AcquisitionLine{{Name: "测试可乐", Unit: "瓶", Quantity: 6}}}
	if _, err = s.db.Exec(`CREATE TRIGGER no_link BEFORE INSERT ON domain_links BEGIN SELECT RAISE(ABORT,'link unavailable'); END;`); err != nil {
		t.Fatal(err)
	}
	out, replay, err := links.Backfill(ctx, s.Items(), f, s, "backfill", tx, input)
	if err == nil || replay || out.Acquisition.ID == "" || out.Link != nil {
		t.Fatal(out, replay, err)
	}
	countTable(t, s, "inventory_events", 1)
	if _, err = s.db.Exec(`DROP TRIGGER no_link`); err != nil {
		t.Fatal(err)
	}
	retry, replay, err := links.Backfill(ctx, s.Items(), f, s, "backfill", tx, input)
	if err != nil || !replay || retry.Acquisition.ID != out.Acquisition.ID || retry.Link == nil {
		t.Fatal(retry, replay, err)
	}
	if _, _, err = s.DeleteLink(ctx, "unlink", retry.Link.ID); err != nil {
		t.Fatal(err)
	}
	countTable(t, s, "domain_links", 0)
	countTable(t, s, "money_transactions", 2)
	balanceIs(t, s, a.ID, 3100)
	item, err := s.Get(ctx, out.Acquisition.Items[0].ItemID)
	if err != nil || item.Quantity != 6 {
		t.Fatal(item, err)
	}
}

func TestFinanceRefundDoesNotWriteItems(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "bank", "BANK", 10000)
	if _, err := s.db.Exec(`CREATE TRIGGER no_stock BEFORE INSERT ON items BEGIN SELECT RAISE(ABORT,'items unavailable'); END;`); err != nil {
		t.Fatal(err)
	}
	f := s.Finance()
	m, _, err := f.Record(ctx, "charge", "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.Record(ctx, "refund", "REFUND", ledger.MoneyInput{Amount: 400, RelatedTransactionID: m.Transactions[0].ID}); err != nil {
		t.Fatal(err)
	}
	balanceIs(t, s, a.ID, 8400)
	countTable(t, s, "items", 0)
	countTable(t, s, "inventory_events", 0)
}
