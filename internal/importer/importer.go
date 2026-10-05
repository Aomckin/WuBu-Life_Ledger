// Package importer is the v0.1 import boundary: adapters produce the same
// PurchaseInput consumed by RecordPurchase; they must not write SQL directly.
package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"stockroom/internal/ledger"
)

// DecodePurchaseDraft accepts already-normalized JSON from an external adapter.
// Alipay/WeChat CSV semantics (refunds, status, columns) are deliberately deferred.
func DecodePurchaseDraft(r io.Reader) (ledger.PurchaseInput, error) {
	var p ledger.PurchaseInput
	d := json.NewDecoder(io.LimitReader(r, 1<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(&p); e != nil {
		return p, e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return p, fmt.Errorf("expected one purchase draft")
	}
	p.Source = "import"
	normalized, err := ledger.ValidatePurchase(p)
	// Keep omitted time omitted until the event's idempotent commit boundary.
	if p.PurchasedAt == "" {
		normalized.PurchasedAt = ""
	}
	return normalized, err
}

// RecordDraft leaves transaction and external_id deduplication to the Store.
func RecordDraft(ctx context.Context, record func(context.Context, string, ledger.PurchaseInput) (ledger.Purchase, bool, error), key string, r io.Reader) (ledger.Purchase, bool, error) {
	p, e := DecodePurchaseDraft(r)
	if e != nil {
		return ledger.Purchase{}, false, e
	}
	return record(ctx, key, p)
}
