package store

import (
	"context"
	"stockroom/internal/ledger"
	"testing"
	"time"
)

func TestRunwayFinalAcceptance(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 28800))
	a := moneyAccount(t, s, "现金", "BANK", 842000)
	consumptionAccount(t, s, "会员", "商家")
	moneyAccount(t, s, "信用卡", "CREDIT_CARD", 100000)
	_, _, e := s.SetRunwaySettings(ctx, "buffer", ledger.RunwaySettings{SafetyBuffer: 100000, HorizonDays: 30})
	if e != nil {
		t.Fatal(e)
	}
	for i, p := range []ledger.PlannedCashflowInput{
		{Name: "房租", Direction: "OUTFLOW", Amount: 298000, DueAt: now.AddDate(0, 0, 3).Format(time.RFC3339), AccountID: a.ID, Mandatory: true, ExpenseNature: "FIXED"},
		{Name: "水电", Direction: "OUTFLOW", Amount: 20000, DueAt: now.AddDate(0, 0, 4).Format(time.RFC3339), Mandatory: true},
		{Name: "手机", Direction: "OUTFLOW", Amount: 10000, DueAt: now.AddDate(0, 0, 5).Format(time.RFC3339), Mandatory: true},
		{Name: "工资", Direction: "INFLOW", Amount: 1000000, DueAt: now.AddDate(0, 0, 18).Format(time.RFC3339)},
	} {
		_, _, e = s.SavePlannedCashflow(ctx, string(rune('a'+i)), "", p)
		if e != nil {
			t.Fatal(e)
		}
	}
	balanceIs(t, s, a.ID, 842000)
	r, e := s.LivingRunway(ctx, now)
	if e != nil || r.LiquidBalance != 842000 || r.ReservedOutflow != 328000 || r.SafeToSpend != 414000 || r.SafeDailySpend != 23000 || r.DaysToNextIncome == nil || *r.DaysToNextIncome != 18 {
		t.Fatal(r, e)
	}
	plans, e := s.UpcomingCashflows(ctx, "", "")
	if e != nil {
		t.Fatal(e)
	}
	rent := plans[0]
	p := ledger.PlannedAction{OccurredAt: now.Format(time.RFC3339)}
	completed, replay, e := s.ActPlannedCashflow(ctx, "pay", rent.ID, "complete", p)
	if e != nil || replay || completed.LinkedTransactionID == "" {
		t.Fatal(completed, replay, e)
	}
	_, replay, e = s.ActPlannedCashflow(ctx, "pay", rent.ID, "complete", p)
	if e != nil || !replay {
		t.Fatal(replay, e)
	}
	if _, _, e = s.ActPlannedCashflow(ctx, "pay-again", rent.ID, "complete", p); e == nil {
		t.Fatal("double completion accepted")
	}
	r, e = s.LivingRunway(ctx, now)
	if e != nil || r.SafeToSpend != 414000 || r.ReservedOutflow != 30000 {
		t.Fatal(r, e)
	}
	reconcileAll(t, s)
}
func TestRunwayFixedHorizonNegativeAndRecurring(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	a := moneyAccount(t, s, "现金", "CASH", 600000)
	_, _, e := s.SetRunwaySettings(ctx, "buffer", ledger.RunwaySettings{SafetyBuffer: 100000, HorizonDays: 30})
	if e != nil {
		t.Fatal(e)
	}
	plan, _, e := s.SavePlannedCashflow(ctx, "weekly", "", ledger.PlannedCashflowInput{Name: "周付", Direction: "OUTFLOW", Amount: 100000, DueAt: now.Format(time.RFC3339), AccountID: a.ID, Recurrence: "WEEKLY", Mandatory: true})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.LivingRunway(ctx, now)
	if e != nil || r.Mode != "FIXED_HORIZON" || r.DaysToNextIncome != nil || r.ReservedOutflow != 500000 || r.SafeDailySpend != 0 {
		t.Fatal(r, e)
	}
	// Invalid completion rolls back the plan, balance, and recurrence generation.
	if _, _, e = s.ActPlannedCashflow(ctx, "bad", plan.ID, "complete", ledger.PlannedAction{AccountID: "missing"}); e == nil {
		t.Fatal("invalid account accepted")
	}
	list, e := s.UpcomingCashflows(ctx, "", "")
	if e != nil || len(list) != 1 {
		t.Fatal(list, e)
	}
	_, _, e = s.SetRunwaySettings(ctx, "larger", ledger.RunwaySettings{SafetyBuffer: 200000, HorizonDays: 30})
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.LivingRunway(ctx, now)
	if e != nil || !r.CashflowWarning || r.SafeDailySpend != 0 {
		t.Fatal(r, e)
	}
	_, _, e = s.ActPlannedCashflow(ctx, "good", plan.ID, "complete", ledger.PlannedAction{})
	if e != nil {
		t.Fatal(e)
	}
	list, e = s.UpcomingCashflows(ctx, "", "")
	if e != nil || len(list) != 1 || list[0].DueAt != now.AddDate(0, 0, 7).Format(time.RFC3339) {
		t.Fatal(list, e)
	}
	jan := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	if nextDue(jan, "MONTHLY", 0).Day() != 28 {
		t.Fatal("month overflow")
	}
}

func TestRecurringIncomeAndShanghaiMonthEnd(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	zone := time.FixedZone("Asia/Shanghai", 28800)
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, zone)
	_, _, e := s.SavePlannedCashflow(ctx, "salary", "", ledger.PlannedCashflowInput{Name: "月薪", Direction: "INFLOW", Amount: 500000, DueAt: "2026-10-18T09:00:00+08:00", Recurrence: "MONTHLY"})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.LivingRunway(ctx, now)
	if e != nil || r.DaysToNextIncome == nil || *r.DaysToNextIncome != 17 || r.ExpectedIncome != 500000 {
		t.Fatal(r, e)
	}
	at, _ := time.Parse(time.RFC3339, "2026-01-31T01:00:00+08:00")
	next := nextDue(at, "MONTHLY", 0).In(zone)
	if next.Day() != 28 || next.Month() != 2 || next.Hour() != 1 {
		t.Fatal(next)
	}
}
func TestSpendingNatureAndRefund(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "现金", "CASH", 2000000)
	for i, p := range []struct {
		nature string
		amount ledger.Money
	}{{"DAILY", 200000}, {"FIXED", 180000}, {"SETUP", 400000}, {"CAPITAL", 200000}} {
		moneyAction(t, s, string(rune('a'+i)), "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: p.amount, ExpenseNature: p.nature})
	}
	out, e := s.SpendingByNature(ctx, ledger.MoneyFilter{})
	if e != nil || out.TotalSpending != 980000 || out.NormalizedLivingCost != 380000 {
		t.Fatal(out, e)
	}
	expense := moneyAction(t, s, "unknown", "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: 10000}).Transactions[0]
	moneyAction(t, s, "refund", "REFUND", ledger.MoneyInput{AccountID: a.ID, Amount: 10000, RelatedTransactionID: expense.ID})
	_, _, e = s.SetExpenseNature(ctx, "classify", expense.ID, ledger.ExpenseNatureInput{ExpenseNature: "SETUP"})
	if e != nil {
		t.Fatal(e)
	}
	out, e = s.SpendingByNature(ctx, ledger.MoneyFilter{})
	if e != nil || out.TotalSpending != 980000 || out.NormalizedLivingCost != 380000 {
		t.Fatal(out, e)
	}
}
