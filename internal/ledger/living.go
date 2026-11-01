package ledger

type ResidenceInput struct {
	Name                  string `json:"name"`
	AddressText           string `json:"address_text,omitempty"`
	MoveInDate            string `json:"move_in_date"`
	MoveOutDate           string `json:"move_out_date,omitempty"`
	MonthlyRent           Money  `json:"monthly_rent"`
	PaymentCycle          string `json:"payment_cycle,omitempty"`
	ContractEndDate       string `json:"contract_end_date,omitempty"`
	NetworkIncluded       bool   `json:"network_included"`
	ManagementFeeIncluded bool   `json:"management_fee_included"`
	Status                string `json:"status"`
	Note                  string `json:"note,omitempty"`
}
type Residence struct {
	ResidenceInput
	ID string `json:"id"`
}
type ResidenceLink struct {
	TransactionID string `json:"transaction_id"`
	Role          string `json:"role"`
}
type ResidenceCost struct {
	ResidenceID    string       `json:"residence_id"`
	Groups         []MoneyGroup `json:"groups"`
	ConsumedCost   SignedMoney  `json:"consumed_cost"`
	SetupCost      SignedMoney  `json:"setup_cost"`
	CapitalCost    SignedMoney  `json:"capital_cost"`
	DepositLocked  Money        `json:"deposit_locked"`
	DepositLoss    Money        `json:"deposit_loss"`
	NetCashOutflow SignedMoney  `json:"net_cash_outflow"`
}

// Claims track cash locked elsewhere without misclassifying it as spending or income.
type ClaimInput struct {
	Amount       Money  `json:"amount"`
	AccountID    string `json:"account_id"`
	ResidenceID  string `json:"residence_id,omitempty"`
	Counterparty string `json:"counterparty,omitempty"`
	Reason       string `json:"reason,omitempty"`
	DueAt        string `json:"due_at,omitempty"`
	OccurredAt   string `json:"occurred_at,omitempty"`
}
type Claim struct {
	ClaimInput
	ID                      string `json:"id"`
	Kind                    string `json:"kind"`
	ReturnedAmount          Money  `json:"returned_amount"`
	ForfeitedAmount         Money  `json:"forfeited_amount"`
	Status                  string `json:"status"`
	SourceTransactionID     string `json:"source_transaction_id"`
	SettlementTransactionID string `json:"settlement_transaction_id,omitempty"`
	ReturnedAt              string `json:"returned_at,omitempty"`
}
type ClaimSettlement struct {
	Amount     Money  `json:"amount"`
	AccountID  string `json:"account_id"`
	OccurredAt string `json:"occurred_at,omitempty"`
}
type ConsumptionForecast struct {
	ItemID                  string   `json:"item_id"`
	CurrentQuantity         float64  `json:"current_quantity"`
	AverageDailyConsumption *float64 `json:"average_daily_consumption"`
	EstimatedDaysRemaining  *float64 `json:"estimated_days_remaining"`
	EventCount              int      `json:"event_count"`
	EffectiveDays           float64  `json:"effective_days"`
	Status                  string   `json:"status"`
	Confidence              string   `json:"confidence"`
}
