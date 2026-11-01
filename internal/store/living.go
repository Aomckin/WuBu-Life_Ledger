package store

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"stockroom/internal/ledger"
	"strings"
	"time"
)

const residenceColumns = `id,name,address_text,move_in_date,move_out_date,monthly_rent_cents,payment_cycle,contract_end_date,network_included,management_fee_included,status,note`

func scanResidence(r scanner) (p ledger.Residence, e error) {
	e = r.Scan(&p.ID, &p.Name, &p.AddressText, &p.MoveInDate, &p.MoveOutDate, &p.MonthlyRent, &p.PaymentCycle, &p.ContractEndDate, &p.NetworkIncluded, &p.ManagementFeeIncluded, &p.Status, &p.Note)
	if e == sql.ErrNoRows {
		e = ErrNotFound
	}
	return
}
func (s *Store) SaveResidence(ctx context.Context, key, id string, p ledger.ResidenceInput) (out ledger.Residence, replay bool, e error) {
	replay, e = s.ledgerTx(ctx, key, "residence:"+id, p, &out, func(tx *sql.Tx) error {
		if strings.TrimSpace(p.Name) == "" || len([]rune(p.Name)) > 200 || len([]rune(p.AddressText)) > 2000 || len([]rune(p.Note)) > 2000 || len(p.PaymentCycle) > 100 || p.MonthlyRent < 0 || p.MonthlyRent > 1e12 {
			return ledger.ErrInvalid
		}
		for _, v := range []string{p.MoveInDate, p.MoveOutDate, p.ContractEndDate} {
			if v != "" {
				if _, e := time.Parse(time.DateOnly, v); e != nil {
					return ledger.ErrInvalid
				}
			}
		}
		if p.MoveInDate == "" || (p.MoveOutDate != "" && p.MoveOutDate < p.MoveInDate) {
			return ledger.ErrInvalid
		}
		if p.Status != "ACTIVE" && p.Status != "PAST" && p.Status != "PLANNED" {
			return ledger.ErrInvalid
		}
		if id == "" {
			id = uuid.NewString()
		} else {
			if _, e := scanResidence(tx.QueryRowContext(ctx, `SELECT `+residenceColumns+` FROM residences WHERE id=?`, id)); e != nil {
				return e
			}
		}
		out = ledger.Residence{ResidenceInput: p, ID: id}
		_, e := tx.ExecContext(ctx, `INSERT INTO residences(`+residenceColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,address_text=excluded.address_text,move_in_date=excluded.move_in_date,move_out_date=excluded.move_out_date,monthly_rent_cents=excluded.monthly_rent_cents,payment_cycle=excluded.payment_cycle,contract_end_date=excluded.contract_end_date,network_included=excluded.network_included,management_fee_included=excluded.management_fee_included,status=excluded.status,note=excluded.note`, id, p.Name, p.AddressText, p.MoveInDate, p.MoveOutDate, p.MonthlyRent, p.PaymentCycle, p.ContractEndDate, p.NetworkIncluded, p.ManagementFeeIncluded, p.Status, p.Note)
		return e
	})
	return
}
func (s *Store) Residences(ctx context.Context) ([]ledger.Residence, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+residenceColumns+` FROM residences ORDER BY move_in_date DESC,id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.Residence{}
	for rows.Next() {
		p, e := scanResidence(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) LinkResidenceCost(ctx context.Context, key, id string, p ledger.ResidenceLink) (out ledger.ResidenceLink, replay bool, e error) {
	replay, e = s.ledgerTx(ctx, key, "residence_link:"+id, p, &out, func(tx *sql.Tx) error {
		if _, err := scanResidence(tx.QueryRowContext(ctx, `SELECT `+residenceColumns+` FROM residences WHERE id=?`, id)); err != nil {
			return err
		}
		switch p.Role {
		case "RENT", "AGENCY_FEE", "UTILITY", "MANAGEMENT", "NETWORK", "MOVING", "SETUP", "OTHER":
		default:
			return ledger.ErrInvalid
		}
		t, e := scanMoney(tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE id=?`, p.TransactionID))
		if e != nil {
			return e
		}
		if t.Type != "EXPENSE" || t.VoidedAt != "" {
			return ledger.ErrConflict
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO residence_cost_links VALUES(?,?,?) ON CONFLICT(transaction_id) DO UPDATE SET residence_id=excluded.residence_id,role=excluded.role`, p.TransactionID, id, p.Role)
		out = p
		return e
	})
	return
}
func (s *Store) ResidenceCost(ctx context.Context, id string) (out ledger.ResidenceCost, e error) {
	out.ResidenceID = id
	out.Groups = []ledger.MoneyGroup{}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if _, e = scanResidence(tx.QueryRowContext(ctx, `SELECT `+residenceColumns+` FROM residences WHERE id=?`, id)); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT l.role,COALESCE(NULLIF(original.expense_nature,''),'UNCLASSIFIED'),SUM(CASE m.type WHEN 'REFUND' THEN -m.amount_cents ELSE m.amount_cents END),COUNT(*) FROM money_transactions m JOIN money_transactions original ON original.id=CASE WHEN m.type='REFUND' THEN m.related_transaction_id ELSE m.id END JOIN residence_cost_links l ON l.transaction_id=original.id WHERE l.residence_id=? AND m.voided_at='' AND original.voided_at='' AND m.type IN ('EXPENSE','REFUND') GROUP BY l.role,original.expense_nature`, id)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var role, nature string
		var amount ledger.SignedMoney
		var count int
		if e = rows.Scan(&role, &nature, &amount, &count); e != nil {
			rows.Close()
			return out, e
		}
		out.Groups = append(out.Groups, ledger.MoneyGroup{Key: role + ":" + nature, Amount: amount, Count: count})
		if nature == "CAPITAL" {
			out.CapitalCost += amount
		} else {
			out.ConsumedCost += amount
		}
		if nature != "CAPITAL" && (nature == "SETUP" || role == "SETUP" || role == "MOVING" || role == "AGENCY_FEE") {
			out.SetupCost += amount
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	e = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_cents-returned_cents-forfeited_cents),0),COALESCE(SUM(forfeited_cents),0) FROM living_claims WHERE kind='DEPOSIT' AND residence_id=?`, id).Scan(&out.DepositLocked, &out.DepositLoss)
	out.NetCashOutflow = out.ConsumedCost + out.CapitalCost + ledger.SignedMoney(out.DepositLocked) + ledger.SignedMoney(out.DepositLoss)
	return
}

const claimColumns = `id,kind,amount_cents,account_id,COALESCE(residence_id,''),counterparty,reason,due_at,occurred_at,returned_cents,forfeited_cents,status,source_transaction_id,settlement_transaction_id,returned_at`

func scanClaim(r scanner) (p ledger.Claim, e error) {
	e = r.Scan(&p.ID, &p.Kind, &p.Amount, &p.AccountID, &p.ResidenceID, &p.Counterparty, &p.Reason, &p.DueAt, &p.OccurredAt, &p.ReturnedAmount, &p.ForfeitedAmount, &p.Status, &p.SourceTransactionID, &p.SettlementTransactionID, &p.ReturnedAt)
	if e == sql.ErrNoRows {
		e = ErrNotFound
	}
	return
}
func (s *Store) CreateClaim(ctx context.Context, key, kind string, p ledger.ClaimInput) (out ledger.Claim, replay bool, e error) {
	replay, e = s.ledgerTx(ctx, key, "claim:"+kind, p, &out, func(tx *sql.Tx) error {
		if (kind != "DEPOSIT" && kind != "RECEIVABLE") || p.Amount <= 0 || p.Amount > 1e12 || len([]rune(p.Counterparty)) > 200 || len([]rune(p.Reason)) > 2000 {
			return ledger.ErrInvalid
		}
		if kind == "DEPOSIT" && p.ResidenceID == "" {
			return ledger.ErrInvalid
		}
		if kind == "RECEIVABLE" && strings.TrimSpace(p.Counterparty) == "" {
			return ledger.ErrInvalid
		}
		if p.DueAt != "" {
			if _, e := time.Parse(time.RFC3339, p.DueAt); e != nil {
				return ledger.ErrInvalid
			}
		}
		var residence any
		if p.ResidenceID != "" {
			residence = p.ResidenceID
			if _, err := scanResidence(tx.QueryRowContext(ctx, `SELECT `+residenceColumns+` FROM residences WHERE id=?`, p.ResidenceID)); err != nil {
				return err
			}
		}
		t, e := postMoney(ctx, tx, "ADJUSTMENT", ledger.MoneyInput{AccountID: p.AccountID, Amount: p.Amount, OccurredAt: p.OccurredAt, Reason: kind + "_ADVANCE", Note: p.Reason}, "", -ledger.SignedMoney(p.Amount))
		if e != nil {
			return e
		}
		p.AccountID = t.AccountID
		p.OccurredAt = t.OccurredAt
		status := "OPEN"
		if kind == "DEPOSIT" {
			status = "HELD"
		}
		out = ledger.Claim{ClaimInput: p, ID: uuid.NewString(), Kind: kind, Status: status, SourceTransactionID: t.ID}
		_, e = tx.ExecContext(ctx, `INSERT INTO living_claims(id,kind,amount_cents,account_id,residence_id,counterparty,reason,due_at,occurred_at,status,source_transaction_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, out.ID, kind, p.Amount, p.AccountID, residence, p.Counterparty, p.Reason, p.DueAt, p.OccurredAt, status, t.ID)
		return e
	})
	return
}
func (s *Store) SettleClaim(ctx context.Context, key, id, kind, action string, p ledger.ClaimSettlement) (out ledger.Claim, replay bool, e error) {
	replay, e = s.ledgerTx(ctx, key, "claim:"+action+":"+id, p, &out, func(tx *sql.Tx) error {
		var e error
		out, e = scanClaim(tx.QueryRowContext(ctx, `SELECT `+claimColumns+` FROM living_claims WHERE id=? AND kind=?`, id, kind))
		if e != nil {
			return e
		}
		remaining := out.Amount - out.ReturnedAmount - out.ForfeitedAmount
		if p.Amount <= 0 || p.Amount > remaining {
			return ledger.ErrConflict
		}
		account := p.AccountID
		if account == "" {
			account = out.AccountID
		}
		moneyKind, delta, reason := "ADJUSTMENT", ledger.SignedMoney(p.Amount), kind+"_RETURN"
		if action == "forfeit" {
			// Reclassify previously locked cash: release and expense offset in one transaction.
			if _, e := postMoney(ctx, tx, "ADJUSTMENT", ledger.MoneyInput{AccountID: account, Amount: p.Amount, OccurredAt: p.OccurredAt, Reason: kind + "_RELEASE", RelatedTransactionID: out.SourceTransactionID}, "", ledger.SignedMoney(p.Amount)); e != nil {
				return e
			}
			moneyKind = "EXPENSE"
			delta = -ledger.SignedMoney(p.Amount)
			reason = kind + "_LOSS"
		} else if action != "return" {
			return ledger.ErrInvalid
		}
		t, e := postMoney(ctx, tx, moneyKind, ledger.MoneyInput{AccountID: account, Amount: p.Amount, ExpenseNature: "EXCEPTIONAL", OccurredAt: p.OccurredAt, Reason: reason, RelatedTransactionID: out.SourceTransactionID}, "", delta)
		if e != nil {
			return e
		}
		if action == "forfeit" {
			out.ForfeitedAmount += p.Amount
		} else {
			out.ReturnedAmount += p.Amount
			out.ReturnedAt = t.OccurredAt
		}
		remaining = out.Amount - out.ReturnedAmount - out.ForfeitedAmount
		if kind == "DEPOSIT" {
			out.Status = "PARTIALLY_RETURNED"
			if remaining == 0 {
				out.Status = "RETURNED"
				if out.ForfeitedAmount > 0 {
					out.Status = "FORFEITED"
				}
			}
		} else {
			out.Status = "PARTIAL"
			if remaining == 0 {
				out.Status = "SETTLED"
				if out.ForfeitedAmount > 0 {
					out.Status = "CANCELLED"
				}
			}
		}
		out.SettlementTransactionID = t.ID
		_, e = tx.ExecContext(ctx, `UPDATE living_claims SET returned_cents=?,forfeited_cents=?,status=?,settlement_transaction_id=?,returned_at=? WHERE id=?`, out.ReturnedAmount, out.ForfeitedAmount, out.Status, t.ID, out.ReturnedAt, id)
		return e
	})
	return
}
func (s *Store) Claims(ctx context.Context, kind string) ([]ledger.Claim, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+claimColumns+` FROM living_claims WHERE kind=? ORDER BY julianday(occurred_at) DESC,id`, kind)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.Claim{}
	for rows.Next() {
		p, e := scanClaim(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) ConsumptionForecast(ctx context.Context, id string, now time.Time) (out ledger.ConsumptionForecast, e error) {
	out.ItemID = id
	out.Status = "INSUFFICIENT_DATA"
	out.Confidence = "LOW"
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var created, kind string
	e = tx.QueryRowContext(ctx, `SELECT quantity,created_at,item_type FROM items WHERE id=?`, id).Scan(&out.CurrentQuantity, &created, &kind)
	if e == sql.ErrNoRows {
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if kind != "CONSUMABLE" {
		return out, ledger.ErrInvalid
	}
	start := now.AddDate(0, 0, -30)
	born, e := time.Parse(time.RFC3339Nano, created)
	if e != nil {
		return out, e
	}
	if born.After(start) {
		start = born
	}
	out.EffectiveDays = now.Sub(start).Hours() / 24
	var consumed float64
	e = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(-quantity_delta),0) FROM inventory_events WHERE item_id=? AND type IN ('CONSUME','FINISH') AND quantity_delta<0 AND julianday(occurred_at)>=julianday(?) AND julianday(occurred_at)<=julianday(?)`, id, start.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)).Scan(&out.EventCount, &consumed)
	if e != nil {
		return out, e
	}
	if out.EventCount < 3 || out.EffectiveDays < 7 || consumed <= 0 {
		return out, nil
	}
	rate := consumed / out.EffectiveDays
	days := out.CurrentQuantity / rate
	out.AverageDailyConsumption = &rate
	out.EstimatedDaysRemaining = &days
	out.Status = "OK"
	out.Confidence = "MEDIUM"
	if out.EventCount >= 10 && out.EffectiveDays >= 21 {
		out.Confidence = "HIGH"
	}
	return out, nil
}
func (s *Store) ItemsRunningLow(ctx context.Context, now time.Time) ([]ledger.ConsumptionForecast, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id FROM items WHERE item_type='CONSUMABLE' ORDER BY id`)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []ledger.ConsumptionForecast{}
	for _, id := range ids {
		f, e := s.ConsumptionForecast(ctx, id, now)
		if e != nil {
			return nil, e
		}
		item, e := s.Get(ctx, id)
		if e != nil {
			return nil, e
		}
		if (item.LowStockThreshold != nil && f.CurrentQuantity <= *item.LowStockThreshold) || f.CurrentQuantity == 0 || (f.EstimatedDaysRemaining != nil && *f.EstimatedDaysRemaining <= 7) {
			out = append(out, f)
		}
	}
	return out, nil
}

func (s *Store) ResidenceDurables(ctx context.Context, id string) ([]ledger.DurableDetails, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT item_id FROM durable_details WHERE residence_id=? ORDER BY item_id`, id)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var item string
		if e = rows.Scan(&item); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []ledger.DurableDetails{}
	for _, item := range ids {
		d, e := s.DurableDetails(ctx, item)
		if e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, nil
}
