package recurring

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgetflow/internal/db"
)

var (
	ErrNotFound            = errors.New("recurring rule not found")
	ErrAccountNotFound     = errors.New("account not found")
	ErrCategoryNotFound    = errors.New("category not found")
	ErrTransactionNotFound = errors.New("transaction not found")
)

// ValidationError marks user-input problems that map to HTTP 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

var validFrequencies = map[string]bool{"weekly": true, "monthly": true, "annual": true, "custom": true}

// Input carries the client-editable fields of a recurring rule. Amount is the
// positive expected bill amount in minor units.
type Input struct {
	Name               string    `json:"name"`
	AccountID          uuid.UUID `json:"accountId"`
	CategoryID         uuid.UUID `json:"categoryId"`
	Amount             int64     `json:"amount"`
	Frequency          string    `json:"frequency"`
	CustomIntervalDays *int32    `json:"customIntervalDays"`
	NextDueDate        string    `json:"nextDueDate"`
	ReminderLeadDays   *int32    `json:"reminderLeadDays"`

	dueDate time.Time
}

func (in *Input) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return ValidationError("name is required")
	}
	if len(in.Name) > 200 {
		return ValidationError("name must be at most 200 characters")
	}
	if in.Amount <= 0 {
		return ValidationError("amount must be a positive number of minor units")
	}
	if !validFrequencies[in.Frequency] {
		return ValidationError("frequency must be one of: weekly, monthly, annual, custom")
	}
	if in.Frequency == "custom" {
		if in.CustomIntervalDays == nil || *in.CustomIntervalDays <= 0 {
			return ValidationError("customIntervalDays must be a positive number of days for custom frequency")
		}
	} else {
		in.CustomIntervalDays = nil
	}
	d, err := time.Parse("2006-01-02", in.NextDueDate)
	if err != nil {
		return ValidationError("nextDueDate must be in YYYY-MM-DD format")
	}
	in.dueDate = d
	if in.ReminderLeadDays != nil && *in.ReminderLeadDays < 0 {
		return ValidationError("reminderLeadDays must not be negative")
	}
	return nil
}

func (in *Input) reminderLeadDays() int32 {
	if in.ReminderLeadDays == nil {
		return 3
	}
	return *in.ReminderLeadDays
}

// PayInput carries the optional overrides for marking a rule paid. Date
// defaults to the rule's due date; Amount defaults to the expected amount.
type PayInput struct {
	Date   string `json:"date"`
	Amount *int64 `json:"amount"`
	Status string `json:"status"`
}

// UpcomingBill is one rule due within the requested window.
type UpcomingBill struct {
	Rule         db.RecurringRule
	DaysUntilDue int
}

// PaidResult reports the outcome of mark-as-paid or match: the transaction
// now linked to the rule and the rule with its advanced due date.
type PaidResult struct {
	Rule        db.RecurringRule
	Transaction db.Transaction
}

// Service implements recurring-rule business logic. It holds the pool because
// mark-as-paid writes the transaction and the schedule advance atomically.
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: db.New(pool)}
}

// List returns the user's active rules ordered by next due date.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]db.RecurringRule, error) {
	rules, err := s.q.ListRecurringRulesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list recurring rules: %w", err)
	}
	if rules == nil {
		rules = []db.RecurringRule{}
	}
	return rules, nil
}

// Create inserts a rule; the chosen due date anchors the schedule.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input) (db.RecurringRule, error) {
	if err := in.validate(); err != nil {
		return db.RecurringRule{}, err
	}
	if err := s.checkRefs(ctx, userID, in); err != nil {
		return db.RecurringRule{}, err
	}
	rule, err := s.q.CreateRecurringRule(ctx, db.CreateRecurringRuleParams{
		UserID: userID, Name: in.Name, AccountID: in.AccountID, CategoryID: in.CategoryID,
		Amount: in.Amount, Frequency: in.Frequency, CustomIntervalDays: in.CustomIntervalDays,
		NextDueDate: in.dueDate, AnchorDate: in.dueDate, ReminderLeadDays: in.reminderLeadDays(),
	})
	if err != nil {
		return db.RecurringRule{}, fmt.Errorf("create recurring rule: %w", err)
	}
	return rule, nil
}

// Get returns one rule (including archived).
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (db.RecurringRule, error) {
	rule, err := s.q.GetRecurringRule(ctx, db.GetRecurringRuleParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.RecurringRule{}, ErrNotFound
	}
	if err != nil {
		return db.RecurringRule{}, fmt.Errorf("load recurring rule: %w", err)
	}
	return rule, nil
}

// Update replaces the rule's editable fields. Changing the due date re-anchors
// the schedule on the new date; otherwise the original anchor (and with it the
// intended day-of-month) is kept.
func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, in Input) (db.RecurringRule, error) {
	existing, err := s.Get(ctx, userID, id)
	if err != nil {
		return db.RecurringRule{}, err
	}
	if err := in.validate(); err != nil {
		return db.RecurringRule{}, err
	}
	if err := s.checkRefs(ctx, userID, in); err != nil {
		return db.RecurringRule{}, err
	}
	anchor := existing.AnchorDate
	if !in.dueDate.Equal(existing.NextDueDate) {
		anchor = in.dueDate
	}
	rule, err := s.q.UpdateRecurringRule(ctx, db.UpdateRecurringRuleParams{
		ID: id, UserID: userID, Name: in.Name, AccountID: in.AccountID, CategoryID: in.CategoryID,
		Amount: in.Amount, Frequency: in.Frequency, CustomIntervalDays: in.CustomIntervalDays,
		NextDueDate: in.dueDate, AnchorDate: anchor, ReminderLeadDays: in.reminderLeadDays(),
	})
	if err != nil {
		return db.RecurringRule{}, fmt.Errorf("update recurring rule: %w", err)
	}
	return rule, nil
}

// SetArchived archives or unarchives a rule; archived rules leave their
// transactions untouched and drop out of listings, upcoming bills, and
// category reservations.
func (s *Service) SetArchived(ctx context.Context, userID, id uuid.UUID, archived bool) (db.RecurringRule, error) {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return db.RecurringRule{}, err
	}
	var at *time.Time
	if archived {
		now := time.Now().UTC()
		at = &now
	}
	if err := s.q.SetRecurringRuleArchived(ctx, db.SetRecurringRuleArchivedParams{ID: id, UserID: userID, ArchivedAt: at}); err != nil {
		return db.RecurringRule{}, fmt.Errorf("set recurring rule archived: %w", err)
	}
	return s.Get(ctx, userID, id)
}

// MarkPaid creates the real expense transaction for the rule's current due
// date and advances the schedule, atomically.
func (s *Service) MarkPaid(ctx context.Context, userID, id uuid.UUID, in PayInput) (PaidResult, error) {
	rule, err := s.Get(ctx, userID, id)
	if err != nil {
		return PaidResult{}, err
	}
	date := rule.NextDueDate
	if in.Date != "" {
		if date, err = time.Parse("2006-01-02", in.Date); err != nil {
			return PaidResult{}, ValidationError("date must be in YYYY-MM-DD format")
		}
	}
	amount := rule.Amount
	if in.Amount != nil {
		amount = *in.Amount
	}
	if amount <= 0 {
		return PaidResult{}, ValidationError("amount must be a positive number of minor units")
	}
	status := in.Status
	if status == "" {
		status = "uncleared"
	}
	if status != "uncleared" && status != "cleared" && status != "reconciled" {
		return PaidResult{}, ValidationError("status must be one of: uncleared, cleared, reconciled")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaidResult{}, fmt.Errorf("begin mark paid: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	txn, err := q.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: userID, AccountID: rule.AccountID, CategoryID: &rule.CategoryID,
		Type: "expense", Status: status, Amount: -amount,
		Date: date, Payee: rule.Name, RecurringRuleID: &rule.ID,
	})
	if err != nil {
		return PaidResult{}, fmt.Errorf("create bill transaction: %w", err)
	}
	updated, err := s.advance(ctx, q, rule)
	if err != nil {
		return PaidResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PaidResult{}, fmt.Errorf("commit mark paid: %w", err)
	}
	return PaidResult{Rule: updated, Transaction: txn}, nil
}

// Match links an existing transaction to the rule as this occurrence's
// payment and advances the schedule, atomically.
func (s *Service) Match(ctx context.Context, userID, id, transactionID uuid.UUID) (PaidResult, error) {
	rule, err := s.Get(ctx, userID, id)
	if err != nil {
		return PaidResult{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaidResult{}, fmt.Errorf("begin match: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	txn, err := q.SetTransactionRecurringRule(ctx, db.SetTransactionRecurringRuleParams{
		ID: transactionID, UserID: userID, RecurringRuleID: &rule.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PaidResult{}, ErrTransactionNotFound
	}
	if err != nil {
		return PaidResult{}, fmt.Errorf("link transaction to rule: %w", err)
	}
	updated, err := s.advance(ctx, q, rule)
	if err != nil {
		return PaidResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PaidResult{}, fmt.Errorf("commit match: %w", err)
	}
	return PaidResult{Rule: updated, Transaction: txn}, nil
}

// Upcoming returns active rules due within the next `days` days (overdue
// rules included), with days-until-due relative to today.
func (s *Service) Upcoming(ctx context.Context, userID uuid.UUID, days int, today time.Time) ([]UpcomingBill, error) {
	if days <= 0 {
		days = 30
	}
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	rules, err := s.q.ListRulesDueBy(ctx, db.ListRulesDueByParams{
		UserID: userID, NextDueDate: today.AddDate(0, 0, days),
	})
	if err != nil {
		return nil, fmt.Errorf("list upcoming bills: %w", err)
	}
	out := make([]UpcomingBill, 0, len(rules))
	for _, r := range rules {
		out = append(out, UpcomingBill{
			Rule:         r,
			DaysUntilDue: int(r.NextDueDate.Sub(today).Hours() / 24),
		})
	}
	return out, nil
}

// ReservedByCategory sums active rules' expected amounts per category for
// rules due in the given (year, month). The budgets package uses this as the
// ReservedUpcomingPayments term of category remaining.
func ReservedByCategory(ctx context.Context, q *db.Queries, userID uuid.UUID, year, month int) (map[uuid.UUID]int64, error) {
	rules, err := q.ListRecurringRulesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list recurring rules: %w", err)
	}
	reserved := map[uuid.UUID]int64{}
	for _, r := range rules {
		if r.NextDueDate.Year() == year && int(r.NextDueDate.Month()) == month {
			reserved[r.CategoryID] += r.Amount
		}
	}
	return reserved, nil
}

// advance moves the rule's next due date one occurrence forward.
func (s *Service) advance(ctx context.Context, q *db.Queries, rule db.RecurringRule) (db.RecurringRule, error) {
	var interval int32
	if rule.CustomIntervalDays != nil {
		interval = *rule.CustomIntervalDays
	}
	next := NextDue(rule.AnchorDate, rule.NextDueDate, rule.Frequency, interval)
	updated, err := q.SetRecurringRuleNextDue(ctx, db.SetRecurringRuleNextDueParams{
		ID: rule.ID, UserID: rule.UserID, NextDueDate: next,
	})
	if err != nil {
		return db.RecurringRule{}, fmt.Errorf("advance recurring rule: %w", err)
	}
	return updated, nil
}

// checkRefs verifies the account and category belong to the user.
func (s *Service) checkRefs(ctx context.Context, userID uuid.UUID, in Input) error {
	if _, err := s.q.GetAccount(ctx, db.GetAccountParams{ID: in.AccountID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAccountNotFound
		}
		return fmt.Errorf("load account: %w", err)
	}
	if _, err := s.q.GetCategory(ctx, db.GetCategoryParams{ID: in.CategoryID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCategoryNotFound
		}
		return fmt.Errorf("load category: %w", err)
	}
	return nil
}
