// Package reports implements the read-side aggregation API: the dashboard
// (available balance, month-to-date income/spending, budget health, at-risk
// categories, upcoming bills, recent transactions, goal progress) and the
// report endpoints (spending by category, monthly spending trend, income vs
// expenses, cash-flow timeline, net worth, top payees).
//
// Every aggregate is scoped to the user's budget currency (their default
// currency): only transactions in accounts of that currency enter the math,
// and foreign-currency accounts appear as informational balances only — no
// silent conversion, ever.
package reports

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgetflow/internal/budgetmath"
	"budgetflow/internal/budgets"
	"budgetflow/internal/db"
	"budgetflow/internal/goals"
	"budgetflow/internal/recurring"
)

// ValidationError marks user-input problems that map to HTTP 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// Budget health values for the dashboard indicator.
const (
	HealthNoBudget         = "no_budget"
	HealthOnTrack          = "on_track"
	HealthApproachingLimit = "approaching_limit"
	HealthOverBudget       = "over_budget"
)

// Defaults for optional request parameters.
const (
	DefaultTrendMonths  = 6
	DefaultTopPayees    = 10
	DefaultUpcomingDays = 14
	DefaultRecentTxns   = 10
	MaxTrendMonths      = 60
	MaxTopPayees        = 100
)

// AccountBalance is one account's computed balance in its own currency.
type AccountBalance struct {
	AccountID uuid.UUID
	Name      string
	Type      string
	Currency  string
	Balance   int64
}

// CategoryStatus is one budget category's computed month state, with its name
// for direct display.
type CategoryStatus struct {
	CategoryID uuid.UUID
	Name       string
	Budgeted   int64
	Rollover   int64
	Spending   int64
	Reserved   int64
	Remaining  int64
	Status     budgetmath.Status
}

// BudgetSummary is the dashboard's view of the current month's budget.
type BudgetSummary struct {
	Exists         bool
	Year           int
	Month          int
	PlannedIncome  int64
	Unallocated    int64
	TotalBudgeted  int64
	TotalSpending  int64
	TotalRemaining int64
	Health         string
}

// Dashboard bundles everything the dashboard screen needs in one response.
type Dashboard struct {
	Currency           string
	AvailableBalance   int64
	ForeignBalances    []AccountBalance
	MTDIncome          int64
	MTDSpending        int64
	Budget             BudgetSummary
	CategoriesAtRisk   []CategoryStatus
	UpcomingBills      []recurring.UpcomingBill
	RecentTransactions []db.Transaction
	Goals              []goals.Detail
}

// CategorySpending is one category's net spending over the requested range.
type CategorySpending struct {
	CategoryID uuid.UUID
	Name       string
	GroupName  string
	Spending   int64
}

// MonthlyFlow is one month of income vs spending (type-based: income
// transactions vs expenses net of refunds; transfers and adjustments excluded).
type MonthlyFlow struct {
	Year     int
	Month    int
	Income   int64
	Spending int64
	Net      int64
}

// CashFlowMonth is one month of raw cash movement across all transaction
// types, transfers and adjustments included.
type CashFlowMonth struct {
	Year    int
	Month   int
	Inflow  int64
	Outflow int64
	Net     int64
}

// NetWorth is the current net-worth snapshot: included budget-currency
// accounts summed, foreign included accounts listed per currency unconverted.
type NetWorth struct {
	Currency        string
	Total           int64
	Accounts        []AccountBalance
	ForeignBalances []AccountBalance
	ForeignTotals   map[string]int64
}

// PayeeSpending is one payee's total over the requested range.
type PayeeSpending struct {
	Payee            string
	TransactionCount int64
	Spending         int64
}

// Service assembles dashboard and report aggregates over the generated
// queries and the feature services (so budget math, upcoming bills, and goal
// projections are computed exactly once, by their owning packages).
type Service struct {
	q         *db.Queries
	budgets   *budgets.Service
	recurring *recurring.Service
	goals     *goals.Service
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{
		q:         db.New(pool),
		budgets:   budgets.NewService(pool),
		recurring: recurring.NewService(pool),
		goals:     goals.NewService(db.New(pool)),
	}
}

// Dashboard assembles the dashboard for the month containing now.
func (s *Service) Dashboard(ctx context.Context, userID uuid.UUID, now time.Time) (Dashboard, error) {
	user, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return Dashboard{}, fmt.Errorf("load user: %w", err)
	}
	today := midnight(now)
	out := Dashboard{
		Currency:         user.DefaultCurrency,
		ForeignBalances:  []AccountBalance{},
		CategoriesAtRisk: []CategoryStatus{},
	}

	included, foreign, err := s.includedBalances(ctx, userID, user.DefaultCurrency)
	if err != nil {
		return Dashboard{}, err
	}
	for _, a := range included {
		out.AvailableBalance += a.Balance
	}
	out.ForeignBalances = foreign

	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	flows, err := s.q.MonthlyIncomeSpending(ctx, db.MonthlyIncomeSpendingParams{
		UserID: userID, Currency: user.DefaultCurrency, DateFrom: monthStart, DateTo: today,
	})
	if err != nil {
		return Dashboard{}, fmt.Errorf("month-to-date flows: %w", err)
	}
	for _, f := range flows {
		out.MTDIncome += f.Income
		out.MTDSpending += f.Spending
	}

	if err := s.fillBudget(ctx, &out, userID, today); err != nil {
		return Dashboard{}, err
	}

	if out.UpcomingBills, err = s.recurring.Upcoming(ctx, userID, DefaultUpcomingDays, now); err != nil {
		return Dashboard{}, err
	}
	if out.RecentTransactions, err = s.q.RecentTransactions(ctx, db.RecentTransactionsParams{
		UserID: userID, MaxRows: DefaultRecentTxns,
	}); err != nil {
		return Dashboard{}, fmt.Errorf("recent transactions: %w", err)
	}
	if out.RecentTransactions == nil {
		out.RecentTransactions = []db.Transaction{}
	}
	if out.Goals, err = s.goals.List(ctx, userID, now); err != nil {
		return Dashboard{}, err
	}
	return out, nil
}

// fillBudget loads the month's budget summary and at-risk categories into the
// dashboard; a missing period is not an error, just Exists=false.
func (s *Service) fillBudget(ctx context.Context, out *Dashboard, userID uuid.UUID, today time.Time) error {
	year, month := today.Year(), int(today.Month())
	out.Budget = BudgetSummary{Year: year, Month: month, Health: HealthNoBudget}

	detail, err := s.budgets.Get(ctx, userID, year, month)
	if errors.Is(err, budgets.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	catNames, err := s.categoryNames(ctx, userID)
	if err != nil {
		return err
	}

	out.Budget.Exists = true
	out.Budget.PlannedIncome = detail.Period.PlannedIncome
	out.Budget.Unallocated = detail.Unallocated
	out.Budget.Health = HealthOnTrack
	for _, c := range detail.Categories {
		out.Budget.TotalBudgeted += c.Amount
		out.Budget.TotalSpending += c.Spending
		out.Budget.TotalRemaining += c.Remaining
		switch c.Status {
		case budgetmath.StatusOverBudget, budgetmath.StatusUnfunded:
			out.Budget.Health = HealthOverBudget
		case budgetmath.StatusApproachingLimit:
			if out.Budget.Health != HealthOverBudget {
				out.Budget.Health = HealthApproachingLimit
			}
		}
		if c.Status != budgetmath.StatusOnTrack {
			out.CategoriesAtRisk = append(out.CategoriesAtRisk, CategoryStatus{
				CategoryID: c.CategoryID, Name: catNames[c.CategoryID],
				Budgeted: c.Amount, Rollover: c.Rollover, Spending: c.Spending,
				Reserved: c.Reserved, Remaining: c.Remaining, Status: c.Status,
			})
		}
	}
	return nil
}

// SpendingByCategory returns net spending per category over [from, to],
// most-spent first, budget-currency accounts only.
func (s *Service) SpendingByCategory(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]CategorySpending, error) {
	user, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	rows, err := s.q.SpendingByCategoryRange(ctx, db.SpendingByCategoryRangeParams{
		UserID: userID, Currency: user.DefaultCurrency, DateFrom: from, DateTo: to,
	})
	if err != nil {
		return nil, fmt.Errorf("spending by category: %w", err)
	}

	cats, err := s.q.ListCategoriesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	groups, err := s.q.ListCategoryGroupsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list category groups: %w", err)
	}
	groupNames := make(map[uuid.UUID]string, len(groups))
	for _, g := range groups {
		groupNames[g.ID] = g.Name
	}
	catByID := make(map[uuid.UUID]db.Category, len(cats))
	for _, c := range cats {
		catByID[c.ID] = c
	}

	out := make([]CategorySpending, 0, len(rows))
	for _, r := range rows {
		cat := catByID[r.CategoryID]
		out = append(out, CategorySpending{
			CategoryID: r.CategoryID,
			Name:       cat.Name,
			GroupName:  groupNames[cat.GroupID],
			Spending:   r.Spending,
		})
	}
	return out, nil
}

// IncomeVsExpenses returns one row per month for the last `months` months
// ending with the month containing now, zero-filled so charts get a
// continuous series. It doubles as the monthly spending trend.
func (s *Service) IncomeVsExpenses(ctx context.Context, userID uuid.UUID, months int, now time.Time) ([]MonthlyFlow, error) {
	user, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	from, to := monthRange(now, months)
	rows, err := s.q.MonthlyIncomeSpending(ctx, db.MonthlyIncomeSpendingParams{
		UserID: userID, Currency: user.DefaultCurrency, DateFrom: from, DateTo: to,
	})
	if err != nil {
		return nil, fmt.Errorf("monthly income/spending: %w", err)
	}

	byMonth := make(map[[2]int]MonthlyFlow, len(rows))
	for _, r := range rows {
		byMonth[[2]int{int(r.Year), int(r.Month)}] = MonthlyFlow{
			Year: int(r.Year), Month: int(r.Month),
			Income: r.Income, Spending: r.Spending, Net: r.Income - r.Spending,
		}
	}
	out := make([]MonthlyFlow, 0, months)
	for m := from; !m.After(to); m = m.AddDate(0, 1, 0) {
		key := [2]int{m.Year(), int(m.Month())}
		if f, ok := byMonth[key]; ok {
			out = append(out, f)
		} else {
			out = append(out, MonthlyFlow{Year: key[0], Month: key[1]})
		}
	}
	return out, nil
}

// CashFlow returns raw monthly cash movement (all transaction types) for the
// last `months` months ending with the month containing now, zero-filled.
func (s *Service) CashFlow(ctx context.Context, userID uuid.UUID, months int, now time.Time) ([]CashFlowMonth, error) {
	user, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	from, to := monthRange(now, months)
	rows, err := s.q.MonthlyCashFlow(ctx, db.MonthlyCashFlowParams{
		UserID: userID, Currency: user.DefaultCurrency, DateFrom: from, DateTo: to,
	})
	if err != nil {
		return nil, fmt.Errorf("monthly cash flow: %w", err)
	}

	byMonth := make(map[[2]int]CashFlowMonth, len(rows))
	for _, r := range rows {
		byMonth[[2]int{int(r.Year), int(r.Month)}] = CashFlowMonth{
			Year: int(r.Year), Month: int(r.Month),
			Inflow: r.Inflow, Outflow: r.Outflow, Net: r.Net,
		}
	}
	out := make([]CashFlowMonth, 0, months)
	for m := from; !m.After(to); m = m.AddDate(0, 1, 0) {
		key := [2]int{m.Year(), int(m.Month())}
		if f, ok := byMonth[key]; ok {
			out = append(out, f)
		} else {
			out = append(out, CashFlowMonth{Year: key[0], Month: key[1]})
		}
	}
	return out, nil
}

// NetWorth returns the current snapshot across included, non-archived
// accounts: budget-currency accounts summed into Total, foreign accounts
// listed and subtotaled per currency, never converted.
func (s *Service) NetWorth(ctx context.Context, userID uuid.UUID) (NetWorth, error) {
	user, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return NetWorth{}, fmt.Errorf("load user: %w", err)
	}
	included, foreign, err := s.includedBalances(ctx, userID, user.DefaultCurrency)
	if err != nil {
		return NetWorth{}, err
	}
	out := NetWorth{
		Currency:        user.DefaultCurrency,
		Accounts:        included,
		ForeignBalances: foreign,
		ForeignTotals:   map[string]int64{},
	}
	for _, a := range included {
		out.Total += a.Balance
	}
	for _, a := range foreign {
		out.ForeignTotals[a.Currency] += a.Balance
	}
	return out, nil
}

// TopPayees returns up to `limit` payees ranked by net spending over [from, to].
func (s *Service) TopPayees(ctx context.Context, userID uuid.UUID, from, to time.Time, limit int) ([]PayeeSpending, error) {
	user, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	rows, err := s.q.TopPayeesRange(ctx, db.TopPayeesRangeParams{
		UserID: userID, Currency: user.DefaultCurrency,
		DateFrom: from, DateTo: to, MaxPayees: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("top payees: %w", err)
	}
	out := make([]PayeeSpending, 0, len(rows))
	for _, r := range rows {
		out = append(out, PayeeSpending{Payee: r.Payee, TransactionCount: r.TransactionCount, Spending: r.Spending})
	}
	return out, nil
}

// includedBalances splits the user's non-archived, net-worth-included
// accounts into budget-currency and foreign lists, each with its computed
// balance in the account's own currency.
func (s *Service) includedBalances(ctx context.Context, userID uuid.UUID, currency string) (included, foreign []AccountBalance, err error) {
	rows, err := s.q.ListAccountBalancesByUser(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("list account balances: %w", err)
	}
	included, foreign = []AccountBalance{}, []AccountBalance{}
	for _, r := range rows {
		a := r.Account
		if a.ArchivedAt != nil || !a.IncludeInNetWorth {
			continue
		}
		b := AccountBalance{AccountID: a.ID, Name: a.Name, Type: a.Type, Currency: a.Currency, Balance: r.Balance}
		if a.Currency == currency {
			included = append(included, b)
		} else {
			foreign = append(foreign, b)
		}
	}
	return included, foreign, nil
}

func (s *Service) categoryNames(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]string, error) {
	cats, err := s.q.ListCategoriesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	names := make(map[uuid.UUID]string, len(cats))
	for _, c := range cats {
		names[c.ID] = c.Name
	}
	return names, nil
}

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// monthRange returns the first day of the month `months-1` months before now
// and the last day of the month containing now.
func monthRange(now time.Time, months int) (from, to time.Time) {
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return first.AddDate(0, -(months - 1), 0), first.AddDate(0, 1, -1)
}
