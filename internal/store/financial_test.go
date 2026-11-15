package store

import (
	"context"
	"stockroom/internal/ledger"
	"stockroom/internal/links"
	"testing"
)

func TestFinancialEvidenceAndAnnotation(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "bank", "BANK", 10000)
	amount := ledger.Money(2000)
	p := ledger.RawFinancialInput{Provider: "WECHAT", CaptureSource: "MANUAL", Amount: &amount, OccurredAt: "2026-11-01T12:03:00+08:00", Direction: "EXPENSE", MerchantRaw: "午餐店", AccountID: a.ID, RawText: "支付20元"}
	r, _, e := s.CaptureFinancial(ctx, "capture", p)
	if e != nil || r.TransactionID == "" {
		t.Fatal(r, e)
	}
	balanceIs(t, s, a.ID, 8000)
	again, _, e := s.CaptureFinancial(ctx, "capture", p)
	if e != nil || again.ID != r.ID {
		t.Fatal(again, e)
	}
	p.CaptureSource = "CSV_IMPORT"
	p.ExternalID = "one"
	p.MerchantRaw = ""
	bank, _, e := s.CaptureFinancial(ctx, "bank", p)
	if e != nil || bank.TransactionID != r.TransactionID {
		t.Fatal(bank, e)
	}
	balanceIs(t, s, a.ID, 8000)
	p.Provider = "WECHAT"
	p.MerchantRaw = "午餐店"
	p.ExternalID = ""
	ambiguous, _, e := s.CaptureFinancial(ctx, "same-price", p)
	if e != nil || ambiguous.Status != "NEEDS_REVIEW" {
		t.Fatal(ambiguous, e)
	}
	second, _, e := s.ReviewFinancial(ctx, "new", ambiguous.ID, ledger.FinancialReview{Action: "CREATE_NEW", AccountID: a.ID, Merchant: "午餐店"})
	if e != nil || second.TransactionID == r.TransactionID {
		t.Fatal(second, e)
	}
	balanceIs(t, s, a.ID, 6000)
	list, e := s.FinancialTransactions(ctx, p.OccurredAt, 60, true)
	if e != nil || len(list) != 2 || list[0].Annotation.Category != nil {
		t.Fatal(list, e)
	}
	category := "餐饮"
	_, _, e = s.AnnotateTransaction(ctx, "annotate", r.TransactionID, ledger.TransactionAnnotation{Category: &category, Confidence: 0.99, AnnotatedBy: "user", AnnotationStatus: "CONFIRMED"})
	if e != nil {
		t.Fatal(e)
	}
	balanceIs(t, s, a.ID, 6000)
	list, e = s.FinancialTransactions(ctx, p.OccurredAt, 60, true)
	if e != nil || len(list) != 1 {
		t.Fatal(list, e)
	}
	countTable(t, s, "financial_event_links", 3)
	countTable(t, s, "raw_financial_events", 3)
}
func TestFinancialCheckpointLateRefund(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "wallet", "WALLET", 132050)
	original := moneyAction(t, s, "expense", "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: 2200, OccurredAt: "2026-10-30T04:00:00Z"})
	actual := ledger.SignedMoney(132050)
	moneyAction(t, s, "align", "ADJUSTMENT", ledger.MoneyInput{AccountID: a.ID, ActualBalance: &actual, OccurredAt: "2026-11-01T00:00:00Z"})
	cp, e := s.AccountReconciliation(ctx, a.ID)
	if e != nil || len(cp) != 1 || cp[0].UnexplainedAmount != 2200 {
		t.Fatal(cp, e)
	}
	moneyAction(t, s, "late-refund", "REFUND", ledger.MoneyInput{AccountID: a.ID, Amount: 2200, RelatedTransactionID: original.Transactions[0].ID, OccurredAt: "2026-10-31T04:00:00Z"})
	balanceIs(t, s, a.ID, 132050)
	cp, e = s.AccountReconciliation(ctx, a.ID)
	if e != nil || cp[0].UnexplainedAmount != 0 {
		t.Fatal(cp, e)
	}
	reconcileAll(t, s)
}
func TestFinancialUnknownRetainedAndAtomicFailure(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	p := ledger.RawFinancialInput{Provider: "WECHAT", CaptureSource: "MANUAL", RawText: "尚未整理的原始证据"}
	r, _, e := s.CaptureFinancial(ctx, "unknown", p)
	if e != nil || r.Status != "CAPTURED" {
		t.Fatal(r, e)
	}
	a := moneyAccount(t, s, "bank", "BANK", 10000)
	amount := ledger.Money(500)
	p.Amount = &amount
	p.AccountID = a.ID
	p.Direction = "EXPENSE"
	p.OccurredAt = "2026-11-01T01:00:00Z"
	if _, e = s.db.Exec(`CREATE TRIGGER link_fail BEFORE INSERT ON financial_event_links BEGIN SELECT RAISE(ABORT,'test');END`); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.CaptureFinancial(ctx, "failure", p); e == nil {
		t.Fatal("expected rollback")
	}
	balanceIs(t, s, a.ID, 10000)
	countTable(t, s, "raw_financial_events", 1)
}
func TestFinancialPurchaseBackfillAndSemanticEvidence(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "bank", "BANK", 10000)
	m := moneyAction(t, s, "existing", "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: 6900, OccurredAt: "2026-11-01T01:00:00Z"})
	p := ledger.AcquisitionInput{Items: []ledger.AcquisitionLine{{Name: "无糖可乐", Quantity: 6, Unit: "瓶"}}}
	result, replay, e := links.Backfill(ctx, s.Items(), s.Finance(), s, "backfill", m.Transactions[0].ID, p)
	purchase := result.Acquisition
	if e != nil || replay {
		t.Fatal(purchase, e)
	}
	balanceIs(t, s, a.ID, 3100)
	_, replay, e = links.Backfill(ctx, s.Items(), s.Finance(), s, "backfill", m.Transactions[0].ID, p)
	if e != nil || !replay {
		t.Fatal(replay, e)
	}
	item, e := s.Get(ctx, purchase.Items[0].ItemID)
	if e != nil || item.Quantity != 6 {
		t.Fatal(item, e)
	}
	countTable(t, s, "money_transactions", 2)
	countTable(t, s, "inventory_events", 1)
	ev, _, e := s.AttachSemanticEvidence(ctx, "photo", ledger.SemanticEvidence{Source: "image", Text: "午餐照片", AttachmentRef: "local://test"})
	if e != nil || ev.Status != "UNMATCHED" {
		t.Fatal(ev, e)
	}
	countTable(t, s, "money_transactions", 2)
	category := "餐饮"
	_, _, e = s.SaveMerchantRule(ctx, "rule", "", ledger.MerchantRule{MerchantRaw: "餐饮公司", MerchantNormalized: "午餐店", UserConfirmed: true, Annotation: ledger.TransactionAnnotation{Category: &category}})
	if e != nil {
		t.Fatal(e)
	}
	amount := ledger.Money(1000)
	r, _, e := s.CaptureFinancial(ctx, "rule-capture", ledger.RawFinancialInput{Provider: "WECHAT", CaptureSource: "MANUAL", AccountID: a.ID, Amount: &amount, Direction: "EXPENSE", OccurredAt: "2026-11-02T01:00:00Z", MerchantRaw: "餐饮公司"})
	if e != nil || r.TransactionID == "" {
		t.Fatal(r, e)
	}
	list, e := s.FinancialTransactions(ctx, "2026-11-02T01:00:00Z", 1, false)
	if e != nil || len(list) != 1 || list[0].Merchant != "午餐店" || list[0].Annotation.Category == nil {
		t.Fatal(list, e)
	}
}

func TestFinancialRepeatedCheckpointKeepsUnexplained(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "checkpoints", "BANK", 10000)
	original := moneyAction(t, s, "expense", "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: 2200, OccurredAt: "2026-10-28T00:00:00Z"})
	actual := ledger.SignedMoney(10000)
	moneyAction(t, s, "cp1", "ADJUSTMENT", ledger.MoneyInput{AccountID: a.ID, ActualBalance: &actual, OccurredAt: "2026-10-30T00:00:00Z"})
	moneyAction(t, s, "cp2", "ADJUSTMENT", ledger.MoneyInput{AccountID: a.ID, ActualBalance: &actual, OccurredAt: "2026-11-01T00:00:00Z"})
	cp, e := s.AccountReconciliation(ctx, a.ID)
	if e != nil || cp[0].UnexplainedAmount != 2200 {
		t.Fatal(cp, e)
	}
	moneyAction(t, s, "refund", "REFUND", ledger.MoneyInput{AccountID: a.ID, RelatedTransactionID: original.Transactions[0].ID, Amount: 2200, OccurredAt: "2026-10-29T00:00:00Z"})
	balanceIs(t, s, a.ID, 10000)
	cp, e = s.AccountReconciliation(ctx, a.ID)
	if e != nil || cp[0].UnexplainedAmount != 0 {
		t.Fatal(cp, e)
	}
}

func TestFinancialBackfillPreservesCheckpointAcrossZero(t *testing.T) {
	for _, tc := range []struct {
		name            string
		observed        ledger.SignedMoney
		income, expense ledger.Money
		afterIncome     ledger.SignedMoney
	}{
		{"net-zero-missing-pair", 10000, 2200, 2200, -2200},
		{"backfill-exceeds-original-difference", 12200, 3000, 800, -800},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ledgerDB(t)
			ctx := context.Background()
			a := moneyAccount(t, s, "anchor", "BANK", 10000)
			actual := tc.observed
			moneyAction(t, s, "anchor", "ADJUSTMENT", ledger.MoneyInput{AccountID: a.ID, ActualBalance: &actual, OccurredAt: "2026-11-02T04:00:00Z"})
			input := ledger.MoneyInput{AccountID: a.ID, Amount: tc.income, OccurredAt: "2026-11-01T04:00:00Z"}
			moneyAction(t, s, "late-income", "INCOME", input)
			balanceIs(t, s, a.ID, int64(tc.observed))
			cp, e := s.AccountReconciliation(ctx, a.ID)
			if e != nil || cp[0].UnexplainedAmount != tc.afterIncome {
				t.Fatal(cp, e)
			}
			if _, replay, e := s.Finance().Record(ctx, "late-income", "INCOME", input); e != nil || !replay {
				t.Fatal(replay, e)
			}
			moneyAction(t, s, "late-expense", "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: tc.expense, OccurredAt: "2026-11-01T05:00:00Z"})
			balanceIs(t, s, a.ID, int64(tc.observed))
			cp, e = s.AccountReconciliation(ctx, a.ID)
			if e != nil || cp[0].UnexplainedAmount != 0 {
				t.Fatal(cp, e)
			}
			reconcileAll(t, s)
		})
	}
}
