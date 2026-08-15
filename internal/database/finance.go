package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrFinanceNotFound = errors.New("finance record not found")
	ErrFinanceConflict = errors.New("finance record version conflict")
	ErrInvalidFinance  = errors.New("invalid finance record")
)

type FinanceSettings struct {
	SalaryAmount   int64  `json:"salaryAmount"`
	BudgetStartDay int    `json:"budgetStartDay"`
	Version        int64  `json:"version"`
	UpdatedAt      string `json:"updatedAt"`
}

type FinanceTransaction struct {
	ID         string `json:"id"`
	Amount     int64  `json:"amount"`
	Category   string `json:"category"`
	Payer      string `json:"payer"`
	OccurredOn string `json:"occurredOn"`
	Memo       string `json:"memo"`
	Version    int64  `json:"version"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type FinanceAccount struct {
	ID                  string `json:"id"`
	Kind                string `json:"kind"`
	Name                string `json:"name"`
	BalanceAmount       int64  `json:"balanceAmount"`
	InterestBasisPoints int    `json:"interestBasisPoints"`
	MonthlyAmount       int64  `json:"monthlyAmount"`
	PaymentDay          int    `json:"paymentDay"`
	StartedOn           string `json:"startedOn"`
	MaturityOn          string `json:"maturityOn"`
	Notes               string `json:"notes"`
	Version             int64  `json:"version"`
	CreatedAt           string `json:"createdAt"`
	UpdatedAt           string `json:"updatedAt"`
}

func (d *Database) FinanceSettings(ctx context.Context) (FinanceSettings, error) {
	var item FinanceSettings
	err := d.db.QueryRowContext(ctx, `SELECT salary_amount,budget_start_day,version,updated_at FROM finance_settings WHERE id=1`).Scan(
		&item.SalaryAmount, &item.BudgetStartDay, &item.Version, &item.UpdatedAt,
	)
	return item, err
}

func (d *Database) SaveFinanceSettings(ctx context.Context, salary int64, startDay int, version int64, now time.Time) (FinanceSettings, error) {
	if salary < 0 || startDay < 1 || startDay > 28 || version < 1 {
		return FinanceSettings{}, ErrInvalidFinance
	}
	result, err := d.db.ExecContext(ctx, `UPDATE finance_settings SET salary_amount=?,budget_start_day=?,version=version+1,updated_at=? WHERE id=1 AND version=?`, salary, startDay, databaseTime(now), version)
	if err != nil {
		return FinanceSettings{}, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return FinanceSettings{}, ErrFinanceConflict
	}
	return d.FinanceSettings(ctx)
}

func validFinanceTransaction(item FinanceTransaction) bool {
	if item.ID == "" || item.Amount <= 0 || len(strings.TrimSpace(item.Memo)) > 500 {
		return false
	}
	if _, err := time.Parse("2006-01-02", item.OccurredOn); err != nil {
		return false
	}
	switch item.Category {
	case "food", "living", "transport", "education", "medical", "leisure", "utilities", "loan_payment", "savings", "other":
	default:
		return false
	}
	return item.Payer == "dad" || item.Payer == "mom" || item.Payer == "family"
}

func (d *Database) CreateFinanceTransaction(ctx context.Context, item FinanceTransaction, deviceID string, now time.Time) (FinanceTransaction, error) {
	if !validFinanceTransaction(item) {
		return FinanceTransaction{}, ErrInvalidFinance
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO finance_transactions(id,amount,category,payer,occurred_on,memo,created_by_device_id,updated_by_device_id,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?, ?,1,?,?)`, item.ID, item.Amount, item.Category, item.Payer, item.OccurredOn, nullable(strings.TrimSpace(item.Memo)), deviceID, deviceID, databaseTime(now), databaseTime(now))
	if err != nil {
		return FinanceTransaction{}, err
	}
	return d.FinanceTransactionByID(ctx, item.ID)
}

func (d *Database) UpdateFinanceTransaction(ctx context.Context, item FinanceTransaction, deviceID string, now time.Time) (FinanceTransaction, error) {
	if !validFinanceTransaction(item) || item.Version < 1 {
		return FinanceTransaction{}, ErrInvalidFinance
	}
	result, err := d.db.ExecContext(ctx, `UPDATE finance_transactions SET amount=?,category=?,payer=?,occurred_on=?,memo=?,updated_by_device_id=?,updated_at=?,version=version+1 WHERE id=? AND version=?`, item.Amount, item.Category, item.Payer, item.OccurredOn, nullable(strings.TrimSpace(item.Memo)), deviceID, databaseTime(now), item.ID, item.Version)
	if err != nil {
		return FinanceTransaction{}, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return FinanceTransaction{}, d.financeWriteError(ctx, "finance_transactions", item.ID)
	}
	return d.FinanceTransactionByID(ctx, item.ID)
}

func (d *Database) FinanceTransactionByID(ctx context.Context, id string) (FinanceTransaction, error) {
	items, err := d.queryFinanceTransactions(ctx, `WHERE id=?`, id)
	if err != nil {
		return FinanceTransaction{}, err
	}
	if len(items) == 0 {
		return FinanceTransaction{}, ErrFinanceNotFound
	}
	return items[0], nil
}

func (d *Database) FinanceTransactionsBetween(ctx context.Context, from, to string) ([]FinanceTransaction, error) {
	if _, err := time.Parse("2006-01-02", from); err != nil {
		return nil, ErrInvalidFinance
	}
	if _, err := time.Parse("2006-01-02", to); err != nil || to < from {
		return nil, ErrInvalidFinance
	}
	return d.queryFinanceTransactions(ctx, `WHERE occurred_on BETWEEN ? AND ? ORDER BY occurred_on DESC,created_at DESC`, from, to)
}

func (d *Database) queryFinanceTransactions(ctx context.Context, clause string, args ...any) ([]FinanceTransaction, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id,amount,category,payer,occurred_on,memo,version,created_at,updated_at FROM finance_transactions `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []FinanceTransaction{}
	for rows.Next() {
		var item FinanceTransaction
		var memo sql.NullString
		if err := rows.Scan(&item.ID, &item.Amount, &item.Category, &item.Payer, &item.OccurredOn, &memo, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Memo = memo.String
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) DeleteFinanceTransaction(ctx context.Context, id string, version int64) error {
	result, err := d.db.ExecContext(ctx, `DELETE FROM finance_transactions WHERE id=? AND version=?`, id, version)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return d.financeWriteError(ctx, "finance_transactions", id)
	}
	return nil
}

func validFinanceAccount(item FinanceAccount) bool {
	name := strings.TrimSpace(item.Name)
	if item.ID == "" || len(name) < 1 || len(name) > 100 || item.BalanceAmount < 0 || item.MonthlyAmount < 0 || item.InterestBasisPoints < 0 || item.InterestBasisPoints > 100000 || item.PaymentDay < 1 || item.PaymentDay > 31 || len(strings.TrimSpace(item.Notes)) > 1000 {
		return false
	}
	if item.Kind != "loan" && item.Kind != "installment_savings" {
		return false
	}
	if item.StartedOn != "" {
		if _, err := time.Parse("2006-01-02", item.StartedOn); err != nil {
			return false
		}
	}
	if item.MaturityOn != "" {
		if _, err := time.Parse("2006-01-02", item.MaturityOn); err != nil || (item.StartedOn != "" && item.MaturityOn < item.StartedOn) {
			return false
		}
	}
	return true
}

func (d *Database) CreateFinanceAccount(ctx context.Context, item FinanceAccount, deviceID string, now time.Time) (FinanceAccount, error) {
	if !validFinanceAccount(item) {
		return FinanceAccount{}, ErrInvalidFinance
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO finance_accounts(id,kind,name,balance_amount,interest_basis_points,monthly_amount,payment_day,started_on,maturity_on,notes,created_by_device_id,updated_by_device_id,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?, ?,1,?,?)`, item.ID, item.Kind, strings.TrimSpace(item.Name), item.BalanceAmount, item.InterestBasisPoints, item.MonthlyAmount, item.PaymentDay, nullable(item.StartedOn), nullable(item.MaturityOn), nullable(strings.TrimSpace(item.Notes)), deviceID, deviceID, databaseTime(now), databaseTime(now))
	if err != nil {
		return FinanceAccount{}, err
	}
	return d.FinanceAccountByID(ctx, item.ID)
}

func (d *Database) UpdateFinanceAccount(ctx context.Context, item FinanceAccount, deviceID string, now time.Time) (FinanceAccount, error) {
	if !validFinanceAccount(item) || item.Version < 1 {
		return FinanceAccount{}, ErrInvalidFinance
	}
	result, err := d.db.ExecContext(ctx, `UPDATE finance_accounts SET kind=?,name=?,balance_amount=?,interest_basis_points=?,monthly_amount=?,payment_day=?,started_on=?,maturity_on=?,notes=?,updated_by_device_id=?,updated_at=?,version=version+1 WHERE id=? AND version=?`, item.Kind, strings.TrimSpace(item.Name), item.BalanceAmount, item.InterestBasisPoints, item.MonthlyAmount, item.PaymentDay, nullable(item.StartedOn), nullable(item.MaturityOn), nullable(strings.TrimSpace(item.Notes)), deviceID, databaseTime(now), item.ID, item.Version)
	if err != nil {
		return FinanceAccount{}, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return FinanceAccount{}, d.financeWriteError(ctx, "finance_accounts", item.ID)
	}
	return d.FinanceAccountByID(ctx, item.ID)
}

func (d *Database) FinanceAccountByID(ctx context.Context, id string) (FinanceAccount, error) {
	items, err := d.queryFinanceAccounts(ctx, `WHERE id=?`, id)
	if err != nil {
		return FinanceAccount{}, err
	}
	if len(items) == 0 {
		return FinanceAccount{}, ErrFinanceNotFound
	}
	return items[0], nil
}

func (d *Database) ListFinanceAccounts(ctx context.Context) ([]FinanceAccount, error) {
	return d.queryFinanceAccounts(ctx, `ORDER BY CASE kind WHEN 'loan' THEN 0 ELSE 1 END, created_at`)
}

func (d *Database) queryFinanceAccounts(ctx context.Context, clause string, args ...any) ([]FinanceAccount, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id,kind,name,balance_amount,interest_basis_points,monthly_amount,payment_day,started_on,maturity_on,notes,version,created_at,updated_at FROM finance_accounts `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []FinanceAccount{}
	for rows.Next() {
		var item FinanceAccount
		var started, maturity, notes sql.NullString
		if err := rows.Scan(&item.ID, &item.Kind, &item.Name, &item.BalanceAmount, &item.InterestBasisPoints, &item.MonthlyAmount, &item.PaymentDay, &started, &maturity, &notes, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.StartedOn, item.MaturityOn, item.Notes = started.String, maturity.String, notes.String
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) DeleteFinanceAccount(ctx context.Context, id string, version int64) error {
	result, err := d.db.ExecContext(ctx, `DELETE FROM finance_accounts WHERE id=? AND version=?`, id, version)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return d.financeWriteError(ctx, "finance_accounts", id)
	}
	return nil
}

func (d *Database) financeWriteError(ctx context.Context, table, id string) error {
	var count int
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id=?`, id).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrFinanceNotFound
	}
	return ErrFinanceConflict
}
