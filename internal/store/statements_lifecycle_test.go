package store

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/deb-sig/double-entry-generator/v2/pkg/ir"
	"path/filepath"
	"stockroom/internal/ledger"
	"stockroom/internal/statement"
	"strings"
	"testing"
	"time"
)

func TestWeeklySummaryDoesNotTruncateToHistoryPage(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	// Historical immutable batch receipts may outlive their displayed page.
	for i := 0; i < 501; i++ {
		b := ledger.ImportBatch{ID: fmt.Sprintf("history-%d", i), ProviderCode: "wechat", RowCount: 1, NewTransactions: 1, SyncID: "long-sync"}
		data, e := json.Marshal(b)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.db.Exec(`INSERT INTO import_batches VALUES(?,?,?,?,?,?,?,?,?)`, b.ID, "wechat", b.ID, "binding", b.SyncID, "history.csv", []byte("source"), string(data), "2026-11-01T00:00:00Z"); e != nil {
			t.Fatal(e)
		}
	}
	page, e := s.StatementBatches(ctx, "long-sync", 500, 0)
	if e != nil || len(page) != 500 {
		t.Fatal(len(page), e)
	}
	summary, e := s.StatementSummary(ctx, "long-sync")
	if e != nil || summary.Totals.NewTransactions != 501 || len(summary.Batches) != 501 {
		t.Fatal(summary.Totals, e)
	}
}

func TestNativeBankCandidatesAndDatePrecision(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "cross-source", "BANK", 10000)
	amount := ledger.Money(2000)
	first, _, e := s.CaptureFinancial(ctx, "wechat-source", ledger.RawFinancialInput{Provider: "WECHAT", ProviderCode: "wechat", CaptureSource: "CSV_IMPORT", AccountID: a.ID, Amount: &amount, Direction: "EXPENSE", OccurredAt: "2026-11-01T12:03:00+08:00", MerchantRaw: "龙记", ExternalID: "wx-one"})
	if e != nil {
		t.Fatal(e)
	}
	bank, _, e := s.CaptureFinancial(ctx, "bank-source", ledger.RawFinancialInput{Provider: "BANK", ProviderCode: "abc_debit", CaptureSource: "CSV_IMPORT", AccountID: a.ID, Amount: &amount, Direction: "EXPENSE", OccurredAt: "2026-11-01T12:03:03+08:00", MerchantRaw: "财付通银行摘要", ExternalID: "bank-one", TimePrecision: "SECOND"})
	if e != nil || bank.Status != "NEEDS_REVIEW" || len(bank.Candidates) != 1 || bank.Candidates[0] != first.TransactionID {
		t.Fatal(bank, e)
	}
	balanceIs(t, s, a.ID, 8000)
	_, _, e = s.ReviewFinancial(ctx, "confirmed-bank-link", bank.ID, ledger.FinancialReview{Action: "MATCH", TransactionID: first.TransactionID, AccountID: a.ID, Reason: "已核对原账单确认同一笔支付"})
	if e != nil {
		t.Fatal(e)
	}
	balanceIs(t, s, a.ID, 8000)
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	for _, test := range []struct {
		code            string
		at              time.Time
		header, values  []string
		want, precision string
	}{
		{"ccb", time.Date(2026, 11, 1, 0, 0, 0, 0, zone), []string{"交易时间"}, []string{"12:34:56"}, "2026-11-01T04:34:56Z", "SECOND"},
		{"boc", time.Date(2026, 11, 1, 20, 0, 0, 0, zone), []string{"交易日"}, []string{"2026-11-01"}, "2026-10-31T16:00:00Z", "DAY"},
		{"hsbchk", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), []string{"Transaction date"}, []string{"01/11/2026"}, "2026-10-31T16:00:00Z", "DAY"},
	} {
		at, precision := statementTime(test.code, &ir.Order{PayTime: test.at}, statement.Row{Header: test.header, Values: test.values})
		if at != test.want || precision != test.precision {
			t.Fatal(test.code, at, precision)
		}
	}
}

func TestNativeThirtyRowsReplayAndClosedEvidence(t *testing.T) {
	s := ledgerDB(t)
	a := moneyAccount(t, s, "thirty-native", "WALLET", 10000)
	var rows strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&rows, "2026-11-01 18:%02d:00,商户消费,同价店,,支出,1.00,零钱,支付成功,thirty-%d,,\n", i, i)
	}
	p := ledger.StatementInput{AccountID: a.ID, Filename: "原生30条.csv", File: nativeWechat(rows.String())}
	first := commitNative(t, s, "thirty-first", "wechat", p)
	if first.Batch.NewTransactions != 30 {
		t.Fatal(first)
	}
	again := commitNative(t, s, "thirty-again", "wechat", p)
	if again.Batch.NewTransactions != 0 || again.Batch.Duplicates != 30 {
		t.Fatal(again)
	}
	balanceIs(t, s, a.ID, 7000)
	p.File = nativeWechat("2026-11-01 21:03:00,商户消费,未完成,,支出,9.00,零钱,支付失败,closed,,\n")
	closed := commitNative(t, s, "closed-native", "wechat", p)
	if closed.Batch.Skipped != 1 || closed.Batch.Conflicts != 0 {
		t.Fatal(closed)
	}
	balanceIs(t, s, a.ID, 7000)
}

func TestNativeConfirmedMerchantRuleAndSemanticMirror(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "rule-native", "WALLET", 10000)
	category, nature := "餐饮", "DAILY"
	_, _, e := s.SaveMerchantRule(ctx, "confirmed-rule", "", ledger.MerchantRule{MerchantRaw: "麦当劳法定公司", MerchantNormalized: "麦当劳", UserConfirmed: true, Annotation: ledger.TransactionAnnotation{Category: &category, ExpenseNature: &nature, Tags: []string{"外食"}}})
	if e != nil {
		t.Fatal(e)
	}
	p := ledger.StatementInput{AccountID: a.ID, Filename: "微信.csv", File: nativeWechat("2026-11-01 12:03:00,商户消费,麦当劳法定公司,,支出,20.00,零钱,支付成功,rule-one,,\n")}
	out := commitNative(t, s, "rule-native", "wechat", p)
	if out.Batch.Unannotated != 0 {
		t.Fatal(out)
	}
	balanceIs(t, s, a.ID, 8000)
	rows, e := s.FinancialTransactions(ctx, "2026-11-01T12:03:00+08:00", 30, false)
	if e != nil || len(rows) != 1 || rows[0].Merchant != "麦当劳" || rows[0].Category != category || rows[0].ExpenseNature != nature || rows[0].Annotation.Category == nil || *rows[0].Annotation.ExpenseNature != nature || len(rows[0].Annotation.Tags) != 1 {
		t.Fatal(rows, e)
	}
	newCategory := "午餐"
	_, _, e = s.AnnotateTransaction(ctx, "rule-note", rows[0].ID, ledger.TransactionAnnotation{Category: &newCategory, ExpenseNature: &nature, AnnotatedBy: "manual", AnnotationStatus: "CONFIRMED", Confidence: 1})
	if e != nil {
		t.Fatal(e)
	}
	balanceIs(t, s, a.ID, 8000)
	raw, e := s.RawFinancialEvents(ctx, "")
	if e != nil || len(raw) != 1 || raw[0].MerchantRaw != "麦当劳法定公司" {
		t.Fatal(raw, e)
	}
}

func TestNativeRepaymentCanLinkTransferWithoutInventingIncome(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	from := moneyAccount(t, s, "repayment-bank", "BANK", 10000)
	to := moneyAccount(t, s, "repayment-card", "CREDIT_CARD", 0)
	p := ledger.StatementInput{AccountID: to.ID, Filename: "广发原生.csv", File: []byte("交易日期,入账日期,交易摘要,交易金额,交易货币,入账金额,入账货币\n2026/11/01,2026/11/01,（还款）银联入账,-20.00,人民币,-20.00,人民币\n")}
	out := commitNative(t, s, "repayment-source", "cgb_credit", p)
	if out.Batch.NewTransactions != 0 || out.Batch.Conflicts != 1 {
		t.Fatal(out)
	}
	balanceIs(t, s, from.ID, 10000)
	balanceIs(t, s, to.ID, 0)
	transfer := moneyAction(t, s, "confirmed-transfer", "TRANSFER", ledger.MoneyInput{FromAccount: from.ID, ToAccount: to.ID, Amount: 2000, OccurredAt: "2026-11-01T12:03:00+08:00"})
	var incoming string
	for _, m := range transfer.Transactions {
		if m.Type == "TRANSFER_IN" {
			incoming = m.ID
		}
	}
	if _, _, e := s.ReviewFinancial(ctx, "invalid-transfer-creation", out.Rows[0].RawEventID, ledger.FinancialReview{Action: "CREATE_NEW", Direction: "TRANSFER_IN", AccountID: to.ID, Reason: "仅关联已有转账"}); e == nil {
		t.Fatal("created standalone transfer leg")
	}
	linked, _, e := s.ReviewFinancial(ctx, "linked-transfer", out.Rows[0].RawEventID, ledger.FinancialReview{Action: "MATCH", Direction: "TRANSFER_IN", TransactionID: incoming, AccountID: to.ID, Reason: "确认还款，关联已核对的转账入账"})
	if e != nil || linked.TransactionID != incoming {
		t.Fatal(linked, e)
	}
	balanceIs(t, s, from.ID, 8000)
	balanceIs(t, s, to.ID, 2000)
	reconcileAll(t, s)
}

func TestNativeLateRefundAndOriginalIdentity(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "anchor-native", "WALLET", 132050)
	p := ledger.StatementInput{AccountID: a.ID, Filename: "微信支出.csv", File: nativeWechat("2026-11-01 12:03:00,商户消费,商家,,支出,22.00,零钱,支付成功,original-order,,\n")}
	out := commitNative(t, s, "original-order", "wechat", p)
	if out.Batch.NewTransactions != 1 {
		t.Fatal(out)
	}
	balanceIs(t, s, a.ID, 129850)
	actual := ledger.SignedMoney(132050)
	moneyAction(t, s, "native-checkpoint", "ADJUSTMENT", ledger.MoneyInput{AccountID: a.ID, ActualBalance: &actual, OccurredAt: "2026-11-03T00:00:00+08:00"})
	p.Filename = "微信退款.csv"
	p.File = nativeWechat("2026-11-02 12:03:00,退款,商家,,收入,22.00,零钱,退款成功,original-order,,\n")
	refund := commitNative(t, s, "native-refund", "wechat", p)
	if refund.Batch.NewTransactions != 1 || refund.Batch.Conflicts != 0 {
		t.Fatal(refund)
	}
	balanceIs(t, s, a.ID, 132050)
	checkpoints, e := s.AccountReconciliation(ctx, a.ID)
	if e != nil || checkpoints[0].UnexplainedAmount != 0 {
		t.Fatal(checkpoints, e)
	}
	again := commitNative(t, s, "native-refund-replay", "wechat", p)
	if again.Batch.NewTransactions != 0 || again.Batch.Duplicates != 1 {
		t.Fatal(again)
	}
	balanceIs(t, s, a.ID, 132050)
	reconcileAll(t, s)
}

func TestNativeFileCannotDebitDifferentAccountOnReimport(t *testing.T) {
	s := ledgerDB(t)
	a := moneyAccount(t, s, "first-native-account", "WALLET", 10000)
	b := moneyAccount(t, s, "other-native-account", "BANK", 10000)
	p := ledger.StatementInput{AccountID: a.ID, Filename: "微信.csv", File: nativeWechat("2026-11-01 12:03:00,商户消费,商家,,支出,20.00,零钱,支付成功,stable-file,,\n")}
	commitNative(t, s, "first-mapping", "wechat", p)
	p.AccountID = b.ID
	out := commitNative(t, s, "changed-mapping", "wechat", p)
	if !out.AlreadyImported || out.Batch.Conflicts != 1 || out.Batch.NewTransactions != 0 {
		t.Fatal(out)
	}
	balanceIs(t, s, a.ID, 8000)
	balanceIs(t, s, b.ID, 10000)
	countTable(t, s, "raw_financial_events", 1)
}

func TestNativeSchema13UpgradeAndRestartKeepsFileAndReplay(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "native-upgrade.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	a := moneyAccount(t, s, "upgrade-bank", "BANK", 10000)
	// Reproduce the v13 boundary while retaining all prior ledger facts.
	for _, table := range []string{"import_batch_rows", "import_batches"} {
		if _, e = s.db.Exec(`DROP TABLE ` + table); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.db.Exec(`PRAGMA user_version=13`); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	p := ledger.StatementInput{AccountID: a.ID, Filename: "原始微信.csv", File: nativeWechat("2026-11-01 12:03:00,商户消费,商家,,支出,20.00,零钱,支付成功,persisted,,\n")}
	preview, e := s.PreviewStatement(ctx, "wechat", p)
	if e != nil {
		t.Fatal(e)
	}
	p.PreviewToken = preview.PreviewToken
	original, _, e := s.CommitStatement(ctx, "persisted", "wechat", p)
	if e != nil {
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
		balanceIs(t, s, a.ID, 8000)
		out, replay, e := s.CommitStatement(ctx, "persisted", "wechat", p)
		if e != nil || !replay || out.Batch.ID != original.Batch.ID {
			t.Fatal(out, replay, e)
		}
		_, file, e := s.StatementFile(ctx, original.Batch.ID)
		if e != nil || string(file) != string(p.File) {
			t.Fatal(e)
		}
		if e = s.Close(); e != nil {
			t.Fatal(e)
		}
	}
}

func TestNativeMatchesLegacyManualFactWithoutInventingRaw(t *testing.T) {
	s := ledgerDB(t)
	a := moneyAccount(t, s, "legacy-manual", "WALLET", 10000)
	moneyAction(t, s, "old-manual", "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: 2000, Merchant: "午饭", Source: "agent", OccurredAt: "2026-11-01T12:05:00+08:00"})
	if _, e := s.db.Exec(`DELETE FROM financial_event_links; DELETE FROM raw_financial_events`); e != nil {
		t.Fatal(e)
	}
	p := ledger.StatementInput{AccountID: a.ID, Filename: "微信.csv", File: nativeWechat("2026-11-01 12:03:00,商户消费,龙记,,支出,20.00,零钱,支付成功,legacy-match,,\n")}
	out := commitNative(t, s, "legacy-match", "wechat", p)
	if out.Batch.NewTransactions != 0 || out.Batch.MatchedExisting != 1 {
		t.Fatal(out)
	}
	balanceIs(t, s, a.ID, 8000)
	countTable(t, s, "raw_financial_events", 1)
}
