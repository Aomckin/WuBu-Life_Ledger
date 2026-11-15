package store

import (
	"context"
	"errors"
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
	out, _, e := s.Finance().Record(context.Background(), key, kind, p)
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
	p := mustPurchase(t, s, "cola", ledger.AcquisitionInput{Items: []ledger.AcquisitionLine{{Name: "无糖可乐", ProductSpec: inventory.ProductSpec{Spec: "2L"}, Quantity: 1, Unit: "瓶"}}})
	moneyAction(t, s, "cola-charge", "EXPENSE", ledger.MoneyInput{Amount: 550})
	balanceIs(t, s, wechat.ID, 9450)
	if p.Items[0].InventoryAfter == nil || *p.Items[0].InventoryAfter != 1 {
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
	keyboard := mustPurchase(t, s, "keyboard", ledger.AcquisitionInput{Items: []ledger.AcquisitionLine{{Name: "键盘", Quantity: 1, Unit: "个", ItemType: ledger.Durable, Location: "宿舍"}}})
	id := keyboard.Items[0].ItemID
	d, e := s.DurableDetails(ctx, id)
	if e != nil || d.Status != "ACTIVE" || d.PurchaseEventID != keyboard.ID {
		t.Fatal(d, e)
	}
	if _, _, e = s.RecordMaintenance(ctx, "keyboard-maintenance", id, ledger.MaintenanceRecord{Title: "换轴"}); e != nil {
		t.Fatal(e)
	}
	balanceIs(t, s, wechat.ID, 9200)

	for _, status := range []string{"STORED", "ACTIVE"} {
		if _, _, e = s.DurableAction(ctx, "status:"+status, id, "status", ledger.DurableActionInput{Status: status}); e != nil {
			t.Fatal(e)
		}
	}
	if _, _, e = s.DurableAction(ctx, "move", id, "move", ledger.DurableActionInput{Location: "家里"}); e != nil {
		t.Fatal(e)
	}
	sold, _, e := s.Sell(ctx, "sold", id, ledger.SaleRecord{})
	if e != nil || sold.Status != "SOLD" || sold.SoldPrice != nil {
		t.Fatal(sold, e)
	}
	moneyAction(t, s, "sale-income", "INCOME", ledger.MoneyInput{AccountID: alipay.ID, Amount: 26000})
	balanceIs(t, s, alipay.ID, 176000)
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
func TestRefundLimitAndReplay(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "现金", "CASH", 10000)
	charge := moneyAction(t, s, "charge", "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: 10000}).Transactions[0]
	moneyAction(t, s, "partial", "REFUND", ledger.MoneyInput{RelatedTransactionID: charge.ID, Amount: 4000})
	balanceIs(t, s, a.ID, 4000)
	if _, _, e := s.Finance().Record(ctx, "over", "REFUND", ledger.MoneyInput{RelatedTransactionID: charge.ID, Amount: 7000}); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	full := ledger.MoneyInput{RelatedTransactionID: charge.ID, Amount: 6000}
	moneyAction(t, s, "full", "REFUND", full)
	if _, replay, e := s.Finance().Record(ctx, "full", "REFUND", full); e != nil || !replay {
		t.Fatal(replay, e)
	}
	balanceIs(t, s, a.ID, 10000)
	countTable(t, s, "inventory_events", 0)
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
	if _, _, e := s.Finance().Record(ctx, "transfer", "TRANSFER", p); e == nil {
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
			if _, _, e := s.Finance().Record(ctx, "transfer", "TRANSFER", p); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	balanceIs(t, s, a.ID, 50000)
	balanceIs(t, s, b.ID, 50000)
	countTable(t, s, "money_transactions", 4)
	p.Amount++
	if _, _, e := s.Finance().Record(ctx, "transfer", "TRANSFER", p); !errors.Is(e, ledger.ErrIdempotency) {
		t.Fatal(e)
	}
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
	if b.TotalBalance != 90000 || b.LiquidBalance != 100000 {
		t.Fatal(b)
	}
	income := moneyAction(t, s, "income", "INCOME", ledger.MoneyInput{AccountID: a.ID, Amount: 400000})
	if income.Transactions[0].BalanceDelta != 400000 {
		t.Fatal(income)
	}
	p := samplePurchase()
	p.Items = p.Items[2:]
	out := mustPurchase(t, s, "owned", p)
	id := out.Items[0].ItemID
	for i := range 2 {
		key := []string{"repair1", "repair2"}[i]
		if _, _, e := s.RecordMaintenance(ctx, key, id, ledger.MaintenanceRecord{Title: "维修"}); e != nil {
			t.Fatal(e)
		}
	}
	cost, e := s.ItemCost(ctx, id, ledger.HistoryFilter{})
	if e != nil || cost.TotalCost != 0 || cost.MaintenanceCount != 2 {
		t.Fatal(cost, e)
	}
	if _, _, e = s.InventoryAction(ctx, "wrong-consume", id, "FINISH", ledger.InventoryInput{}); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	if _, _, e = s.DurableAction(ctx, "retire", id, "retire", ledger.DurableActionInput{}); e != nil {
		t.Fatal(e)
	}
	d, _ := s.DurableDetails(ctx, id)
	if d.Status != "RETIRED" {
		t.Fatal(d)
	}
	reconcileAll(t, s)
}
func TestV02ItemStatisticsIncludeMaintenanceAndRefunds(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "资金", "WALLET", 100000)
	p := samplePurchase()
	p.Items = p.Items[2:]
	out := mustPurchase(t, s, "price", p)
	id := out.Items[0].ItemID
	maintenance, _, e := s.RecordMaintenance(ctx, "repair", id, ledger.MaintenanceRecord{Title: "维修"})
	if e != nil {
		t.Fatal(e)
	}
	payment := moneyAction(t, s, "payment", "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: 50000}).Transactions[0]
	fee := moneyAction(t, s, "fee", "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: 10000}).Transactions[0]
	refund := moneyAction(t, s, "maintenance-refund", "REFUND", ledger.MoneyInput{RelatedTransactionID: fee.ID, Amount: 5000}).Transactions[0]
	for _, link := range []ledger.LinkInput{
		{SourceType: "ITEM_ACQUISITION", SourceID: out.ID, TransactionID: payment.ID, Relation: "PURCHASE"},
		{SourceType: "MAINTENANCE", SourceID: maintenance.ID, TransactionID: fee.ID, Relation: "MAINTENANCE"},
		{SourceType: "MAINTENANCE", SourceID: maintenance.ID, TransactionID: refund.ID, Relation: "REFUND"},
	} {
		if _, _, e = s.CreateLink(ctx, "", link); e != nil {
			t.Fatal(e)
		}
	}
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
