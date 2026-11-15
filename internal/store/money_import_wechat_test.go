package store

import (
	"context"
	"stockroom/internal/ledger"
	"testing"
)

func TestWechatImportReviewRowsAndReplay(t *testing.T) {
	s := ledgerDB(t)
	ctx := context.Background()
	a := moneyAccount(t, s, "测试钱包", "WALLET", 0)
	csv := "微信支付账单明细\n交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注\n" +
		"2026/11/1 9:30,商户消费,测试商家,商品,支出,12.50,零钱,支付成功,000001,,\n" +
		"2026-11-01 10:00:01,转账,测试人,转账,收入,20.00,零钱,已存入零钱,000002,,\n" +
		"2026-11-01 11:00:00,商户消费,测试商家,商品,支出,10.00,零钱,已全额退款,000003,,\n" +
		"2026-11-01 11:10:00,转账-退款,测试人,退款,收入,10.00,零钱,已全额退款,000004,,\n" +
		"2026-11-01 12:00:00,零钱提现,测试银行,提现,/,10.00,零钱,提现已到账,000005,,\n"
	p := MoneyImportInput{AccountID: a.ID, Adapter: "wechat", CSV: csv}
	preview, e := s.PreviewMoneyImport(ctx, p)
	if e != nil || len(preview.Rows) != 2 || preview.Skipped != 3 || len(preview.SkippedRows) != 3 || preview.SkippedRows[0].Row != 5 {
		t.Fatal(preview, e)
	}
	balanceIs(t, s, a.ID, 0)
	countTable(t, s, "money_transactions", 1)
	result, _, e := s.ImportMoney(ctx, "wechat-first", p)
	if e != nil || result.Imported != 2 || result.Skipped != 3 {
		t.Fatal(result, e)
	}
	balanceIs(t, s, a.ID, 750)
	result, _, e = s.ImportMoney(ctx, "wechat-repeat", p)
	if e != nil || result.Imported != 0 || result.Duplicates != 2 {
		t.Fatal(result, e)
	}
	balanceIs(t, s, a.ID, 750)
	reconcileAll(t, s)
}
func TestWechatImportAmountsAndDateFormats(t *testing.T) {
	for _, date := range []string{"2026/11/1 9:30", "2026/11/1 09:30:05", "2026-11-01 09:30:05", "2026-11-01T09:30:05+08:00"} {
		rows, _, e := (CSVAdapter{Name: "wechat"}).Parse("交易时间,收/支,金额(元),当前状态\n" + date + ",支出,\"￥1,234.50\",支付成功\n")
		if e != nil || len(rows) != 1 || rows[0].Amount != ledger.Money(123450) {
			t.Fatal(date, rows, e)
		}
	}
	if _, _, e := (CSVAdapter{Name: "wechat"}).Parse("交易时间,收/支,金额(元),当前状态\n2026/11/1 9:30,支出,\"1,2\",支付成功\n"); e == nil {
		t.Fatal("invalid grouping accepted")
	}
}
