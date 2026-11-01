package store

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/google/uuid"
	"stockroom/internal/ledger"
	"strings"
	"time"
)

func validNature(n string) bool {
	switch n {
	case "", "DAILY", "FIXED", "SETUP", "CAPITAL", "EXCEPTIONAL":
		return true
	}
	return false
}

const plannedColumns = `id,name,direction,amount_cents,due_at,account_id,category,expense_nature,recurrence,interval_days,mandatory,status,linked_transaction_id,note,created_at,updated_at`

func scanPlanned(r scanner) (p ledger.PlannedCashflow, e error) {
	e = r.Scan(&p.ID, &p.Name, &p.Direction, &p.Amount, &p.DueAt, &p.AccountID, &p.Category, &p.ExpenseNature, &p.Recurrence, &p.IntervalDays, &p.Mandatory, &p.Status, &p.LinkedTransactionID, &p.Note, &p.CreatedAt, &p.UpdatedAt)
	if e == sql.ErrNoRows {
		e = ErrNotFound
	}
	return
}
func validatePlanned(p *ledger.PlannedCashflowInput) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len([]rune(p.Name)) > 200 || len([]rune(p.Note)) > 2000 || len(p.Category) > 100 || p.Amount <= 0 || p.Amount > 1e12 || !validNature(p.ExpenseNature) {
		return ledger.ErrInvalid
	}
	if p.Direction != "OUTFLOW" && p.Direction != "INFLOW" {
		return ledger.ErrInvalid
	}
	at, e := time.Parse(time.RFC3339, p.DueAt)
	if e != nil {
		return fmt.Errorf("%w: due_at requires RFC3339", ledger.ErrInvalid)
	}
	p.DueAt = at.UTC().Format(time.RFC3339)
	if p.ExpenseNature == "" && p.Direction == "OUTFLOW" && p.Recurrence != "" {
		p.ExpenseNature = "FIXED"
	}
	switch p.Recurrence {
	case "", "WEEKLY", "MONTHLY", "YEARLY":
		if p.IntervalDays != 0 {
			return ledger.ErrInvalid
		}
	case "CUSTOM":
		if p.IntervalDays < 1 || p.IntervalDays > 3660 {
			return ledger.ErrInvalid
		}
	default:
		return ledger.ErrInvalid
	}
	return nil
}
func insertPlanned(ctx context.Context, tx *sql.Tx, p ledger.PlannedCashflow) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO planned_cashflows(`+plannedColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.ID, p.Name, p.Direction, p.Amount, p.DueAt, p.AccountID, p.Category, p.ExpenseNature, p.Recurrence, p.IntervalDays, p.Mandatory, p.Status, p.LinkedTransactionID, p.Note, p.CreatedAt, p.UpdatedAt)
	return e
}
func (s *Store) SavePlannedCashflow(ctx context.Context, key, id string, p ledger.PlannedCashflowInput) (out ledger.PlannedCashflow, replay bool, e error) {
	replay, e = s.ledgerTx(ctx, key, "save_planned:"+id, p, &out, func(tx *sql.Tx) error {
		if e := validatePlanned(&p); e != nil {
			return e
		}
		if p.AccountID != "" {
			a, e := accountTx(ctx, tx, p.AccountID)
			if e != nil {
				return e
			}
			p.AccountID = a.ID
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		out = ledger.PlannedCashflow{PlannedCashflowInput: p, ID: uuid.NewString(), Status: "PENDING", CreatedAt: now, UpdatedAt: now}
		if id != "" {
			old, e := scanPlanned(tx.QueryRowContext(ctx, `SELECT `+plannedColumns+` FROM planned_cashflows WHERE id=?`, id))
			if e != nil {
				return e
			}
			if old.Status != "PENDING" {
				return ledger.ErrConflict
			}
			out.ID = id
			out.CreatedAt = old.CreatedAt
			if _, e = tx.ExecContext(ctx, `DELETE FROM planned_cashflows WHERE id=?`, id); e != nil {
				return e
			}
		}
		return insertPlanned(ctx, tx, out)
	})
	return
}

// Completing a plan posts the real payment and advances recurrence in one event.
func (s *Store) ActPlannedCashflow(ctx context.Context, key, id, action string, p ledger.PlannedAction) (out ledger.PlannedCashflow, replay bool, e error) {
	replay, e = s.ledgerTx(ctx, key, "planned:"+action+":"+id, p, &out, func(tx *sql.Tx) error {
		var e error
		out, e = scanPlanned(tx.QueryRowContext(ctx, `SELECT `+plannedColumns+` FROM planned_cashflows WHERE id=?`, id))
		if e != nil {
			return e
		}
		if out.Status != "PENDING" {
			return ledger.ErrConflict
		}
		switch action {
		case "cancel":
			out.Status = "CANCELLED"
		case "skip":
			out.Status = "SKIPPED"
		case "complete":
			account := p.AccountID
			if account == "" {
				account = out.AccountID
			}
			kind, delta := "EXPENSE", -ledger.SignedMoney(out.Amount)
			if out.Direction == "INFLOW" {
				kind = "INCOME"
				delta = -delta
			}
			t, e := postMoney(ctx, tx, kind, ledger.MoneyInput{AccountID: account, Amount: out.Amount, Category: out.Category, ExpenseNature: out.ExpenseNature, OccurredAt: p.OccurredAt, Note: out.Note, Reason: "PLANNED:" + id}, "", delta)
			if e != nil {
				return e
			}
			out.Status = "COMPLETED"
			out.LinkedTransactionID = t.ID
		default:
			return ledger.ErrInvalid
		}
		out.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if _, e = tx.ExecContext(ctx, `UPDATE planned_cashflows SET status=?,linked_transaction_id=?,updated_at=? WHERE id=?`, out.Status, out.LinkedTransactionID, out.UpdatedAt, id); e != nil {
			return e
		}
		if out.Recurrence != "" && action != "cancel" {
			next := out
			next.ID = uuid.NewString()
			next.Status = "PENDING"
			next.LinkedTransactionID = ""
			next.CreatedAt = out.UpdatedAt
			at, _ := time.Parse(time.RFC3339, out.DueAt)
			next.DueAt = nextDue(at, out.Recurrence, out.IntervalDays).UTC().Format(time.RFC3339)
			return insertPlanned(ctx, tx, next)
		}
		return nil
	})
	return
}

// Clamp month ends instead of Go's date overflow (Jan 31 -> Feb 28).
func nextDue(at time.Time, recurrence string, days int) time.Time {
	at = at.In(time.FixedZone("Asia/Shanghai", 28800))
	switch recurrence {
	case "WEEKLY":
		return at.AddDate(0, 0, 7)
	case "CUSTOM":
		return at.AddDate(0, 0, days)
	}
	months := 1
	if recurrence == "YEARLY" {
		months = 12
	}
	first := time.Date(at.Year(), at.Month()+time.Month(months), 1, at.Hour(), at.Minute(), at.Second(), 0, at.Location())
	last := first.AddDate(0, 1, -1).Day()
	day := at.Day()
	if day > last {
		day = last
	}
	return first.AddDate(0, 0, day-1)
}
func (s *Store) UpcomingCashflows(ctx context.Context, from, to string) ([]ledger.PlannedCashflow, error) {
	if _, _, e := ledger.PeriodRange("", from, to, time.Now()); e != nil {
		return nil, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+plannedColumns+` FROM planned_cashflows WHERE status='PENDING' AND (?='' OR julianday(due_at)>=julianday(?)) AND (?='' OR julianday(due_at)<julianday(?)) ORDER BY julianday(due_at),id LIMIT 500`, from, from, to, to)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ledger.PlannedCashflow{}
	for rows.Next() {
		p, e := scanPlanned(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) RunwaySettings(ctx context.Context) (out ledger.RunwaySettings, e error) {
	e = s.db.QueryRowContext(ctx, `SELECT safety_buffer_cents,horizon_days FROM runway_settings WHERE id=1`).Scan(&out.SafetyBuffer, &out.HorizonDays)
	return
}
func (s *Store) SetRunwaySettings(ctx context.Context, key string, p ledger.RunwaySettings) (out ledger.RunwaySettings, replay bool, e error) {
	replay, e = s.ledgerTx(ctx, key, "runway_settings", p, &out, func(tx *sql.Tx) error {
		if p.SafetyBuffer < 0 || p.SafetyBuffer > 1e12 || p.HorizonDays < 1 || p.HorizonDays > 3660 {
			return ledger.ErrInvalid
		}
		out = p
		_, e := tx.ExecContext(ctx, `UPDATE runway_settings SET safety_buffer_cents=?,horizon_days=? WHERE id=1`, p.SafetyBuffer, p.HorizonDays)
		return e
	})
	return
}
func (s *Store) LivingRunway(ctx context.Context, now time.Time) (out ledger.LivingRunway, e error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = tx.QueryRowContext(ctx, `SELECT safety_buffer_cents,horizon_days FROM runway_settings WHERE id=1`).Scan(&out.SafetyBuffer, &out.HorizonDays); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(balance_cents),0) FROM accounts WHERE active=1 AND account_class='STORAGE' AND type<>'CREDIT_CARD' AND id<>'unknown'`).Scan(&out.LiquidBalance); e != nil {
		return out, e
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	local := now.In(zone)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	end := today.AddDate(0, 0, out.HorizonDays)
	out.Mode = "FIXED_HORIZON"
	incomeRows, e := tx.QueryContext(ctx, `SELECT `+plannedColumns+` FROM planned_cashflows WHERE status='PENDING' AND direction='INFLOW'`)
	if e != nil {
		return out, e
	}
	var next time.Time
	for incomeRows.Next() {
		p, err := scanPlanned(incomeRows)
		if err != nil {
			incomeRows.Close()
			return out, err
		}
		due, _ := time.Parse(time.RFC3339, p.DueAt)
		for due.Before(today) && p.Recurrence != "" {
			due = nextDue(due, p.Recurrence, p.IntervalDays)
		}
		if !due.Before(today) && (next.IsZero() || due.Before(next)) {
			next = due
		}
	}
	e = incomeRows.Err()
	incomeRows.Close()
	if e != nil {
		return out, e
	}
	days := out.HorizonDays
	if !next.IsZero() {
		at := next.In(zone)
		end = time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, zone)
		days = int(end.Sub(today).Hours() / 24)
		out.DaysToNextIncome = &days
		out.Mode = "NEXT_INCOME"
	}
	// Include overdue obligations. A same-day income uses one budget day.
	windowEnd := end
	if out.Mode == "NEXT_INCOME" {
		windowEnd = end.AddDate(0, 0, 1)
	}
	out.WindowEnd = windowEnd.Format(time.RFC3339)
	rows, e := tx.QueryContext(ctx, `SELECT `+plannedColumns+` FROM planned_cashflows WHERE status='PENDING' AND julianday(due_at)<julianday(?)`, out.WindowEnd)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPlanned(rows)
		if err != nil {
			return out, err
		}
		due, _ := time.Parse(time.RFC3339, p.DueAt)
		// Expand recurring reservations virtually; never create real transactions on reads.
		for due.Before(windowEnd) {
			if p.Direction == "OUTFLOW" && p.Mandatory {
				out.ReservedOutflow += p.Amount
			}
			if p.Direction == "INFLOW" && !due.Before(today) {
				out.ExpectedIncome += p.Amount
			}
			if p.Recurrence == "" {
				break
			}
			due = nextDue(due, p.Recurrence, p.IntervalDays)
		}
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	out.SafeToSpend = out.LiquidBalance - ledger.SignedMoney(out.ReservedOutflow) - ledger.SignedMoney(out.SafetyBuffer)
	out.CashflowWarning = out.SafeToSpend < 0
	if days < 1 {
		days = 1
	}
	if out.SafeToSpend > 0 {
		out.SafeDailySpend = ledger.Money(out.SafeToSpend / ledger.SignedMoney(days))
	}
	return out, nil
}
func (s *Store) SetExpenseNature(ctx context.Context, key, id string, p ledger.ExpenseNatureInput) (out ledger.MoneyTransaction, replay bool, e error) {
	replay, e = s.ledgerTx(ctx, key, "expense_nature:"+id, p, &out, func(tx *sql.Tx) error {
		if !validNature(p.ExpenseNature) {
			return ledger.ErrInvalid
		}
		t, e := scanMoney(tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM money_transactions WHERE id=?`, id))
		if e != nil {
			return e
		}
		if t.Type != "EXPENSE" || t.VoidedAt != "" {
			return ledger.ErrConflict
		}
		if _, e = tx.ExecContext(ctx, `UPDATE money_transactions SET expense_nature=? WHERE id=?`, p.ExpenseNature, id); e != nil {
			return e
		}
		t.ExpenseNature = p.ExpenseNature
		out = t
		return nil
	})
	return
}
func (s *Store) SpendingByNature(ctx context.Context, f ledger.MoneyFilter) (out ledger.SpendingByNature, e error) {
	where, args, e := moneyWhere(f, false)
	if e != nil {
		return out, e
	}
	out.Groups = []ledger.MoneyGroup{}
	rows, e := s.db.QueryContext(ctx, `SELECT COALESCE(NULLIF(CASE WHEN m.type='REFUND' THEN (SELECT expense_nature FROM money_transactions original WHERE original.id=m.related_transaction_id) ELSE m.expense_nature END,''),'UNCLASSIFIED'),SUM(CASE WHEN m.type='REFUND' THEN -m.amount_cents ELSE m.amount_cents END),COUNT(*) FROM money_transactions m WHERE m.type IN ('EXPENSE','REFUND') AND `+where+` GROUP BY 1 ORDER BY 1`, args...)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var g ledger.MoneyGroup
		if e = rows.Scan(&g.Key, &g.Amount, &g.Count); e != nil {
			return out, e
		}
		out.Groups = append(out.Groups, g)
		out.TotalSpending += g.Amount
		if g.Key == "DAILY" || g.Key == "FIXED" {
			out.NormalizedLivingCost += g.Amount
		}
		if g.Key == "UNCLASSIFIED" {
			out.Unclassified += g.Amount
		}
	}
	return out, rows.Err()
}
