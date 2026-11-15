package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/deb-sig/double-entry-generator/v2/pkg/ir"
	"github.com/google/uuid"
	"stockroom/internal/ledger"
	"stockroom/internal/statement"
)

func validateStatement(code string, p ledger.StatementInput) ([]statement.Row, error) {
	if p.Filename != filepath.Base(p.Filename) || len(p.Filename) > 200 || len(p.SyncID) > 100 || len(p.PaymentAccounts) > 100 {
		return nil, ledger.ErrInvalid
	}
	return statement.Parse(code, p.Filename, p.File)
}
func statementBinding(p ledger.StatementInput) string {
	accounts := map[string]string{}
	for method, account := range p.PaymentAccounts {
		if account != p.AccountID {
			accounts[method] = account
		}
	}
	b, _ := json.Marshal(struct {
		AccountID       string
		PaymentAccounts map[string]string
	}{p.AccountID, accounts})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func statementToken(ctx context.Context, tx *sql.Tx, code string, p ledger.StatementInput) (string, error) {
	var revision int64
	if e := tx.QueryRowContext(ctx, `SELECT revision FROM ledger_lock WHERE id=1`).Scan(&revision); e != nil {
		return "", e
	}
	p.PreviewToken = ""
	b, e := json.Marshal(struct {
		Code     string
		Revision int64
		Input    ledger.StatementInput
	}{code, revision, p})
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

func (s *Store) PreviewStatement(ctx context.Context, code string, p ledger.StatementInput) (ledger.StatementResult, error) {
	parsed, e := validateStatement(code, p)
	if e != nil {
		return ledger.StatementResult{}, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return ledger.StatementResult{}, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE ledger_lock SET revision=revision+1 WHERE id=1`); e != nil {
		return ledger.StatementResult{}, e
	}
	out, e := reconcileStatement(ctx, tx, code, p, parsed)
	if e != nil {
		return out, e
	}
	out.PreviewToken, e = statementToken(ctx, tx, code, p)
	// IDs from the rolled-back simulation are not usable resources.
	out.Batch.ID = ""
	for i := range out.Rows {
		out.Rows[i].RawEventID = ""
		if out.Rows[i].Status == "CREATE_NEW" {
			out.Rows[i].TransactionID = ""
		}
	}
	return out, e
}
func (s *Store) CommitStatement(ctx context.Context, key, code string, p ledger.StatementInput) (ledger.StatementResult, bool, error) {
	var out ledger.StatementResult
	if key == "" || p.PreviewToken == "" {
		return out, false, fmt.Errorf("%w: 请先预览再确认", ledger.ErrInvalid)
	}
	parsed, e := validateStatement(code, p)
	if e != nil {
		return out, false, e
	}
	replay, e := s.ledgerTx(ctx, key, "statement_import:"+code, p, &out, func(tx *sql.Tx) error {
		token, e := statementToken(ctx, tx, code, p)
		if e != nil {
			return e
		}
		if token != p.PreviewToken {
			return fmt.Errorf("%w: 账簿或文件已变化，请重新预览", ledger.ErrConflict)
		}
		out, e = reconcileStatement(ctx, tx, code, p, parsed)
		return e
	})
	return out, replay, e
}

func reconcileStatement(ctx context.Context, tx *sql.Tx, code string, p ledger.StatementInput, rows []statement.Row) (ledger.StatementResult, error) {
	out := ledger.StatementResult{Rows: []ledger.StatementRowResult{}}
	fileHash := fmt.Sprintf("%x", sha256.Sum256(p.File))
	binding := statementBinding(p)
	batch := ledger.ImportBatch{ID: uuid.NewString(), ProviderCode: code, Filename: p.Filename, FileHash: fileHash, ImportedAt: financialNow(), SyncID: p.SyncID, ParserVersion: statement.Version, RowCount: len(rows)}
	e := tx.QueryRowContext(ctx, `SELECT id FROM import_batches WHERE provider_code=? AND file_hash=? ORDER BY rowid DESC LIMIT 1`, code, fileHash).Scan(&out.PreviousBatchID)
	if e == nil {
		out.AlreadyImported = true
	} else if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	// Insert the file and its rows inside the same transaction as all money changes.
	if _, e = tx.ExecContext(ctx, `INSERT INTO import_batches VALUES(?,?,?,?,?,?,?,?,?)`, batch.ID, code, fileHash, binding, p.SyncID, p.Filename, p.File, "{}", batch.ImportedAt); e != nil {
		return out, e
	}
	for _, row := range rows {
		input, ignored := statementEvidence(code, batch.ID, fileHash, p, row)
		if len(input.RawText) > 32000 || len(input.RawPayload) > 128000 {
			manifest, _ := json.Marshal(map[string]any{"file_hash": fileHash, "filename": p.Filename, "row": row.Number, "parser_version": statement.Version})
			input.RawText = "该行过长，已完整保留在原始账单中，请下载核对"
			input.RawPayload = string(manifest)
			input.Amount = nil
			input.Direction = ""
			input.ReviewReason = "原始记录过长，需核对原始文件"
			ignored = false
		}
		if reason := []rune(input.ReviewReason); len(reason) > 512 {
			input.ReviewReason = string(reason[:512]) + "…（请查看原始账单）"
		}
		if input.TimePrecision == "DAY" && input.AccountID != "" && input.ReviewReason == "" {
			var checkpoints int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM balance_checkpoints WHERE account_id=? AND date(observed_at,'+8 hours')=date(?,'+8 hours')`, input.AccountID, input.OccurredAt).Scan(&checkpoints); e != nil {
				return out, e
			}
			if checkpoints > 0 {
				input.ReviewReason = "银行账单仅有日期，与余额检查点同日，需核对先后顺序"
			}
		}
		// A refund may reuse the original provider order number. Only an exact,
		// unique source identity authorizes linking it without a manual review.
		if input.Direction == "REFUND" && input.ReviewReason == "退款需核对原支出" && input.ExternalID != "" && input.AccountID != "" && input.Amount != nil && input.Currency == "CNY" {
			matches, e := tx.QueryContext(ctx, `SELECT DISTINCT m.id FROM money_transactions m JOIN financial_event_links l ON l.transaction_id=m.id JOIN raw_financial_events r ON r.id=l.raw_event_id WHERE m.type='EXPENSE' AND m.voided_at='' AND m.account_id=? AND json_extract(r.payload,'$.provider')=? AND COALESCE(json_extract(r.payload,'$.provider_code'),'') IN (?,'') AND json_extract(r.payload,'$.external_id')=?`, input.AccountID, input.Provider, input.ProviderCode, input.ExternalID)
			if e != nil {
				return out, e
			}
			ids := []string{}
			for matches.Next() {
				var id string
				if e = matches.Scan(&id); e != nil {
					matches.Close()
					return out, e
				}
				ids = append(ids, id)
			}
			e = matches.Err()
			matches.Close()
			if e != nil {
				return out, e
			}
			if len(ids) == 1 {
				var remaining ledger.Money
				if e = tx.QueryRowContext(ctx, `SELECT amount_cents-COALESCE((SELECT SUM(amount_cents) FROM money_transactions refunds WHERE refunds.type='REFUND' AND refunds.related_transaction_id=original.id AND refunds.voided_at=''),0) FROM money_transactions original WHERE id=?`, ids[0]).Scan(&remaining); e != nil {
					return out, e
				}
				if remaining >= *input.Amount {
					input.RelatedTransactionID = ids[0]
					input.ReviewReason = ""
				} else {
					input.ReviewReason = "退款金额超过剩余可退金额，请核对原流水"
				}
			}
		}
		var old int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM raw_financial_events WHERE json_extract(payload,'$.evidence_key')=?`, input.EvidenceKey).Scan(&old); e != nil {
			return out, e
		}
		event, e := captureFinancialTx(ctx, tx, input)
		if e != nil {
			return out, e
		}
		if ignored && old == 0 {
			event.Status = "IGNORED"
			event.Decision = input.ReviewReason
			if e = saveRawTx(ctx, tx, event); e != nil {
				return out, e
			}
		}
		result := ledger.StatementRowResult{Row: row.Number, Amount: event.Amount, Currency: input.Currency, Merchant: input.MerchantRaw, PaymentMethod: input.PaymentMethodRaw, AccountID: input.AccountID, OccurredAt: input.OccurredAt, TimePrecision: input.TimePrecision, Direction: input.Direction, RawEventID: event.ID, TransactionID: event.TransactionID, Reason: event.Decision}
		accountMismatch := false
		if old > 0 && event.TransactionID != "" {
			var account string
			if e = tx.QueryRowContext(ctx, `SELECT account_id FROM money_transactions WHERE id=?`, event.TransactionID).Scan(&account); e != nil {
				return out, e
			}
			result.AccountID = account
			accountMismatch = input.AccountID != "" && input.AccountID != account
		}
		switch {
		case accountMismatch:
			result.Status = "CONFLICT"
			result.Reason = "该原始记录已关联其他账户，请先纠正已有流水，不能重复扣款"
			batch.Conflicts++
		case event.Status == "IGNORED":
			result.Status = "SKIPPED"
			batch.Skipped++
		case event.Status == "NEEDS_REVIEW" || event.TransactionID == "":
			result.Status = "CONFLICT"
			batch.Conflicts++
		case old > 0:
			result.Status = "DUPLICATE"
			batch.Duplicates++
		case event.Decision == "CREATE_NEW":
			result.Status = "CREATE_NEW"
			batch.NewTransactions++
		default:
			var prior int
			e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM raw_financial_events WHERE transaction_id=? AND id!=? AND json_extract(payload,'$.capture_source')='CSV_IMPORT' AND json_extract(payload,'$.provider')=? AND COALESCE(json_extract(payload,'$.provider_code'),'') IN (?, '')`, event.TransactionID, event.ID, input.Provider, input.ProviderCode).Scan(&prior)
			if e != nil {
				return out, e
			}
			if prior > 0 {
				result.Status = "DUPLICATE"
				batch.Duplicates++
			} else {
				result.Status = "MATCHED_EXISTING"
				batch.MatchedExisting++
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO import_batch_rows VALUES(?,?,?)`, batch.ID, row.Number, event.ID); e != nil {
			return out, e
		}
		out.Rows = append(out.Rows, result)
	}
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT l.transaction_id) FROM import_batch_rows r JOIN financial_event_links l ON l.raw_event_id=r.raw_event_id JOIN money_transactions m ON m.id=l.transaction_id WHERE r.batch_id=? AND m.voided_at='' AND NOT EXISTS(SELECT 1 FROM transaction_annotations a WHERE a.transaction_id=l.transaction_id AND json_extract(a.payload,'$.annotation_status')!='UNANNOTATED')`, batch.ID).Scan(&batch.Unannotated); e != nil {
		return out, e
	}
	out.Batch = batch
	b, e := json.Marshal(batch)
	if e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, `UPDATE import_batches SET payload=? WHERE id=?`, string(b), batch.ID)
	return out, e
}

func statementEvidence(code, batchID, fileHash string, p ledger.StatementInput, row statement.Row) (ledger.RawFinancialInput, bool) {
	providerName := "BANK"
	if code == "wechat" {
		providerName = "WECHAT"
	} else if code == "alipay" {
		providerName = "ALIPAY"
	}
	input := ledger.RawFinancialInput{Provider: providerName, ProviderCode: code, CaptureSource: "CSV_IMPORT", ImportBatchID: batchID, RowNumber: row.Number, RawText: row.RawRecord, AccountID: p.AccountID, EvidenceKey: fmt.Sprintf("native:%s:%s:%d", code, fileHash, row.Number)}
	payload, payloadError := json.Marshal(map[string]any{"header": row.Header, "values": row.Values, "file_hash": fileHash, "filename": p.Filename, "parser_version": statement.Version, "upstream_ir": row.Order})
	if payloadError != nil {
		payload, _ = json.Marshal(map[string]any{"header": row.Header, "values": row.Values, "file_hash": fileHash, "filename": p.Filename, "parser_version": statement.Version})
		input.RawPayload = string(payload)
		input.ReviewReason = "上游产生非法金额，保留原始证据待核对"
		return input, false
	}
	input.RawPayload = string(payload)
	for i, h := range row.Header {
		if i < len(row.Values) && (strings.TrimSpace(h) == "交易状态" || strings.TrimSpace(h) == "当前状态") {
			status := strings.TrimSpace(row.Values[i])
			if status == "交易关闭" || status == "支付失败" || status == "已关闭" || status == "等待付款" {
				input.ReviewReason = "交易未完成，保留原始证据"
				return input, true
			}
		}
	}
	if row.Order == nil {
		input.ReviewReason = row.Error
		return input, false
	}
	o := row.Order
	input.MerchantRaw = o.Peer
	input.DescriptionRaw = o.Item
	input.PaymentMethodRaw = o.Method
	input.OccurredAt, input.TimePrecision = statementTime(code, o, row)
	input.Currency = statementCurrency(code, o, row)
	if o.OrderID != nil {
		input.ExternalID = *o.OrderID
	}
	if input.ExternalID == "" {
		input.ExternalID = o.Metadata["logNumber"]
	}

	if account, exists := p.PaymentAccounts[o.Method]; exists {
		input.AccountID = account
	}
	if o.Type == ir.TypeSend {
		input.Direction = "EXPENSE"
	} else if o.Type == ir.TypeRecv {
		input.Direction = "INCOME"
	}
	if math.IsNaN(o.Money) || math.IsInf(o.Money, 0) || o.Money <= 0 || o.Money > 1e10 {
		input.ReviewReason = "金额无效或为零"
	} else {
		var amount ledger.Money
		if e := json.Unmarshal([]byte(strconv.FormatFloat(o.Money, 'f', -1, 64)), &amount); e != nil || amount <= 0 {
			input.ReviewReason = "金额不是有效的整数分，需核对原始账单"
		} else {
			input.Amount = &amount
		}
	}
	if input.Direction == "" {
		input.ReviewReason = "未确定收入、支出或内部划转"
	}
	if input.Currency != "CNY" {
		input.ReviewReason = "外币原始证据待审核，不自动换汇"
		input.Amount = nil
	}
	txType := o.TxTypeOriginal
	if txType == "" {
		txType = o.Category
	}
	status := o.Metadata["status"]
	ignored := status == "交易关闭" || status == "支付失败" || status == "已关闭" || status == "等待付款"
	if ignored {
		input.ReviewReason = "交易未完成"
	}
	if strings.Contains(txType, "退款") || strings.Contains(txType, "退货") || (input.Direction == "INCOME" && strings.Contains(status, "退款")) {
		input.Direction = "REFUND"
		input.ReviewReason = "退款需核对原支出"
	}
	if (providerName != "BANK" && o.TypeOriginal != "支出" && o.TypeOriginal != "收入" && input.Direction != "REFUND") || txType == "零钱充值" || txType == "零钱提现" || txType == "还款" || txType == "信用借还" || txType == "投资理财" || strings.Contains(txType, "信用卡还款") || strings.Contains(txType, "转入零钱通") || strings.Contains(txType, "零钱通转出") {
		input.ReviewReason = "内部划转需明确双方账户"
	}
	creditStatement := code == "citic" || code == "cgb_credit" || code == "bocom_credit"
	if (code == "boc" || code == "cmb") && len(row.Header) > 0 && strings.TrimSpace(row.Header[0]) == "交易日" {
		creditStatement = true
	}
	for _, header := range row.Header {
		if code == "hsbchk" && header == "Credit / Debit" || code == "icbc" && header == "记账日期" || code == "bmo" && header == "Item #" {
			creditStatement = true
		}
	}
	if creditStatement && input.Direction == "INCOME" && input.ReviewReason == "" {
		input.ReviewReason = "信用卡入账需确认是还款、退款或真实收入"
	}
	if input.Direction == "EXPENSE" && strings.Contains(status, "退款") {
		input.ReviewReason = "原交易含退款状态，请核对实际支付与退款"
	}
	if input.AccountID == "" {
		input.ReviewReason = "请确认付款方式对应的资金账户"
	}
	if row.Error != "" {
		input.ReviewReason = row.Error
	}
	if o.Money == 0 {
		ignored = true
		input.ReviewReason = "零金额流水，保留原始证据"
	}
	if input.Currency != "CNY" {
		input.ReviewReason = "外币原始证据待审核，不自动换汇"
		input.Amount = nil
	}
	if len(input.ExternalID) > 180 {
		input.ExternalID = ""
		input.ReviewReason = "原始交易单号过长，请核对原始账单"
	}
	if len([]rune(input.MerchantRaw)) > 200 {
		input.MerchantRaw = ""
		input.ReviewReason = "原始商家字段过长，请核对原始账单"
	}
	return input, ignored
}

func statementTime(code string, o *ir.Order, row statement.Row) (string, string) {
	at := o.PayTime
	if code == "wechat" || code == "alipay" {
		return at.UTC().Format(time.RFC3339Nano), "SECOND"
	}
	zone := at.Location()
	if code != "bmo" && code != "td" {
		zone = time.FixedZone("Asia/Shanghai", 8*3600)
	}
	precision := "DAY"
	if at.Hour() != 0 || at.Minute() != 0 || at.Second() != 0 {
		precision = "SECOND"
	}
	// BOC's upstream credit converter inserts 20:00 for a date-only source.
	// That is ordering metadata, not evidence of the transaction's real time.
	if code == "boc" && len(row.Header) > 0 && strings.TrimSpace(row.Header[0]) == "交易日" {
		precision = "DAY"
	}
	for i, h := range row.Header {
		if i >= len(row.Values) {
			continue
		}
		h = strings.TrimSpace(h)
		value := strings.TrimSpace(row.Values[i])
		if h != "交易时间" && h != "记账时间" && h != "时间" && h != "交易日期" {
			continue
		}
		for _, layout := range []string{"15:04:05", "15:04", "150405", "2006-01-02 15:04:05", "2006/01/02 15:04:05", "2006/1/2 15:04:05", "20060102150405"} {
			if parsed, e := time.ParseInLocation(layout, value, zone); e == nil {
				year, month, day := at.Date()
				if parsed.Year() != 0 {
					year, month, day = parsed.Date()
				}
				at = time.Date(year, month, day, parsed.Hour(), parsed.Minute(), parsed.Second(), 0, zone)
				precision = "SECOND"
				break
			}
		}
	}
	if precision == "DAY" {
		at = time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, zone)
	}
	return at.UTC().Format(time.RFC3339Nano), precision
}

func statementCurrency(code string, o *ir.Order, row statement.Row) string {
	for _, name := range []string{"记账币种", "入账货币", "入账币种/金额", "Billing currency", "货币", "币别", "币种", "交易货币"} {
		for i, h := range row.Header {
			if strings.TrimSpace(h) == name && i < len(row.Values) {
				currency := strings.TrimSpace(row.Values[i])
				if currency == "人民币" || currency == "RMB" {
					return "CNY"
				}
				if len(currency) >= 3 && currency[0] >= 'A' && currency[0] <= 'Z' {
					return currency[:3]
				}
				if currency != "" {
					return currency
				}
			}
		}
	}
	if o.Currency != "" {
		if o.Currency == "人民币" || o.Currency == "RMB" {
			return "CNY"
		}
		return o.Currency
	}
	if code == "bmo" || code == "td" {
		return "CAD"
	}
	return "CNY"
}

func (s *Store) StatementBatches(ctx context.Context, syncID string, limit, offset int) ([]ledger.ImportBatch, error) {
	if limit < 1 || limit > 500 || offset < 0 || len(syncID) > 100 {
		return nil, ledger.ErrInvalid
	}
	return s.statementBatches(ctx, syncID, limit, offset)
}
func (s *Store) statementBatches(ctx context.Context, syncID string, limit, offset int) ([]ledger.ImportBatch, error) {
	query := `SELECT payload FROM import_batches WHERE (?='' OR sync_id=?) ORDER BY rowid DESC`
	args := []any{syncID, syncID}
	if limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}
	rows, e := s.db.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.ImportBatch{}
	for rows.Next() {
		var b string
		var batch ledger.ImportBatch
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &batch); e != nil {
			return nil, e
		}
		out = append(out, batch)
	}
	return out, rows.Err()
}
func (s *Store) StatementFile(ctx context.Context, id string) (string, []byte, error) {
	var name string
	var data []byte
	e := s.db.QueryRowContext(ctx, `SELECT filename,original_file FROM import_batches WHERE id=?`, id).Scan(&name, &data)
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return name, data, e
}

func (s *Store) StatementSummary(ctx context.Context, syncID string) (ledger.StatementSummary, error) {
	out := ledger.StatementSummary{Batches: []ledger.ImportBatch{}, AccountHealth: []ledger.AccountHealth{}}
	if len(syncID) > 100 {
		return out, ledger.ErrInvalid
	}
	var e error
	out.Batches, e = s.statementBatches(ctx, syncID, 0, 0)
	if e != nil {
		return out, e
	}
	for _, b := range out.Batches {
		out.Totals.RowCount += b.RowCount
		out.Totals.NewTransactions += b.NewTransactions
		out.Totals.MatchedExisting += b.MatchedExisting
		out.Totals.Duplicates += b.Duplicates
		out.Totals.Conflicts += b.Conflicts
		out.Totals.Skipped += b.Skipped
	}
	e = s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT l.transaction_id) FROM import_batches b JOIN import_batch_rows r ON r.batch_id=b.id JOIN financial_event_links l ON l.raw_event_id=r.raw_event_id JOIN money_transactions m ON m.id=l.transaction_id WHERE (?='' OR b.sync_id=?) AND m.voided_at='' AND NOT EXISTS(SELECT 1 FROM transaction_annotations a WHERE a.transaction_id=l.transaction_id AND json_extract(a.payload,'$.annotation_status')!='UNANNOTATED')`, syncID, syncID).Scan(&out.Totals.Unannotated)
	if e != nil {
		return out, e
	}
	e = s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT r.id) FROM raw_financial_events r JOIN import_batch_rows br ON br.raw_event_id=r.id JOIN import_batches b ON b.id=br.batch_id WHERE (?='' OR b.sync_id=?) AND r.status NOT IN ('COMMITTED','IGNORED')`, syncID, syncID).Scan(&out.PendingReview)
	if e != nil {
		return out, e
	}
	accounts, e := s.Accounts(ctx)
	if e != nil {
		return out, e
	}
	checkpoints, e := s.AccountReconciliation(ctx, "")
	if e != nil {
		return out, e
	}
	for _, a := range accounts {
		if a.ID == "unknown" || !a.Active {
			continue
		}
		health := ledger.AccountHealth{AccountID: a.ID, Name: a.Name, CurrentBalance: a.CurrentBalance, Status: "NEVER_RECONCILED"}
		for _, c := range checkpoints {
			if c.AccountID == a.ID {
				health.ObservedAt = c.ObservedAt
				health.UnexplainedAmount = c.UnexplainedAmount
				health.Status = "RECONCILED"
				if c.UnexplainedAmount != 0 {
					health.Status = "UNEXPLAINED"
				}
				break
			}
		}
		out.AccountHealth = append(out.AccountHealth, health)
	}
	return out, nil
}
