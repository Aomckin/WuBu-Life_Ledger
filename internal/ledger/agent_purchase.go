package ledger

import (
	"math"
	"stockroom/internal/inventory"
)

// Agent inputs intentionally do not mirror the persistence DTO.
type AgentPurchaseLine struct {
	ItemType string  `json:"item_type,omitempty"`
	Location string  `json:"location,omitempty"`
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	Price    *Money  `json:"price,omitempty"`
	Spec     string  `json:"spec,omitempty"`
}
type AgentPurchaseInput struct {
	AccountID     string              `json:"account_id,omitempty"`
	Items         []AgentPurchaseLine `json:"items"`
	Total         *Money              `json:"total,omitempty"`
	Merchant      string              `json:"merchant,omitempty"`
	PaymentMethod string              `json:"payment_method,omitempty"`
	Note          string              `json:"note,omitempty"`
}

func (a AgentPurchaseInput) PurchaseInput() PurchaseInput {
	p := PurchaseInput{AccountID: a.AccountID, Merchant: a.Merchant, PaymentMethod: a.PaymentMethod, Note: a.Note, Source: "agent", Currency: "CNY", Items: []PurchaseLine{}}
	for _, l := range a.Items {
		price := Money(0)
		if l.Price != nil {
			price = *l.Price
		} else if len(a.Items) == 1 && a.Total != nil && l.Quantity > 0 {
			price = Money(math.Round(float64(*a.Total) / l.Quantity))
		}
		subtotal := Money(math.Round(float64(price) * l.Quantity))
		if len(a.Items) == 1 && l.Price == nil && a.Total != nil {
			subtotal = *a.Total
		}
		kind := l.ItemType
		if kind == "" {
			kind = Consumable
		}
		p.Items = append(p.Items, PurchaseLine{Location: l.Location, Name: l.Name, ProductSpec: inventory.ProductSpec{Spec: l.Spec}, Quantity: l.Quantity, Unit: l.Unit, UnitPrice: price, Subtotal: subtotal, ItemType: kind})
		p.TotalAmount += subtotal
	}
	if a.Total != nil {
		p.TotalAmount = *a.Total
	}
	return p
}
