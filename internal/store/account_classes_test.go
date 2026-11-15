package store

import (
	"context"
	"errors"
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
	if balance.LiquidBalance != 40000 || balance.ConsumptionBalance != 11000 || balance.TotalValue != 51000 {
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
	charge := moneyAction(t, s, "milk-tea", "EXPENSE", ledger.MoneyInput{Amount: 2000}).Transactions[0]
	if charge.Merchant != "茶颜悦色" || charge.AccountID != b.ID {
		t.Fatal(charge)
	}
	balanceIs(t, s, b.ID, 9000)
	sums, _ = s.MoneySummary(ctx, ledger.MoneyFilter{})
	if sums.NetExpense != 2000 || sums.Income != 0 {
		t.Fatal(sums)
	}
	stats, e := s.AccountStatistics(ctx, b.Name)
	if e != nil || stats.TopUpPrincipal != 10000 || stats.Bonus != 1000 || stats.Expense != 2000 || stats.CurrentBalance != 9000 || stats.BonusRatio != 0.1 {
		t.Fatal(stats, e)
	}
	moneyAction(t, s, "refund", "REFUND", ledger.MoneyInput{RelatedTransactionID: charge.ID, Amount: 1000})
	balanceIs(t, s, b.ID, 10000)
	moneyAction(t, s, "refund-to-wallet", "REFUND", ledger.MoneyInput{RelatedTransactionID: charge.ID, Amount: 1000, AccountID: a.ID})
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
	if _, _, e = s.Finance().Record(ctx, "cash-bonus", "BONUS", ledger.MoneyInput{AccountID: a.ID, Amount: 500}); !errors.Is(e, ledger.ErrInvalid) {
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
	if _, _, e = s.Finance().Record(ctx, "mismatch", "EXPENSE", ledger.MoneyInput{Merchant: "麦当劳", Amount: 2000}); !errors.Is(e, ledger.ErrAccountMerchantMismatch) {
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
