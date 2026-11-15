package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"stockroom/internal/ledger"
	"testing"
)

func nativeWechat(rows string) []byte {
	return []byte("交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注\r\n" + rows)
}
func commitNative(t *testing.T, s *Store, key, code string, p ledger.StatementInput) ledger.StatementResult {
	t.Helper()
	preview, e := s.PreviewStatement(context.Background(), code, p)
	if e != nil {
		t.Fatal(e)
	}
	p.PreviewToken = preview.PreviewToken
	out, replay, e := s.CommitStatement(context.Background(), key, code, p)
	if e != nil || replay {
		t.Fatal(out, replay, e)
	}
	if preview.Batch.NewTransactions != out.Batch.NewTransactions || preview.Batch.MatchedExisting != out.Batch.MatchedExisting || preview.Batch.Duplicates != out.Batch.Duplicates || preview.Batch.Conflicts != out.Batch.Conflicts {
		t.Fatal("preview and commit differ", preview, out)
	}
	return out
}
func TestNativeStatementWeeklyCasesAndReplay(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "native", "WALLET", 10000)
	moneyAction(t, s, "manual", "EXPENSE", ledger.MoneyInput{AccountID: a.ID, Amount: 2000, Merchant: "午饭", Source: "agent", OccurredAt: "2026-11-01T12:05:00+08:00"})
	p := ledger.StatementInput{AccountID: a.ID, Filename: "微信账单.csv", SyncID: "week-one", File: nativeWechat("2026-11-01 12:03:00,商户消费,龙记,,支出,20.00,零钱,支付成功,lunch,,\r\n2026-11-01 18:00:00,商户消费,爱我便利店,,支出,3.00,零钱,支付成功,first,,\r\n2026-11-01 18:02:00,商户消费,爱我便利店,,支出,3.00,零钱,支付成功,second,,\r\n2026-11-01 19:00:00,商户消费,爱我便利店,,支出,7.00,零钱,支付成功,unknown,,\r\n")}
	preview, e := s.PreviewStatement(ctx, "wechat", p)
	if e != nil {
		t.Fatal(e)
	}
	if preview.Batch.NewTransactions != 3 || preview.Batch.MatchedExisting != 1 || preview.Batch.Conflicts != 0 {
		t.Fatal(preview)
	}
	countTable(t, s, "import_batches", 0)
	countTable(t, s, "raw_financial_events", 1)
	balanceIs(t, s, a.ID, 8000)
	p.PreviewToken = preview.PreviewToken
	out, replay, e := s.CommitStatement(ctx, "native", "wechat", p)
	if e != nil || replay {
		t.Fatal(out, replay, e)
	}
	balanceIs(t, s, a.ID, 6700)
	replayed, replay, e := s.CommitStatement(ctx, "native", "wechat", p)
	if e != nil || !replay || replayed.Batch.ID != out.Batch.ID {
		t.Fatal(replayed, replay, e)
	}
	originalName, original, e := s.StatementFile(ctx, out.Batch.ID)
	if e != nil || originalName != p.Filename || !bytes.Equal(original, p.File) {
		t.Fatal("original file changed", e)
	}
	again := commitNative(t, s, "native-repeat", "wechat", p)
	if !again.AlreadyImported || again.Batch.NewTransactions != 0 || again.Batch.Duplicates != 4 {
		t.Fatal(again)
	}
	balanceIs(t, s, a.ID, 6700)
	countTable(t, s, "import_batches", 2)
	countTable(t, s, "raw_financial_events", 5)
	un, e := s.FinancialTransactions(ctx, "2026-11-01T12:00:00+08:00", 1440, true)
	if e != nil || len(un) != 4 {
		t.Fatal(un, e)
	}
	summary, e := s.StatementSummary(ctx, "week-one")
	if e != nil || summary.Totals.NewTransactions != 3 || summary.Totals.MatchedExisting != 1 || summary.Totals.Unannotated != 4 {
		t.Fatal(summary, e)
	}
}
func TestNativeBadRowsMappingsAndAtomicity(t *testing.T) {
	s := ledgerDB(t)
	a := moneyAccount(t, s, "mapped", "WALLET", 10000)
	p := ledger.StatementInput{Filename: "wechat.csv", SyncID: "map", File: nativeWechat("2026-11-01 12:03:00,商户消费,商家,,支出,20.00,零钱,支付成功,one,,\n2026-11-01 12:04:00,商户消费,商家,,支出,bad,零钱,支付成功,two,,\n2026-11-01 12:05:00,商户消费,商家,,支出,7.00,零钱,支付成功,three,,\n")}
	preview, e := s.PreviewStatement(context.Background(), "wechat", p)
	if e != nil || preview.Batch.Conflicts != 3 {
		t.Fatal(preview, e)
	}
	p.PaymentAccounts = map[string]string{"零钱": a.ID}
	out := commitNative(t, s, "mapped", "wechat", p)
	if out.Batch.NewTransactions != 2 || out.Batch.Conflicts != 1 || len(out.Rows) != 3 {
		t.Fatal(out)
	}
	balanceIs(t, s, a.ID, 7300)
	countTable(t, s, "raw_financial_events", 3)
	// Every write, including the batch's original file and first debit, rolls back together.
	p.File = nativeWechat("2026-11-01 13:03:00,商户消费,商家,,支出,9.00,零钱,支付成功,rollback,,\n")
	preview, e = s.PreviewStatement(context.Background(), "wechat", p)
	if e != nil {
		t.Fatal(e)
	}
	p.PreviewToken = preview.PreviewToken
	if _, e = s.db.Exec(`CREATE TRIGGER batch_row_fail BEFORE INSERT ON import_batch_rows BEGIN SELECT RAISE(ABORT,'test');END`); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.CommitStatement(context.Background(), "failure", "wechat", p); e == nil {
		t.Fatal("expected rollback")
	}
	balanceIs(t, s, a.ID, 7300)
	countTable(t, s, "import_batches", 1)
	countTable(t, s, "raw_financial_events", 3)
}
func TestNativePreviewStaleAndBankOriginal(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "bank", "BANK", 10000)
	data, e := os.ReadFile(filepath.Join("..", "statement", "testdata", "abc_debit", "example-abc_debit-records.csv"))
	if e != nil {
		t.Fatal(e)
	}
	p := ledger.StatementInput{Filename: "银行原生.csv", File: data, AccountID: a.ID, SyncID: "bank-week"}
	pre, e := s.PreviewStatement(ctx, "abc_debit", p)
	if e != nil {
		t.Fatal(e)
	}
	p.PreviewToken = pre.PreviewToken
	moneyAction(t, s, "changed", "INCOME", ledger.MoneyInput{AccountID: a.ID, Amount: 100})
	if _, _, e = s.CommitStatement(ctx, "stale", "abc_debit", p); e == nil {
		t.Fatal("stale preview committed")
	}
	out := commitNative(t, s, "bank", "abc_debit", p)
	if out.Batch.NewTransactions == 0 || out.Batch.RowCount != 5 {
		t.Fatal(out)
	}
	before, e := s.GetAccount(ctx, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	again := commitNative(t, s, "bank-repeat", "abc_debit", p)
	if again.Batch.NewTransactions != 0 {
		t.Fatal(again)
	}
	balanceIs(t, s, a.ID, int64(before.CurrentBalance))
	// Different banks sharing a native ID must produce different facts.
	amount := ledger.Money(1000)
	for i, code := range []string{"abc_debit", "boc"} {
		raw, _, e := s.CaptureFinancial(ctx, fmt.Sprintf("bank-code-%d", i), ledger.RawFinancialInput{Provider: "BANK", ProviderCode: code, CaptureSource: "CSV_IMPORT", AccountID: a.ID, Amount: &amount, Direction: "EXPENSE", OccurredAt: "2026-11-02T12:00:00+08:00", ExternalID: "shared", MerchantRaw: "商家"})
		if e != nil || raw.Decision != "CREATE_NEW" {
			t.Fatal(raw, e)
		}
	}
}
func TestNativeNoIDRowsAreNotFingerprintMerged(t *testing.T) {
	s := ledgerDB(t)
	a := moneyAccount(t, s, "no-id", "WALLET", 10000)
	row := "2026-11-01 18:00:00,商户消费,同价店,,支出,3.00,零钱,支付成功,,,\n"
	p := ledger.StatementInput{AccountID: a.ID, Filename: "无单号.csv", File: nativeWechat(row + row)}
	out := commitNative(t, s, "no-id", "wechat", p)
	if out.Batch.NewTransactions != 2 {
		b, _ := json.Marshal(out)
		t.Fatal(string(b))
	}
	balanceIs(t, s, a.ID, 9400)
	again := commitNative(t, s, "no-id-repeat", "wechat", p)
	if again.Batch.NewTransactions != 0 || again.Batch.Duplicates != 2 {
		t.Fatal(again)
	}
}
