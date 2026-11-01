package ledger

import (
	"fmt"
	"math/big"
	"time"
)

// SignedMoney is used only for balances and deltas, never for absolute payments.
type SignedMoney int64

func (m SignedMoney) MarshalJSON() ([]byte, error) { return Money(m).MarshalJSON() }
func (m *SignedMoney) UnmarshalJSON(b []byte) error {
	r, ok := new(big.Rat).SetString(string(b))
	if !ok {
		return fmt.Errorf("%w: invalid balance", ErrInvalid)
	}
	r.Mul(r, big.NewRat(100, 1))
	if !r.IsInt() || !r.Num().IsInt64() || r.Num().Int64() < -1e12 || r.Num().Int64() > 1e12 {
		return fmt.Errorf("%w: balance requires two decimals within supported range", ErrInvalid)
	}
	*m = SignedMoney(r.Num().Int64())
	return nil
}

const StorageAccount = "STORAGE"
const ConsumptionAccount = "CONSUMPTION"

// Merchant mismatches require explicit account or merchant correction before payment.
var ErrAccountMerchantMismatch = fmt.Errorf("%w: ACCOUNT_MERCHANT_MISMATCH", ErrConflict)

type AccountFilter struct {
	AccountClass, Merchant string
	Active                 *bool
}

type AccountInput struct {
	AccountClass   string      `json:"account_class,omitempty"`
	Merchant       string      `json:"merchant,omitempty"`
	Name           string      `json:"name"`
	Type           string      `json:"type"`
	Currency       string      `json:"currency"`
	InitialBalance SignedMoney `json:"initial_balance"`
	CreditLimit    *Money      `json:"credit_limit,omitempty"`
	Note           string      `json:"note"`
}
type Account struct {
	AccountClass   string      `json:"account_class"`
	Merchant       string      `json:"merchant"`
	Warnings       []Warning   `json:"warnings"`
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Type           string      `json:"type"`
	Currency       string      `json:"currency"`
	CurrentBalance SignedMoney `json:"current_balance"`
	CreditLimit    *Money      `json:"credit_limit"`
	Active         bool        `json:"active"`
	Note           string      `json:"note"`
	CreatedAt      string      `json:"created_at"`
	UpdatedAt      string      `json:"updated_at"`
}
type AccountPatch struct {
	AccountClass *string `json:"account_class,omitempty"`
	Merchant     *string `json:"merchant,omitempty"`
	Name         *string `json:"name,omitempty"`
	Active       *bool   `json:"active,omitempty"`
	Note         *string `json:"note,omitempty"`
	CreditLimit  *Money  `json:"credit_limit,omitempty"`
}
type MoneySettings struct {
	DefaultPaymentAccount string `json:"default_payment_account"`
}
type ReturnItem struct {
	ItemID   string  `json:"item_id"`
	Quantity float64 `json:"quantity"`
}
type MoneyInput struct {
	ExpenseNature        string       `json:"expense_nature,omitempty"`
	AccountID            string       `json:"account_id,omitempty"`
	FromAccount          string       `json:"from_account,omitempty"`
	ToAccount            string       `json:"to_account,omitempty"`
	Amount               Money        `json:"amount,omitempty"`
	ActualBalance        *SignedMoney `json:"actual_balance,omitempty"`
	Category             string       `json:"category,omitempty"`
	Merchant             string       `json:"merchant,omitempty"`
	PurchaseEventID      string       `json:"purchase_event_id,omitempty"`
	RelatedTransactionID string       `json:"related_transaction_id,omitempty"`
	ExternalID           string       `json:"external_id,omitempty"`
	OccurredAt           string       `json:"occurred_at,omitempty"`
	Source               string       `json:"source,omitempty"`
	Note                 string       `json:"note,omitempty"`
	Reason               string       `json:"reason,omitempty"`
	ReturnItems          []ReturnItem `json:"return_items,omitempty"`
}
type MoneyTransaction struct {
	ExpenseNature        string      `json:"expense_nature"`
	ID                   string      `json:"id"`
	AccountID            string      `json:"account_id"`
	Type                 string      `json:"type"`
	Amount               Money       `json:"amount"`
	BalanceDelta         SignedMoney `json:"balance_delta"`
	BalanceBefore        SignedMoney `json:"balance_before"`
	BalanceAfter         SignedMoney `json:"balance_after"`
	Category             string      `json:"category"`
	Merchant             string      `json:"merchant"`
	PurchaseEventID      string      `json:"purchase_event_id,omitempty"`
	MaintenanceEventID   string      `json:"maintenance_event_id,omitempty"`
	RelatedTransactionID string      `json:"related_transaction_id,omitempty"`
	ExternalID           string      `json:"external_id,omitempty"`
	OccurredAt           string      `json:"occurred_at"`
	Source               string      `json:"source"`
	Note                 string      `json:"note"`
	Reason               string      `json:"reason"`
	CreatedAt            string      `json:"created_at"`
	VoidedAt             string      `json:"voided_at,omitempty"`
}
type TopUpInput struct {
	FromAccount    string `json:"from_account"`
	ToAccount      string `json:"to_account"`
	PaidAmount     Money  `json:"paid_amount"`
	CreditedAmount Money  `json:"credited_amount"`
	OccurredAt     string `json:"occurred_at,omitempty"`
	Source         string `json:"source,omitempty"`
	Note           string `json:"note,omitempty"`
}
type AccountStatistics struct {
	AccountID      string      `json:"account_id"`
	TopUpPrincipal Money       `json:"top_up_principal"`
	Bonus          Money       `json:"bonus"`
	Expense        Money       `json:"expense"`
	Refund         Money       `json:"refund"`
	NetExpense     SignedMoney `json:"net_expense"`
	CurrentBalance SignedMoney `json:"current_balance"`
	BonusRatio     float64     `json:"bonus_ratio"`
}
type MoneyResult struct {
	Warnings     []Warning          `json:"warnings"`
	Transactions []MoneyTransaction `json:"transactions"`
}
type MoneyFilter struct {
	HistoryFilter
	AccountID, Type, Merchant, GroupBy, ToAccount string
}
type MoneyGroup struct {
	Key    string      `json:"key"`
	Amount SignedMoney `json:"amount"`
	Count  int         `json:"count"`
}
type MoneySummary struct {
	Bonus       Money        `json:"bonus"`
	Currency    string       `json:"currency"`
	Expense     Money        `json:"expense"`
	Refund      Money        `json:"refund"`
	NetExpense  SignedMoney  `json:"net_expense"`
	Income      Money        `json:"income"`
	TransferOut Money        `json:"transfer_out"`
	Groups      []MoneyGroup `json:"groups"`
}
type BalanceSummary struct {
	LiquidBalance      SignedMoney `json:"liquid_balance"`
	ConsumptionBalance SignedMoney `json:"consumption_balance"`
	TotalValue         SignedMoney `json:"total_value"`
	Currency           string      `json:"currency"`
	TotalBalance       SignedMoney `json:"total_balance"`
	AvailableBalance   SignedMoney `json:"available_balance"`
	Accounts           []Account   `json:"accounts"`
}
type DurableEvent struct {
	ID         string `json:"id"`
	ItemID     string `json:"item_id"`
	Type       string `json:"type"`
	OccurredAt string `json:"occurred_at"`
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
	Location   string `json:"location"`
	Note       string `json:"note"`
	Source     string `json:"source"`
}
type DurableActionInput struct {
	Status     string `json:"status,omitempty"`
	Location   string `json:"location,omitempty"`
	Amount     Money  `json:"amount,omitempty"`
	AccountID  string `json:"account_id,omitempty"`
	OccurredAt string `json:"occurred_at,omitempty"`
	Source     string `json:"source,omitempty"`
	Note       string `json:"note,omitempty"`
}
type ItemCost struct {
	ItemID           string      `json:"item_id"`
	PurchaseCost     Money       `json:"purchase_cost"`
	MaintenanceCost  Money       `json:"maintenance_cost"`
	TotalCost        Money       `json:"total_cost"`
	Refund           Money       `json:"refund"`
	NetCost          SignedMoney `json:"net_cost"`
	PurchaseCount    int         `json:"purchase_count"`
	PurchaseQuantity float64     `json:"purchase_quantity"`
	LatestPrice      *Money      `json:"latest_price"`
	MaintenanceCount int         `json:"maintenance_count"`
}

// PeriodRange uses Shanghai calendar boundaries; weeks start on Monday.
func PeriodRange(period, from, to string, now time.Time) (string, string, error) {
	if from != "" || to != "" {
		if period != "" {
			return "", "", fmt.Errorf("%w: period and custom range are exclusive", ErrInvalid)
		}
		for _, v := range []string{from, to} {
			if v != "" {
				if _, e := time.Parse(time.RFC3339Nano, v); e != nil {
					return "", "", fmt.Errorf("%w: range timestamp", ErrInvalid)
				}
			}
		}
		if from != "" && to != "" {
			a, _ := time.Parse(time.RFC3339Nano, from)
			b, _ := time.Parse(time.RFC3339Nano, to)
			if !a.Before(b) {
				return "", "", fmt.Errorf("%w: invalid range", ErrInvalid)
			}
		}
		return from, to, nil
	}
	if period == "" {
		return "", "", nil
	}
	z := time.FixedZone("Asia/Shanghai", 8*3600)
	n := now.In(z)
	start := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, z)
	var end time.Time
	switch period {
	case "today":
		end = start.AddDate(0, 0, 1)
	case "week":
		start = start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
		end = start.AddDate(0, 0, 7)
	case "month":
		start = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, z)
		end = start.AddDate(0, 1, 0)
	case "year":
		start = time.Date(n.Year(), 1, 1, 0, 0, 0, 0, z)
		end = start.AddDate(1, 0, 0)
	default:
		return "", "", fmt.Errorf("%w: period must be today/week/month/year", ErrInvalid)
	}
	return start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339), nil
}
