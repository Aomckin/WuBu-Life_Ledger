package ledger

type LinkInput struct {
	SourceType    string `json:"source_type"`
	SourceID      string `json:"source_id"`
	TransactionID string `json:"transaction_id"`
	Relation      string `json:"relation"`
}

type Link struct {
	LinkInput
	ID                string `json:"id"`
	CreatedAt         string `json:"created_at"`
	TransactionAmount *Money `json:"transaction_amount,omitempty"`
}
