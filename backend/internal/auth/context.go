package auth

import (
	"context"

	"budgetflow/internal/db"
)

type ctxKey int

const (
	userCtxKey ctxKey = iota
	sessionCtxKey
	tokenCtxKey
)

// WithIdentity attaches the authenticated user, their session row, and the
// raw session token (needed to derive the CSRF token) to the context.
func WithIdentity(ctx context.Context, user db.User, session db.Session, rawToken string) context.Context {
	ctx = context.WithValue(ctx, userCtxKey, user)
	ctx = context.WithValue(ctx, sessionCtxKey, session)
	return context.WithValue(ctx, tokenCtxKey, rawToken)
}

// UserFrom returns the authenticated user, if any.
func UserFrom(ctx context.Context) (db.User, bool) {
	u, ok := ctx.Value(userCtxKey).(db.User)
	return u, ok
}

// SessionFrom returns the authenticated session, if any.
func SessionFrom(ctx context.Context) (db.Session, bool) {
	s, ok := ctx.Value(sessionCtxKey).(db.Session)
	return s, ok
}

// TokenFrom returns the raw session token, if any.
func TokenFrom(ctx context.Context) (string, bool) {
	t, ok := ctx.Value(tokenCtxKey).(string)
	return t, ok
}
