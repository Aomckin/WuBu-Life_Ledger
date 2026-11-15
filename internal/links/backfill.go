package links

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"stockroom/internal/ledger"
)

type Acquisitions interface {
	RecordLinkedAcquisition(context.Context, string, string, ledger.AcquisitionInput) (ledger.Purchase, bool, error)
}

type Transactions interface {
	Transaction(context.Context, string) (ledger.MoneyTransaction, error)
}

type Associations interface {
	CreateLink(context.Context, string, ledger.LinkInput) (ledger.Link, bool, error)
}

type BackfillResult struct {
	Acquisition ledger.Purchase `json:"acquisition"`
	Link        *ledger.Link    `json:"link,omitempty"`
}

func Backfill(ctx context.Context, items Acquisitions, finance Transactions, links Associations, key, transactionID string, p ledger.AcquisitionInput) (BackfillResult, bool, error) {
	var out BackfillResult
	if key == "" || len(key) > 200 || strings.TrimSpace(key) != key {
		return out, false, fmt.Errorf("%w: a stable Idempotency-Key is required", ledger.ErrInvalid)
	}
	t, err := finance.Transaction(ctx, transactionID)
	if err != nil {
		return out, false, err
	}
	if t.Type != "EXPENSE" || t.VoidedAt != "" {
		return out, false, ledger.ErrConflict
	}
	// Keep request defaults stable across retries and keep user keys out of stored IDs.
	if p.OccurredAt == "" {
		p.OccurredAt = t.OccurredAt
	}
	if p.Merchant == "" {
		p.Merchant = t.Merchant
	}
	// Use the original user key at the acquiring step so reusing it for a different
	// endpoint is rejected by the shared receipt store.
	acquired, replay, err := items.RecordLinkedAcquisition(ctx, key, transactionID, p)
	if err != nil {
		return out, false, err
	}
	out.Acquisition = acquired
	linkKey := fmt.Sprintf("links:backfill:%x", sha256.Sum256([]byte(key)))
	link, _, err := links.CreateLink(ctx, linkKey, ledger.LinkInput{
		SourceType: "ITEM_ACQUISITION", SourceID: acquired.ID, TransactionID: t.ID, Relation: "PURCHASE",
	})
	if err != nil {
		return out, replay, err
	}
	out.Link = &link
	return out, replay, nil
}
