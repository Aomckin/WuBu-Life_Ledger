package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"stockroom/internal/ledger"
)

type MoneyImportRow struct {
	Type string `json:"type"`
	ledger.MoneyInput
}
type MoneyImportInput struct {
	AccountID string `json:"account_id"`
	Adapter   string `json:"adapter"`
	CSV       string `json:"csv"`
}
type MoneyImportResult struct {
	Rows         []MoneyImportRow          `json:"rows"`
	Imported     int                       `json:"imported"`
	Duplicates   int                       `json:"duplicates"`
	Skipped      int                       `json:"skipped"`
	Transactions []ledger.MoneyTransaction `json:"transactions"`
}

// ImportAdapter only interprets files. It never writes accounts, stock or purchases.
type ImportAdapter interface {
	Parse(string) ([]MoneyImportRow, int, error)
}
type CSVAdapter struct{ Name string }

func (a CSVAdapter) Parse(raw string) ([]MoneyImportRow, int, error) {
	if len(raw) > 900000 {
		return nil, 0, ledger.ErrInvalid
	}
	raw = strings.TrimPrefix(raw, "\ufeff")
	lines := strings.Split(raw, "\n")
	start := -1
	for i, line := range lines {
		if strings.Contains(line, "occurred_at") || strings.Contains(line, "交易时间") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, 0, fmt.Errorf("%w: CSV header requires occurred_at or 交易时间", ledger.ErrInvalid)
	}
	reader := csv.NewReader(strings.NewReader(strings.Join(lines[start:], "\n")))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	header, e := reader.Read()
	if e != nil {
		return nil, 0, ledger.ErrInvalid
	}
	indexes := map[string]int{}
	for i, v := range header {
		indexes[strings.TrimSpace(strings.Trim(v, "\ufeff"))] = i
	}
	get := func(values []string, names ...string) string {
		for _, name := range names {
			if i, ok := indexes[name]; ok && i < len(values) {
				return strings.TrimSpace(values[i])
			}
		}
		return ""
	}
	out := []MoneyImportRow{}
	skipped := 0
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	for rowNumber := 2; ; rowNumber++ {
		values, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, skipped, fmt.Errorf("%w: CSV row %d: %v", ledger.ErrInvalid, rowNumber, e)
		}
		if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(values[0]), "---") {
			break
		}
		status := get(values, "交易状态", "当前状态", "status")
		if status == "交易关闭" || status == "支付失败" || status == "已关闭" || status == "等待付款" {
			skipped++
			continue
		}
		at := get(values, "occurred_at", "交易时间")
		parsed, e := time.Parse(time.RFC3339Nano, at)
		if e != nil {
			parsed, e = time.ParseInLocation("2006-01-02 15:04:05", at, zone)
		}
		if e != nil {
			return nil, skipped, fmt.Errorf("%w: row %d invalid time", ledger.ErrInvalid, rowNumber)
		}
		amountText := get(values, "amount", "金额(元)", "金额（元）", "金额")
		amountText = strings.TrimSpace(strings.TrimLeft(amountText, "¥￥"))
		var amount ledger.Money
		if e = json.Unmarshal([]byte(amountText), &amount); e != nil || amount <= 0 {
			return nil, skipped, fmt.Errorf("%w: row %d invalid amount", ledger.ErrInvalid, rowNumber)
		}
		kind := strings.ToUpper(get(values, "type"))
		direction := get(values, "收/支", "收支")
		if kind == "" {
			switch direction {
			case "支出":
				kind = "EXPENSE"
			case "收入":
				kind = "INCOME"
			default:
				return nil, skipped, fmt.Errorf("%w: row %d transfer/non-payment needs explicit type and accounts in generic CSV", ledger.ErrInvalid, rowNumber)
			}
		}
		switch kind {
		case "EXPENSE", "INCOME", "REFUND", "TRANSFER":
		default:
			return nil, skipped, fmt.Errorf("%w: unsupported import type at row %d", ledger.ErrInvalid, rowNumber)
		}
		// A provider export may label refunds as income. Link them explicitly before posting.
		description := get(values, "交易类型", "交易分类")
		if strings.Contains(description, "退款") {
			kind = "REFUND"
		}
		p := ledger.MoneyInput{Amount: amount, OccurredAt: parsed.UTC().Format(time.RFC3339Nano), Merchant: get(values, "merchant", "交易对方"), Category: get(values, "category", "交易分类"), Note: get(values, "note", "备注", "商品", "商品说明"), ExternalID: get(values, "external_id", "交易单号", "交易订单号"), RelatedTransactionID: get(values, "related_transaction_id"), FromAccount: get(values, "from_account"), ToAccount: get(values, "to_account")}
		if strings.Contains(status, "退款") && kind == "EXPENSE" {
			return nil, skipped, fmt.Errorf("%w: row %d has refunded status; use generic EXPENSE and linked REFUND rows to preserve actual cash flow", ledger.ErrInvalid, rowNumber)
		}
		out = append(out, MoneyImportRow{Type: kind, MoneyInput: p})
		if len(out) > 500 {
			return nil, skipped, fmt.Errorf("%w: import at most 500 rows per batch", ledger.ErrInvalid)
		}
	}
	return out, skipped, nil
}
func importRows(ctx context.Context, tx *sql.Tx, p MoneyImportInput) (ledger.Account, []MoneyImportRow, int, error) {
	var empty ledger.Account
	if p.AccountID == "" {
		return empty, nil, 0, fmt.Errorf("%w: import requires explicit account_id", ledger.ErrInvalid)
	}
	switch p.Adapter {
	case "generic", "alipay", "wechat":
	default:
		return empty, nil, 0, fmt.Errorf("%w: adapter must be generic/alipay/wechat", ledger.ErrInvalid)
	}
	rows, skipped, e := (CSVAdapter{Name: p.Adapter}).Parse(p.CSV)
	if e != nil {
		return empty, nil, skipped, e
	}
	account, e := accountTx(ctx, tx, p.AccountID)
	if e != nil {
		return account, nil, skipped, e
	}
	for i := range rows {
		r := &rows[i]
		r.AccountID = account.ID
		r.Source = "import"
		if r.ExternalID == "" {
			fingerprint := fmt.Sprintf("%s|%s|%s|%d|%s", account.ID, r.Type, r.OccurredAt, r.Amount, r.Merchant)
			r.ExternalID = fmt.Sprintf("fallback:%x", sha256.Sum256([]byte(fingerprint)))
		} else {
			r.ExternalID = p.Adapter + ":" + r.ExternalID
		}
		if len(r.ExternalID) > 200 {
			return account, nil, skipped, ledger.ErrInvalid
		}
		if r.Type == "TRANSFER" {
			if r.FromAccount == "" {
				r.FromAccount = account.ID
			}
			a, e := accountTx(ctx, tx, r.FromAccount)
			if e != nil {
				return account, nil, skipped, e
			}
			b, e := accountTx(ctx, tx, r.ToAccount)
			if e != nil {
				return account, nil, skipped, e
			}
			if r.ToAccount == "" || a.ID == b.ID || a.ID != account.ID {
				return account, nil, skipped, fmt.Errorf("%w: imported transfer needs current account as from_account and a different to_account", ledger.ErrInvalid)
			}
			r.FromAccount = a.ID
			r.ToAccount = b.ID
		}
		if r.Type == "REFUND" {
			if r.RelatedTransactionID == "" {
				return account, nil, skipped, fmt.Errorf("%w: imported refund requires related_transaction_id", ledger.ErrInvalid)
			}
			if _, e := refundOriginal(ctx, tx, r.MoneyInput); e != nil {
				return account, nil, skipped, e
			}
		}
	}
	return account, rows, skipped, nil
}
func (s *Store) PreviewMoneyImport(ctx context.Context, p MoneyImportInput) (MoneyImportResult, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return MoneyImportResult{}, e
	}
	defer tx.Rollback()
	_, rows, skipped, e := importRows(ctx, tx, p)
	out := MoneyImportResult{Rows: rows, Skipped: skipped, Transactions: []ledger.MoneyTransaction{}}
	if e != nil {
		return out, e
	}
	seen := map[string]bool{}
	for _, r := range rows {
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM money_transactions WHERE source='import' AND account_id=? AND external_id=?`, r.AccountID, r.ExternalID).Scan(&count); e != nil {
			return out, e
		}
		if count > 0 || seen[r.ExternalID] {
			out.Duplicates++
		}
		seen[r.ExternalID] = true
	}
	return out, nil
}
func (s *Store) ImportMoney(ctx context.Context, key string, p MoneyImportInput) (MoneyImportResult, bool, error) {
	var out MoneyImportResult
	replay, e := s.ledgerTx(ctx, key, "money_import", p, &out, func(tx *sql.Tx) error {
		_, rows, skipped, e := importRows(ctx, tx, p)
		if e != nil {
			return e
		}
		out = MoneyImportResult{Rows: rows, Skipped: skipped, Transactions: []ledger.MoneyTransaction{}}
		for _, r := range rows {
			var count int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM money_transactions WHERE source='import' AND account_id=? AND external_id=?`, r.AccountID, r.ExternalID).Scan(&count); e != nil {
				return e
			}
			if count > 0 {
				out.Duplicates++
				continue
			}
			input := r.MoneyInput
			delta := ledger.SignedMoney(r.Amount)
			if r.Type == "EXPENSE" || r.Type == "TRANSFER" {
				delta = -delta
			}
			kind := r.Type
			if kind == "REFUND" {
				original, e := refundOriginal(ctx, tx, input)
				if e != nil {
					return e
				}
				var refunded ledger.Money
				if e = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_cents),0) FROM money_transactions WHERE type='REFUND' AND related_transaction_id=?`, original.ID).Scan(&refunded); e != nil {
					return e
				}
				if refunded+r.Amount > original.Amount {
					return ledger.ErrConflict
				}
				input.PurchaseEventID = original.PurchaseEventID
				input.Category = original.Category
				input.Merchant = original.Merchant
			}
			if kind == "TRANSFER" {
				kind = "TRANSFER_OUT"
			}
			t, e := postMoney(ctx, tx, kind, input, "", delta)
			if e != nil {
				return e
			}
			out.Transactions = append(out.Transactions, t)
			if kind == "TRANSFER_OUT" {
				input.AccountID = r.ToAccount
				input.RelatedTransactionID = t.ID
				incoming, e := postMoney(ctx, tx, "TRANSFER_IN", input, "", -delta)
				if e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, `UPDATE money_transactions SET related_transaction_id=? WHERE id=?`, incoming.ID, t.ID); e != nil {
					return e
				}
				out.Transactions[len(out.Transactions)-1].RelatedTransactionID = incoming.ID
				out.Transactions = append(out.Transactions, incoming)
			}
			if kind == "REFUND" && input.PurchaseEventID != "" {
				if _, e = tx.ExecContext(ctx, `UPDATE purchase_events SET updated_at=? WHERE id=?`, nextPurchaseVersion(t.CreatedAt), input.PurchaseEventID); e != nil {
					return e
				}
			}
			out.Imported++
		}
		return nil
	})
	return out, replay, e
}
