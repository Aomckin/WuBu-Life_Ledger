// Package ledger defines reality-event DTOs. Money is stored as integer cents.
package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"stockroom/internal/inventory"
)

const Consumable = "CONSUMABLE"
const Durable = "DURABLE"
const Untracked = "UNTRACKED"

var ErrInvalid = errors.New("invalid ledger input")
var ErrConflict = errors.New("ledger conflict")
var ErrIdempotency = errors.New("idempotency key reused with a different request")

type Money int64

func (m *Money) UnmarshalJSON(b []byte) error {
	r, ok := new(big.Rat).SetString(string(b))
	if !ok {
		return fmt.Errorf("%w: money must be a number", ErrInvalid)
	}
	r.Mul(r, big.NewRat(100, 1))
	if !r.IsInt() || !r.Num().IsInt64() || r.Sign() < 0 || r.Num().Int64() > 1_000_000_000_000 {
		return fmt.Errorf("%w: money needs at most two decimals and must be between 0 and 10000000000", ErrInvalid)
	}
	*m = Money(r.Num().Int64())
	return nil
}
func (m Money) MarshalJSON() ([]byte, error) {
	sign := ""
	if m < 0 {
		sign = "-"
		m = -m
	}
	return []byte(fmt.Sprintf("%s%d.%02d", sign, m/100, m%100)), nil
}

type PurchaseInput struct {
	ExpenseNature string         `json:"expense_nature,omitempty"`
	AccountID     string         `json:"account_id,omitempty"`
	Merchant      string         `json:"merchant"`
	PurchasedAt   string         `json:"purchased_at"`
	TotalAmount   Money          `json:"total_amount"`
	Currency      string         `json:"currency"`
	PaymentMethod string         `json:"payment_method"`
	Source        string         `json:"source"`
	ExternalID    string         `json:"external_id"`
	Category      string         `json:"category"`
	Discount      Money          `json:"discount"`
	ExtraFee      Money          `json:"extra_fee"`
	Note          string         `json:"note"`
	Items         []PurchaseLine `json:"items"`
}
type PurchaseLine struct {
	inventory.ProductSpec
	QuantityAdded  float64  `json:"quantity_added,omitempty"`
	InventoryAfter *float64 `json:"inventory_after,omitempty"`
	ID             string   `json:"id,omitempty"`
	ItemID         string   `json:"item_id,omitempty"`
	Name           string   `json:"name"`
	Quantity       float64  `json:"quantity"`
	Unit           string   `json:"unit"`
	UnitPrice      Money    `json:"unit_price"`
	Subtotal       Money    `json:"subtotal"`
	ItemType       string   `json:"item_type"`
	Category       string   `json:"category,omitempty"`
	Location       string   `json:"location,omitempty"`
	Confidence     *float64 `json:"confidence,omitempty"`
	Note           string   `json:"note,omitempty"`
	Brand          string   `json:"brand,omitempty"`
	Model          string   `json:"model,omitempty"`
	SerialNumber   string   `json:"serial_number,omitempty"`
	WarrantyUntil  string   `json:"warranty_until,omitempty"`
}
type Purchase struct {
	Status         string `json:"status"`
	RefundedAmount Money  `json:"refunded_amount"`
	ID             string `json:"id"`
	PurchaseInput
	ItemTotal        Money     `json:"item_total"`
	CreatedAt        string    `json:"created_at"`
	UpdatedAt        string    `json:"updated_at"`
	PurchaseID       string    `json:"purchase_id"`
	ExpenseAmount    Money     `json:"expense_amount"`
	ValidationStatus string    `json:"validation_status"`
	Warnings         []Warning `json:"warnings"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Line    *int   `json:"line,omitempty"`
}

// Amendments replace the entire purchase; IDs retain existing line identities.
type PurchaseAmendment struct {
	PurchaseInput
	ExpectedUpdatedAt string `json:"expected_updated_at"`
}
type PurchaseDeletion struct {
	ExpectedUpdatedAt string `json:"expected_updated_at"`
}
type PurchaseDeletionResult struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}
type InventoryInput struct {
	Name string `json:"name,omitempty"`
	Spec string `json:"spec,omitempty"`
	// Delta is for legacy /adjust; Quantity is consume amount or absolute stock.
	Delta      *float64 `json:"delta,omitempty"`
	Quantity   *float64 `json:"quantity,omitempty"`
	OccurredAt string   `json:"occurred_at"`
	Source     string   `json:"source"`
	Note       string   `json:"note"`
}
type InventoryEvent struct {
	ID              string  `json:"id"`
	ItemID          string  `json:"item_id"`
	Type            string  `json:"type"`
	QuantityDelta   float64 `json:"quantity_delta"`
	QuantityBefore  float64 `json:"quantity_before"`
	QuantityAfter   float64 `json:"quantity_after"`
	PurchaseEventID *string `json:"purchase_event_id"`
	OccurredAt      string  `json:"occurred_at"`
	Source          string  `json:"source"`
	Note            string  `json:"note"`
}
type InventoryResult struct {
	Item  inventory.Item `json:"item"`
	Event InventoryEvent `json:"event"`
}
type DurableDetails struct {
	HasOriginalPackaging *bool  `json:"has_original_packaging"`
	PurchaseProof        string `json:"purchase_proof"`
	EstimatedResaleValue *Money `json:"estimated_resale_value"`
	ResidenceID          string `json:"residence_id"`
	PurchasePrice        Money  `json:"purchase_price"`
	PurchaseEventID      string `json:"purchase_event_id"`
	Location             string `json:"location"`
	UseDays              int    `json:"use_days"`
	WarrantyDays         *int   `json:"warranty_days"`
	ItemID               string `json:"item_id"`
	Brand                string `json:"brand"`
	Model                string `json:"model"`
	SerialNumber         string `json:"serial_number"`
	PurchaseDate         string `json:"purchase_date"`
	WarrantyUntil        string `json:"warranty_until"`
	Status               string `json:"status"`
	SoldAt               string `json:"sold_at"`
	SoldPrice            *Money `json:"sold_price"`
	Note                 string `json:"note"`
}
type DurablePatch struct {
	HasOriginalPackaging *bool   `json:"has_original_packaging,omitempty"`
	PurchaseProof        *string `json:"purchase_proof,omitempty"`
	EstimatedResaleValue *Money  `json:"estimated_resale_value,omitempty"`
	ResidenceID          *string `json:"residence_id,omitempty"`
	Brand                *string `json:"brand"`
	Model                *string `json:"model"`
	SerialNumber         *string `json:"serial_number"`
	WarrantyUntil        *string `json:"warranty_until"`
	Status               *string `json:"status"`
	SoldAt               *string `json:"sold_at"`
	SoldPrice            *Money  `json:"sold_price"`
	Note                 *string `json:"note"`
}
type MaintenanceInput struct {
	AccountID   string `json:"account_id,omitempty"`
	OccurredAt  string `json:"occurred_at"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Cost        Money  `json:"cost"`
	Status      string `json:"status"`
	Source      string `json:"source"`
	Note        string `json:"note"`
}
type Maintenance struct {
	ID        string  `json:"id"`
	ItemID    string  `json:"item_id"`
	ExpenseID *string `json:"expense_id"`
	MaintenanceInput
}
type Expense struct {
	ID                 string  `json:"id"`
	PurchaseEventID    *string `json:"purchase_event_id"`
	MaintenanceEventID *string `json:"maintenance_event_id"`
	Amount             Money   `json:"amount"`
	Currency           string  `json:"currency"`
	Category           string  `json:"category"`
	Merchant           string  `json:"merchant"`
	PaymentMethod      string  `json:"payment_method"`
	OccurredAt         string  `json:"occurred_at"`
	Source             string  `json:"source"`
	Note               string  `json:"note"`
}
type Alias struct {
	ID              string `json:"id"`
	ItemID          string `json:"item_id"`
	Alias           string `json:"alias"`
	NormalizedAlias string `json:"normalized_alias"`
}
type HistoryFilter struct {
	ItemID, From, To, Category string
	Limit, Offset              int
}
type Price struct {
	PurchaseEventID string  `json:"purchase_event_id"`
	Merchant        string  `json:"merchant"`
	PurchasedAt     string  `json:"purchased_at"`
	Currency        string  `json:"currency"`
	UnitPrice       Money   `json:"unit_price"`
	Quantity        float64 `json:"quantity"`
	Subtotal        Money   `json:"subtotal"`
}
type Dashboard struct {
	LiquidBalance      SignedMoney        `json:"liquid_balance"`
	ConsumptionBalance SignedMoney        `json:"consumption_balance"`
	TotalValue         SignedMoney        `json:"total_value"`
	Accounts           []Account          `json:"accounts"`
	ConsumableCount    int                `json:"consumable_count"`
	TotalBalance       SignedMoney        `json:"total_balance"`
	AvailableBalance   SignedMoney        `json:"available_balance"`
	TodayExpense       Money              `json:"today_expense"`
	DurableCount       int                `json:"durable_count"`
	RecentTransactions []MoneyTransaction `json:"recent_transactions"`
	Month              string             `json:"month"`
	Currency           string             `json:"currency"`
	MonthExpense       Money              `json:"month_expense"`
	Inventory          inventory.Summary  `json:"inventory"`
	RecentPurchases    []Purchase         `json:"recent_purchases"`
	RecentConsumption  []InventoryEvent   `json:"recent_consumption"`
	RecentMaintenance  []Maintenance      `json:"recent_maintenance"`
}

func NormalizeName(s string) string {
	name, p, e := inventory.NormalizeProduct(s, inventory.ProductSpec{})
	if e != nil {
		return inventory.NameKey(s)
	}
	key := inventory.NameKey(name)
	if p.Spec != "" {
		key += "#" + inventory.SpecKey(p.Spec)
	}
	return key
}
func Timestamp(s string) (string, error) {
	if s == "" {
		return time.Now().UTC().Format(time.RFC3339Nano), nil
	}
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return "", fmt.Errorf("%w: timestamp must be RFC3339 with timezone", ErrInvalid)
	}
	return t.UTC().Format(time.RFC3339Nano), nil
}
func Source(s string) (string, error) {
	if s == "" {
		s = "manual"
	}
	switch s {
	case "manual", "agent", "import", "system", "receipt":
		return s, nil
	}
	return "", fmt.Errorf("%w: unsupported source", ErrInvalid)
}
func ValidatePurchase(p PurchaseInput) (PurchaseInput, error) {
	switch p.ExpenseNature {
	case "", "DAILY", "FIXED", "SETUP", "CAPITAL", "EXCEPTIONAL":
	default:
		return p, ErrInvalid
	}

	p.Items = append([]PurchaseLine(nil), p.Items...)
	var e error
	p.Merchant = strings.TrimSpace(p.Merchant)
	p.ExternalID = strings.TrimSpace(p.ExternalID)
	if len([]rune(p.Merchant)) > 200 || len(p.ExternalID) > 200 || len([]rune(p.Note)) > 2000 || len(p.PaymentMethod) > 100 || len(p.Category) > 100 {
		return p, fmt.Errorf("%w: purchase text too long", ErrInvalid)
	}
	if p.TotalAmount < 0 || p.TotalAmount > 1e12 || p.Discount < 0 || p.ExtraFee < 0 {
		return p, fmt.Errorf("%w: invalid amount", ErrInvalid)
	}
	if p.PurchasedAt, e = Timestamp(p.PurchasedAt); e != nil {
		return p, e
	}
	if p.Source, e = Source(p.Source); e != nil {
		return p, e
	}
	if p.Currency == "" {
		p.Currency = "CNY"
	}
	if p.Currency != "CNY" {
		return p, fmt.Errorf("%w: v0.1 supports CNY only", ErrInvalid)
	}
	if p.Category == "" {
		p.Category = "other"
	}
	if len(p.Items) < 1 || len(p.Items) > 500 {
		return p, fmt.Errorf("%w: purchase needs 1 to 500 items", ErrInvalid)
	}
	for i := range p.Items {
		l := &p.Items[i]
		l.ID = ""
		l.QuantityAdded = 0
		l.InventoryAfter = nil
		l.Name = strings.TrimSpace(l.Name)
		l.Name, l.ProductSpec, e = inventory.NormalizeProduct(l.Name, l.ProductSpec)
		if e != nil {
			return p, fmt.Errorf("%w: %s", ErrInvalid, e)
		}
		l.ItemType = strings.ToUpper(l.ItemType)
		if l.ItemType == "" {
			l.ItemType = Consumable
		}
		if l.ItemType != Consumable && l.ItemType != Durable && l.ItemType != Untracked {
			return p, fmt.Errorf("%w: unsupported item_type", ErrInvalid)
		}
		if math.IsNaN(l.Quantity) || math.IsInf(l.Quantity, 0) || l.Quantity <= 0 || l.Quantity > inventory.MaxQuantity || math.Abs(l.Quantity*1000-math.Round(l.Quantity*1000)) > 0.0001 {
			return p, fmt.Errorf("%w: quantity needs 3 decimals and must be positive", ErrInvalid)
		}
		if l.UnitPrice < 0 || l.UnitPrice > inventory.MaxUnitPriceCents || l.Subtotal < 0 || l.Subtotal > 1e12 {
			return p, fmt.Errorf("%w: invalid line amount", ErrInvalid)
		}
		if l.ItemType == Durable && l.Quantity != 1 {
			return p, fmt.Errorf("%w: durable items are individual instances; use one line per instance", ErrInvalid)
		}
		if l.ItemType == Untracked && l.ItemID != "" {
			return p, fmt.Errorf("%w: untracked line cannot link stock", ErrInvalid)
		}
		if l.Confidence != nil && (math.IsNaN(*l.Confidence) || math.IsInf(*l.Confidence, 0) || *l.Confidence < 0 || *l.Confidence > 1) {
			return p, fmt.Errorf("%w: confidence must be between 0 and 1", ErrInvalid)
		}
		if l.WarrantyUntil != "" {
			if _, e := time.Parse(time.DateOnly, l.WarrantyUntil); e != nil {
				return p, fmt.Errorf("%w: warranty_until must be YYYY-MM-DD", ErrInvalid)
			}
		}
		input, e := inventory.NormalizeAndValidate(inventory.ItemInput{Name: l.Name, Quantity: l.Quantity, Unit: l.Unit, Category: l.Category, Location: l.Location, Notes: l.Note})
		if e != nil {
			return p, fmt.Errorf("%w: line %d: %s", ErrInvalid, i+1, e)
		}
		l.Unit = input.Unit
		l.Category = input.Category
		if len(l.Brand) > 200 || len(l.Model) > 200 || len(l.SerialNumber) > 200 || len(l.ItemID) > 100 {
			return p, fmt.Errorf("%w: item text too long", ErrInvalid)
		}
	}
	return p, nil
}

// CanonicalRequest fingerprints the submitted DTO before time/default resolution.
func CanonicalRequest(v any) ([]byte, error) { return json.Marshal(v) }
