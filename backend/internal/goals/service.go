package goals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"budgetflow/internal/budgetmath"
	"budgetflow/internal/db"
)

var (
	ErrNotFound             = errors.New("goal not found")
	ErrContributionNotFound = errors.New("contribution not found")
	ErrAccountNotFound      = errors.New("account not found")
	ErrCategoryNotFound     = errors.New("category not found")
	ErrTransactionNotFound  = errors.New("transaction not found")
)

// ValidationError marks user-input problems that map to HTTP 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

var validTypes = map[string]bool{"savings": true, "payoff": true, "purchase": true}

// Input carries the client-editable fields of a goal. TargetAmount is the
// positive target in minor units; TargetDate, CategoryID, and AccountID are
// optional links.
type Input struct {
	Name         string     `json:"name"`
	Type         string     `json:"type"`
	TargetAmount int64      `json:"targetAmount"`
	TargetDate   *string    `json:"targetDate"`
	CategoryID   *uuid.UUID `json:"categoryId"`
	AccountID    *uuid.UUID `json:"accountId"`

	targetDate *time.Time
}

func (in *Input) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return ValidationError("name is required")
	}
	if len(in.Name) > 200 {
		return ValidationError("name must be at most 200 characters")
	}
	if !validTypes[in.Type] {
		return ValidationError("type must be one of: savings, payoff, purchase")
	}
	if in.TargetAmount <= 0 {
		return ValidationError("targetAmount must be a positive number of minor units")
	}
	in.targetDate = nil
	if in.TargetDate != nil && *in.TargetDate != "" {
		d, err := time.Parse("2006-01-02", *in.TargetDate)
		if err != nil {
			return ValidationError("targetDate must be in YYYY-MM-DD format")
		}
		in.targetDate = &d
	}
	return nil
}

// ContributionInput carries one contribution. Amount may be negative to
// record a withdrawal; Date defaults to today; TransactionID optionally links
// the contribution to a ledger transaction.
type ContributionInput struct {
	Amount        int64      `json:"amount"`
	Date          string     `json:"date"`
	TransactionID *uuid.UUID `json:"transactionId"`
	Notes         string     `json:"notes"`

	date time.Time
}

func (in *ContributionInput) validate(today time.Time) error {
	if in.Amount == 0 {
		return ValidationError("amount must be a non-zero number of minor units")
	}
	if in.Date == "" {
		in.date = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
		return nil
	}
	d, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		return ValidationError("date must be in YYYY-MM-DD format")
	}
	in.date = d
	return nil
}

// Progress is the derived state of a goal: balance from contributions plus
// the budgetmath projection. MonthsRemaining and RequiredMonthly are nil for
// goals without a target date.
type Progress struct {
	CurrentBalance  int64
	AmountRemaining int64
	MonthsRemaining *int
	RequiredMonthly *int64
	BehindSchedule  bool
}

// Detail bundles a goal with its progress (and, for Get, contributions).
type Detail struct {
	Goal          db.Goal
	Progress      Progress
	Contributions []db.GoalContribution
}

// Service implements goal business logic over the generated queries.
type Service struct {
	q *db.Queries
}

func NewService(q *db.Queries) *Service {
	return &Service{q: q}
}

// progress derives the goal's math from its contribution balance as of now.
// The linear behind-schedule pace starts at the goal's creation time.
func progress(goal db.Goal, balance int64, now time.Time) Progress {
	g := budgetmath.Goal{Target: goal.TargetAmount, Current: balance}
	p := Progress{CurrentBalance: balance, AmountRemaining: g.AmountRemaining()}
	if goal.TargetDate == nil {
		return p
	}
	months := budgetmath.MonthsRemaining(now, *goal.TargetDate)
	required := g.RequiredMonthlyContribution(months)
	p.MonthsRemaining = &months
	p.RequiredMonthly = &required
	p.BehindSchedule = g.BehindSchedule(goal.CreatedAt, now, *goal.TargetDate)
	return p
}

// List returns the user's active goals with progress.
func (s *Service) List(ctx context.Context, userID uuid.UUID, now time.Time) ([]Detail, error) {
	goals, err := s.q.ListGoalsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	rows, err := s.q.ListGoalBalances(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list goal balances: %w", err)
	}
	balances := make(map[uuid.UUID]int64, len(rows))
	for _, r := range rows {
		balances[r.GoalID] = r.Balance
	}
	out := make([]Detail, 0, len(goals))
	for _, g := range goals {
		out = append(out, Detail{Goal: g, Progress: progress(g, balances[g.ID], now)})
	}
	return out, nil
}

// Create inserts a goal after validating input and linked references.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input, now time.Time) (Detail, error) {
	if err := in.validate(); err != nil {
		return Detail{}, err
	}
	if err := s.checkRefs(ctx, userID, in); err != nil {
		return Detail{}, err
	}
	goal, err := s.q.CreateGoal(ctx, db.CreateGoalParams{
		UserID: userID, Name: in.Name, Type: in.Type, TargetAmount: in.TargetAmount,
		TargetDate: in.targetDate, CategoryID: in.CategoryID, AccountID: in.AccountID,
	})
	if err != nil {
		return Detail{}, fmt.Errorf("create goal: %w", err)
	}
	return Detail{Goal: goal, Progress: progress(goal, 0, now)}, nil
}

// Get returns one goal (including archived) with progress and contributions.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID, now time.Time) (Detail, error) {
	goal, err := s.get(ctx, userID, id)
	if err != nil {
		return Detail{}, err
	}
	balance, err := s.q.GetGoalBalance(ctx, db.GetGoalBalanceParams{GoalID: id, UserID: userID})
	if err != nil {
		return Detail{}, fmt.Errorf("goal balance: %w", err)
	}
	contribs, err := s.q.ListContributionsByGoal(ctx, db.ListContributionsByGoalParams{GoalID: id, UserID: userID})
	if err != nil {
		return Detail{}, fmt.Errorf("list contributions: %w", err)
	}
	if contribs == nil {
		contribs = []db.GoalContribution{}
	}
	return Detail{Goal: goal, Progress: progress(goal, balance, now), Contributions: contribs}, nil
}

// Update replaces the goal's editable fields.
func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, in Input, now time.Time) (Detail, error) {
	if _, err := s.get(ctx, userID, id); err != nil {
		return Detail{}, err
	}
	if err := in.validate(); err != nil {
		return Detail{}, err
	}
	if err := s.checkRefs(ctx, userID, in); err != nil {
		return Detail{}, err
	}
	goal, err := s.q.UpdateGoal(ctx, db.UpdateGoalParams{
		ID: id, UserID: userID, Name: in.Name, Type: in.Type, TargetAmount: in.TargetAmount,
		TargetDate: in.targetDate, CategoryID: in.CategoryID, AccountID: in.AccountID,
	})
	if err != nil {
		return Detail{}, fmt.Errorf("update goal: %w", err)
	}
	balance, err := s.q.GetGoalBalance(ctx, db.GetGoalBalanceParams{GoalID: id, UserID: userID})
	if err != nil {
		return Detail{}, fmt.Errorf("goal balance: %w", err)
	}
	return Detail{Goal: goal, Progress: progress(goal, balance, now)}, nil
}

// SetArchived archives or unarchives a goal; contributions are kept.
func (s *Service) SetArchived(ctx context.Context, userID, id uuid.UUID, archived bool, now time.Time) (Detail, error) {
	if _, err := s.get(ctx, userID, id); err != nil {
		return Detail{}, err
	}
	var at *time.Time
	if archived {
		t := now.UTC()
		at = &t
	}
	if err := s.q.SetGoalArchived(ctx, db.SetGoalArchivedParams{ID: id, UserID: userID, ArchivedAt: at}); err != nil {
		return Detail{}, fmt.Errorf("set goal archived: %w", err)
	}
	return s.Get(ctx, userID, id, now)
}

// AddContribution records a contribution (or withdrawal, if negative) and
// returns the goal's refreshed detail.
func (s *Service) AddContribution(ctx context.Context, userID, goalID uuid.UUID, in ContributionInput, now time.Time) (db.GoalContribution, Detail, error) {
	if _, err := s.get(ctx, userID, goalID); err != nil {
		return db.GoalContribution{}, Detail{}, err
	}
	if err := in.validate(now); err != nil {
		return db.GoalContribution{}, Detail{}, err
	}
	if in.TransactionID != nil {
		txn, err := s.q.GetTransaction(ctx, db.GetTransactionParams{ID: *in.TransactionID, UserID: userID})
		if errors.Is(err, pgx.ErrNoRows) {
			return db.GoalContribution{}, Detail{}, ErrTransactionNotFound
		}
		if err != nil {
			return db.GoalContribution{}, Detail{}, fmt.Errorf("load linked transaction: %w", err)
		}
		if txn.DeletedAt != nil {
			return db.GoalContribution{}, Detail{}, ErrTransactionNotFound
		}
	}
	contrib, err := s.q.CreateGoalContribution(ctx, db.CreateGoalContributionParams{
		UserID: userID, GoalID: goalID, TransactionID: in.TransactionID,
		Amount: in.Amount, ContributedOn: in.date, Notes: strings.TrimSpace(in.Notes),
	})
	if err != nil {
		return db.GoalContribution{}, Detail{}, fmt.Errorf("create contribution: %w", err)
	}
	detail, err := s.Get(ctx, userID, goalID, now)
	if err != nil {
		return db.GoalContribution{}, Detail{}, err
	}
	return contrib, detail, nil
}

// DeleteContribution removes a contribution and returns the refreshed detail.
func (s *Service) DeleteContribution(ctx context.Context, userID, goalID, contributionID uuid.UUID, now time.Time) (Detail, error) {
	if _, err := s.get(ctx, userID, goalID); err != nil {
		return Detail{}, err
	}
	n, err := s.q.DeleteGoalContribution(ctx, db.DeleteGoalContributionParams{ID: contributionID, UserID: userID, GoalID: goalID})
	if err != nil {
		return Detail{}, fmt.Errorf("delete contribution: %w", err)
	}
	if n == 0 {
		return Detail{}, ErrContributionNotFound
	}
	return s.Get(ctx, userID, goalID, now)
}

func (s *Service) get(ctx context.Context, userID, id uuid.UUID) (db.Goal, error) {
	goal, err := s.q.GetGoal(ctx, db.GetGoalParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Goal{}, ErrNotFound
	}
	if err != nil {
		return db.Goal{}, fmt.Errorf("load goal: %w", err)
	}
	return goal, nil
}

// checkRefs verifies optional category and account links belong to the user.
func (s *Service) checkRefs(ctx context.Context, userID uuid.UUID, in Input) error {
	if in.CategoryID != nil {
		if _, err := s.q.GetCategory(ctx, db.GetCategoryParams{ID: *in.CategoryID, UserID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrCategoryNotFound
			}
			return fmt.Errorf("load category: %w", err)
		}
	}
	if in.AccountID != nil {
		if _, err := s.q.GetAccount(ctx, db.GetAccountParams{ID: *in.AccountID, UserID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrAccountNotFound
			}
			return fmt.Errorf("load account: %w", err)
		}
	}
	return nil
}
