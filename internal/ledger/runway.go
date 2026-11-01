package ledger

type PlannedCashflowInput struct {
	Name          string `json:"name"`
	Direction     string `json:"direction"`
	Amount        Money  `json:"amount"`
	DueAt         string `json:"due_at"`
	AccountID     string `json:"account_id,omitempty"`
	Category      string `json:"category,omitempty"`
	ExpenseNature string `json:"expense_nature,omitempty"`
	Recurrence    string `json:"recurrence,omitempty"`
	IntervalDays  int    `json:"interval_days,omitempty"`
	Mandatory     bool   `json:"mandatory"`
	Note          string `json:"note,omitempty"`
}
type PlannedCashflow struct {
	PlannedCashflowInput
	ID                  string `json:"id"`
	Status              string `json:"status"`
	LinkedTransactionID string `json:"linked_transaction_id,omitempty"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}
type PlannedAction struct {
	AccountID  string `json:"account_id,omitempty"`
	OccurredAt string `json:"occurred_at,omitempty"`
}
type RunwaySettings struct {
	SafetyBuffer Money `json:"safety_buffer"`
	HorizonDays  int   `json:"runway_horizon_days"`
}
type LivingRunway struct {
	RunwaySettings
	LiquidBalance    SignedMoney `json:"liquid_balance"`
	ReservedOutflow  Money       `json:"reserved_outflow"`
	ExpectedIncome   Money       `json:"expected_income"`
	SafeToSpend      SignedMoney `json:"safe_to_spend"`
	DaysToNextIncome *int        `json:"days_to_next_income"`
	SafeDailySpend   Money       `json:"safe_daily_spend"`
	Mode             string      `json:"mode"`
	WindowEnd        string      `json:"window_end"`
	CashflowWarning  bool        `json:"cashflow_warning"`
}
type SpendingByNature struct {
	Groups               []MoneyGroup `json:"groups"`
	TotalSpending        SignedMoney  `json:"total_spending"`
	NormalizedLivingCost SignedMoney  `json:"normalized_living_cost"`
	Unclassified         SignedMoney  `json:"unclassified"`
}
type ExpenseNatureInput struct {
	ExpenseNature string `json:"expense_nature"`
}
