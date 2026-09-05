// Package budgets implements monthly budget periods and per-category
// allocations: creating a period in the user's default currency (optionally
// copying the prior month), applying rollover rules, appending every income
// and allocation change to allocation_history, and computing unallocated funds
// and per-category remaining/status via the budgetmath package.
package budgets

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgetflow/internal/audit"
	"budgetflow/internal/budgetmath"
	"budgetflow/internal/db"
)

// EventPeriodCreated is the audit event recorded on period creation. Income
// and allocation edits are logged in allocation_history instead.
const EventPeriodCreated = "budget_period_created"

var (
	ErrNotFound         = errors.New("budget period not found")
	ErrExists           = errors.New("budget period already exists for that month")
	ErrCategoryNotFound = errors.New("category not found")
)

// ValidationError marks user-input problems that map to HTTP 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// CreateInput carries the fields for creating a monthly budget period.
type CreateInput struct {
	Year          int    `json:"year"`
	Month         int    `json:"month"`
	CopyPrior     bool   `json:"copyPrior"`
	PlannedIncome int64  `json:"plannedIncome"`
	Notes         string `json:"notes"`
}

func (in *CreateInput) validate() error {
	if in.Year < 2000 || in.Year > 2200 {
		return ValidationError("year must be between 2000 and 2200")
	}
	if in.Month < 1 || in.Month > 12 {
		return ValidationError("month must be between 1 and 12")
	}
	if in.PlannedIncome < 0 {
		return ValidationError("plannedIncome must not be negative")
	}
	if len(in.Notes) > 2000 {
		return ValidationError("notes must be at most 2000 characters")
	}
	return nil
}

// UpdateInput carries the editable period fields.
type UpdateInput struct {
	PlannedIncome int64  `json:"plannedIncome"`
	Notes         string `json:"notes"`
}

// CategoryDetail is one allocation with its computed math for the period.
type CategoryDetail struct {
	CategoryID uuid.UUID
	Amount     int64
	Rollover   int64
	Spending   int64
	Remaining  int64
	Status     budgetmath.Status
}

// PeriodDetail is a period with its computed unallocated funds and per-category
// remaining/status.
type PeriodDetail struct {
	Period      db.BudgetPeriod
	Unallocated int64
	Categories  []CategoryDetail
}

// Service implements budget-period business logic. It holds the pool because
// creation and allocation edits are multi-statement transactions.
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: db.New(pool)}
}

// List returns all of the user's budget periods in chronological order.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]db.BudgetPeriod, error) {
	periods, err := s.q.ListBudgetPeriodsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list budget periods: %w", err)
	}
	if periods == nil {
		periods = []db.BudgetPeriod{}
	}
	return periods, nil
}

// Create creates the (year, month) period in the user's default currency.
// If a prior period exists, each of its allocations carries rollover into the
// new period per the category's rollover rule; with CopyPrior the allocation
// amounts are copied as well, and each copied amount is appended to
// allocation_history.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in CreateInput) (PeriodDetail, error) {
	if err := in.validate(); err != nil {
		return PeriodDetail{}, err
	}
	user, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return PeriodDetail{}, fmt.Errorf("load user: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PeriodDetail{}, fmt.Errorf("begin create period: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	period, err := q.CreateBudgetPeriod(ctx, db.CreateBudgetPeriodParams{
		UserID: userID, Year: int32(in.Year), Month: int32(in.Month),
		Currency: user.DefaultCurrency, PlannedIncome: in.PlannedIncome, Notes: in.Notes,
	})
	if isUniqueViolation(err) {
		return PeriodDetail{}, ErrExists
	}
	if err != nil {
		return PeriodDetail{}, fmt.Errorf("create budget period: %w", err)
	}

	prior, err := q.LatestBudgetPeriodBefore(ctx, db.LatestBudgetPeriodBeforeParams{
		UserID: userID, Year: int32(in.Year), Month: int32(in.Month),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// first period: nothing to copy or roll over
	case err != nil:
		return PeriodDetail{}, fmt.Errorf("find prior period: %w", err)
	default:
		if err := s.seedFromPrior(ctx, q, userID, period, prior, in.CopyPrior); err != nil {
			return PeriodDetail{}, err
		}
	}

	if in.PlannedIncome != 0 {
		if err := recordHistory(ctx, q, userID, period.ID, nil, "planned_income", 0, in.PlannedIncome); err != nil {
			return PeriodDetail{}, err
		}
	}
	if err := audit.Record(ctx, q, &userID, EventPeriodCreated, "budget_period", []uuid.UUID{period.ID}, map[string]any{
		"year": in.Year, "month": in.Month, "copy_prior": in.CopyPrior,
	}); err != nil {
		return PeriodDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PeriodDetail{}, fmt.Errorf("commit create period: %w", err)
	}
	return s.detail(ctx, period)
}

// seedFromPrior creates the new period's allocation rows from the prior
// period: rollover per the category's rule, and the prior amounts when
// copyPrior is set. For reset_to_target the carried balance is capped at the
// prior period's budgeted amount (the schema has no separate target field).
func (s *Service) seedFromPrior(ctx context.Context, q *db.Queries, userID uuid.UUID,
	period, prior db.BudgetPeriod, copyPrior bool) error {
	priorAllocs, err := q.ListAllocationsByPeriod(ctx, db.ListAllocationsByPeriodParams{PeriodID: prior.ID, UserID: userID})
	if err != nil {
		return fmt.Errorf("list prior allocations: %w", err)
	}
	if len(priorAllocs) == 0 {
		return nil
	}
	rules, err := s.rolloverRules(ctx, q, userID)
	if err != nil {
		return err
	}
	spending, err := s.spendingByMonth(ctx, q, userID)
	if err != nil {
		return err
	}
	for _, pa := range priorAllocs {
		cat := budgetmath.Category{
			Budgeted: pa.Amount,
			Rollover: pa.Rollover,
			Spending: spending[monthKey{prior.Year, prior.Month, pa.CategoryID}],
		}
		rollover := budgetmath.NextRollover(rules[pa.CategoryID], cat.Remaining(), pa.Amount)
		amount := int64(0)
		if copyPrior {
			amount = pa.Amount
		}
		if amount == 0 && rollover == 0 && !copyPrior {
			continue
		}
		if _, err := q.UpsertBudgetAllocation(ctx, db.UpsertBudgetAllocationParams{
			UserID: userID, PeriodID: period.ID, CategoryID: pa.CategoryID, Amount: amount, Rollover: rollover,
		}); err != nil {
			return fmt.Errorf("seed allocation: %w", err)
		}
		if amount != 0 {
			if err := recordHistory(ctx, q, userID, period.ID, &pa.CategoryID, "allocation", 0, amount); err != nil {
				return err
			}
		}
	}
	return nil
}

// Get returns the (year, month) period with computed math.
func (s *Service) Get(ctx context.Context, userID uuid.UUID, year, month int) (PeriodDetail, error) {
	period, err := s.q.GetBudgetPeriod(ctx, db.GetBudgetPeriodParams{UserID: userID, Year: int32(year), Month: int32(month)})
	if errors.Is(err, pgx.ErrNoRows) {
		return PeriodDetail{}, ErrNotFound
	}
	if err != nil {
		return PeriodDetail{}, fmt.Errorf("load budget period: %w", err)
	}
	return s.detail(ctx, period)
}

// Update sets planned income and notes; an income change is appended to
// allocation_history.
func (s *Service) Update(ctx context.Context, userID, periodID uuid.UUID, in UpdateInput) (PeriodDetail, error) {
	if in.PlannedIncome < 0 {
		return PeriodDetail{}, ValidationError("plannedIncome must not be negative")
	}
	if len(in.Notes) > 2000 {
		return PeriodDetail{}, ValidationError("notes must be at most 2000 characters")
	}
	current, err := s.q.GetBudgetPeriodByID(ctx, db.GetBudgetPeriodByIDParams{ID: periodID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return PeriodDetail{}, ErrNotFound
	}
	if err != nil {
		return PeriodDetail{}, fmt.Errorf("load budget period: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PeriodDetail{}, fmt.Errorf("begin update period: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	period, err := q.UpdateBudgetPeriod(ctx, db.UpdateBudgetPeriodParams{
		ID: periodID, UserID: userID, PlannedIncome: in.PlannedIncome, Notes: in.Notes,
	})
	if err != nil {
		return PeriodDetail{}, fmt.Errorf("update budget period: %w", err)
	}
	if in.PlannedIncome != current.PlannedIncome {
		if err := recordHistory(ctx, q, userID, periodID, nil, "planned_income", current.PlannedIncome, in.PlannedIncome); err != nil {
			return PeriodDetail{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PeriodDetail{}, fmt.Errorf("commit update period: %w", err)
	}
	return s.detail(ctx, period)
}

// SetAllocation sets one category's allocated amount for the period, preserving
// its rollover, and appends the change to allocation_history.
func (s *Service) SetAllocation(ctx context.Context, userID, periodID, categoryID uuid.UUID, amount int64) (PeriodDetail, error) {
	if amount < 0 {
		return PeriodDetail{}, ValidationError("amount must not be negative")
	}
	period, err := s.q.GetBudgetPeriodByID(ctx, db.GetBudgetPeriodByIDParams{ID: periodID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return PeriodDetail{}, ErrNotFound
	}
	if err != nil {
		return PeriodDetail{}, fmt.Errorf("load budget period: %w", err)
	}
	if _, err := s.q.GetCategory(ctx, db.GetCategoryParams{ID: categoryID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PeriodDetail{}, ErrCategoryNotFound
		}
		return PeriodDetail{}, fmt.Errorf("load category: %w", err)
	}

	old, rollover := int64(0), int64(0)
	existing, err := s.q.GetBudgetAllocation(ctx, db.GetBudgetAllocationParams{PeriodID: periodID, CategoryID: categoryID, UserID: userID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return PeriodDetail{}, fmt.Errorf("load allocation: %w", err)
	default:
		old, rollover = existing.Amount, existing.Rollover
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PeriodDetail{}, fmt.Errorf("begin set allocation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	if _, err := q.UpsertBudgetAllocation(ctx, db.UpsertBudgetAllocationParams{
		UserID: userID, PeriodID: periodID, CategoryID: categoryID, Amount: amount, Rollover: rollover,
	}); err != nil {
		return PeriodDetail{}, fmt.Errorf("upsert allocation: %w", err)
	}
	if amount != old {
		if err := recordHistory(ctx, q, userID, periodID, &categoryID, "allocation", old, amount); err != nil {
			return PeriodDetail{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PeriodDetail{}, fmt.Errorf("commit set allocation: %w", err)
	}
	return s.detail(ctx, period)
}

// History returns the period's append-only allocation/income change log.
func (s *Service) History(ctx context.Context, userID, periodID uuid.UUID) ([]db.AllocationHistory, error) {
	if _, err := s.q.GetBudgetPeriodByID(ctx, db.GetBudgetPeriodByIDParams{ID: periodID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load budget period: %w", err)
	}
	rows, err := s.q.ListAllocationHistoryByPeriod(ctx, db.ListAllocationHistoryByPeriodParams{PeriodID: periodID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("list allocation history: %w", err)
	}
	if rows == nil {
		rows = []db.AllocationHistory{}
	}
	return rows, nil
}

type monthKey struct {
	year, month int32
	category    uuid.UUID
}

// detail computes the period's unallocated funds and per-category math.
//
// Unallocated follows the zero-based formula
// Unallocated = AvailableIncome + PriorRollover − Σ Allocations, where the
// prior rollover into each period is the previous period's unallocated plus
// funds released back to the pool by category rollover rules (a 'none'
// category returns its unused balance; 'reset_to_target' returns the excess
// over the cap; 'rollover' keeps everything with the category). It is
// recomputed from live data by walking the user's periods in order, so later
// edits to past months are always reflected.
func (s *Service) detail(ctx context.Context, target db.BudgetPeriod) (PeriodDetail, error) {
	userID := target.UserID
	periods, err := s.q.ListBudgetPeriodsByUser(ctx, userID)
	if err != nil {
		return PeriodDetail{}, fmt.Errorf("list budget periods: %w", err)
	}
	allocs, err := s.q.ListAllocationsWithRuleByUser(ctx, userID)
	if err != nil {
		return PeriodDetail{}, fmt.Errorf("list allocations: %w", err)
	}
	spending, err := s.spendingByMonth(ctx, s.q, userID)
	if err != nil {
		return PeriodDetail{}, err
	}

	byPeriod := make(map[uuid.UUID][]db.ListAllocationsWithRuleByUserRow)
	for _, a := range allocs {
		byPeriod[a.PeriodID] = append(byPeriod[a.PeriodID], a)
	}

	out := PeriodDetail{Period: target, Categories: []CategoryDetail{}}
	carried := int64(0)
	for _, p := range periods { // ordered by year, month
		if p.Year > target.Year || (p.Year == target.Year && p.Month > target.Month) {
			break
		}
		var sumAlloc, released int64
		for _, a := range byPeriod[p.ID] {
			sumAlloc += a.Amount
			cat := budgetmath.Category{
				Budgeted: a.Amount,
				Rollover: a.Rollover,
				Spending: spending[monthKey{p.Year, p.Month, a.CategoryID}],
			}
			remaining := cat.Remaining()
			released += max(remaining, 0) - budgetmath.NextRollover(budgetmath.RolloverRule(a.RolloverRule), remaining, a.Amount)
			if p.ID == target.ID {
				out.Categories = append(out.Categories, CategoryDetail{
					CategoryID: a.CategoryID,
					Amount:     a.Amount,
					Rollover:   a.Rollover,
					Spending:   cat.Spending,
					Remaining:  remaining,
					Status:     cat.Classify(budgetmath.DefaultWarningThresholdPct),
				})
			}
		}
		unallocated := budgetmath.Unallocated(p.PlannedIncome, carried, sumAlloc)
		if p.ID == target.ID {
			out.Unallocated = unallocated
			return out, nil
		}
		carried = unallocated + released
	}
	return PeriodDetail{}, ErrNotFound
}

// rolloverRules maps category id to its rollover rule.
func (s *Service) rolloverRules(ctx context.Context, q *db.Queries, userID uuid.UUID) (map[uuid.UUID]budgetmath.RolloverRule, error) {
	cats, err := q.ListCategoriesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	rules := make(map[uuid.UUID]budgetmath.RolloverRule, len(cats))
	for _, c := range cats {
		rules[c.ID] = budgetmath.RolloverRule(c.RolloverRule)
	}
	return rules, nil
}

// spendingByMonth maps (year, month, category) to net spending in minor units.
func (s *Service) spendingByMonth(ctx context.Context, q *db.Queries, userID uuid.UUID) (map[monthKey]int64, error) {
	rows, err := q.CategoryMonthlySpending(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("category monthly spending: %w", err)
	}
	m := make(map[monthKey]int64, len(rows))
	for _, r := range rows {
		m[monthKey{r.Year, r.Month, r.CategoryID}] = r.Spending
	}
	return m, nil
}

func recordHistory(ctx context.Context, q *db.Queries, userID, periodID uuid.UUID,
	categoryID *uuid.UUID, field string, oldAmount, newAmount int64) error {
	if _, err := q.CreateAllocationHistory(ctx, db.CreateAllocationHistoryParams{
		UserID: userID, PeriodID: periodID, CategoryID: categoryID,
		Field: field, OldAmount: oldAmount, NewAmount: newAmount,
	}); err != nil {
		return fmt.Errorf("record allocation history: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
