package importer

import (
	"strings"
	"testing"
)

func TestDraftKeepsOmittedTimeForStableIdempotency(t *testing.T) {
	p, err := DecodePurchaseDraft(strings.NewReader(`{"total_amount":30,"items":[{"name":"理发","quantity":1,"unit":"次","unit_price":30,"subtotal":30,"item_type":"UNTRACKED"}]}`))
	if err != nil || p.Source != "import" || p.PurchasedAt != "" || p.TotalAmount != 3000 {
		t.Fatal(p, err)
	}
	if _, err = DecodePurchaseDraft(strings.NewReader(`{"items":[]} {}`)); err == nil {
		t.Fatal("trailing data accepted")
	}
}
