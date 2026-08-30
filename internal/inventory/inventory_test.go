package inventory

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeAndValidate(t *testing.T) {
	input, err := NormalizeAndValidate(ItemInput{Name: "  Dish soap  ", Quantity: 2.12349})
	if err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	if input.Name != "Dish soap" || input.Category != "other" || input.Icon != "inventory_2" || input.Unit != "pcs" {
		t.Fatalf("unexpected defaults: %#v", input)
	}
	if input.Quantity != 2.123 {
		t.Fatalf("quantity = %v, want 2.123", input.Quantity)
	}
}

func TestShortLivedDefaultsToSevenDaysAndClearsAtZero(t *testing.T) {
	before := time.Now().UTC().Add(ShortLivedTTL - time.Minute)
	input, err := NormalizeAndValidate(ItemInput{Name: "Pretzel", Quantity: 1, ShortLived: true})
	if err != nil || input.AutoConsumeAt == nil {
		t.Fatalf("NormalizeAndValidate() = %#v, %v", input, err)
	}
	scheduled, err := time.Parse(time.RFC3339Nano, *input.AutoConsumeAt)
	if err != nil || scheduled.Before(before) || scheduled.After(time.Now().UTC().Add(ShortLivedTTL+time.Minute)) {
		t.Fatalf("auto consume time = %v, %v", scheduled, err)
	}
	input.Quantity = 0
	input, err = NormalizeAndValidate(input)
	if err != nil || input.AutoConsumeAt != nil || !input.ShortLived {
		t.Fatalf("zero quantity input = %#v, %v", input, err)
	}
}

func TestNormalizeAndValidateRejectsInvalidFields(t *testing.T) {
	threshold := -1.0
	price := int64(-1)
	expiry := "27-08-2026"
	_, err := NormalizeAndValidate(ItemInput{
		Name: "", Category: "unknown", Icon: "bad icon", Quantity: -1,
		LowStockThreshold: &threshold, ExpiryDate: &expiry, UnitPriceCents: &price,
	})
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	for _, field := range []string{"name", "category", "icon", "quantity", "unit_price_cents", "low_stock_threshold", "expiry_date"} {
		if validation.Fields[field] == "" {
			t.Errorf("missing validation message for %q", field)
		}
	}
}

func TestApplyPatchClearsOptionalValues(t *testing.T) {
	threshold := 2.0
	price := int64(299)
	expiry := "2026-09-01"
	item := Item{Name: "Soap", Category: "cleaning", Icon: "soap", Quantity: 3, Unit: "bottles", UnitPriceCents: &price, LowStockThreshold: &threshold, ExpiryDate: &expiry}
	patched := ApplyPatch(item, ItemPatch{ClearLowStock: true, ClearExpiryDate: true, ClearUnitPrice: true})
	if patched.LowStockThreshold != nil || patched.ExpiryDate != nil || patched.UnitPriceCents != nil {
		t.Fatalf("optional values were not cleared: %#v", patched)
	}
}

func TestValidateAdjustment(t *testing.T) {
	value, err := ValidateAdjustment(-0.12349)
	if err != nil || value != -0.123 {
		t.Fatalf("ValidateAdjustment() = %v, %v; want -0.123, nil", value, err)
	}
	for _, invalid := range []float64{0, MaxQuantity + 1} {
		if _, err := ValidateAdjustment(invalid); err == nil {
			t.Errorf("ValidateAdjustment(%v) succeeded, want error", invalid)
		}
	}
}
