package store

import (
	"context"
	"path/filepath"
	"stockroom/internal/inventory"
	"stockroom/internal/ledger"
	"testing"
	"time"
)

func TestLivingRestartPreservesPlansAndClaims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	ctx := context.Background()
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	a := moneyAccount(t, s, "银行", "BANK", 100000)
	r, _, e := s.SaveResidence(ctx, "home", "", ledger.ResidenceInput{Name: "本地住处", MoveInDate: "2026-11-01", Status: "ACTIVE"})
	if e != nil {
		t.Fatal(e)
	}
	d, _, e := s.CreateClaim(ctx, "deposit", "DEPOSIT", ledger.ClaimInput{Amount: 60000, AccountID: a.ID, ResidenceID: r.ID})
	if e != nil {
		t.Fatal(e)
	}
	p, _, e := s.SavePlannedCashflow(ctx, "plan", "", ledger.PlannedCashflowInput{Name: "手机费", Direction: "OUTFLOW", Amount: 10000, DueAt: "2026-11-18T00:00:00+08:00", Mandatory: true})
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	balanceIs(t, s, a.ID, 40000)
	plans, e := s.UpcomingCashflows(ctx, "", "")
	if e != nil || len(plans) != 1 || plans[0].ID != p.ID {
		t.Fatal(plans, e)
	}
	claims, e := s.Claims(ctx, "DEPOSIT")
	if e != nil || len(claims) != 1 || claims[0].ID != d.ID || claims[0].Status != "HELD" {
		t.Fatal(claims, e)
	}
	_, replay, e := s.CreateClaim(ctx, "deposit", "DEPOSIT", ledger.ClaimInput{Amount: 60000, AccountID: a.ID, ResidenceID: r.ID})
	if e != nil || !replay {
		t.Fatal(replay, e)
	}
	balanceIs(t, s, a.ID, 40000)
	reconcileAll(t, s)
}

func TestResidenceDepositsAndReceivables(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "现金", "BANK", 2000000)
	residence, _, e := s.SaveResidence(ctx, "residence", "", ledger.ResidenceInput{Name: "深圳", MoveInDate: "2026-11-01", MonthlyRent: 298000, Status: "ACTIVE"})
	if e != nil {
		t.Fatal(e)
	}
	deposit, _, e := s.CreateClaim(ctx, "deposit", "DEPOSIT", ledger.ClaimInput{Amount: 600000, AccountID: a.ID, ResidenceID: residence.ID})
	if e != nil || deposit.Status != "HELD" {
		t.Fatal(deposit, e)
	}
	balanceIs(t, s, a.ID, 1400000)
	spending, e := s.SpendingByNature(ctx, ledger.MoneyFilter{})
	if e != nil || spending.TotalSpending != 0 {
		t.Fatal(spending, e)
	}
	input := ledger.ClaimSettlement{Amount: 200000}
	partial, _, e := s.SettleClaim(ctx, "partial", deposit.ID, "DEPOSIT", "return", input)
	if e != nil || partial.Status != "PARTIALLY_RETURNED" {
		t.Fatal(partial, e)
	}
	_, replay, e := s.SettleClaim(ctx, "partial", deposit.ID, "DEPOSIT", "return", input)
	if e != nil || !replay {
		t.Fatal(replay, e)
	}
	if _, _, e = s.SettleClaim(ctx, "too-much", deposit.ID, "DEPOSIT", "return", ledger.ClaimSettlement{Amount: 500000}); e == nil {
		t.Fatal("over-return accepted")
	}
	returned, _, e := s.SettleClaim(ctx, "rest", deposit.ID, "DEPOSIT", "return", ledger.ClaimSettlement{Amount: 400000})
	if e != nil || returned.Status != "RETURNED" {
		t.Fatal(returned, e)
	}
	balanceIs(t, s, a.ID, 2000000)
	// Returned deposits are never ordinary income.
	summary, e := s.MoneySummary(ctx, ledger.MoneyFilter{})
	if e != nil || summary.Income != 0 || summary.Expense != 0 {
		t.Fatal(summary, e)
	}
	for _, p := range []struct {
		role, nature string
		amount       ledger.Money
	}{{"RENT", "FIXED", 298000}, {"AGENCY_FEE", "SETUP", 100000}, {"SETUP", "CAPITAL", 150000}, {"UTILITY", "FIXED", 20000}} {
		expense := moneyAction(t, s, p.role, "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: p.amount, ExpenseNature: p.nature}).Transactions[0]
		if _, _, e = s.LinkResidenceCost(ctx, "link:"+p.role, residence.ID, ledger.ResidenceLink{TransactionID: expense.ID, Role: p.role}); e != nil {
			t.Fatal(e)
		}
	}
	cost, e := s.ResidenceCost(ctx, residence.ID)
	if e != nil || cost.ConsumedCost != 418000 || cost.CapitalCost != 150000 || cost.DepositLocked != 0 || cost.NetCashOutflow != 568000 {
		t.Fatal(cost, e)
	}
	claim, _, e := s.CreateClaim(ctx, "advance", "RECEIVABLE", ledger.ClaimInput{Amount: 30000, AccountID: a.ID, Counterparty: "公司", Reason: "代垫"})
	if e != nil {
		t.Fatal(e)
	}
	spending, e = s.SpendingByNature(ctx, ledger.MoneyFilter{})
	if e != nil || spending.TotalSpending != 568000 {
		t.Fatal(spending, e)
	}
	cash, e := s.LivingRunway(ctx, time.Now())
	if e != nil || cash.LiquidBalance != 1402000 {
		t.Fatal(cash, e)
	}
	_, _, e = s.SettleClaim(ctx, "reimbursement", claim.ID, "RECEIVABLE", "return", ledger.ClaimSettlement{Amount: 30000})
	if e != nil {
		t.Fatal(e)
	}
	balanceIs(t, s, a.ID, 1432000)
	reconcileAll(t, s)
}
func TestClaimForfeitAtomicity(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "现金", "BANK", 100000)
	r, _, e := s.SaveResidence(ctx, "r", "", ledger.ResidenceInput{Name: "房", MoveInDate: "2026-11-01", Status: "ACTIVE"})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.CreateClaim(ctx, "bad", "DEPOSIT", ledger.ClaimInput{Amount: 60000, AccountID: a.ID, ResidenceID: "missing"}); e == nil {
		t.Fatal("missing residence accepted")
	}
	balanceIs(t, s, a.ID, 100000)
	d, _, e := s.CreateClaim(ctx, "d", "DEPOSIT", ledger.ClaimInput{Amount: 60000, AccountID: a.ID, ResidenceID: r.ID})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = s.SettleClaim(ctx, "loss", d.ID, "DEPOSIT", "forfeit", ledger.ClaimSettlement{Amount: 10000})
	if e != nil {
		t.Fatal(e)
	}
	balanceIs(t, s, a.ID, 40000)
	c, e := s.ResidenceCost(ctx, r.ID)
	if e != nil || c.DepositLoss != 10000 || c.DepositLocked != 50000 || c.NetCashOutflow != 60000 {
		t.Fatal(c, e)
	}
	n, e := s.SpendingByNature(ctx, ledger.MoneyFilter{})
	if e != nil || n.TotalSpending != 10000 || n.NormalizedLivingCost != 0 {
		t.Fatal(n, e)
	}
}
func TestConsumptionForecastIgnoresAdjustments(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	item, e := s.Create(ctx, "cola", inventory.ItemInput{Name: "可乐", Category: "other", Icon: "inventory_2", Quantity: 9, Unit: "瓶"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`UPDATE items SET created_at=? WHERE id=?`, now.AddDate(0, 0, -10).Format(time.RFC3339Nano), item.ID); e != nil {
		t.Fatal(e)
	}
	for i, q := range []float64{2, 2, 1} {
		_, _, e = s.InventoryAction(ctx, string(rune('a'+i)), item.ID, "CONSUME", ledger.InventoryInput{Quantity: &q, OccurredAt: now.AddDate(0, 0, -9+i*3).Format(time.RFC3339)})
		if e != nil {
			t.Fatal(e)
		}
	}
	f, e := s.ConsumptionForecast(ctx, item.ID, now)
	if e != nil || f.Status != "OK" || f.AverageDailyConsumption == nil || *f.AverageDailyConsumption < 0.499 || *f.AverageDailyConsumption > 0.501 || *f.EstimatedDaysRemaining < 7.99 || *f.EstimatedDaysRemaining > 8.01 {
		t.Fatal(f, e)
	}
	q := 9.0
	_, _, e = s.InventoryAction(ctx, "adjust", item.ID, "ADJUST", ledger.InventoryInput{Quantity: &q})
	if e != nil {
		t.Fatal(e)
	}
	f, e = s.ConsumptionForecast(ctx, item.ID, now)
	if e != nil || f.EventCount != 3 || *f.AverageDailyConsumption < 0.499 {
		t.Fatal(f, e)
	}
	fresh, e := s.Create(ctx, "shampoo", inventory.ItemInput{Name: "洗发水", Category: "other", Icon: "inventory_2", Quantity: 1, Unit: "瓶"})
	if e != nil {
		t.Fatal(e)
	}
	f, e = s.ConsumptionForecast(ctx, fresh.ID, now.Add(time.Second))
	if e != nil || f.Status != "INSUFFICIENT_DATA" || f.EstimatedDaysRemaining != nil {
		t.Fatal(f, e)
	}
}
