package ledger

// RawFinancialInput preserves evidence independently of its interpretation.
type RawFinancialInput struct {
	ProviderCode         string                  `json:"provider_code,omitempty"`
	EvidenceKey          string                  `json:"evidence_key,omitempty"`
	ImportBatchID        string                  `json:"import_batch_id,omitempty"`
	RowNumber            int                     `json:"row_number,omitempty"`
	DescriptionRaw       string                  `json:"description_raw,omitempty"`
	PaymentMethodRaw     string                  `json:"payment_method_raw,omitempty"`
	Currency             string                  `json:"currency,omitempty"`
	ReviewReason         string                  `json:"review_reason,omitempty"`
	TimePrecision        string                  `json:"time_precision,omitempty"`
	RelatedTransactionID string                  `json:"related_transaction_id,omitempty"`
	Provider             string                  `json:"provider"`
	CaptureSource        string                  `json:"capture_source"`
	ExternalID           string                  `json:"external_id,omitempty"`
	CapturedAt           string                  `json:"captured_at,omitempty"`
	OccurredAt           string                  `json:"occurred_at,omitempty"`
	Amount               *Money                  `json:"amount,omitempty"`
	Direction            string                  `json:"direction,omitempty"`
	MerchantRaw          string                  `json:"merchant_raw,omitempty"`
	PaymentChannel       string                  `json:"payment_channel,omitempty"`
	FundingHint          string                  `json:"funding_hint,omitempty"`
	AccountID            string                  `json:"account_id,omitempty"`
	RawTitle             string                  `json:"raw_title,omitempty"`
	RawText              string                  `json:"raw_text"`
	RawPayload           string                  `json:"raw_payload,omitempty"`
	ManualDetails        *ManualFinancialDetails `json:"manual_details,omitempty"`
}

type ManualFinancialDetails struct {
	Category      string   `json:"category,omitempty"`
	ExpenseNature string   `json:"expense_nature,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}
type RawFinancialEvent struct {
	RawFinancialInput
	ID            string   `json:"id"`
	Status        string   `json:"status"`
	Fingerprint   string   `json:"fingerprint"`
	TransactionID string   `json:"transaction_id,omitempty"`
	Decision      string   `json:"decision"`
	Candidates    []string `json:"candidates"`
	Confidence    float64  `json:"confidence"`
}
type FinancialReview struct {
	Amount               *Money `json:"amount,omitempty"`
	OccurredAt           string `json:"occurred_at,omitempty"`
	Direction            string `json:"direction,omitempty"`
	RelatedTransactionID string `json:"related_transaction_id,omitempty"`
	Action               string `json:"action"`
	TransactionID        string `json:"transaction_id,omitempty"`
	AccountID            string `json:"account_id,omitempty"`
	Merchant             string `json:"merchant,omitempty"`
	Reason               string `json:"reason"`
	Note                 string `json:"note,omitempty"`
}
type TransactionAnnotation struct {
	Category         *string  `json:"category"`
	ExpenseNature    *string  `json:"expense_nature"`
	MealType         *string  `json:"meal_type"`
	ItemName         *string  `json:"item_name"`
	ItemDescription  *string  `json:"item_description"`
	Tags             []string `json:"tags"`
	Note             string   `json:"note"`
	Confidence       float64  `json:"confidence"`
	AnnotatedBy      string   `json:"annotated_by"`
	AnnotationStatus string   `json:"annotation_status"`
}
type AnnotatedTransaction struct {
	MoneyTransaction
	Annotation TransactionAnnotation `json:"annotation"`
}
type BalanceCheckpoint struct {
	ID                string      `json:"id"`
	AccountID         string      `json:"account_id"`
	ObservedBalance   SignedMoney `json:"observed_balance"`
	ExpectedBalance   SignedMoney `json:"expected_balance"`
	Difference        SignedMoney `json:"difference"`
	UnexplainedAmount SignedMoney `json:"unexplained_amount"`
	ObservedAt        string      `json:"observed_at"`
}
type FinancialSettings struct {
	AutoCommitThreshold float64 `json:"auto_commit_threshold"`
}

type SemanticEvidence struct {
	ID                   string `json:"id,omitempty"`
	OccurredAt           string `json:"occurred_at,omitempty"`
	Source               string `json:"source"`
	Text                 string `json:"text"`
	AttachmentRef        string `json:"attachment_ref,omitempty"`
	MatchedTransactionID string `json:"matched_transaction_id,omitempty"`
	Status               string `json:"status"`
}
type MerchantRule struct {
	ID                 string                `json:"id,omitempty"`
	MerchantRaw        string                `json:"merchant_raw"`
	MerchantNormalized string                `json:"merchant_normalized"`
	Annotation         TransactionAnnotation `json:"annotation"`
	UserConfirmed      bool                  `json:"user_confirmed"`
}
type FinancialQuery struct {
	MoneyFilter
	Amount           *Money
	AnnotationStatus string
	Search           string
	RefundAt         string
	RefundAmount     Money
	EditingRefund    string
	TransactionID    string
}
type RawFinancialQuery struct {
	HistoryFilter
	Provider, CaptureSource, Merchant string
}

// TransactionDetailsPatch contains the semantic fields explicitly changed by the user.
type TransactionDetailsPatch struct {
	Category        *string   `json:"category,omitempty"`
	Merchant        *string   `json:"merchant,omitempty"`
	ItemDescription *string   `json:"item_description,omitempty"`
	Note            *string   `json:"note,omitempty"`
	Tags            *[]string `json:"tags,omitempty"`
}

type FundingVersion struct {
	AccountID            string `json:"account_id"`
	Amount               Money  `json:"amount"`
	Direction            string `json:"direction"`
	RelatedTransactionID string `json:"related_transaction_id,omitempty"`
}

type FundingCorrection struct {
	Expected             FundingVersion `json:"expected"`
	AccountID            *string        `json:"account_id,omitempty"`
	Amount               *Money         `json:"amount,omitempty"`
	Direction            *string        `json:"direction,omitempty"`
	RelatedTransactionID *string        `json:"related_transaction_id,omitempty"`
	Note                 string         `json:"note,omitempty"`
}
