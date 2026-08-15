package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/DCLee87/family-dashboard/internal/database"
	"github.com/DCLee87/family-dashboard/internal/security"
)

type financeTransactionRequest struct {
	Amount     int64  `json:"amount"`
	Category   string `json:"category"`
	Payer      string `json:"payer"`
	OccurredOn string `json:"occurredOn"`
	Memo       string `json:"memo"`
	Version    int64  `json:"version"`
}

type financeAccountRequest struct {
	Kind          string  `json:"kind"`
	Name          string  `json:"name"`
	BalanceAmount int64   `json:"balanceAmount"`
	InterestRate  float64 `json:"interestRate"`
	MonthlyAmount int64   `json:"monthlyAmount"`
	PaymentDay    int     `json:"paymentDay"`
	StartedOn     string  `json:"startedOn"`
	MaturityOn    string  `json:"maturityOn"`
	Notes         string  `json:"notes"`
	Version       int64   `json:"version"`
}

type financeAccountView struct {
	database.FinanceAccount
	InterestRate float64 `json:"interestRate"`
}

func (a *App) financeOverview(w http.ResponseWriter, r *http.Request) {
	if !a.requireFinanceViewer(w, r) {
		return
	}
	settings, err := a.db.FinanceSettings(r.Context())
	if err != nil {
		writeAPIError(w, 500, "finance_load_failed", "가계부 설정을 불러오지 못했습니다.")
		return
	}
	location, _ := time.LoadLocation("Asia/Seoul")
	offset, err := financePeriodOffset(r.URL.Query().Get("offset"))
	if err != nil {
		writeAPIError(w, 400, "invalid_finance_period", "조회할 가계부 기간을 다시 확인해 주세요.")
		return
	}
	from, to := financePeriod(time.Now().In(location).AddDate(0, offset, 0), settings.BudgetStartDay)
	transactions, err := a.db.FinanceTransactionsBetween(r.Context(), from, to)
	if err != nil {
		writeAPIError(w, 500, "finance_load_failed", "가계부 내역을 불러오지 못했습니다.")
		return
	}
	accounts, err := a.db.ListFinanceAccounts(r.Context())
	if err != nil {
		writeAPIError(w, 500, "finance_load_failed", "대출·적금 내역을 불러오지 못했습니다.")
		return
	}
	views := make([]financeAccountView, 0, len(accounts))
	for _, account := range accounts {
		views = append(views, financeAccountView{FinanceAccount: account, InterestRate: float64(account.InterestBasisPoints) / 100})
	}
	writeJSON(w, 200, map[string]any{"settings": settings, "period": map[string]any{"from": from, "to": to, "offset": offset}, "transactions": transactions, "accounts": views})
}

func (a *App) saveFinanceSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdmin(w, r); !ok {
		return
	}
	var request struct {
		SalaryAmount   int64 `json:"salaryAmount"`
		BudgetStartDay int   `json:"budgetStartDay"`
		Version        int64 `json:"version"`
	}
	if !decodeFinanceRequest(w, r, &request) {
		return
	}
	item, err := a.db.SaveFinanceSettings(r.Context(), request.SalaryAmount, request.BudgetStartDay, request.Version, time.Now().UTC())
	if errors.Is(err, database.ErrFinanceConflict) {
		writeAPIError(w, 409, "finance_version_conflict", "다른 기기에서 예산이 변경되었습니다. 새로고침해 주세요.")
		return
	}
	if err != nil {
		writeAPIError(w, 400, "invalid_finance_settings", "월급 예산을 다시 확인해 주세요.")
		return
	}
	writeJSON(w, 200, item)
}

func (a *App) createFinanceTransaction(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	var request financeTransactionRequest
	if !decodeFinanceRequest(w, r, &request) {
		return
	}
	item := financeTransactionFromRequest(security.NewToken(), request)
	created, err := a.db.CreateFinanceTransaction(r.Context(), item, device.ID, time.Now().UTC())
	if err != nil {
		writeAPIError(w, 400, "invalid_finance_transaction", "지출 내역을 다시 확인해 주세요.")
		return
	}
	writeJSON(w, 201, created)
}

func (a *App) updateFinanceTransaction(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	var request financeTransactionRequest
	if !decodeFinanceRequest(w, r, &request) {
		return
	}
	updated, err := a.db.UpdateFinanceTransaction(r.Context(), financeTransactionFromRequest(r.PathValue("id"), request), device.ID, time.Now().UTC())
	if !financeWriteResult(w, err) {
		return
	}
	writeJSON(w, 200, updated)
}

func (a *App) deleteFinanceTransaction(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdmin(w, r); !ok {
		return
	}
	var request struct {
		Version int64 `json:"version"`
	}
	if !decodeFinanceRequest(w, r, &request) {
		return
	}
	if !financeWriteResult(w, a.db.DeleteFinanceTransaction(r.Context(), r.PathValue("id"), request.Version)) {
		return
	}
	w.WriteHeader(204)
}

func (a *App) createFinanceAccount(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	var request financeAccountRequest
	if !decodeFinanceRequest(w, r, &request) {
		return
	}
	created, err := a.db.CreateFinanceAccount(r.Context(), financeAccountFromRequest(security.NewToken(), request), device.ID, time.Now().UTC())
	if err != nil {
		writeAPIError(w, 400, "invalid_finance_account", "대출·적금 정보를 다시 확인해 주세요.")
		return
	}
	writeJSON(w, 201, financeAccountView{FinanceAccount: created, InterestRate: float64(created.InterestBasisPoints) / 100})
}

func (a *App) updateFinanceAccount(w http.ResponseWriter, r *http.Request) {
	device, ok := a.requireContentAdmin(w, r)
	if !ok {
		return
	}
	var request financeAccountRequest
	if !decodeFinanceRequest(w, r, &request) {
		return
	}
	updated, err := a.db.UpdateFinanceAccount(r.Context(), financeAccountFromRequest(r.PathValue("id"), request), device.ID, time.Now().UTC())
	if !financeWriteResult(w, err) {
		return
	}
	writeJSON(w, 200, financeAccountView{FinanceAccount: updated, InterestRate: float64(updated.InterestBasisPoints) / 100})
}

func (a *App) deleteFinanceAccount(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireContentAdmin(w, r); !ok {
		return
	}
	var request struct {
		Version int64 `json:"version"`
	}
	if !decodeFinanceRequest(w, r, &request) {
		return
	}
	if !financeWriteResult(w, a.db.DeleteFinanceAccount(r.Context(), r.PathValue("id"), request.Version)) {
		return
	}
	w.WriteHeader(204)
}

func (a *App) requireFinanceViewer(w http.ResponseWriter, r *http.Request) bool {
	device, ok := a.authenticatedDevice(w, r)
	if !ok {
		return false
	}
	if device.Type != "trusted_pc" && device.Type != "parent_mobile" {
		writeAPIError(w, 403, "finance_private", "가계부와 대출·적금 정보는 부모 기기에서만 볼 수 있습니다.")
		return false
	}
	return true
}

func decodeFinanceRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeAPIError(w, 400, "invalid_finance_request", "입력 내용을 다시 확인해 주세요.")
		return false
	}
	return true
}

func financeTransactionFromRequest(id string, request financeTransactionRequest) database.FinanceTransaction {
	return database.FinanceTransaction{ID: id, Amount: request.Amount, Category: request.Category, Payer: request.Payer, OccurredOn: request.OccurredOn, Memo: request.Memo, Version: request.Version}
}

func financeAccountFromRequest(id string, request financeAccountRequest) database.FinanceAccount {
	return database.FinanceAccount{ID: id, Kind: request.Kind, Name: request.Name, BalanceAmount: request.BalanceAmount, InterestBasisPoints: int(request.InterestRate*100 + .5), MonthlyAmount: request.MonthlyAmount, PaymentDay: request.PaymentDay, StartedOn: request.StartedOn, MaturityOn: request.MaturityOn, Notes: request.Notes, Version: request.Version}
}

func financeWriteResult(w http.ResponseWriter, err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, database.ErrFinanceConflict) {
		writeAPIError(w, 409, "finance_version_conflict", "다른 기기에서 내용이 변경되었습니다. 새로고침해 주세요.")
	} else if errors.Is(err, database.ErrFinanceNotFound) {
		writeAPIError(w, 404, "finance_not_found", "가계부 항목을 찾을 수 없습니다.")
	} else {
		writeAPIError(w, 400, "invalid_finance", "입력 내용을 다시 확인해 주세요.")
	}
	return false
}

func financePeriod(now time.Time, startDay int) (string, string) {
	start := time.Date(now.Year(), now.Month(), startDay, 0, 0, 0, 0, now.Location())
	if now.Day() < startDay {
		start = start.AddDate(0, -1, 0)
	}
	end := start.AddDate(0, 1, 0).AddDate(0, 0, -1)
	return start.Format("2006-01-02"), end.Format("2006-01-02")
}

func financePeriodOffset(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(value)
	if err != nil || offset < -120 || offset > 12 {
		return 0, errors.New("finance period offset out of range")
	}
	return offset, nil
}
