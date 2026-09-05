package notifications

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"budgetflow/internal/auth"
	"budgetflow/internal/budgetmath"
	"budgetflow/internal/budgets"
	"budgetflow/internal/db"
	"budgetflow/internal/money"
)

// Engine evaluates notification conditions and records the results. Category
// triggers run after transaction/allocation writes (via the AfterWrite
// middleware); the remaining conditions run through RunScheduled, on request
// or from a ticker.
type Engine struct {
	q       *db.Queries
	budgets *budgets.Service
	log     *slog.Logger
}

func NewEngine(q *db.Queries, budgetsSvc *budgets.Service, log *slog.Logger) *Engine {
	return &Engine{q: q, budgets: budgetsSvc, log: log}
}

// AfterWrite is HTTP middleware for authenticated write routes that can change
// category spending or allocations. After a successful mutating request it
// re-evaluates the current month's category triggers; evaluation errors are
// logged, never surfaced to the client.
func (e *Engine) AfterWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if ww.Status() < 200 || ww.Status() >= 300 {
			return
		}
		user, ok := auth.UserFrom(r.Context())
		if !ok {
			return
		}
		if err := e.CheckBudget(r.Context(), user.ID, time.Now().UTC()); err != nil {
			e.log.Error("notification budget check failed", "error", err, "user_id", user.ID)
		}
	})
}

// CheckBudget evaluates the event-driven category triggers — approaching the
// warning threshold and over budget — against the current month's budget
// period. Without a current period there is nothing to evaluate.
func (e *Engine) CheckBudget(ctx context.Context, userID uuid.UUID, now time.Time) error {
	year, month := now.Year(), int(now.Month())
	detail, err := e.budgets.Get(ctx, userID, year, month)
	if errors.Is(err, budgets.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	prefs, err := loadPreferences(ctx, e.q, userID)
	if err != nil {
		return err
	}
	if !enabled(prefs, TypeCategoryThreshold) && !enabled(prefs, TypeCategoryOverBudget) {
		return nil
	}
	names, err := e.categoryNames(ctx, userID)
	if err != nil {
		return err
	}

	threshold := warningThreshold(prefs)
	currency := detail.Period.Currency
	for _, cd := range detail.Categories {
		cat := budgetmath.Category{Budgeted: cd.Amount, Rollover: cd.Rollover, Spending: cd.Spending, Reserved: cd.Reserved}
		name := names[cd.CategoryID]
		switch cat.Classify(threshold) {
		case budgetmath.StatusOverBudget, budgetmath.StatusUnfunded:
			if !enabled(prefs, TypeCategoryOverBudget) {
				continue
			}
			over := money.Money{Amount: -cat.Remaining(), Currency: currency}
			if err := e.notify(ctx, userID, TypeCategoryOverBudget,
				fmt.Sprintf("%s is over budget", name),
				fmt.Sprintf("Spending in %s exceeds its available funds by %s. Move money from another category to cover it.", name, over),
				"/budget",
				fmt.Sprintf("%s:%s:%d-%02d", TypeCategoryOverBudget, cd.CategoryID, year, month),
			); err != nil {
				return err
			}
		case budgetmath.StatusApproachingLimit:
			if !enabled(prefs, TypeCategoryThreshold) {
				continue
			}
			left := money.Money{Amount: cat.Remaining(), Currency: currency}
			if err := e.notify(ctx, userID, TypeCategoryThreshold,
				fmt.Sprintf("%s is approaching its limit", name),
				fmt.Sprintf("%s has %s left (within the %d%% warning threshold). Consider slowing down or moving money in.", name, left, threshold),
				"/budget",
				fmt.Sprintf("%s:%s:%d-%02d", TypeCategoryThreshold, cd.CategoryID, year, month),
			); err != nil {
				return err
			}
		}
	}
	return nil
}

// RunScheduled evaluates the time-based conditions: bills due within their
// reminder lead time, current budget month not created, goals behind
// schedule, and CSV imports waiting for review.
func (e *Engine) RunScheduled(ctx context.Context, userID uuid.UUID, now time.Time) error {
	prefs, err := loadPreferences(ctx, e.q, userID)
	if err != nil {
		return err
	}
	if enabled(prefs, TypeBillDue) {
		if err := e.checkBillsDue(ctx, userID, now); err != nil {
			return err
		}
	}
	if enabled(prefs, TypeBudgetMonthMissing) {
		if err := e.checkBudgetMonthMissing(ctx, userID, now); err != nil {
			return err
		}
	}
	if enabled(prefs, TypeGoalBehindSchedule) {
		if err := e.checkGoalsBehindSchedule(ctx, userID, now); err != nil {
			return err
		}
	}
	if enabled(prefs, TypeImportNeedsReview) {
		if err := e.checkImportsNeedReview(ctx, userID); err != nil {
			return err
		}
	}
	return nil
}

// checkBillsDue notifies for each active rule inside its reminder window,
// keyed by rule and due date so the next occurrence notifies afresh.
func (e *Engine) checkBillsDue(ctx context.Context, userID uuid.UUID, now time.Time) error {
	rules, err := e.q.ListRecurringRulesByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list recurring rules: %w", err)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	for _, r := range rules {
		days := int(r.NextDueDate.Sub(today).Hours() / 24)
		if days > int(r.ReminderLeadDays) {
			continue
		}
		var title string
		switch {
		case days < 0:
			title = fmt.Sprintf("%s was due %s", r.Name, r.NextDueDate.Format("Jan 2"))
		case days == 0:
			title = fmt.Sprintf("%s is due today", r.Name)
		case days == 1:
			title = fmt.Sprintf("%s is due tomorrow", r.Name)
		default:
			title = fmt.Sprintf("%s is due in %d days", r.Name, days)
		}
		body := "Mark it paid once the payment goes through, or match it to an existing transaction."
		if err := e.notify(ctx, userID, TypeBillDue, title, body, "/recurring",
			fmt.Sprintf("%s:%s:%s", TypeBillDue, r.ID, r.NextDueDate.Format("2006-01-02")),
		); err != nil {
			return err
		}
	}
	return nil
}

// checkBudgetMonthMissing nudges users who budget (have at least one period)
// but have not created the current month yet.
func (e *Engine) checkBudgetMonthMissing(ctx context.Context, userID uuid.UUID, now time.Time) error {
	periods, err := e.q.ListBudgetPeriodsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list budget periods: %w", err)
	}
	if len(periods) == 0 {
		return nil
	}
	year, month := int32(now.Year()), int32(now.Month())
	for _, p := range periods {
		if p.Year == year && p.Month == month {
			return nil
		}
	}
	return e.notify(ctx, userID, TypeBudgetMonthMissing,
		fmt.Sprintf("Your %s budget is not set up yet", now.Format("January")),
		"Create this month's budget to keep allocating your income — you can copy last month's structure to start quickly.",
		"/budget",
		fmt.Sprintf("%s:%d-%02d", TypeBudgetMonthMissing, year, month),
	)
}

// checkGoalsBehindSchedule notifies once per month for each active goal that
// is behind its linear pace.
func (e *Engine) checkGoalsBehindSchedule(ctx context.Context, userID uuid.UUID, now time.Time) error {
	goals, err := e.q.ListGoalsByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list goals: %w", err)
	}
	if len(goals) == 0 {
		return nil
	}
	rows, err := e.q.ListGoalBalances(ctx, userID)
	if err != nil {
		return fmt.Errorf("list goal balances: %w", err)
	}
	balances := make(map[uuid.UUID]int64, len(rows))
	for _, r := range rows {
		balances[r.GoalID] = r.Balance
	}
	user, err := e.q.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("load user: %w", err)
	}
	for _, g := range goals {
		if g.TargetDate == nil {
			continue
		}
		gm := budgetmath.Goal{Target: g.TargetAmount, Current: balances[g.ID]}
		if !gm.BehindSchedule(g.CreatedAt, now, *g.TargetDate) {
			continue
		}
		months := budgetmath.MonthsRemaining(now, *g.TargetDate)
		required := gm.RequiredMonthlyContribution(months)
		if err := e.notify(ctx, userID, TypeGoalBehindSchedule,
			fmt.Sprintf("%s is behind schedule", g.Name),
			fmt.Sprintf("Contribute %s per month to reach the target by %s.",
				money.Money{Amount: required, Currency: user.DefaultCurrency}, g.TargetDate.Format("Jan 2, 2006")),
			"/goals",
			fmt.Sprintf("%s:%s:%d-%02d", TypeGoalBehindSchedule, g.ID, now.Year(), now.Month()),
		); err != nil {
			return err
		}
	}
	return nil
}

// checkImportsNeedReview notifies once per pending (uploaded but uncommitted)
// import batch.
func (e *Engine) checkImportsNeedReview(ctx context.Context, userID uuid.UUID) error {
	batches, err := e.q.ListImportBatchesByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list import batches: %w", err)
	}
	for _, b := range batches {
		if b.Status != "pending" {
			continue
		}
		name := b.FileName
		if name == "" {
			name = "A CSV import"
		}
		if err := e.notify(ctx, userID, TypeImportNeedsReview,
			fmt.Sprintf("%s is waiting for review", name),
			"Finish the import by reviewing the mapped rows and committing or discarding the batch.",
			"/import",
			fmt.Sprintf("%s:%s", TypeImportNeedsReview, b.ID),
		); err != nil {
			return err
		}
	}
	return nil
}

// notify inserts a notification unless an unread one with the same dedupe key
// already exists (the partial unique index turns that into a no-row result).
func (e *Engine) notify(ctx context.Context, userID uuid.UUID, typ, title, body, actionURL, dedupeKey string) error {
	_, err := e.q.CreateNotification(ctx, db.CreateNotificationParams{
		UserID: userID, Type: typ, Title: title, Body: body,
		ActionUrl: actionURL, DedupeKey: dedupeKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create %s notification: %w", typ, err)
	}
	return nil
}

func (e *Engine) categoryNames(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]string, error) {
	cats, err := e.q.ListCategoriesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	names := make(map[uuid.UUID]string, len(cats))
	for _, c := range cats {
		names[c.ID] = c.Name
	}
	return names, nil
}
