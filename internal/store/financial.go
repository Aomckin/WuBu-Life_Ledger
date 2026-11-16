package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"stockroom/internal/ledger"
	"strings"
	"time"
)

func financialNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func rawTx(ctx context.Context, tx *sql.Tx, id string) (ledger.RawFinancialEvent, error) {
	var r ledger.RawFinancialEvent
	var p, c string
	e := tx.QueryRowContext(ctx, `SELECT id,payload,status,fingerprint,transaction_id,decision,candidates,confidence FROM raw_financial_events WHERE id=?`, id).Scan(&r.ID, &p, &r.Status, &r.Fingerprint, &r.TransactionID, &r.Decision, &c, &r.Confidence)
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal([]byte(p), &r.RawFinancialInput); e != nil {
		return r, e
	}
	e = json.Unmarshal([]byte(c), &r.Candidates)
	if e != nil {
		return r, e
	}
	return r, e
}
func saveRawTx(ctx context.Context, tx *sql.Tx, r ledger.RawFinancialEvent) error {
	p, e := json.Marshal(r.RawFinancialInput)
	if e != nil {
		return e
	}
	c, e := json.Marshal(r.Candidates)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO raw_financial_events VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,transaction_id=excluded.transaction_id,decision=excluded.decision,candidates=excluded.candidates,confidence=excluded.confidence`, r.ID, string(p), r.Status, r.Fingerprint, r.TransactionID, r.Decision, string(c), r.Confidence)
	return e
}
func linkRawTx(ctx context.Context, tx *sql.Tx, r *ledger.RawFinancialEvent, id, relation string) error {
	r.TransactionID = id
	r.Status = "COMMITTED"
	r.Decision = "MATCHED_EXISTING"
	_, e := tx.ExecContext(ctx, `INSERT INTO financial_event_links VALUES(?,?,?,?,?)`, r.ID, id, relation, r.Confidence, financialNow())
	if e != nil {
		return e
	}
	return saveRawTx(ctx, tx, *r)
}

// Only structured financial fields become facts. Unparsed text stays raw evidence.
func normalizeFinancial(p ledger.RawFinancialInput) (ledger.RawFinancialInput, error) {
	switch p.Provider {
	case "WECHAT", "ALIPAY", "BANK", "UNIONPAY", "CASH", "OTHER":
	default:
		return p, ledger.ErrInvalid
	}
	switch p.CaptureSource {
	case "CSV_IMPORT", "MANUAL", "AGENT", "SYSTEM":
	default:
		return p, ledger.ErrInvalid
	}
	if p.TimePrecision != "" && p.TimePrecision != "DAY" && p.TimePrecision != "SECOND" {
		return p, ledger.ErrInvalid
	}
	if p.Currency != "" && p.Currency != "CNY" {
		p.ReviewReason = "外币证据需人工确认人民币金额，不自动换汇"
	}
	if len(p.RawText) > 32000 || len(p.RawPayload) > 128000 || len(p.ExternalID) > 180 || len([]rune(p.MerchantRaw)) > 200 || len(p.ProviderCode) > 40 || len(p.EvidenceKey) > 200 {
		return p, ledger.ErrInvalid
	}
	if p.Amount != nil && (*p.Amount <= 0 || *p.Amount > 1e12) {
		return p, ledger.ErrInvalid
	}
	if d := p.ManualDetails; d != nil && (p.CaptureSource != "MANUAL" || len(p.RawText) > 8000 || len([]rune(p.RawText)) > 2000 || len(d.Category) > 100 || !validNature(d.ExpenseNature) || !validFinancialTags(d.Tags)) {
		return p, ledger.ErrInvalid
	}
	if p.Direction != "" && p.Direction != "EXPENSE" && p.Direction != "INCOME" && p.Direction != "REFUND" {
		return p, ledger.ErrInvalid
	}
	var e error
	if p.CapturedAt == "" {
		p.CapturedAt = financialNow()
	} else if p.CapturedAt, e = ledger.Timestamp(p.CapturedAt); e != nil {
		return p, e
	}
	if p.OccurredAt != "" {
		p.OccurredAt, e = ledger.Timestamp(p.OccurredAt)
		if e != nil {
			return p, e
		}
	}
	return p, nil
}
func captureFinancialTx(ctx context.Context, tx *sql.Tx, p ledger.RawFinancialInput) (ledger.RawFinancialEvent, error) {
	var r ledger.RawFinancialEvent
	var e error
	p, e = normalizeFinancial(p)
	if e != nil {
		return r, e
	}
	if p.EvidenceKey != "" {
		var existing string
		e = tx.QueryRowContext(ctx, `SELECT id FROM raw_financial_events WHERE json_extract(payload,'$.evidence_key')=?`, p.EvidenceKey).Scan(&existing)
		if e == nil {
			return rawTx(ctx, tx, existing)
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return r, e
		}
	}
	fingerprint := fmt.Sprintf("%s|%s|%v|%s|%s|%s", p.Provider, p.ProviderCode, func() any {
		if p.Amount == nil {
			return nil
		}
		return *p.Amount
	}(), p.Direction, p.OccurredAt, p.MerchantRaw)
	r = ledger.RawFinancialEvent{RawFinancialInput: p, ID: uuid.NewString(), Status: "CAPTURED", Decision: "INSUFFICIENT_FIELDS", Candidates: []string{}, Fingerprint: fmt.Sprintf("%x", sha256.Sum256([]byte(fingerprint)))}
	if e = saveRawTx(ctx, tx, r); e != nil {
		return r, e
	}
	if p.ReviewReason != "" {
		r.Status = "NEEDS_REVIEW"
		r.Decision = p.ReviewReason
		return r, saveRawTx(ctx, tx, r)
	}
	if p.AccountID == "" || p.Amount == nil || p.OccurredAt == "" || p.Direction == "" {
		return r, nil
	}
	a, e := accountTx(ctx, tx, p.AccountID)
	if errors.Is(e, ErrNotFound) || errors.Is(e, ledger.ErrConflict) {
		r.Status = "NEEDS_REVIEW"
		r.Decision = "ACCOUNT_REQUIRES_REVIEW"
		return r, saveRawTx(ctx, tx, r)
	}
	if e != nil {
		return r, e
	}
	p.AccountID = a.ID
	r.RawFinancialInput.AccountID = a.ID
	// Contradictory evidence for the same provider ID is reviewable, never a new debit.
	if p.ExternalID != "" {
		var conflicting int
		e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM raw_financial_events WHERE id!=? AND json_extract(payload,'$.provider')=? AND COALESCE(json_extract(payload,'$.provider_code'),'')=? AND json_extract(payload,'$.external_id')=? AND json_extract(payload,'$.account_id')=? AND (CAST(ROUND(json_extract(payload,'$.amount')*100) AS INTEGER)!=? OR json_extract(payload,'$.direction')!=?) AND NOT (?='REFUND' AND ?!='' AND transaction_id=? AND json_extract(payload,'$.direction')='EXPENSE')`, r.ID, p.Provider, p.ProviderCode, p.ExternalID, p.AccountID, *p.Amount, p.Direction, p.Direction, p.RelatedTransactionID, p.RelatedTransactionID).Scan(&conflicting)
		if e != nil {
			return r, e
		}
		if conflicting > 0 {
			r.Status = "NEEDS_REVIEW"
			r.Decision = "CONFLICTING_EXTERNAL_ID"
			return r, saveRawTx(ctx, tx, r)
		}
	}
	// Exact external IDs are scoped by provider and account.
	if p.ExternalID != "" {
		rows, e := tx.QueryContext(ctx, `SELECT DISTINCT m.id FROM money_transactions m
 JOIN financial_event_links l ON l.transaction_id=m.id JOIN raw_financial_events r ON r.id=l.raw_event_id
 WHERE (COALESCE(NULLIF(json_extract(r.payload,'$.account_id'),''),m.account_id)=?
   OR (m.account_id=? AND EXISTS (SELECT 1 FROM financial_corrections WHERE transaction_id=m.id))) AND m.voided_at=''
 AND COALESCE(CAST(ROUND(json_extract(r.payload,'$.amount')*100) AS INTEGER),m.amount_cents)=?
 AND COALESCE(NULLIF(json_extract(r.payload,'$.direction'),''),m.type)=?
 AND json_extract(r.payload,'$.provider')=? AND COALESCE(json_extract(r.payload,'$.provider_code'),'') IN (?, '')
 AND json_extract(r.payload,'$.external_id')=?`, a.ID, a.ID, *p.Amount, p.Direction, p.Provider, p.ProviderCode, p.ExternalID)
		if e != nil {
			return r, e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return r, e
			}
			r.Candidates = append(r.Candidates, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return r, e
		}
		if len(r.Candidates) == 1 {
			r.Confidence = 1
			e = linkRawTx(ctx, tx, &r, r.Candidates[0], "DUPLICATE")
			return r, e
		}
	}
	uncertainCandidate := p.Provider == "BANK" && p.TimePrecision == "DAY"
	if len(r.Candidates) == 0 {
		rows, e := tx.QueryContext(ctx, `SELECT m.id,m.merchant,EXISTS(SELECT 1 FROM raw_financial_events r WHERE r.transaction_id=m.id AND json_extract(r.payload,'$.merchant_raw')=?), (m.source IN ('manual','agent') OR EXISTS(SELECT 1 FROM raw_financial_events r WHERE r.transaction_id=m.id AND json_extract(r.payload,'$.capture_source') IN ('MANUAL','AGENT'))), EXISTS(SELECT 1 FROM raw_financial_events r WHERE r.transaction_id=m.id AND json_extract(r.payload,'$.provider')='BANK'),EXISTS(SELECT 1 FROM raw_financial_events r WHERE r.transaction_id=m.id AND json_extract(r.payload,'$.provider')='BANK' AND json_extract(r.payload,'$.time_precision')='DAY') FROM money_transactions m WHERE account_id=? AND amount_cents=? AND type=? AND voided_at='' AND ABS(julianday(occurred_at)-julianday(?))*86400<=CASE WHEN ?='BANK' OR EXISTS(SELECT 1 FROM raw_financial_events r WHERE r.transaction_id=m.id AND json_extract(r.payload,'$.provider')='BANK') THEN 300.001 ELSE 120.001 END AND NOT EXISTS(SELECT 1 FROM raw_financial_events same WHERE same.transaction_id=m.id AND json_extract(same.payload,'$.provider')=? AND COALESCE(json_extract(same.payload,'$.provider_code'),'') IN (?, '') AND json_extract(same.payload,'$.capture_source')=? AND COALESCE(json_extract(same.payload,'$.external_id'),'')!='' AND ?!='' AND json_extract(same.payload,'$.external_id')!=?) AND NOT EXISTS(SELECT 1 FROM raw_financial_events same_batch WHERE same_batch.transaction_id=m.id AND ?!='' AND json_extract(same_batch.payload,'$.import_batch_id')=? AND json_extract(same_batch.payload,'$.row_number')!=?) AND NOT EXISTS(SELECT 1 FROM raw_financial_events different_bank WHERE different_bank.transaction_id=m.id AND ?='BANK' AND json_extract(different_bank.payload,'$.provider')='BANK' AND COALESCE(json_extract(different_bank.payload,'$.provider_code'),'') NOT IN (?,''))`, p.MerchantRaw, a.ID, *p.Amount, p.Direction, p.OccurredAt, p.Provider, p.Provider, p.ProviderCode, p.CaptureSource, p.ExternalID, p.ExternalID, p.ImportBatchID, p.ImportBatchID, p.RowNumber, p.Provider, p.ProviderCode)
		if e != nil {
			return r, e
		}
		for rows.Next() {
			var id, merchant string
			var rawMerchantMatch, manualSource, bankSource, bankDayOnly bool
			if e = rows.Scan(&id, &merchant, &rawMerchantMatch, &manualSource, &bankSource, &bankDayOnly); e != nil {
				rows.Close()
				return r, e
			}
			merchantAgrees := merchant == p.MerchantRaw || merchant == "" || p.MerchantRaw == "" || rawMerchantMatch
			if merchantAgrees || (p.CaptureSource == "CSV_IMPORT" && manualSource) || bankSource || p.Provider == "BANK" {
				r.Candidates = append(r.Candidates, id)
				uncertainCandidate = uncertainCandidate || bankDayOnly || (!merchantAgrees && !manualSource)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return r, e
		}
	}
	if len(r.Candidates) > 0 {
		r.Status = "NEEDS_REVIEW"
		r.Decision = "AMBIGUOUS_MATCH"
		r.Confidence = 0.7
		if len(r.Candidates) == 1 {
			// Auto merge only across independent sources with no contradictory external ID.
			var n int
			e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM raw_financial_events WHERE transaction_id=? AND NOT EXISTS (SELECT 1 FROM raw_financial_events same WHERE same.transaction_id=raw_financial_events.transaction_id AND json_extract(same.payload,'$.provider')=? AND json_extract(same.payload,'$.capture_source')=?) AND (json_extract(payload,'$.capture_source')!=? OR json_extract(payload,'$.provider')!=?) AND (json_extract(payload,'$.provider')!=? OR COALESCE(json_extract(payload,'$.external_id'),'')='' OR ?='' OR json_extract(payload,'$.external_id')=?)`, r.Candidates[0], p.Provider, p.CaptureSource, p.CaptureSource, p.Provider, p.Provider, p.ExternalID, p.ExternalID).Scan(&n)
			if e != nil {
				return r, e
			}
			if n == 0 && p.CaptureSource == "CSV_IMPORT" {
				e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM money_transactions m WHERE m.id=? AND m.source IN ('manual','agent') AND NOT EXISTS(SELECT 1 FROM raw_financial_events r WHERE r.transaction_id=m.id)`, r.Candidates[0]).Scan(&n)
				if e != nil {
					return r, e
				}
			}
			var threshold float64
			if e = tx.QueryRowContext(ctx, `SELECT auto_commit_threshold FROM financial_settings WHERE id=1`).Scan(&threshold); e != nil {
				return r, e
			}
			if n > 0 && threshold <= 0.97 && !uncertainCandidate {
				r.Confidence = 0.97
				e = linkRawTx(ctx, tx, &r, r.Candidates[0], "SUPPORTING")
				return r, e
			}
		}
		return r, saveRawTx(ctx, tx, r)
	}
	r.Confidence = 1
	var threshold float64
	if e = tx.QueryRowContext(ctx, `SELECT auto_commit_threshold FROM financial_settings WHERE id=1`).Scan(&threshold); e != nil {
		return r, e
	}
	if r.Confidence < threshold {
		r.Status = "NEEDS_REVIEW"
		r.Decision = "BELOW_THRESHOLD"
		return r, saveRawTx(ctx, tx, r)
	}
	return commitFinancialTx(ctx, tx, r, p.AccountID, p.MerchantRaw)
}
func commitFinancialTx(ctx context.Context, tx *sql.Tx, r ledger.RawFinancialEvent, account, merchant string) (ledger.RawFinancialEvent, error) {
	if r.Direction != "EXPENSE" && r.Direction != "INCOME" && r.Direction != "REFUND" {
		return r, fmt.Errorf("%w: 内部划转请关联现有转账流水", ledger.ErrInvalid)
	}
	if r.Amount == nil || r.OccurredAt == "" || r.Direction == "" || account == "" {
		return r, ledger.ErrInvalid
	}
	delta := ledger.SignedMoney(*r.Amount)
	if r.Direction == "EXPENSE" {
		delta = -delta
	}
	// Unlinked refunds are preserved as review items; never invent an original expense.
	if r.Direction == "REFUND" && r.RelatedTransactionID == "" {
		r.Status = "NEEDS_REVIEW"
		r.Decision = "REFUND_REQUIRES_ORIGINAL"
		return r, saveRawTx(ctx, tx, r)
	}
	var rule ledger.MerchantRule
	var rulePayload string
	ruleErr := sql.ErrNoRows
	if r.ManualDetails == nil {
		ruleErr = tx.QueryRowContext(ctx, `SELECT payload FROM merchant_rules WHERE merchant_raw=?`, merchant).Scan(&rulePayload)
	}
	if ruleErr == nil {
		if e := json.Unmarshal([]byte(rulePayload), &rule); e != nil {
			return r, e
		}
		if rule.MerchantNormalized != "" {
			merchant = rule.MerchantNormalized
		}
	} else if !errors.Is(ruleErr, sql.ErrNoRows) {
		return r, ruleErr
	}
	input := ledger.MoneyInput{AccountID: account, Amount: *r.Amount, Merchant: merchant, OccurredAt: r.OccurredAt, Source: "import", RelatedTransactionID: r.RelatedTransactionID}
	if d := r.ManualDetails; d != nil {
		input.Source, input.Note = "manual", r.RawText
		input.Category, input.ExpenseNature, input.Tags = d.Category, d.ExpenseNature, d.Tags
	}
	if r.Direction == "REFUND" {
		original, e := refundOriginal(ctx, tx, input)
		if e != nil {
			return r, e
		}
		var refunded ledger.Money
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_cents),0) FROM money_transactions WHERE type='REFUND' AND related_transaction_id=? AND voided_at=''`, original.ID).Scan(&refunded); e != nil {
			return r, e
		}
		if refunded+input.Amount > original.Amount {
			return r, ledger.ErrConflict
		}
		input.Category = original.Category
		input.ExpenseNature = original.ExpenseNature
	}
	t, e := postMoney(ctx, tx, r.Direction, input, delta)
	if e != nil {
		return r, e
	}

	if _, e = tx.ExecContext(ctx, `UPDATE money_transactions SET category=? WHERE id=?`, input.Category, t.ID); e != nil {
		return r, e
	}
	if ruleErr == nil {
		annotation := rule.Annotation
		annotation.AnnotatedBy = "merchant-rule:" + rule.ID
		annotation.AnnotationStatus = "AUTO_ANNOTATED"
		annotation.Confidence = 1
		if annotation.Tags == nil {
			annotation.Tags = []string{}
		}
		b, e := json.Marshal(annotation)
		if e != nil {
			return r, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO transaction_annotations VALUES(?,?,?)`, t.ID, string(b), financialNow()); e != nil {
			return r, e
		}
		if e = mirrorAnnotationTx(ctx, tx, t.ID, annotation); e != nil {
			return r, e
		}
	}
	r.Confidence = 1
	e = linkRawTx(ctx, tx, &r, t.ID, "PRIMARY")
	if e != nil {
		return r, e
	}
	r.Decision = "CREATE_NEW"
	return r, saveRawTx(ctx, tx, r)
}
func (s FinanceRepository) CaptureFinancial(ctx context.Context, key string, p ledger.RawFinancialInput) (ledger.RawFinancialEvent, bool, error) {
	var out ledger.RawFinancialEvent
	r, e := s.ledgerTx(ctx, key, "capture_financial", p, &out, func(tx *sql.Tx) error { var e error; out, e = captureFinancialTx(ctx, tx, p); return e })
	return out, r, e
}
func (s FinanceRepository) ReviewFinancial(ctx context.Context, key, id string, p ledger.FinancialReview) (ledger.RawFinancialEvent, bool, error) {
	var out ledger.RawFinancialEvent
	r, e := s.ledgerTx(ctx, key, "review_financial:"+id, p, &out, func(tx *sql.Tx) error {
		var e error
		out, e = rawTx(ctx, tx, id)
		if e != nil {
			return e
		}
		if out.TransactionID != "" || out.Status == "IGNORED" {
			return ledger.ErrConflict
		}
		if out.Currency != "" && out.Currency != "CNY" && p.Action != "IGNORE" && p.Amount == nil {
			return fmt.Errorf("%w: 外币证据需显式提供人民币金额", ledger.ErrInvalid)
		}
		if len([]rune(p.Note)) > 2000 {
			return ledger.ErrInvalid
		}
		if strings.TrimSpace(p.Reason) == "" {
			p.Reason = map[string]string{"MATCH": "CONFIRMED_EXISTING_TRANSACTION", "CREATE_NEW": "CONFIRMED_INDEPENDENT_TRANSACTION", "IGNORE": "CONFIRMED_IGNORE"}[p.Action]
		}
		review, e := json.Marshal(p)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO financial_reviews VALUES(?,?,?,?)`, uuid.NewString(), id, string(review), financialNow()); e != nil {
			return e
		}
		// Reviewed candidate corrections never overwrite the immutable raw payload.
		if p.Amount != nil {
			if *p.Amount <= 0 || *p.Amount > 1e12 {
				return ledger.ErrInvalid
			}
			out.Amount = p.Amount
		}
		if p.OccurredAt != "" {
			out.OccurredAt, e = ledger.Timestamp(p.OccurredAt)
			if e != nil {
				return e
			}
		}
		if p.Direction != "" {
			if p.Direction != "EXPENSE" && p.Direction != "INCOME" && p.Direction != "REFUND" && p.Direction != "TRANSFER_IN" && p.Direction != "TRANSFER_OUT" {
				return ledger.ErrInvalid
			}
			out.Direction = p.Direction
		}
		if p.RelatedTransactionID != "" {
			out.RelatedTransactionID = p.RelatedTransactionID
		}
		switch p.Action {
		case "IGNORE":
			out.Status = "IGNORED"
			out.Decision = p.Reason
			return saveRawTx(ctx, tx, out)
		case "MATCH":
			t, e := scanMoney(tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE id=?`, p.TransactionID))
			if e != nil {
				return e
			}
			if out.Amount == nil || t.Amount != *out.Amount || t.Type != out.Direction || t.VoidedAt != "" {
				return ledger.ErrConflict
			}
			account := p.AccountID
			if account == "" {
				account = out.AccountID
			}
			if account != "" {
				a, e := accountTx(ctx, tx, account)
				if e != nil {
					return e
				}
				if t.AccountID != a.ID {
					return ledger.ErrConflict
				}
			}
			out.Confidence = 1
			return linkRawTx(ctx, tx, &out, t.ID, "SUPPORTING")
		case "CREATE_NEW":
			if out.Direction == "REFUND" && out.RelatedTransactionID == "" {
				return fmt.Errorf("%w: 请先选择退款对应的原支出", ledger.ErrInvalid)
			}
			account := p.AccountID
			if account == "" {
				account = out.AccountID
			}
			merchant := p.Merchant
			if merchant == "" {
				merchant = out.MerchantRaw
			}
			out, e = commitFinancialTx(ctx, tx, out, account, merchant)
			return e
		default:
			return ledger.ErrInvalid
		}
	})
	return out, r, e
}
func (s FinanceRepository) RawFinancialEvents(ctx context.Context, status string, filters ...ledger.RawFinancialQuery) ([]ledger.RawFinancialEvent, error) {
	f := ledger.RawFinancialQuery{HistoryFilter: ledger.HistoryFilter{Limit: 500}}
	if len(filters) > 0 {
		f = filters[0]
	}
	if f.Limit < 1 || f.Limit > 500 || f.Offset < 0 {
		return nil, ledger.ErrInvalid
	}
	rows, e := s.db.QueryContext(ctx, `SELECT id FROM raw_financial_events
		WHERE (?='' OR status=? OR (?='PENDING' AND status NOT IN ('COMMITTED','IGNORED')) OR (?='PROCESSED' AND status IN ('COMMITTED','IGNORED')))
		AND (?='' OR json_extract(payload,'$.provider')=?) AND (?='' OR json_extract(payload,'$.capture_source')=?) AND (?='' OR json_extract(payload,'$.merchant_raw')=?)
		ORDER BY rowid DESC LIMIT ? OFFSET ?`, status, status, status, status, f.Provider, f.Provider, f.CaptureSource, f.CaptureSource, f.Merchant, f.Merchant, f.Limit, f.Offset)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	out := []ledger.RawFinancialEvent{}
	for _, id := range ids {
		r, e := rawTx(ctx, tx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, nil
}
func (s FinanceRepository) AnnotateTransaction(ctx context.Context, key, id string, p ledger.TransactionAnnotation) (ledger.TransactionAnnotation, bool, error) {
	out := p
	if out.Tags == nil {
		out.Tags = []string{}
	}
	r, e := s.ledgerTx(ctx, key, "annotate:"+id, p, &out, func(tx *sql.Tx) error {
		if p.Confidence < 0 || p.Confidence > 1 || p.AnnotatedBy == "" || len(p.Note) > 8000 || len(p.Tags) > 50 {
			return ledger.ErrInvalid
		}
		if p.ExpenseNature != nil && !validNature(*p.ExpenseNature) {
			return ledger.ErrInvalid
		}
		if p.Category != nil && len(*p.Category) > 100 {
			return ledger.ErrInvalid
		}
		switch p.AnnotationStatus {
		case "AUTO_ANNOTATED", "REVIEW_NEEDED", "CONFIRMED":
		default:
			return ledger.ErrInvalid
		}
		if p.Confidence < 0.95 && p.AnnotationStatus == "AUTO_ANNOTATED" {
			out.AnnotationStatus = "REVIEW_NEEDED"
		}
		if _, e := scanMoney(tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE id=?`, id)); e != nil {
			return e
		}
		b, e := json.Marshal(out)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO transaction_annotations VALUES(?,?,?) ON CONFLICT(transaction_id) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`, id, string(b), financialNow()); e != nil {
			return e
		}
		return mirrorAnnotationTx(ctx, tx, id, out)
	})
	return out, r, e
}

// Mirror trusted semantic metadata for v0.3 reports; money amounts/deltas remain immutable.
func mirrorAnnotationTx(ctx context.Context, tx *sql.Tx, id string, p ledger.TransactionAnnotation) error {
	if p.AnnotationStatus == "REVIEW_NEEDED" {
		return nil
	}
	if p.Category != nil {
		if _, e := tx.ExecContext(ctx, `UPDATE money_transactions SET category=? WHERE id=?`, *p.Category, id); e != nil {
			return e
		}
	}
	if p.ExpenseNature != nil {
		if _, e := tx.ExecContext(ctx, `UPDATE money_transactions SET expense_nature=? WHERE id=?`, *p.ExpenseNature, id); e != nil {
			return e
		}
	}
	return nil
}

// A query returns every candidate. Selection is never inferred from the number of results.
func (s FinanceRepository) FinancialTransactions(ctx context.Context, at string, window int, unannotated bool, filters ...ledger.FinancialQuery) ([]ledger.AnnotatedTransaction, error) {
	if window < 1 || window > 43200 {
		return nil, ledger.ErrInvalid
	}
	if at != "" {
		var e error
		at, e = ledger.Timestamp(at)
		if e != nil {
			return nil, e
		}
	}
	f := ledger.FinancialQuery{MoneyFilter: ledger.MoneyFilter{HistoryFilter: ledger.HistoryFilter{Limit: 500}}}
	if len(filters) > 0 {
		f = filters[0]
	}
	if f.Limit < 1 || f.Limit > 500 || f.Offset < 0 {
		return nil, ledger.ErrInvalid
	}
	if len([]rune(f.Search)) > 200 || f.RefundAmount < 0 {
		return nil, ledger.ErrInvalid
	}
	if f.RefundAt != "" {
		var err error
		f.RefundAt, err = ledger.Timestamp(f.RefundAt)
		if err != nil {
			return nil, err
		}
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions AS m
		WHERE voided_at='' AND type IN ('EXPENSE','INCOME','REFUND')
		AND (?='' OR id=?) AND (?='' OR type=?)
		AND (?='' OR ABS(julianday(occurred_at)-julianday(?))*1440<=?)
		AND (?='' OR julianday(occurred_at)>=julianday(?)) AND (?='' OR julianday(occurred_at)<julianday(?))
		AND (?='' OR merchant=?) AND (?='' OR account_id=?) AND (? IS NULL OR amount_cents=?)
		AND (?='' OR instr(lower(merchant||' '||note),lower(?))>0
			OR EXISTS(SELECT 1 FROM transaction_annotations a WHERE a.transaction_id=m.id
				AND instr(lower(COALESCE(json_extract(a.payload,'$.item_description'),json_extract(a.payload,'$.item_name'),'')),lower(?))>0)
			OR EXISTS(SELECT 1 FROM financial_event_links l JOIN raw_financial_events r ON r.id=l.raw_event_id
				WHERE l.transaction_id=m.id AND instr(lower(json_extract(r.payload,'$.description_raw')),lower(?))>0))
		AND (?='' OR julianday(occurred_at)<=julianday(?))
		AND (?=0 OR (type='EXPENSE' AND reason NOT IN ('DEPOSIT_LOSS','RECEIVABLE_LOSS')
			AND amount_cents-COALESCE((SELECT SUM(r.amount_cents) FROM money_transactions r WHERE r.type='REFUND' AND r.voided_at='' AND r.related_transaction_id=m.id AND r.id!=?),0)>=?))
		AND (?='' OR COALESCE((SELECT json_extract(payload,'$.annotation_status') FROM transaction_annotations WHERE transaction_id=m.id),'UNANNOTATED')=?)
		AND (?=0 OR id NOT IN (SELECT transaction_id FROM transaction_annotations WHERE json_extract(payload,'$.annotation_status')!='UNANNOTATED'))
		ORDER BY occurred_at DESC,id LIMIT ? OFFSET ?`, f.TransactionID, f.TransactionID, f.Type, f.Type,
		at, at, window, f.From, f.From, f.To, f.To, f.Merchant, f.Merchant, f.AccountID, f.AccountID, f.Amount, f.Amount,
		f.Search, f.Search, f.Search, f.Search, f.RefundAt, f.RefundAt, f.RefundAmount, f.EditingRefund, f.RefundAmount, f.AnnotationStatus, f.AnnotationStatus, unannotated, f.Limit, f.Offset)
	if e != nil {
		return nil, e
	}
	items := []ledger.MoneyTransaction{}
	for rows.Next() {
		t, e := scanMoney(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		items = append(items, t)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []ledger.AnnotatedTransaction{}
	for _, t := range items {
		a := ledger.TransactionAnnotation{AnnotationStatus: "UNANNOTATED", Tags: []string{}}
		var b string
		e = s.db.QueryRowContext(ctx, `SELECT payload FROM transaction_annotations WHERE transaction_id=?`, t.ID).Scan(&b)
		if e == nil {
			if e = json.Unmarshal([]byte(b), &a); e != nil {
				return nil, e
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if !unannotated || a.AnnotationStatus == "UNANNOTATED" {
			out = append(out, ledger.AnnotatedTransaction{MoneyTransaction: t, Annotation: a})
		}
	}
	return out, nil
}
func (s FinanceRepository) AccountReconciliation(ctx context.Context, account string) ([]ledger.BalanceCheckpoint, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id,account_id,observed_cents,expected_cents,difference_cents,unexplained_cents,observed_at FROM balance_checkpoints WHERE (?='' OR account_id=?) ORDER BY julianday(observed_at) DESC,rowid DESC`, account, account)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.BalanceCheckpoint{}
	for rows.Next() {
		var c ledger.BalanceCheckpoint
		if e = rows.Scan(&c.ID, &c.AccountID, &c.ObservedBalance, &c.ExpectedBalance, &c.Difference, &c.UnexplainedAmount, &c.ObservedAt); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Compensation is a money fact in the SAME transaction. It never recursively backfills.
func compensateCheckpoint(ctx context.Context, tx *sql.Tx, t ledger.MoneyTransaction) error {
	if t.Type != "EXPENSE" && t.Type != "INCOME" && t.Type != "REFUND" {
		return nil
	}
	var id string
	var unexplained ledger.SignedMoney
	e := tx.QueryRowContext(ctx, `SELECT id,unexplained_cents FROM balance_checkpoints WHERE account_id=? AND julianday(observed_at)>=julianday(?) ORDER BY julianday(observed_at) DESC,rowid DESC LIMIT 1`, t.AccountID, t.OccurredAt).Scan(&id, &unexplained)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	// The observed balance already includes all earlier events, including missing
	// inflow/outflow pairs whose net difference was zero. Preserve that anchor and
	// let the signed unexplained amount cross zero as history becomes complete.
	d := t.BalanceDelta
	if d == 0 {
		return nil
	}
	amount := ledger.Money(d)
	if amount < 0 {
		amount = -amount
	}
	_, e = postMoney(ctx, tx, "ADJUSTMENT", ledger.MoneyInput{AccountID: t.AccountID, Amount: amount, Source: "system", Reason: "RECONCILIATION_COMPENSATION", RelatedTransactionID: t.ID}, -d)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE balance_checkpoints SET unexplained_cents=unexplained_cents-? WHERE id=?`, d, id)
	return e
}
