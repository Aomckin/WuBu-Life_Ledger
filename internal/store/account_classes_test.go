package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"stockroom/internal/ledger"
	"sync"
	"testing"
)

func consumptionAccount(t *testing.T, s *Store, name, merchant string) ledger.Account {
	t.Helper()
	a, _, e := s.CreateAccount(context.Background(), "account:"+name, ledger.AccountInput{Name: name, Type: "MERCHANT_BALANCE", AccountClass: ledger.ConsumptionAccount, Merchant: merchant})
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestV021AccountClassesTopUpAndConsumption(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "微信", "WALLET", 50000)
	b := consumptionAccount(t, s, "茶颜会员", "茶颜悦色")
	p := ledger.TopUpInput{FromAccount: a.Name, ToAccount: b.Name, PaidAmount: 10000, CreditedAmount: 11000}
	result, replay, e := s.TopUpAccount(ctx, "top-up", p)
	if e != nil || replay || len(result.Transactions) != 3 {
		t.Fatal(result, replay, e)
	}
	for i, kind := range []string{"TRANSFER_OUT", "TRANSFER_IN", "BONUS"} {
		if result.Transactions[i].Type != kind {
			t.Fatal(result)
		}
	}
	if result.Transactions[2].Amount != 1000 || result.Transactions[2].RelatedTransactionID != result.Transactions[1].ID {
		t.Fatal(result)
	}
	balanceIs(t, s, a.ID, 40000)
	balanceIs(t, s, b.ID, 11000)
	sums, _ := s.MoneySummary(ctx, ledger.MoneyFilter{})
	if sums.NetExpense != 0 || sums.Income != 0 || sums.Bonus != 1000 {
		t.Fatal(sums)
	}
	balance, _ := s.Balances(ctx)
	if balance.LiquidBalance != 40000 || balance.AvailableBalance != 40000 || balance.ConsumptionBalance != 11000 || balance.TotalValue != 51000 {
		t.Fatal(balance)
	}
	filtered, e := s.Accounts(ctx, ledger.AccountFilter{AccountClass: ledger.ConsumptionAccount, Merchant: "茶颜悦色"})
	if e != nil || len(filtered) != 1 || filtered[0].ID != b.ID {
		t.Fatal(filtered, e)
	}
	_, _, e = s.SetMoneySettings(ctx, "default", ledger.MoneySettings{DefaultPaymentAccount: b.ID})
	if e != nil {
		t.Fatal(e)
	}
	purchase := mustPurchase(t, s, "milk-tea", ledger.PurchaseInput{TotalAmount: 2000, Items: []ledger.PurchaseLine{{Name: "奶茶", Quantity: 1, Unit: "杯", UnitPrice: 2000, Subtotal: 2000, ItemType: ledger.Untracked}}})
	if purchase.Merchant != "茶颜悦色" || purchase.AccountID != b.ID {
		t.Fatal(purchase)
	}
	balanceIs(t, s, b.ID, 9000)
	saved, _ := s.GetPurchase(ctx, purchase.ID)
	if saved.Merchant != "茶颜悦色" {
		t.Fatal(saved)
	}
	sums, _ = s.MoneySummary(ctx, ledger.MoneyFilter{})
	if sums.NetExpense != 2000 || sums.Income != 0 {
		t.Fatal(sums)
	}
	stats, e := s.AccountStatistics(ctx, b.Name)
	if e != nil || stats.TopUpPrincipal != 10000 || stats.Bonus != 1000 || stats.Expense != 2000 || stats.CurrentBalance != 9000 || stats.BonusRatio != 0.1 {
		t.Fatal(stats, e)
	}
	moneyAction(t, s, "refund", "REFUND", ledger.MoneyInput{PurchaseEventID: purchase.ID, Amount: 1000})
	balanceIs(t, s, b.ID, 10000)
	moneyAction(t, s, "refund-to-wallet", "REFUND", ledger.MoneyInput{PurchaseEventID: purchase.ID, Amount: 1000, AccountID: a.ID})
	balanceIs(t, s, a.ID, 41000)
	balanceIs(t, s, b.ID, 10000)
	stats, e = s.AccountStatistics(ctx, b.ID)
	if e != nil || stats.NetExpense != 0 || stats.Refund != 2000 {
		t.Fatal(stats, e)
	}
	sums, _ = s.MoneySummary(ctx, ledger.MoneyFilter{})
	if sums.NetExpense != 0 {
		t.Fatal(sums)
	}
	if _, r, e := s.TopUpAccount(ctx, "top-up", p); e != nil || !r {
		t.Fatal(r, e)
	}
	reconcileAll(t, s)
}
func TestV021TopUpAtomicRetryLossAndBonus(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "现金", "CASH", 50000)
	b := consumptionAccount(t, s, "校园卡", "")
	p := ledger.TopUpInput{FromAccount: a.ID, ToAccount: b.ID, PaidAmount: 10000, CreditedAmount: 11000}
	for _, kind := range []string{"TRANSFER_IN", "BONUS"} {
		if _, e := s.db.Exec(`CREATE TRIGGER topup_failure BEFORE INSERT ON money_transactions WHEN NEW.type='` + kind + `' BEGIN SELECT RAISE(ABORT,'injected topup failure');END`); e != nil {
			t.Fatal(e)
		}
		if _, _, e := s.TopUpAccount(ctx, "retry", p); e == nil {
			t.Fatal("expected rollback")
		}
		balanceIs(t, s, a.ID, 50000)
		balanceIs(t, s, b.ID, 0)
		countTable(t, s, "money_transactions", 2)
		s.db.Exec(`DROP TRIGGER topup_failure`)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, e := s.TopUpAccount(ctx, "retry", p); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	balanceIs(t, s, a.ID, 40000)
	balanceIs(t, s, b.ID, 11000)
	countTable(t, s, "money_transactions", 5)
	p.CreditedAmount = 10000
	if _, _, e := s.TopUpAccount(ctx, "retry", p); !errors.Is(e, ledger.ErrIdempotency) {
		t.Fatal(e)
	}
	out, _, e := s.TopUpAccount(ctx, "no-bonus", p)
	if e != nil || len(out.Transactions) != 2 {
		t.Fatal(out, e)
	}
	p.CreditedAmount = 9000
	out, _, e = s.TopUpAccount(ctx, "loss", p)
	if e != nil || len(out.Transactions) != 3 || len(out.Warnings) != 1 || out.Warnings[0].Code != "TOP_UP_VALUE_LOSS" || out.Transactions[2].Type != "ADJUSTMENT" || out.Transactions[2].BalanceDelta != -1000 {
		t.Fatal(out, e)
	}
	balanceIs(t, s, b.ID, 30000)
	balanceIs(t, s, a.ID, 20000)
	moneyAction(t, s, "bonus-only", "BONUS", ledger.MoneyInput{AccountID: b.ID, Amount: 500})
	if _, _, e = s.MoneyAction(ctx, "cash-bonus", "BONUS", ledger.MoneyInput{AccountID: a.ID, Amount: 500}); !errors.Is(e, ledger.ErrInvalid) {
		t.Fatal(e)
	}
	summary, _ := s.MoneySummary(ctx, ledger.MoneyFilter{})
	if summary.Income != 0 || summary.Expense != 0 || summary.Bonus != 1500 {
		t.Fatal(summary)
	}
	bonus, e := s.Transactions(ctx, ledger.MoneyFilter{Type: "BONUS"})
	if e != nil || len(bonus) != 2 {
		t.Fatal(bonus, e)
	}
	for _, bad := range []ledger.TopUpInput{
		{FromAccount: a.ID, ToAccount: a.ID, PaidAmount: 100, CreditedAmount: 100},
		{FromAccount: b.ID, ToAccount: a.ID, PaidAmount: 100, CreditedAmount: 100},
		{FromAccount: a.ID, ToAccount: b.ID, PaidAmount: 0, CreditedAmount: 100},
		{FromAccount: a.ID, ToAccount: b.ID, PaidAmount: 100, CreditedAmount: 0},
	} {
		if _, _, e = s.TopUpAccount(ctx, "", bad); !errors.Is(e, ledger.ErrInvalid) {
			t.Fatal(bad, e)
		}
	}
	reconcileAll(t, s)
}
func TestV021ClassEditingMismatchAndDeactivation(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := consumptionAccount(t, s, "茶颜会员", "茶颜悦色")
	moneyAction(t, s, "gift", "BONUS", ledger.MoneyInput{AccountID: a.ID, Amount: 7300})
	before, _ := s.Transactions(ctx, ledger.MoneyFilter{AccountID: a.ID})
	class := ledger.StorageAccount
	changed, _, e := s.PatchAccount(ctx, "class-storage", a.ID, ledger.AccountPatch{AccountClass: &class})
	if e != nil || changed.AccountClass != class {
		t.Fatal(changed, e)
	}
	b, _ := s.Balances(ctx)
	if b.LiquidBalance != 7300 || b.ConsumptionBalance != 0 {
		t.Fatal(b)
	}
	class = ledger.ConsumptionAccount
	_, _, e = s.PatchAccount(ctx, "class-consumption", a.ID, ledger.AccountPatch{AccountClass: &class})
	if e != nil {
		t.Fatal(e)
	}
	after, _ := s.Transactions(ctx, ledger.MoneyFilter{AccountID: a.ID})
	if !reflect.DeepEqual(before, after) {
		t.Fatal("class edit rewrote transactions")
	}
	s.SetMoneySettings(ctx, "default", ledger.MoneySettings{DefaultPaymentAccount: a.ID})
	p := ledger.PurchaseInput{Merchant: "麦当劳", TotalAmount: 2000, Items: []ledger.PurchaseLine{{Name: "饮品", Quantity: 1, Unit: "杯", Subtotal: 2000, UnitPrice: 2000}}}
	if _, e = s.ValidatePurchase(ctx, p); !errors.Is(e, ledger.ErrAccountMerchantMismatch) {
		t.Fatal(e)
	}
	if _, _, e = s.RecordPurchase(ctx, "mismatch", p); !errors.Is(e, ledger.ErrAccountMerchantMismatch) {
		t.Fatal(e)
	}
	balanceIs(t, s, a.ID, 7300)
	countTable(t, s, "purchase_events", 0)
	countTable(t, s, "items", 0)
	s.SetMoneySettings(ctx, "clear-default", ledger.MoneySettings{})
	active := false
	changed, _, e = s.PatchAccount(ctx, "deactivate", a.ID, ledger.AccountPatch{Active: &active})
	if e != nil || len(changed.Warnings) != 1 || changed.Warnings[0].Code != "ACCOUNT_HAS_REMAINING_BALANCE" {
		t.Fatal(changed, e)
	}
	b, _ = s.Balances(ctx)
	if b.ConsumptionBalance != 0 {
		t.Fatal(b)
	}
	balanceIs(t, s, a.ID, 7300)
	if _, _, e = s.TopUpAccount(ctx, "inactive", ledger.TopUpInput{FromAccount: moneyAccount(t, s, "微信", "WALLET", 50000).ID, ToAccount: a.ID, PaidAmount: 100, CreditedAmount: 100}); !errors.Is(e, ledger.ErrConflict) {
		t.Fatal(e)
	}
	reconcileAll(t, s)
}
func snapshotV02(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	rows, e := db.Query(query)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := [][]any{}
	for rows.Next() {
		v := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range v {
			ptr[i] = &v[i]
		}
		if e = rows.Scan(ptr...); e != nil {
			t.Fatal(e)
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestV021MigrationPreservesV02AndRestart(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "upgrade.db")
	db, e := sql.Open("sqlite", file)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	fixture, e := os.ReadFile("testdata/v01.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(string(fixture)); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO items(id,name,category,icon,quantity,unit,created_at,updated_at,item_type) VALUES('old-item','耳机','other','inventory_2',1,'个','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z','DURABLE');
 INSERT INTO purchase_events VALUES('old-purchase','商家','2026-10-01T00:00:00Z',50000,50000,0,0,'CNY','支付宝','manual','','other','','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z');
 INSERT INTO purchase_items VALUES('old-line','old-purchase','old-item','耳机',1,'个',50000,50000,'DURABLE',NULL,'');
 INSERT INTO durable_details(item_id,purchase_date,status,brand) VALUES('old-item','2026-10-01T00:00:00Z','ACTIVE','品牌');
 INSERT INTO inventory_events VALUES('old-stock','old-item','PURCHASE',1,0,1,'old-purchase','2026-10-01T00:00:00Z','manual','原购买');
 INSERT INTO maintenance_events VALUES('old-maintenance','old-item',NULL,'2026-10-02T00:00:00Z','维修','',6500,'DONE','manual','');
 INSERT INTO expenses VALUES('old-expense','old-purchase',NULL,50000,'CNY','other','商家','支付宝','2026-10-01T00:00:00Z','manual','');
 INSERT INTO expenses VALUES('old-maint-expense',NULL,'old-maintenance',6500,'CNY','maintenance','','微信','2026-10-02T00:00:00Z','manual','');
 UPDATE maintenance_events SET expense_id='old-maint-expense' WHERE id='old-maintenance';`); e != nil {
		t.Fatal(e)
	}
	old := &Store{db: db}
	if e = old.ensureColumn(ctx, "purchase_events", "deleted_at", `ALTER TABLE purchase_events ADD COLUMN deleted_at TEXT`); e != nil {
		t.Fatal(e)
	}
	if e = old.migrateProducts(ctx); e != nil {
		t.Fatal(e)
	}
	if e = old.migrateMoney(ctx); e != nil {
		t.Fatal(e)
	}
	// Seed v0.2 account and import history in the temporary database only.
	if _, e = db.Exec(`INSERT INTO accounts VALUES('old-wallet','旧微信','WALLET','CNY',11300,NULL,1,'旧余额','2026-10-09T00:00:00Z','2026-10-09T00:00:00Z');
 INSERT INTO money_transactions(id,account_id,type,amount_cents,delta_cents,before_cents,after_cents,occurred_at,source,reason,created_at) VALUES('opening','old-wallet','ADJUSTMENT',10000,10000,0,10000,'2026-10-09T00:00:00Z','system','INITIAL_BALANCE','2026-10-09T00:00:00Z');
 INSERT INTO money_transactions(id,account_id,type,amount_cents,delta_cents,before_cents,after_cents,occurred_at,source,external_id,created_at) VALUES('imported','old-wallet','INCOME',1300,1300,10000,11300,'2026-10-09T01:00:00Z','import','old-import','2026-10-09T01:00:00Z');`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO money_transactions(id,account_id,type,amount_cents,delta_cents,before_cents,after_cents,category,merchant,purchase_event_id,related_transaction_id,occurred_at,source,created_at)
 SELECT 'old-refund',m.account_id,'REFUND',100,100,a.balance_cents,a.balance_cents+100,m.category,m.merchant,m.purchase_event_id,m.id,'2026-10-09T02:00:00Z','manual','2026-10-09T02:00:00Z' FROM money_transactions m JOIN accounts a ON a.id=m.account_id WHERE m.type='EXPENSE' AND m.purchase_event_id<>'' LIMIT 1;
 UPDATE accounts SET balance_cents=balance_cents+100 WHERE id='unknown';
 INSERT INTO idempotency_keys VALUES('old-import-key','money_import','old-hash','{"imported":1}','2026-10-09T01:00:00Z');`); e != nil {
		t.Fatal(e)
	}
	queries := map[string]string{"accounts": "SELECT rowid,id,name,type,currency,balance_cents,credit_limit_cents,active,note,created_at,updated_at FROM accounts ORDER BY rowid"}
	for _, table := range []string{"money_transactions", "purchase_events", "purchase_items", "items", "inventory_events", "durable_details", "durable_events", "maintenance_events", "refund_returns", "idempotency_keys", "money_settings"} {
		queries[table] = "SELECT rowid,* FROM " + table + " ORDER BY rowid"
	}
	// Compare all original columns; v0.3 adds expense_nature without rewriting history.
	queries["money_transactions"] = "SELECT rowid,id,account_id,type,amount_cents,delta_cents,before_cents,after_cents,category,merchant,purchase_event_id,maintenance_event_id,related_transaction_id,external_id,occurred_at,source,note,reason,created_at,voided_at,payment_method FROM money_transactions ORDER BY rowid"
	queries["durable_details"] = "SELECT rowid,item_id,brand,model,serial_number,purchase_date,warranty_until,status,sold_at,sold_price_cents,note FROM durable_details ORDER BY rowid"
	before := map[string]string{}
	for name, q := range queries {
		before[name] = snapshotV02(t, db, q)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		upgraded, e := Open(file)
		if e != nil {
			t.Fatal(e)
		}
		defer upgraded.Close()
		for name, q := range queries {
			if got := snapshotV02(t, upgraded.db, q); got != before[name] {
				t.Fatalf("migration changed %s", name)
			}
		}
		accounts, e := upgraded.Accounts(ctx)
		if e != nil {
			t.Fatal(e)
		}
		for _, a := range accounts {
			if a.AccountClass != ledger.StorageAccount {
				t.Fatal(a)
			}
		}
		var version, foreign int
		upgraded.db.QueryRow(`PRAGMA user_version`).Scan(&version)
		upgraded.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreign)
		if version != 12 || foreign != 1 {
			t.Fatal(version, foreign)
		}
		reconcileAll(t, upgraded)
		upgraded.Close()
	}
}
