// Package accounts implements CRUD, archiving, balance computation, and
// reconciliation for manual accounts. Balances are never stored: the single
// source of truth is opening_balance plus the sum of non-deleted transactions.
package accounts

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"budgetflow/internal/audit"
	"budgetflow/internal/db"
)

// Audit event types recorded by this package.
const (
	EventAccountCreated    = "account_created"
	EventAccountUpdated    = "account_updated"
	EventAccountArchived   = "account_archived"
	EventAccountUnarchived = "account_unarchived"
	EventAccountReconciled = "account_reconciled"
)

var ErrNotFound = errors.New("account not found")

// ValidationError marks user-input problems that map to HTTP 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

var validTypes = map[string]bool{
	"cash": true, "checking": true, "savings": true,
	"credit_card": true, "ewallet": true, "custom": true,
}

// Input carries the client-editable account fields.
type Input struct {
	Name              string `json:"name"`
	Institution       string `json:"institution"`
	Type              string `json:"type"`
	Currency          string `json:"currency"`
	OpeningBalance    int64  `json:"openingBalance"`
	IncludeInNetWorth *bool  `json:"includeInNetWorth"`
}

func (in *Input) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return ValidationError("account name is required")
	}
	if !validTypes[in.Type] {
		return ValidationError("account type must be one of: cash, checking, savings, credit_card, ewallet, custom")
	}
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if !currencyRe.MatchString(in.Currency) {
		return ValidationError("currency must be a 3-letter ISO code")
	}
	return nil
}

// AccountWithBalance pairs an account row with its computed current balance.
type AccountWithBalance struct {
	db.Account
	Balance int64
}

// Service implements account business logic over the generated queries.
type Service struct {
	q *db.Queries
}

func NewService(q *db.Queries) *Service {
	return &Service{q: q}
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input) (AccountWithBalance, error) {
	if err := in.validate(); err != nil {
		return AccountWithBalance{}, err
	}
	include := true
	if in.IncludeInNetWorth != nil {
		include = *in.IncludeInNetWorth
	}
	acc, err := s.q.CreateAccount(ctx, db.CreateAccountParams{
		UserID:            userID,
		Name:              in.Name,
		Institution:       strings.TrimSpace(in.Institution),
		Type:              in.Type,
		Currency:          in.Currency,
		OpeningBalance:    in.OpeningBalance,
		IncludeInNetWorth: include,
	})
	if err != nil {
		return AccountWithBalance{}, fmt.Errorf("create account: %w", err)
	}
	if err := audit.Record(ctx, s.q, &userID, EventAccountCreated, "account", []uuid.UUID{acc.ID}, in); err != nil {
		return AccountWithBalance{}, err
	}
	return AccountWithBalance{Account: acc, Balance: acc.OpeningBalance}, nil
}

func (s *Service) Update(ctx context.Context, userID, accountID uuid.UUID, in Input) (AccountWithBalance, error) {
	if err := in.validate(); err != nil {
		return AccountWithBalance{}, err
	}
	current, err := s.q.GetAccount(ctx, db.GetAccountParams{ID: accountID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountWithBalance{}, ErrNotFound
	}
	if err != nil {
		return AccountWithBalance{}, fmt.Errorf("load account: %w", err)
	}
	include := current.IncludeInNetWorth
	if in.IncludeInNetWorth != nil {
		include = *in.IncludeInNetWorth
	}
	acc, err := s.q.UpdateAccount(ctx, db.UpdateAccountParams{
		ID:                accountID,
		UserID:            userID,
		Name:              in.Name,
		Institution:       strings.TrimSpace(in.Institution),
		Type:              in.Type,
		Currency:          in.Currency,
		OpeningBalance:    in.OpeningBalance,
		IncludeInNetWorth: include,
	})
	if err != nil {
		return AccountWithBalance{}, fmt.Errorf("update account: %w", err)
	}
	if err := audit.Record(ctx, s.q, &userID, EventAccountUpdated, "account", []uuid.UUID{acc.ID}, in); err != nil {
		return AccountWithBalance{}, err
	}
	return s.withBalance(ctx, acc)
}

func (s *Service) Get(ctx context.Context, userID, accountID uuid.UUID) (AccountWithBalance, error) {
	acc, err := s.q.GetAccount(ctx, db.GetAccountParams{ID: accountID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountWithBalance{}, ErrNotFound
	}
	if err != nil {
		return AccountWithBalance{}, fmt.Errorf("load account: %w", err)
	}
	return s.withBalance(ctx, acc)
}

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]AccountWithBalance, error) {
	rows, err := s.q.ListAccountBalancesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	out := make([]AccountWithBalance, 0, len(rows))
	for _, r := range rows {
		out = append(out, AccountWithBalance{Account: r.Account, Balance: r.Balance})
	}
	return out, nil
}

// SetArchived archives or unarchives an account. Historical transactions are
// untouched either way.
func (s *Service) SetArchived(ctx context.Context, userID, accountID uuid.UUID, archived bool) (AccountWithBalance, error) {
	var at *time.Time
	event := EventAccountUnarchived
	if archived {
		now := time.Now()
		at = &now
		event = EventAccountArchived
	}
	acc, err := s.q.SetAccountArchived(ctx, db.SetAccountArchivedParams{ID: accountID, UserID: userID, ArchivedAt: at})
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountWithBalance{}, ErrNotFound
	}
	if err != nil {
		return AccountWithBalance{}, fmt.Errorf("set account archived: %w", err)
	}
	if err := audit.Record(ctx, s.q, &userID, event, "account", []uuid.UUID{acc.ID}, nil); err != nil {
		return AccountWithBalance{}, err
	}
	return s.withBalance(ctx, acc)
}

// ReconcileResult reports the outcome of a reconciliation: the (possibly new)
// balance and the adjustment transaction, if one was needed.
type ReconcileResult struct {
	Account    AccountWithBalance
	Adjustment *db.Transaction
}

// Reconcile compares the account's computed balance against the statement
// balance the user entered and, if they differ, records an adjustment
// transaction for exactly the difference so the balances match afterwards.
func (s *Service) Reconcile(ctx context.Context, userID, accountID uuid.UUID, statementBalance int64) (ReconcileResult, error) {
	acc, err := s.q.GetAccount(ctx, db.GetAccountParams{ID: accountID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ReconcileResult{}, ErrNotFound
	}
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("load account: %w", err)
	}
	balance, err := s.q.GetAccountBalance(ctx, db.GetAccountBalanceParams{ID: accountID, UserID: userID})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("compute balance: %w", err)
	}

	diff := statementBalance - balance
	if diff == 0 {
		return ReconcileResult{Account: AccountWithBalance{Account: acc, Balance: balance}}, nil
	}

	adj, err := s.q.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:    userID,
		AccountID: accountID,
		Type:      "adjustment",
		Status:    "reconciled",
		Amount:    diff,
		Date:      time.Now().UTC().Truncate(24 * time.Hour),
		Payee:     "Reconciliation adjustment",
		Notes:     fmt.Sprintf("Adjusted balance from %d to %d to match statement", balance, statementBalance),
	})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("create adjustment transaction: %w", err)
	}
	if err := audit.Record(ctx, s.q, &userID, EventAccountReconciled, "account", []uuid.UUID{accountID},
		map[string]int64{"statement_balance": statementBalance, "previous_balance": balance, "adjustment": diff}); err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{
		Account:    AccountWithBalance{Account: acc, Balance: statementBalance},
		Adjustment: &adj,
	}, nil
}

func (s *Service) withBalance(ctx context.Context, acc db.Account) (AccountWithBalance, error) {
	balance, err := s.q.GetAccountBalance(ctx, db.GetAccountBalanceParams{ID: acc.ID, UserID: acc.UserID})
	if err != nil {
		return AccountWithBalance{}, fmt.Errorf("compute balance: %w", err)
	}
	return AccountWithBalance{Account: acc, Balance: balance}, nil
}
