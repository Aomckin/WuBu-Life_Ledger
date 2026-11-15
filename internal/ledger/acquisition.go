package ledger

import "stockroom/internal/inventory"

// Acquisition is an Items request. Payment and pricing belong to Finance.
type AcquisitionInput struct {
	Merchant   string            `json:"merchant,omitempty"`
	OccurredAt string            `json:"occurred_at,omitempty"`
	Source     string            `json:"source,omitempty"`
	ExternalID string            `json:"external_id,omitempty"`
	Note       string            `json:"note,omitempty"`
	Items      []AcquisitionLine `json:"items"`
}

type AcquisitionLine struct {
	inventory.ProductSpec
	ID            string  `json:"id,omitempty"`
	ItemID        string  `json:"item_id,omitempty"`
	Name          string  `json:"name"`
	Quantity      float64 `json:"quantity"`
	Unit          string  `json:"unit"`
	ItemType      string  `json:"item_type,omitempty"`
	Category      string  `json:"category,omitempty"`
	Location      string  `json:"location,omitempty"`
	Note          string  `json:"note,omitempty"`
	Brand         string  `json:"brand,omitempty"`
	Model         string  `json:"model,omitempty"`
	SerialNumber  string  `json:"serial_number,omitempty"`
	WarrantyUntil string  `json:"warranty_until,omitempty"`
}

func (a AcquisitionInput) PurchaseInput() PurchaseInput {
	p := PurchaseInput{Merchant: a.Merchant, PurchasedAt: a.OccurredAt, Source: a.Source, ExternalID: a.ExternalID, Note: a.Note}
	for _, l := range a.Items {
		p.Items = append(p.Items, PurchaseLine{
			ProductSpec: l.ProductSpec, ID: l.ID, ItemID: l.ItemID, Name: l.Name, Quantity: l.Quantity,
			Unit: l.Unit, ItemType: l.ItemType, Category: l.Category, Location: l.Location,
			Note: l.Note, Brand: l.Brand, Model: l.Model, SerialNumber: l.SerialNumber, WarrantyUntil: l.WarrantyUntil,
		})
	}
	return p
}

type AcquisitionAmendment struct {
	AcquisitionInput
	ExpectedUpdatedAt string `json:"expected_updated_at"`
}

type MaintenanceRecord struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	OccurredAt  string `json:"occurred_at,omitempty"`
	Source      string `json:"source,omitempty"`
	Note        string `json:"note,omitempty"`
}

type SaleRecord struct {
	OccurredAt string `json:"occurred_at,omitempty"`
	Source     string `json:"source,omitempty"`
	Note       string `json:"note,omitempty"`
}
