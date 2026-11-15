package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"stockroom/internal/ledger"
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
	p := ledger.PurchaseInput{AccountID: a.ID, Merchant: "拼多多", PurchasedAt: "2026-11-01T01:00:00Z", TotalAmount: 6900, Currency: "CNY", Source: "agent", Category: "food", Items: []ledger.PurchaseLine{{Name: "无糖可乐", Quantity: 6, Unit: "瓶", Subtotal: 6900, UnitPrice: 1150, ItemType: ledger.Consumable}}}
	purchase, replay, e := s.CreatePurchaseFromTransaction(ctx, "backfill", m.Transactions[0].ID, p)
	if e != nil || replay {
		t.Fatal(purchase, e)
	}
	balanceIs(t, s, a.ID, 3100)
	_, replay, e = s.CreatePurchaseFromTransaction(ctx, "backfill", m.Transactions[0].ID, p)
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

func TestFinancialSchema12UpgradeRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "schema12.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	a := moneyAccount(t, s, "migration", "BANK", 100000)
	p := samplePurchase()
	p.AccountID = a.ID
	purchase := mustPurchase(t, s, "legacy-key", p)
	// Remove only new empty tables to reproduce the schema-12 boundary.
	for _, table := range []string{"financial_reviews", "financial_event_links", "raw_financial_events", "transaction_annotations", "balance_checkpoints", "financial_settings", "semantic_evidence", "merchant_rules"} {
		if _, e = s.db.Exec(`DROP TABLE ` + table); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.db.Exec(`PRAGMA user_version=12`); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		s, e = Open(path)
		if e != nil {
			t.Fatal(e)
		}
		var version int
		if e = s.db.QueryRow(`PRAGMA user_version`).Scan(&version); e != nil || version != 14 {
			t.Fatal(version, e)
		}
		old, e := s.GetPurchase(ctx, purchase.ID)
		if e != nil || old.TotalAmount != purchase.TotalAmount {
			t.Fatal(old, e)
		}
		replay, r, e := s.RecordPurchase(ctx, "legacy-key", p)
		if e != nil || !r || replay.ID != purchase.ID {
			t.Fatal(replay, r, e)
		}
		balanceIs(t, s, a.ID, 100000-int64(p.TotalAmount))
		if e = s.Close(); e != nil {
			t.Fatal(e)
		}
	}
}
func TestFinancialCSVPreviewMatchesReconciliationWithoutWrites(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "preview", "BANK", 10000)
	amount := ledger.Money(550)
	_, _, e := s.CaptureFinancial(ctx, "manual-record", ledger.RawFinancialInput{Provider: "WECHAT", CaptureSource: "MANUAL", AccountID: a.ID, Amount: &amount, Direction: "EXPENSE", OccurredAt: "2026-11-02T04:00:00Z", MerchantRaw: "商家", ExternalID: "wx-one", RawText: "已支付 ¥5.50"})
	if e != nil {
		t.Fatal(e)
	}
	p := MoneyImportInput{Adapter: "wechat", AccountID: a.ID, CSV: "occurred_at,type,amount,merchant,external_id\n2026-11-02T04:00:00Z,EXPENSE,5.50,商家,wx-one"}
	preview, e := s.PreviewMoneyImport(ctx, p)
	if e != nil || preview.Duplicates != 1 || preview.Imported != 0 || preview.NeedsReview != 0 {
		t.Fatal(preview, e)
	}
	countTable(t, s, "raw_financial_events", 1)
	countTable(t, s, "financial_event_links", 1)
	balanceIs(t, s, a.ID, 9450)
	for _, key := range []string{"a", "b"} {
		moneyAction(t, s, key, "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: 300, Merchant: "水店", OccurredAt: "2026-11-02T05:00:00Z"})
	}
	p.CSV = "occurred_at,type,amount,merchant,external_id\n2026-11-02T05:00:00Z,EXPENSE,3.00,水店,wx-two"
	preview, e = s.PreviewMoneyImport(ctx, p)
	if e != nil || preview.NeedsReview != 1 || preview.Imported != 0 || preview.Duplicates != 0 {
		t.Fatal(preview, e)
	}
	countTable(t, s, "raw_financial_events", 3)
	balanceIs(t, s, a.ID, 8850)
	result, _, e := s.ImportMoney(ctx, "csv", p)
	if e != nil || result.NeedsReview != preview.NeedsReview || result.Imported != preview.Imported || result.Duplicates != preview.Duplicates {
		t.Fatal(result, e)
	}
	countTable(t, s, "raw_financial_events", 4)
	balanceIs(t, s, a.ID, 8850)
}
func TestFinancialCSVRetainsCompleteMultilineSkippedEvidence(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "evidence", "BANK", 10000)
	header := "交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号\r\n"
	record := "2026/11/2 12:00,退款,测试商家,\"第一行\r\n第二行\",收入,5.50,零钱,退款成功,refund-evidence\r\n"
	p := MoneyImportInput{Adapter: "wechat", AccountID: a.ID, CSV: header + record}
	result, _, e := s.ImportMoney(ctx, "multiline-evidence", p)
	if e != nil || result.Skipped != 1 {
		t.Fatal(result, e)
	}
	rows, e := s.RawFinancialEvents(ctx, "")
	if e != nil || len(rows) != 1 || rows[0].RawText != record {
		t.Fatal(rows, e)
	}
	var payload struct {
		Header []string `json:"header"`
		Values []string `json:"values"`
	}
	if e = json.Unmarshal([]byte(rows[0].RawPayload), &payload); e != nil {
		t.Fatal(e)
	}
	if len(payload.Header) != 9 || len(payload.Values) != 9 || payload.Values[3] != "第一行\n第二行" {
		t.Fatal(payload)
	}
	balanceIs(t, s, a.ID, 10000)
	countTable(t, s, "money_transactions", 1)
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
			if _, replay, e := s.MoneyAction(ctx, "late-income", "INCOME", input); e != nil || !replay {
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
