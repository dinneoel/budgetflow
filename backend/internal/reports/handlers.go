package reports

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"budgetflow/internal/auth"
	"budgetflow/internal/db"
	"budgetflow/internal/goals"
	"budgetflow/internal/recurring"
)

// Handler exposes the dashboard and report endpoints over HTTP.
type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Mount attaches the read-only dashboard and report routes to an
// authenticated router group.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/dashboard", h.Dashboard)
	r.Route("/reports", func(r chi.Router) {
		r.Get("/spending-by-category", h.SpendingByCategory)
		r.Get("/monthly-trend", h.MonthlyTrend)
		r.Get("/income-vs-expenses", h.IncomeVsExpenses)
		r.Get("/cash-flow", h.CashFlow)
		r.Get("/net-worth", h.NetWorth)
		r.Get("/top-payees", h.TopPayees)
	})
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	d, err := h.svc.Dashboard(r.Context(), user.ID, time.Now().UTC())
	if err != nil {
		h.writeError(w, err)
		return
	}

	atRisk := make([]map[string]any, 0, len(d.CategoriesAtRisk))
	for _, c := range d.CategoriesAtRisk {
		atRisk = append(atRisk, categoryStatusJSON(c))
	}
	bills := make([]map[string]any, 0, len(d.UpcomingBills))
	for _, b := range d.UpcomingBills {
		bills = append(bills, upcomingBillJSON(b))
	}
	recent := make([]map[string]any, 0, len(d.RecentTransactions))
	for _, t := range d.RecentTransactions {
		recent = append(recent, transactionJSON(t))
	}
	goalList := make([]map[string]any, 0, len(d.Goals))
	for _, g := range d.Goals {
		goalList = append(goalList, goalJSON(g))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"currency":         d.Currency,
		"availableBalance": d.AvailableBalance,
		"foreignBalances":  accountBalancesJSON(d.ForeignBalances),
		"mtdIncome":        d.MTDIncome,
		"mtdSpending":      d.MTDSpending,
		"budget": map[string]any{
			"exists":         d.Budget.Exists,
			"year":           d.Budget.Year,
			"month":          d.Budget.Month,
			"plannedIncome":  d.Budget.PlannedIncome,
			"unallocated":    d.Budget.Unallocated,
			"totalBudgeted":  d.Budget.TotalBudgeted,
			"totalSpending":  d.Budget.TotalSpending,
			"totalRemaining": d.Budget.TotalRemaining,
			"health":         d.Budget.Health,
		},
		"categoriesAtRisk":   atRisk,
		"upcomingBills":      bills,
		"recentTransactions": recent,
		"goals":              goalList,
	})
}

func (h *Handler) SpendingByCategory(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	from, to, err := dateRange(r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	rows, err := h.svc.SpendingByCategory(r.Context(), user.ID, from, to)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	var total int64
	for _, c := range rows {
		total += c.Spending
		out = append(out, map[string]any{
			"categoryId": c.CategoryID,
			"name":       c.Name,
			"group":      c.GroupName,
			"spending":   c.Spending,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"),
		"totalSpending": total, "categories": out,
	})
}

// MonthlyTrend returns the spending series of the income-vs-expenses data,
// as its own endpoint so chart clients need no reshaping.
func (h *Handler) MonthlyTrend(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	months, err := monthsParam(r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	rows, err := h.svc.IncomeVsExpenses(r.Context(), user.ID, months, time.Now().UTC())
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, map[string]any{"year": m.Year, "month": m.Month, "spending": m.Spending})
	}
	writeJSON(w, http.StatusOK, map[string]any{"months": out})
}

func (h *Handler) IncomeVsExpenses(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	months, err := monthsParam(r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	rows, err := h.svc.IncomeVsExpenses(r.Context(), user.ID, months, time.Now().UTC())
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, map[string]any{
			"year": m.Year, "month": m.Month,
			"income": m.Income, "expenses": m.Spending, "net": m.Net,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"months": out})
}

func (h *Handler) CashFlow(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	months, err := monthsParam(r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	rows, err := h.svc.CashFlow(r.Context(), user.ID, months, time.Now().UTC())
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, map[string]any{
			"year": m.Year, "month": m.Month,
			"inflow": m.Inflow, "outflow": m.Outflow, "net": m.Net,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"months": out})
}

func (h *Handler) NetWorth(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	nw, err := h.svc.NetWorth(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"currency":        nw.Currency,
		"total":           nw.Total,
		"accounts":        accountBalancesJSON(nw.Accounts),
		"foreignBalances": accountBalancesJSON(nw.ForeignBalances),
		"foreignTotals":   nw.ForeignTotals,
	})
}

func (h *Handler) TopPayees(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	from, to, err := dateRange(r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	limit := DefaultTopPayees
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > MaxTopPayees {
			h.writeError(w, ValidationError("limit must be a number between 1 and 100"))
			return
		}
		limit = n
	}
	rows, err := h.svc.TopPayees(r.Context(), user.ID, from, to, limit)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		out = append(out, map[string]any{
			"payee": p.Payee, "transactionCount": p.TransactionCount, "spending": p.Spending,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"), "payees": out,
	})
}

// dateRange parses optional from/to query params, defaulting to the calendar
// month containing today.
func dateRange(r *http.Request) (from, to time.Time, err error) {
	now := time.Now().UTC()
	from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to = from.AddDate(0, 1, -1)
	if raw := r.URL.Query().Get("from"); raw != "" {
		if from, err = time.Parse("2006-01-02", raw); err != nil {
			return from, to, ValidationError("from must be in YYYY-MM-DD format")
		}
	}
	if raw := r.URL.Query().Get("to"); raw != "" {
		if to, err = time.Parse("2006-01-02", raw); err != nil {
			return from, to, ValidationError("to must be in YYYY-MM-DD format")
		}
	}
	if to.Before(from) {
		return from, to, ValidationError("to must not be before from")
	}
	return from, to, nil
}

func monthsParam(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("months")
	if raw == "" {
		return DefaultTrendMonths, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > MaxTrendMonths {
		return 0, ValidationError("months must be a number between 1 and 60")
	}
	return n, nil
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	var ve ValidationError
	if errors.As(err, &ve) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ve.Error()})
		return
	}
	h.log.Error("reports handler error", "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
}

func accountBalancesJSON(list []AccountBalance) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, map[string]any{
			"accountId": a.AccountID,
			"name":      a.Name,
			"type":      a.Type,
			"currency":  a.Currency,
			"balance":   a.Balance,
		})
	}
	return out
}

func categoryStatusJSON(c CategoryStatus) map[string]any {
	return map[string]any{
		"categoryId": c.CategoryID,
		"name":       c.Name,
		"budgeted":   c.Budgeted,
		"rollover":   c.Rollover,
		"spending":   c.Spending,
		"reserved":   c.Reserved,
		"remaining":  c.Remaining,
		"status":     c.Status,
	}
}

func upcomingBillJSON(b recurring.UpcomingBill) map[string]any {
	return map[string]any{
		"ruleId":       b.Rule.ID,
		"name":         b.Rule.Name,
		"amount":       b.Rule.Amount,
		"accountId":    b.Rule.AccountID,
		"categoryId":   b.Rule.CategoryID,
		"dueDate":      b.Rule.NextDueDate.Format("2006-01-02"),
		"daysUntilDue": b.DaysUntilDue,
	}
}

func transactionJSON(t db.Transaction) map[string]any {
	return map[string]any{
		"id":         t.ID,
		"accountId":  t.AccountID,
		"categoryId": t.CategoryID,
		"type":       t.Type,
		"status":     t.Status,
		"amount":     t.Amount,
		"date":       t.Date.Format("2006-01-02"),
		"payee":      t.Payee,
		"notes":      t.Notes,
	}
}

func goalJSON(d goals.Detail) map[string]any {
	var requiredMonthly any
	if d.Progress.RequiredMonthly != nil {
		requiredMonthly = *d.Progress.RequiredMonthly
	}
	var targetDate any
	if d.Goal.TargetDate != nil {
		targetDate = d.Goal.TargetDate.Format("2006-01-02")
	}
	return map[string]any{
		"id":                          d.Goal.ID,
		"name":                        d.Goal.Name,
		"type":                        d.Goal.Type,
		"targetAmount":                d.Goal.TargetAmount,
		"targetDate":                  targetDate,
		"currentBalance":              d.Progress.CurrentBalance,
		"amountRemaining":             d.Progress.AmountRemaining,
		"requiredMonthlyContribution": requiredMonthly,
		"behindSchedule":              d.Progress.BehindSchedule,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
