package ledger

import (
	"fmt"
	"math"

	"stockroom/internal/inventory"
)

// Amount mismatches are reviewable warnings; invalid numbers remain errors.
func PurchaseWarnings(p PurchaseInput) []Warning {
	warnings := []Warning{}
	var sum Money
	for i, l := range p.Items {
		sum += l.Subtotal
		line := i + 1
		if math.Abs(float64(l.Subtotal)-math.Round(l.Quantity*float64(l.UnitPrice))) > 1 {
			warnings = append(warnings, Warning{"PURCHASE_AMOUNT_INCONSISTENT", fmt.Sprintf("第 %d 行小计与数量 × 单价不一致，请核对折扣或商品数量。", line), &line})
		}
		if l.PackageSize != nil && math.Abs(l.Quantity-*l.PackageSize) < 0.0001 && inventory.UnitKey(l.Unit) == inventory.UnitKey(l.PackageUnit) && p.TotalAmount == l.UnitPrice && l.Quantity != 1 {
			warnings = append(warnings, Warning{"SPEC_QUANTITY_CONFLICT", fmt.Sprintf("第 %d 行可能把包装规格 %s 当成了购买数量，请确认数量和库存单位。", line, l.Spec), &line})
		}
	}
	if math.Abs(float64(sum-p.Discount+p.ExtraFee-p.TotalAmount)) > 1 {
		warnings = append(warnings, Warning{"PURCHASE_AMOUNT_INCONSISTENT", "商品合计 − 优惠 + 附加费用与实际付款不一致，请核对优惠、费用或抹零。", nil})
	}
	return warnings
}

func DecoratePurchase(p *Purchase) {
	p.PurchaseID, p.ExpenseAmount = p.ID, p.TotalAmount
	p.Warnings = PurchaseWarnings(p.PurchaseInput)
	p.ValidationStatus = "VALID"
	if len(p.Warnings) > 0 {
		p.ValidationStatus = "WARNING"
	}
}
