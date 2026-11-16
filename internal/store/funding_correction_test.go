package store

import (
	"context"
	"errors"
	"testing"

	"stockroom/internal/ledger"
)

func TestFundingCorrectionPreservesEvidenceRefundsAndCheckpoint(t *testing.T) {
	ctx := context.Background()
	s := ledgerDB(t)
	a := moneyAccount(t, s, "wallet", "WALLET", 10000)
	b := moneyAccount(t, s, "bank", "BANK", 5000)
	at := "2026-11-01T04:00:00Z"
	amount := ledger.Money(2000)
	p := ledger.RawFinancialInput{Provider: "WECHAT", CaptureSource: "CSV_IMPORT", ExternalID: "lunch", Amount: &amount, Direction: "EXPENSE", OccurredAt: at, AccountID: a.ID, MerchantRaw: "便利店", RawText: "消费 20 元"}
	event, _, err := s.CaptureFinancial(ctx, "source", p)
	if err != nil || event.TransactionID == "" {
		t.Fatal(event, err)
	}
	var beforePayload string
	if err = s.db.QueryRow("SELECT payload FROM raw_financial_events WHERE id=?", event.ID).Scan(&beforePayload); err != nil {
		t.Fatal(err)
	}

	category := "餐饮"
	edited, _, err := s.EditTransactionDetails(ctx, "details", event.TransactionID, ledger.TransactionDetailsPatch{Category: &category})
	if err != nil || edited.Category != category || edited.Amount != amount || edited.Merchant != p.MerchantRaw {
		t.Fatal(edited, err)
	}
	balanceIs(t, s, a.ID, 8000)
	refund := moneyAction(t, s, "return", "REFUND", ledger.MoneyInput{AccountID: a.ID, Amount: 500, RelatedTransactionID: event.TransactionID, OccurredAt: "2026-11-01T08:00:00Z"}).Transactions[0]
	actual := ledger.SignedMoney(8500)
	moneyAction(t, s, "checkpoint", "ADJUSTMENT", ledger.MoneyInput{AccountID: a.ID, ActualBalance: &actual, OccurredAt: "2026-11-02T00:00:00Z"})
	stale := fundingVersion(edited.MoneyTransaction)
	tooSmall := ledger.Money(499)
	_, _, err = s.CorrectFunding(ctx, "too-small", event.TransactionID, ledger.FundingCorrection{Expected: stale, Amount: &tooSmall})
	if !errors.Is(err, ledger.ErrConflict) {
		t.Fatal(err)
	}
	countTable(t, s, "financial_corrections", 0)
	balanceIs(t, s, a.ID, 8500)

	correctedAmount := ledger.Money(1700)
	change := ledger.FundingCorrection{Expected: stale, Amount: &correctedAmount, AccountID: &b.ID}
	corrected, replay, err := s.CorrectFunding(ctx, "correction", event.TransactionID, change)
	if err != nil || replay || corrected.AccountID != b.ID || corrected.Amount != correctedAmount || corrected.ID != event.TransactionID {
		t.Fatal(corrected, replay, err)
	}
	balanceIs(t, s, a.ID, 8500)
	balanceIs(t, s, b.ID, 3300)
	checkpoints, err := s.AccountReconciliation(ctx, a.ID)
	if err != nil || len(checkpoints) != 1 || checkpoints[0].UnexplainedAmount != -2000 {
		t.Fatal(checkpoints, err)
	}
	_, replay, err = s.CorrectFunding(ctx, "correction", event.TransactionID, change)
	if err != nil || !replay {
		t.Fatal(replay, err)
	}
	if _, _, err = s.CorrectFunding(ctx, "stale", event.TransactionID, change); !errors.Is(err, ledger.ErrConflict) {
		t.Fatal(err)
	}
	countTable(t, s, "financial_corrections", 1)
	var afterPayload, original string
	if err = s.db.QueryRow("SELECT payload FROM raw_financial_events WHERE id=?", event.ID).Scan(&afterPayload); err != nil || beforePayload != afterPayload {
		t.Fatal("immutable evidence was changed", err)
	}
	if err = s.db.QueryRow("SELECT related_transaction_id FROM money_transactions WHERE id=?", refund.ID).Scan(&original); err != nil || original != event.TransactionID {
		t.Fatal(original, err)
	}
	again, _, err := s.CaptureFinancial(ctx, "same-source-new-key", p)
	if err != nil || again.TransactionID != event.TransactionID {
		t.Fatal(again, err)
	}
	p.AccountID = b.ID
	again, _, err = s.CaptureFinancial(ctx, "same-source-corrected-account", p)
	if err != nil || again.TransactionID != event.TransactionID {
		t.Fatal(again, err)
	}
	balanceIs(t, s, b.ID, 3300)
	var discontinuities int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM (SELECT before_cents,LAG(after_cents) OVER (PARTITION BY account_id ORDER BY rowid) AS prior FROM money_transactions) WHERE before_cents!=prior`).Scan(&discontinuities); err != nil || discontinuities != 0 {
		t.Fatal(discontinuities, err)
	}
	reconcileAll(t, s)
}

func TestFundingCorrectionChangesDirectionOnce(t *testing.T) {
	ctx := context.Background()
	s := ledgerDB(t)
	a := moneyAccount(t, s, "wallet", "WALLET", 10000)
	original := moneyAction(t, s, "mistaken-income", "INCOME", ledger.MoneyInput{AccountID: a.ID, Amount: 1600, Note: "晚饭"}).Transactions[0]
	expense := "EXPENSE"
	request := ledger.FundingCorrection{Expected: fundingVersion(original), Direction: &expense}
	corrected, replay, err := s.CorrectFunding(ctx, "expense", original.ID, request)
	if err != nil || replay || corrected.Type != expense || corrected.BalanceDelta != -1600 || corrected.Note != original.Note {
		t.Fatal(corrected, replay, err)
	}
	balanceIs(t, s, a.ID, 8400)
	if _, replay, err = s.CorrectFunding(ctx, "expense", original.ID, request); err != nil || !replay {
		t.Fatal(replay, err)
	}
	balanceIs(t, s, a.ID, 8400)
	countTable(t, s, "financial_corrections", 1)
	reconcileAll(t, s)
}
