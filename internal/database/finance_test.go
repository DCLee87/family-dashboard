package database

import (
	"context"
	"testing"
	"time"
)

func TestFinanceSettingsTransactionsAndAccounts(t *testing.T) {
	database, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.db.Exec(`INSERT INTO devices(id,name,device_type,local_only) VALUES('parent','Parent','trusted_pc',0)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	settings, err := database.FinanceSettings(ctx)
	if err != nil || settings.BudgetStartDay != 21 {
		t.Fatalf("default settings: %+v, %v", settings, err)
	}
	settings, err = database.SaveFinanceSettings(ctx, 4_000_000, 21, settings.Version, now)
	if err != nil || settings.SalaryAmount != 4_000_000 || settings.Version != 2 {
		t.Fatalf("saved settings: %+v, %v", settings, err)
	}

	transaction, err := database.CreateFinanceTransaction(ctx, FinanceTransaction{ID: "expense", Amount: 32_000, Category: "food", Payer: "dad", OccurredOn: "2026-08-10", Memo: "장보기"}, "parent", now)
	if err != nil || transaction.Version != 1 {
		t.Fatalf("created transaction: %+v, %v", transaction, err)
	}
	items, err := database.FinanceTransactionsBetween(ctx, "2026-07-21", "2026-08-20")
	if err != nil || len(items) != 1 || items[0].Amount != 32_000 {
		t.Fatalf("listed transactions: %+v, %v", items, err)
	}
	transaction.Amount = 35_000
	transaction, err = database.UpdateFinanceTransaction(ctx, transaction, "parent", now.Add(time.Minute))
	if err != nil || transaction.Version != 2 || transaction.Amount != 35_000 {
		t.Fatalf("updated transaction: %+v, %v", transaction, err)
	}

	loan, err := database.CreateFinanceAccount(ctx, FinanceAccount{ID: "loan", Kind: "loan", Name: "주택 대출", BalanceAmount: 200_000_000, InterestBasisPoints: 425, MonthlyAmount: 1_200_000, PaymentDay: 25}, "parent", now)
	if err != nil || loan.InterestBasisPoints != 425 {
		t.Fatalf("created loan: %+v, %v", loan, err)
	}
	savings, err := database.CreateFinanceAccount(ctx, FinanceAccount{ID: "savings", Kind: "installment_savings", Name: "가족 적금", BalanceAmount: 3_000_000, InterestBasisPoints: 350, MonthlyAmount: 500_000, PaymentDay: 21, StartedOn: "2026-01-21", MaturityOn: "2027-01-21"}, "parent", now)
	if err != nil || savings.Kind != "installment_savings" {
		t.Fatalf("created savings: %+v, %v", savings, err)
	}
	accounts, err := database.ListFinanceAccounts(ctx)
	if err != nil || len(accounts) != 2 || accounts[0].Kind != "loan" {
		t.Fatalf("listed accounts: %+v, %v", accounts, err)
	}
	if err := database.DeleteFinanceTransaction(ctx, transaction.ID, transaction.Version); err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteFinanceAccount(ctx, loan.ID, loan.Version); err != nil {
		t.Fatal(err)
	}
}
