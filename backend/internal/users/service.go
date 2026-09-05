// Package users implements account deletion: the user re-authenticates with
// their password, then every row they own is removed in one database
// transaction (via the users-row cascade) and a final audit event — whose
// user_id is nulled by the cascade but whose row survives — records that the
// deletion happened.
package users

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgetflow/internal/audit"
	"budgetflow/internal/auth"
	"budgetflow/internal/db"
)

// EventAccountDeleted is the final audit event written inside the deletion
// transaction.
const EventAccountDeleted = "account_deleted"

// ErrReauthFailed is returned when the confirmation password is wrong; the
// account is left untouched.
var ErrReauthFailed = errors.New("password confirmation failed")

// Service implements account deletion over the connection pool (the audit
// event and the cascade delete must commit atomically).
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: db.New(pool)}
}

// DeleteAccount verifies the password and then deletes the user row; every
// user-owned table cascades from it, so one DELETE removes all data
// atomically. The audit event is written first in the same transaction: on
// commit the cascade sets its user_id to NULL but the event itself survives
// as the record that the account existed and was deleted.
func (s *Service) DeleteAccount(ctx context.Context, user db.User, password string) error {
	ok, err := auth.VerifyPassword(user.PasswordHash, password)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return ErrReauthFailed
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin account deletion: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	if err := audit.Record(ctx, q, &user.ID, EventAccountDeleted, "user", []uuid.UUID{user.ID},
		map[string]string{"email": user.Email}); err != nil {
		return err
	}
	if err := q.DeleteUser(ctx, user.ID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit account deletion: %w", err)
	}
	return nil
}
