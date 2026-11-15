package ledger

// StatementInput carries the original native file without lossy browser conversion.
type StatementInput struct {
	Filename        string            `json:"filename"`
	File            []byte            `json:"file_base64"`
	AccountID       string            `json:"account_id"`
	PaymentAccounts map[string]string `json:"payment_accounts"`
	SyncID          string            `json:"sync_id"`
	PreviewToken    string            `json:"preview_token"`
}

type ImportBatch struct {
	ID              string `json:"id"`
	ProviderCode    string `json:"provider_code"`
	Filename        string `json:"filename"`
	FileHash        string `json:"file_hash"`
	ImportedAt      string `json:"imported_at"`
	SyncID          string `json:"sync_id"`
	ParserVersion   string `json:"parser_version"`
	RowCount        int    `json:"row_count"`
	NewTransactions int    `json:"new_transactions"`
	MatchedExisting int    `json:"matched_existing"`
	Duplicates      int    `json:"duplicates"`
	Conflicts       int    `json:"conflicts"`
	Skipped         int    `json:"skipped"`
	Unannotated     int    `json:"unannotated"`
}

type StatementRowResult struct {
	Row           int    `json:"row"`
	Amount        *Money `json:"amount"`
	Currency      string `json:"currency"`
	Merchant      string `json:"merchant"`
	PaymentMethod string `json:"payment_method"`
	AccountID     string `json:"account_id"`
	OccurredAt    string `json:"occurred_at"`
	TimePrecision string `json:"time_precision"`
	Direction     string `json:"direction"`
	Status        string `json:"status"`
	Reason        string `json:"reason"`
	TransactionID string `json:"transaction_id"`
	RawEventID    string `json:"raw_event_id"`
}

type StatementResult struct {
	Batch           ImportBatch          `json:"batch"`
	Rows            []StatementRowResult `json:"rows"`
	PreviewToken    string               `json:"preview_token"`
	AlreadyImported bool                 `json:"already_imported"`
	PreviousBatchID string               `json:"previous_batch_id"`
}

type StatementSummary struct {
	PendingReview int             `json:"pending_review"`
	Batches       []ImportBatch   `json:"batches"`
	Totals        ImportBatch     `json:"totals"`
	AccountHealth []AccountHealth `json:"account_health"`
}
type AccountHealth struct {
	AccountID         string      `json:"account_id"`
	Name              string      `json:"name"`
	CurrentBalance    SignedMoney `json:"current_balance"`
	UnexplainedAmount SignedMoney `json:"unexplained_amount"`
	ObservedAt        string      `json:"observed_at"`
	Status            string      `json:"status"`
}
