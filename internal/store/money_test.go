package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
)

func moneyAccount(t *testing.T, s *Store, name, kind string, cents int64) ledger.Account {
	t.Helper()
	a, _, e := s.CreateAccount(context.Background(), "account:"+name, ledger.AccountInput{Name: name, Type: kind, InitialBalance: ledger.SignedMoney(cents)})
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func moneyAction(t *testing.T, s *Store, key, kind string, p ledger.MoneyInput) ledger.MoneyResult {
	t.Helper()
	out, _, e := s.MoneyAction(context.Background(), key, kind, p)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func balanceIs(t *testing.T, s *Store, id string, want int64) {
	t.Helper()
	a, e := s.GetAccount(context.Background(), id)
	if e != nil || a.CurrentBalance != ledger.SignedMoney(want) {
		t.Fatalf("balance %+v want %d: %v", a, want, e)
	}
}
func reconcileAll(t *testing.T, s *Store) {
	t.Helper()
	rows, e := s.db.Query(`SELECT a.id,a.balance_cents,COALESCE(SUM(t.delta_cents),0) FROM accounts a LEFT JOIN money_transactions t ON t.account_id=a.id GROUP BY a.id`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var balance, sum int64
		if e = rows.Scan(&id, &balance, &sum); e != nil {
			t.Fatal(e)
		}
		if balance != sum {
			t.Fatalf("unexplained account %s: %d != events %d", id, balance, sum)
		}
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
}
func TestV02AcceptanceAtoF(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	wechat := moneyAccount(t, s, "微信零钱", "WALLET", 10000)
	alipay := moneyAccount(t, s, "支付宝", "WALLET", 100000)
	bank := moneyAccount(t, s, "银行卡", "BANK", 500000)
	if _, _, e := s.SetMoneySettings(ctx, "default", ledger.MoneySettings{DefaultPaymentAccount: wechat.ID}); e != nil {
		t.Fatal(e)
	}
	p := mustPurchase(t, s, "cola", ledger.PurchaseInput{TotalAmount: 550, Items: []ledger.PurchaseLine{{Name: "无糖可乐", ProductSpec: inventory.ProductSpec{Spec: "2L"}, Quantity: 1, Unit: "瓶", UnitPrice: 550, Subtotal: 550}}})
	balanceIs(t, s, wechat.ID, 9450)
	if p.AccountID != wechat.ID || p.Items[0].InventoryAfter == nil || *p.Items[0].InventoryAfter != 1 {
		t.Fatal(p)
	}
	before, e := s.Balances(ctx)
	if e != nil {
		t.Fatal(e)
	}
	summary, e := s.MoneySummary(ctx, ledger.MoneyFilter{})
	if e != nil {
		t.Fatal(e)
	}
	transfer := moneyAction(t, s, "transfer", "TRANSFER", ledger.MoneyInput{FromAccount: bank.ID, ToAccount: alipay.ID, Amount: 50000})
	if len(transfer.Transactions) != 2 || transfer.Transactions[0].RelatedTransactionID != transfer.Transactions[1].ID {
		t.Fatal(transfer)
	}
	after, _ := s.Balances(ctx)
	second, _ := s.MoneySummary(ctx, ledger.MoneyFilter{})
	if before.TotalBalance != after.TotalBalance || summary.NetExpense != second.NetExpense || second.Income != 0 {
		t.Fatal(after, second)
	}
	actual := ledger.SignedMoney(9200)
	moneyAction(t, s, "reconcile", "ADJUSTMENT", ledger.MoneyInput{AccountID: wechat.ID, ActualBalance: &actual, Reason: "实际余额"})
	balanceIs(t, s, wechat.ID, 9200)
	keyboard := mustPurchase(t, s, "keyboard", ledger.PurchaseInput{AccountID: alipay.ID, TotalAmount: 39900, Items: []ledger.PurchaseLine{{Name: "键盘", Quantity: 1, Unit: "个", UnitPrice: 39900, Subtotal: 39900, ItemType: ledger.Durable, Location: "宿舍"}}})
	id := keyboard.Items[0].ItemID
	d, e := s.DurableDetails(ctx, id)
	if e != nil || d.Status != "ACTIVE" || d.PurchasePrice != 39900 || d.PurchaseEventID != keyboard.ID {
		t.Fatal(d, e)
	}
	if _, _, e = s.AddMaintenance(ctx, "keyboard-maintenance", id, ledger.MaintenanceInput{AccountID: wechat.ID, Title: "换轴", Cost: 5000}); e != nil {
		t.Fatal(e)
	}
	balanceIs(t, s, wechat.ID, 4200)
	cost, e := s.ItemCost(ctx, id, ledger.HistoryFilter{})
	if e != nil || cost.TotalCost != 44900 {
		t.Fatal(cost, e)
	}
	for _, status := range []string{"STORED", "ACTIVE"} {
		if _, _, e = s.DurableAction(ctx, "status:"+status, id, "status", ledger.DurableActionInput{Status: status}); e != nil {
			t.Fatal(e)
		}
	}
	if _, _, e = s.DurableAction(ctx, "move", id, "move", ledger.DurableActionInput{Location: "家里"}); e != nil {
		t.Fatal(e)
	}
	sold, _, e := s.DurableAction(ctx, "sold", id, "sell", ledger.DurableActionInput{AccountID: alipay.ID, Amount: 26000})
	if e != nil || sold.Status != "SOLD" || sold.PurchasePrice != 39900 || sold.SoldPrice == nil || *sold.SoldPrice != 26000 {
		t.Fatal(sold, e)
	}
	balanceIs(t, s, alipay.ID, 136100)
	events, e := s.DurableEvents(ctx, ledger.HistoryFilter{ItemID: id})
	if e != nil || len(events) != 5 {
		t.Fatal(events, e)
	}
	if _, _, e = s.DurableAction(ctx, "restore-sold", id, "status", ledger.DurableActionInput{Status: "ACTIVE"}); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	dashboard, e := s.Dashboard(ctx, "")
	if e != nil || dashboard.DurableCount != 0 || len(dashboard.RecentTransactions) == 0 {
		t.Fatal(dashboard, e)
	}
	reconcileAll(t, s)
}
func TestV02RefundAndRollback(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "现金", "CASH", 10000)
	p := mustPurchase(t, s, "p", ledger.PurchaseInput{AccountID: a.ID, TotalAmount: 10000, Items: []ledger.PurchaseLine{{Name: "礼盒", Quantity: 2, Unit: "盒", Subtotal: 10000}}})
	moneyAction(t, s, "partial", "REFUND", ledger.MoneyInput{PurchaseEventID: p.ID, Amount: 4000, ReturnItems: []ledger.ReturnItem{{ItemID: p.Items[0].ItemID, Quantity: 1}}})
	balanceIs(t, s, a.ID, 4000)
	if out, e := s.GetPurchase(ctx, p.ID); e != nil || out.Status != "PARTIAL_REFUND" || out.RefundedAmount != 4000 {
		t.Fatal(out, e)
	}
	if _, _, e := s.MoneyAction(ctx, "over", "REFUND", ledger.MoneyInput{PurchaseEventID: p.ID, Amount: 7000}); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	if _, _, e := s.MoneyAction(ctx, "bad-stock", "REFUND", ledger.MoneyInput{PurchaseEventID: p.ID, Amount: 6000, ReturnItems: []ledger.ReturnItem{{ItemID: p.Items[0].ItemID, Quantity: 2}}}); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	balanceIs(t, s, a.ID, 4000)
	input := ledger.MoneyInput{PurchaseEventID: p.ID, Amount: 6000, ReturnItems: []ledger.ReturnItem{{ItemID: p.Items[0].ItemID, Quantity: 1}}}
	moneyAction(t, s, "full", "REFUND", input)
	balanceIs(t, s, a.ID, 10000)
	if _, replay, e := s.MoneyAction(ctx, "full", "REFUND", input); e != nil || !replay {
		t.Fatal(replay, e)
	}
	out, _ := s.GetPurchase(ctx, p.ID)
	item, _ := s.Get(ctx, p.Items[0].ItemID)
	sum, _ := s.MoneySummary(ctx, ledger.MoneyFilter{})
	if out.Status != "REFUNDED" || item.Quantity != 0 || sum.NetExpense != 0 {
		t.Fatal(out, item, sum)
	}
	// Refunded purchases cannot have their original expense rewritten.
	if _, _, e := s.DeletePurchase(ctx, "delete-refund", p.ID, ledger.PurchaseDeletion{ExpectedUpdatedAt: out.UpdatedAt}); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	reconcileAll(t, s)
}
func TestV02TransferConcurrentRetryAndFailure(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "A", "BANK", 100000)
	b := moneyAccount(t, s, "B", "WALLET", 0)
	p := ledger.MoneyInput{FromAccount: a.ID, ToAccount: b.ID, Amount: 50000}
	if _, e := s.db.Exec(`CREATE TRIGGER transfer_failure BEFORE INSERT ON money_transactions WHEN NEW.type='TRANSFER_IN' BEGIN SELECT RAISE(ABORT,'injected incoming failure');END`); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.MoneyAction(ctx, "transfer", "TRANSFER", p); e == nil {
		t.Fatal("expected rollback")
	}
	balanceIs(t, s, a.ID, 100000)
	balanceIs(t, s, b.ID, 0)
	countTable(t, s, "money_transactions", 2)
	s.db.Exec(`DROP TRIGGER transfer_failure`)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, e := s.MoneyAction(ctx, "transfer", "TRANSFER", p); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	balanceIs(t, s, a.ID, 50000)
	balanceIs(t, s, b.ID, 50000)
	countTable(t, s, "money_transactions", 4)
	p.Amount++
	if _, _, e := s.MoneyAction(ctx, "transfer", "TRANSFER", p); !errors.Is(e, ledger.ErrIdempotency) {
		t.Fatal(e)
	}
	reconcileAll(t, s)
}
func TestV02CorrectionsAndLifecycleRollback(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "账户", "WALLET", 100000)
	p := samplePurchase()
	p.AccountID = a.ID
	out := mustPurchase(t, s, "purchase", p)
	amended := out.PurchaseInput
	amended.TotalAmount = 22000
	amended.Items[2].ID = out.Items[2].ID
	changed, _, e := s.AmendPurchase(ctx, "amend", out.ID, ledger.PurchaseAmendment{PurchaseInput: amended, ExpectedUpdatedAt: out.UpdatedAt})
	if e != nil {
		t.Fatal(e)
	}
	balanceIs(t, s, a.ID, 78000)
	reconcileAll(t, s)
	if _, _, e = s.DeletePurchase(ctx, "cancel", changed.ID, ledger.PurchaseDeletion{ExpectedUpdatedAt: changed.UpdatedAt}); e != nil {
		t.Fatal(e)
	}
	balanceIs(t, s, a.ID, 100000)
	sum, _ := s.MoneySummary(ctx, ledger.MoneyFilter{})
	if sum.NetExpense != 0 {
		t.Fatal(sum)
	}
	p = samplePurchase()
	p.AccountID = a.ID
	p.Items = p.Items[2:]
	p.TotalAmount = 19900
	out = mustPurchase(t, s, "durable", p)
	id := out.Items[0].ItemID
	s.db.Exec(`CREATE TRIGGER sale_failure BEFORE INSERT ON money_transactions WHEN NEW.type='INCOME' BEGIN SELECT RAISE(ABORT,'sale failure');END`)
	if _, _, e = s.DurableAction(ctx, "sale", id, "sell", ledger.DurableActionInput{AccountID: a.ID, Amount: 10000}); e == nil {
		t.Fatal("sale should roll back")
	}
	d, _ := s.DurableDetails(ctx, id)
	item, _ := s.Get(ctx, id)
	events, _ := s.DurableEvents(ctx, ledger.HistoryFilter{ItemID: id})
	if d.Status != "ACTIVE" || item.Quantity != 1 || len(events) != 1 {
		t.Fatal(d, item, events)
	}
	balanceIs(t, s, a.ID, 80100)
	s.db.Exec(`DROP TRIGGER sale_failure`)
	if _, _, e = s.DurableAction(ctx, "sale", id, "sell", ledger.DurableActionInput{AccountID: a.ID, Amount: 10000}); e != nil {
		t.Fatal(e)
	}
	reconcileAll(t, s)
}
func TestV02LegacyMigrationAndRestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "legacy.db")
	db, e := sql.Open("sqlite", file)
	if e != nil {
		t.Fatal(e)
	}
	schema, e := os.ReadFile("testdata/v01.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(string(schema)); e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`INSERT INTO items(id,name,category,icon,quantity,unit,created_at,updated_at,item_type) VALUES('old-item','耳机','other','inventory_2',1,'个','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z','DURABLE');
 INSERT INTO purchase_events VALUES('old-purchase','商家','2026-10-01T00:00:00Z',50000,50000,0,0,'CNY','支付宝','manual','','other','','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z');
 INSERT INTO purchase_items VALUES('old-line','old-purchase','old-item','耳机',1,'个',50000,50000,'DURABLE',NULL,'');
 INSERT INTO durable_details(item_id,purchase_date,status,brand) VALUES('old-item','2026-10-01T00:00:00Z','ACTIVE','品牌');
 INSERT INTO maintenance_events VALUES('old-maintenance','old-item',NULL,'2026-10-02T00:00:00Z','维修','',6500,'DONE','manual','');
 INSERT INTO expenses VALUES('old-expense','old-purchase',NULL,50000,'CNY','other','商家','支付宝','2026-10-01T00:00:00Z','manual','');
 INSERT INTO expenses VALUES('old-maint-expense',NULL,'old-maintenance',6500,'CNY','maintenance','','微信','2026-10-02T00:00:00Z','manual','');
 UPDATE maintenance_events SET expense_id='old-maint-expense' WHERE id='old-maintenance';`)
	db.Close()
	if e != nil {
		t.Fatal(e)
	}
	s, e := Open(file)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	balanceIs(t, s, "unknown", -56500)
	a, _ := s.Accounts(ctx)
	if len(a) != 1 || a[0].Name != "未指定账户" {
		t.Fatal(a)
	}
	p, e := s.GetPurchase(ctx, "old-purchase")
	if e != nil || p.TotalAmount != 50000 || p.AccountID != "unknown" {
		t.Fatal(p, e)
	}
	d, e := s.DurableDetails(ctx, "old-item")
	if e != nil || d.Brand != "品牌" || d.PurchasePrice != 50000 {
		t.Fatal(d, e)
	}
	cost, e := s.ItemCost(ctx, "old-item", ledger.HistoryFilter{})
	if e != nil || cost.TotalCost != 56500 {
		t.Fatal(cost, e)
	}
	reconcileAll(t, s)
	var kind string
	s.db.QueryRow(`SELECT type FROM sqlite_master WHERE name='expenses'`).Scan(&kind)
	if kind != "view" {
		t.Fatal(kind)
	}
	if _, e = s.db.Exec(`INSERT INTO expenses VALUES('illegal',NULL,NULL,1,'CNY','other','','','2026-10-09T00:00:00Z','manual','')`); e == nil {
		t.Fatal("compatibility view must not be another fact source")
	}
	s.Close()
	s, e = Open(file)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	countTable(t, s, "money_transactions", 2)
	countTable(t, s, "durable_events", 1)
	balanceIs(t, s, "unknown", -56500)
	reconcileAll(t, s)
}
func TestV02ImportPreviewDedupAndAtomicity(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "支付宝", "WALLET", 10000)
	p := MoneyImportInput{AccountID: a.ID, Adapter: "generic", CSV: "occurred_at,type,amount,merchant,external_id\n2026-10-09T10:00:00+08:00,EXPENSE,5.50,便利店,one\n2026-10-09T11:00:00+08:00,INCOME,10,工资,"}
	preview, e := s.PreviewMoneyImport(ctx, p)
	if e != nil || len(preview.Rows) != 2 {
		t.Fatal(preview, e)
	}
	countTable(t, s, "money_transactions", 1)
	balanceIs(t, s, a.ID, 10000)
	result, replay, e := s.ImportMoney(ctx, "import-1", p)
	if e != nil || replay || result.Imported != 2 {
		t.Fatal(result, e)
	}
	balanceIs(t, s, a.ID, 10450)
	result, _, e = s.ImportMoney(ctx, "import-2", p)
	if e != nil || result.Imported != 0 || result.Duplicates != 2 {
		t.Fatal(result, e)
	}
	p.CSV += "\n2026-10-09T12:00:00+08:00,EXPENSE,2.50,便利店,two\n2026-10-09T12:01:00+08:00,REFUND,999,错误,three"
	if _, _, e = s.ImportMoney(ctx, "invalid-import", p); e == nil {
		t.Fatal("ambiguous refund must fail atomically")
	}
	balanceIs(t, s, a.ID, 10450)
	countTable(t, s, "money_transactions", 3)
	reconcileAll(t, s)
}
func TestV02IncomeCreditAndTotalCost(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "微信", "WALLET", 100000)
	card := moneyAccount(t, s, "信用卡", "CREDIT_CARD", 0)
	moneyAction(t, s, "card-expense", "EXPENSE", ledger.MoneyInput{AccountID: card.ID, Amount: 10000})
	balanceIs(t, s, card.ID, -10000)
	b, _ := s.Balances(ctx)
	if b.TotalBalance != 90000 || b.AvailableBalance != 100000 {
		t.Fatal(b)
	}
	income := moneyAction(t, s, "income", "INCOME", ledger.MoneyInput{AccountID: a.ID, Amount: 400000})
	if income.Transactions[0].BalanceDelta != 400000 {
		t.Fatal(income)
	}
	p := samplePurchase()
	p.AccountID = a.ID
	p.Items = p.Items[2:]
	p.TotalAmount = 50000
	p.Items[0].Subtotal = 50000
	p.Items[0].UnitPrice = 50000
	out := mustPurchase(t, s, "owned", p)
	id := out.Items[0].ItemID
	for i, cost := range []ledger.Money{10000, 5000} {
		key := []string{"repair1", "repair2"}[i]
		if _, _, e := s.AddMaintenance(ctx, key, id, ledger.MaintenanceInput{Title: "维修", Cost: cost, AccountID: a.ID}); e != nil {
			t.Fatal(e)
		}
	}
	cost, e := s.ItemCost(ctx, id, ledger.HistoryFilter{})
	if e != nil || cost.TotalCost != 65000 || cost.MaintenanceCount != 2 {
		t.Fatal(cost, e)
	}
	if _, _, e = s.InventoryAction(ctx, "wrong-consume", id, "FINISH", ledger.InventoryInput{}); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	if _, _, e = s.DurableAction(ctx, "retire", id, "retire", ledger.DurableActionInput{}); e != nil {
		t.Fatal(e)
	}
	d, _ := s.DurableDetails(ctx, id)
	if d.PurchasePrice != 50000 || d.Status != "RETIRED" {
		t.Fatal(d)
	}
	reconcileAll(t, s)
}
func TestV02ItemStatisticsIncludeMaintenanceAndRefunds(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "资金", "WALLET", 100000)
	p := samplePurchase()
	p.AccountID = a.ID
	p.Items = p.Items[2:]
	p.TotalAmount = 50000
	p.Items[0].Subtotal = 50000
	p.Items[0].UnitPrice = 50000
	out := mustPurchase(t, s, "price", p)
	id := out.Items[0].ItemID
	maintenance, _, e := s.AddMaintenance(ctx, "repair", id, ledger.MaintenanceInput{Title: "维修", Cost: 10000, AccountID: a.ID})
	if e != nil {
		t.Fatal(e)
	}
	moneyAction(t, s, "maintenance-refund", "REFUND", ledger.MoneyInput{RelatedTransactionID: *maintenance.ExpenseID, Amount: 5000})
	moneyAction(t, s, "ride", "EXPENSE", ledger.MoneyInput{Amount: 2000, AccountID: a.ID, Category: "交通"})
	summary, e := s.MoneySummary(ctx, ledger.MoneyFilter{GroupBy: "item"})
	if e != nil || summary.NetExpense != 57000 || len(summary.Groups) != 2 {
		t.Fatal(summary, e)
	}
	for _, group := range summary.Groups {
		if group.Key == id && group.Amount != 55000 {
			t.Fatal(group)
		}
	}
	summary, e = s.MoneySummary(ctx, ledger.MoneyFilter{HistoryFilter: ledger.HistoryFilter{ItemID: id}, GroupBy: "item"})
	if e != nil || summary.NetExpense != 55000 || len(summary.Groups) != 1 || summary.Groups[0].Amount != 55000 {
		t.Fatal(summary, e)
	}
	cost, e := s.ItemCost(ctx, id, ledger.HistoryFilter{})
	if e != nil || cost.TotalCost != 60000 || cost.NetCost != 55000 || cost.Refund != 5000 {
		t.Fatal(cost, e)
	}
	if _, _, e = s.SaveMoneyCategory(ctx, "rename", CategoryInput{Name: "出行", PreviousName: "交通"}); e != nil {
		t.Fatal(e)
	}
	summary, e = s.MoneySummary(ctx, ledger.MoneyFilter{HistoryFilter: ledger.HistoryFilter{Category: "出行"}})
	if e != nil || summary.NetExpense != 2000 {
		t.Fatal(summary, e)
	}
}
func TestV02MigrationFailurePreservesLegacyExpense(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rollback.db")
	db, e := sql.Open("sqlite", file)
	if e != nil {
		t.Fatal(e)
	}
	schema, e := os.ReadFile("testdata/v01.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(string(schema) + `INSERT INTO expenses VALUES('legacy-expense',NULL,NULL,1000,'CNY','other','旧商家','','2026-10-01T00:00:00Z','manual','保留');CREATE TRIGGER migration_failure BEFORE DELETE ON expenses BEGIN SELECT RAISE(ABORT,'injected migration failure');END;`); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if s, e := Open(file); e == nil {
		s.Close()
		t.Fatal("migration should fail")
	}
	db, e = sql.Open("sqlite", file)
	if e != nil {
		t.Fatal(e)
	}
	var amount, count int
	var kind, note string
	if e = db.QueryRow(`SELECT amount_cents,note FROM expenses WHERE id='legacy-expense'`).Scan(&amount, &note); e != nil || amount != 1000 || note != "保留" {
		t.Fatal(amount, note, e)
	}
	db.QueryRow(`SELECT type FROM sqlite_master WHERE name='expenses'`).Scan(&kind)
	db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='money_transactions'`).Scan(&count)
	if kind != "table" || count != 0 {
		t.Fatal(kind, count)
	}
	if _, e = db.Exec(`DROP TRIGGER migration_failure`); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(file)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	balanceIs(t, s, "unknown", -1000)
	countTable(t, s, "expenses", 1)
	reconcileAll(t, s)
}
