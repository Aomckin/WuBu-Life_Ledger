package inventory

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

const (
	MaxNameLength     = 80
	MaxLocationLength = 60
	MaxUnitLength     = 20
	MaxNotesLength    = 500
	MaxQuantity       = 1_000_000_000
	MaxUnitPriceCents = 1_000_000_000
	ShortLivedTTL     = 7 * 24 * time.Hour
)

type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
}

var Categories = []Choice{
	{Value: "pantry", Label: "Pantry", Icon: "grocery"},
	{Value: "drinks", Label: "Drinks", Icon: "local_drink"},
	{Value: "cleaning", Label: "Cleaning", Icon: "cleaning_services"},
	{Value: "bathroom", Label: "Bathroom", Icon: "shower"},
	{Value: "laundry", Label: "Laundry", Icon: "laundry"},
	{Value: "household", Label: "Household", Icon: "home"},
	{Value: "health", Label: "Health", Icon: "medication"},
	{Value: "pets", Label: "Pets", Icon: "pets"},
	{Value: "baby", Label: "Baby", Icon: "child_friendly"},
	{Value: "other", Label: "Other", Icon: "inventory_2"},
}

var SuggestedUnits = []string{
	"pcs", "packs", "boxes", "bags", "bottles", "cans", "jars", "rolls",
	"tubes", "tablets", "g", "kg", "ml", "l",
}

var SuggestedLocations = []string{
	"Pantry", "Kitchen", "Fridge", "Freezer", "Bathroom", "Laundry room",
	"Utility closet", "Basement", "Garage", "Bedroom", "Office",
}

type Item struct {
	ItemType          string   `json:"item_type"`
	OpenedAt          *string  `json:"opened_at"`
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Category          string   `json:"category"`
	Icon              string   `json:"icon"`
	Quantity          float64  `json:"quantity"`
	UnitPriceCents    *int64   `json:"unit_price_cents"`
	Unit              string   `json:"unit"`
	Location          string   `json:"location"`
	LowStockThreshold *float64 `json:"low_stock_threshold"`
	ExpiryDate        *string  `json:"expiry_date"`
	ShortLived        bool     `json:"short_lived"`
	AutoConsumeAt     *string  `json:"auto_consume_at"`
	Notes             string   `json:"notes"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
	Version           int64    `json:"version"`
}

type ItemInput struct {
	Name              string   `json:"name"`
	Category          string   `json:"category"`
	Icon              string   `json:"icon"`
	Quantity          float64  `json:"quantity"`
	UnitPriceCents    *int64   `json:"unit_price_cents"`
	Unit              string   `json:"unit"`
	Location          string   `json:"location"`
	LowStockThreshold *float64 `json:"low_stock_threshold"`
	ExpiryDate        *string  `json:"expiry_date"`
	ShortLived        bool     `json:"short_lived"`
	AutoConsumeAt     *string  `json:"auto_consume_at"`
	Notes             string   `json:"notes"`
}

type ItemPatch struct {
	Name              *string  `json:"name"`
	Category          *string  `json:"category"`
	Icon              *string  `json:"icon"`
	Quantity          *float64 `json:"quantity"`
	UnitPriceCents    *int64   `json:"unit_price_cents"`
	ClearUnitPrice    bool     `json:"clear_unit_price"`
	Unit              *string  `json:"unit"`
	Location          *string  `json:"location"`
	LowStockThreshold *float64 `json:"low_stock_threshold"`
	ClearLowStock     bool     `json:"clear_low_stock_threshold"`
	ExpiryDate        *string  `json:"expiry_date"`
	ClearExpiryDate   bool     `json:"clear_expiry_date"`
	ShortLived        *bool    `json:"short_lived"`
	AutoConsumeAt     *string  `json:"auto_consume_at"`
	ClearAutoConsume  bool     `json:"clear_auto_consume_at"`
	Notes             *string  `json:"notes"`
}

type ListFilter struct {
	Search     string
	Category   string
	Location   string
	Status     string
	ExcludeOut bool
	Limit      int
	Offset     int
}

type ListResult struct {
	Items  []Item `json:"items"`
	Total  int    `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type Summary struct {
	Tracked             int   `json:"tracked"`
	LowStock            int   `json:"low_stock"`
	ExpiringSoon        int   `json:"expiring_soon"`
	OutOfStock          int   `json:"out_of_stock"`
	InventoryValueCents int64 `json:"inventory_value_cents"`
}

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "item validation failed" }

var iconPattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

func NormalizeAndValidate(input ItemInput) (ItemInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Category = strings.TrimSpace(strings.ToLower(input.Category))
	input.Icon = strings.TrimSpace(input.Icon)
	input.Unit = strings.TrimSpace(input.Unit)
	input.Location = strings.TrimSpace(input.Location)
	input.Notes = strings.TrimSpace(input.Notes)

	if input.Category == "" {
		input.Category = "other"
	}
	if input.Icon == "" {
		input.Icon = "inventory_2"
	}
	if input.Unit == "" {
		input.Unit = "pcs"
	}
	input.Quantity = roundQuantity(input.Quantity)

	fields := map[string]string{}
	if input.Name == "" {
		fields["name"] = "Name is required."
	} else if len([]rune(input.Name)) > MaxNameLength {
		fields["name"] = fmt.Sprintf("Name must be at most %d characters.", MaxNameLength)
	}
	if !validCategory(input.Category) {
		fields["category"] = "Choose a supported category."
	}
	if !iconPattern.MatchString(input.Icon) {
		fields["icon"] = "Icon must be a Material Symbol name."
	}
	if len([]rune(input.Unit)) > MaxUnitLength {
		fields["unit"] = fmt.Sprintf("Unit must be at most %d characters.", MaxUnitLength)
	}
	if len([]rune(input.Location)) > MaxLocationLength {
		fields["location"] = fmt.Sprintf("Location must be at most %d characters.", MaxLocationLength)
	}
	if len([]rune(input.Notes)) > MaxNotesLength {
		fields["notes"] = fmt.Sprintf("Notes must be at most %d characters.", MaxNotesLength)
	}
	if invalidQuantity(input.Quantity) {
		fields["quantity"] = fmt.Sprintf("Quantity must be between 0 and %d.", MaxQuantity)
	}
	if input.UnitPriceCents != nil && (*input.UnitPriceCents < 0 || *input.UnitPriceCents > MaxUnitPriceCents) {
		fields["unit_price_cents"] = fmt.Sprintf("Unit price must be between 0 and %d cents.", MaxUnitPriceCents)
	}
	if input.LowStockThreshold != nil {
		threshold := roundQuantity(*input.LowStockThreshold)
		input.LowStockThreshold = &threshold
		if invalidQuantity(threshold) {
			fields["low_stock_threshold"] = fmt.Sprintf("Threshold must be between 0 and %d.", MaxQuantity)
		}
	}
	if input.ExpiryDate != nil {
		date := strings.TrimSpace(*input.ExpiryDate)
		input.ExpiryDate = &date
		if date == "" {
			input.ExpiryDate = nil
		} else if _, err := time.Parse(time.DateOnly, date); err != nil {
			fields["expiry_date"] = "Expiry date must use YYYY-MM-DD."
		}
	}
	if !input.ShortLived || input.Quantity <= 0 {
		input.AutoConsumeAt = nil
	} else if input.AutoConsumeAt == nil {
		value := time.Now().UTC().Add(ShortLivedTTL).Format(time.RFC3339Nano)
		input.AutoConsumeAt = &value
	} else {
		value := strings.TrimSpace(*input.AutoConsumeAt)
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			fields["auto_consume_at"] = "Auto-consume time must use RFC 3339."
		} else {
			value = parsed.UTC().Format(time.RFC3339Nano)
			input.AutoConsumeAt = &value
		}
	}

	if len(fields) > 0 {
		return ItemInput{}, &ValidationError{Fields: fields}
	}
	return input, nil
}

func ApplyPatch(item Item, patch ItemPatch) ItemInput {
	input := ItemInput{
		Name: item.Name, Category: item.Category, Icon: item.Icon, Quantity: item.Quantity,
		Unit: item.Unit, UnitPriceCents: item.UnitPriceCents, Location: item.Location, LowStockThreshold: item.LowStockThreshold,
		ExpiryDate: item.ExpiryDate, ShortLived: item.ShortLived, AutoConsumeAt: item.AutoConsumeAt, Notes: item.Notes,
	}
	if patch.Name != nil {
		input.Name = *patch.Name
	}
	if patch.Category != nil {
		input.Category = *patch.Category
	}
	if patch.Icon != nil {
		input.Icon = *patch.Icon
	}
	if patch.Quantity != nil {
		input.Quantity = *patch.Quantity
	}
	if patch.ClearUnitPrice {
		input.UnitPriceCents = nil
	} else if patch.UnitPriceCents != nil {
		input.UnitPriceCents = patch.UnitPriceCents
	}
	if patch.Unit != nil {
		input.Unit = *patch.Unit
	}
	if patch.Location != nil {
		input.Location = *patch.Location
	}
	if patch.ClearLowStock {
		input.LowStockThreshold = nil
	} else if patch.LowStockThreshold != nil {
		input.LowStockThreshold = patch.LowStockThreshold
	}
	if patch.ClearExpiryDate {
		input.ExpiryDate = nil
	} else if patch.ExpiryDate != nil {
		input.ExpiryDate = patch.ExpiryDate
	}
	if patch.ShortLived != nil {
		input.ShortLived = *patch.ShortLived
	}
	if patch.ClearAutoConsume {
		input.AutoConsumeAt = nil
	} else if patch.AutoConsumeAt != nil {
		input.AutoConsumeAt = patch.AutoConsumeAt
	}
	if patch.Notes != nil {
		input.Notes = *patch.Notes
	}
	return input
}

func ValidateAdjustment(delta float64) (float64, error) {
	delta = roundQuantity(delta)
	if math.IsNaN(delta) || math.IsInf(delta, 0) || delta == 0 || math.Abs(delta) > MaxQuantity {
		return 0, errors.New("delta must be a non-zero number within the supported quantity range")
	}
	return delta, nil
}

func validCategory(value string) bool {
	for _, category := range Categories {
		if category.Value == value {
			return true
		}
	}
	return false
}

func invalidQuantity(value float64) bool {
	return math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > MaxQuantity
}

func roundQuantity(value float64) float64 {
	return math.Round(value*1000) / 1000
}
