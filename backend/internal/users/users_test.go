// Integration tests for account deletion: real HTTP router against a real
// PostgreSQL database, verifying re-authentication gating and that deletion
// removes every row the user owned.
package users_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgetflow/internal/auth"
	"budgetflow/internal/config"
	"budgetflow/internal/db"
	"budgetflow/internal/httpserver"
	"budgetflow/internal/testdb"
	"budgetflow/internal/users"
)

const password = "s3cret-password"

type testEnv struct {
	t      *testing.T
	router http.Handler
	pool   *pgxpool.Pool
	q      *db.Queries
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	pool := testdb.New(t)
	cfg := config.Config{Port: "0", Env: "test", SessionSecret: "test-secret", ShutdownTimeout: time.Second}
	srv := httpserver.New(cfg, slog.New(slog.DiscardHandler), pool)
	return &testEnv{t: t, router: srv.Router(), pool: pool, q: db.New(pool)}
}

type client struct {
	env     *testEnv
	cookies map[string]*http.Cookie
	csrf    string
	userID  uuid.UUID
}

func (e *testEnv) signUp(email string) *client {
	e.t.Helper()
	c := &client{env: e, cookies: map[string]*http.Cookie{}}
	rec := c.do(http.MethodPost, "/api/auth/sign-up", map[string]string{"email": email, "password": password})
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("sign-up status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		e.t.Fatalf("sign-up response not JSON: %v", err)
	}
	c.csrf, _ = resp["csrfToken"].(string)
	idStr, _ := resp["user"].(map[string]any)["id"].(string)
	id, err := uuid.Parse(idStr)
	if err != nil {
		e.t.Fatalf("sign-up returned bad user id %q: %v", idStr, err)
	}
	c.userID = id
	return c
}

func (c *client) do(method, path string, body any) *httptest.ResponseRecorder {
	c.env.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.env.t.Fatalf("marshal request body: %v", err)
		}
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	if c.csrf != "" {
		req.Header.Set(auth.CSRFHeader, c.csrf)
	}
	rec := httptest.NewRecorder()
	c.env.router.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.MaxAge < 0 {
			delete(c.cookies, ck.Name)
		} else {
			c.cookies[ck.Name] = ck
		}
	}
	return rec
}

// seed populates one row in every major user-owned table so the deletion test
// can verify the cascade reaches all of them.
func (e *testEnv) seed(c *client) {
	e.t.Helper()
	ctx := context.Background()

	acc, err := e.q.CreateAccount(ctx, db.CreateAccountParams{
		UserID: c.userID, Name: "Checking", Type: "checking", Currency: "USD", OpeningBalance: 5000, IncludeInNetWorth: true,
	})
	if err != nil {
		e.t.Fatalf("create account: %v", err)
	}
	grp, err := e.q.CreateCategoryGroup(ctx, db.CreateCategoryGroupParams{UserID: c.userID, Name: "Essentials"})
	if err != nil {
		e.t.Fatalf("create group: %v", err)
	}
	cat, err := e.q.CreateCategory(ctx, db.CreateCategoryParams{
		UserID: c.userID, GroupID: grp.ID, Name: "Groceries", BudgetType: "variable", RolloverRule: "none",
	})
	if err != nil {
		e.t.Fatalf("create category: %v", err)
	}
	period, err := e.q.CreateBudgetPeriod(ctx, db.CreateBudgetPeriodParams{
		UserID: c.userID, Year: 2026, Month: 9, Currency: "USD", PlannedIncome: 100000,
	})
	if err != nil {
		e.t.Fatalf("create period: %v", err)
	}
	if _, err := e.q.UpsertBudgetAllocation(ctx, db.UpsertBudgetAllocationParams{
		UserID: c.userID, PeriodID: period.ID, CategoryID: cat.ID, Amount: 40000,
	}); err != nil {
		e.t.Fatalf("create allocation: %v", err)
	}
	d, _ := time.Parse("2006-01-02", "2026-09-02")
	tx, err := e.q.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: c.userID, AccountID: acc.ID, CategoryID: &cat.ID,
		Type: "expense", Status: "cleared", Amount: -2500, Date: d, Payee: "Silpo",
	})
	if err != nil {
		e.t.Fatalf("create transaction: %v", err)
	}
	tag, err := e.q.CreateTag(ctx, db.CreateTagParams{UserID: c.userID, Name: "weekly"})
	if err != nil {
		e.t.Fatalf("create tag: %v", err)
	}
	if err := e.q.TagTransaction(ctx, db.TagTransactionParams{TransactionID: tx.ID, TagID: tag.ID, UserID: c.userID}); err != nil {
		e.t.Fatalf("tag transaction: %v", err)
	}
	goal, err := e.q.CreateGoal(ctx, db.CreateGoalParams{
		UserID: c.userID, Name: "Vacation", Type: "savings", TargetAmount: 120000,
	})
	if err != nil {
		e.t.Fatalf("create goal: %v", err)
	}
	if _, err := e.q.CreateGoalContribution(ctx, db.CreateGoalContributionParams{
		UserID: c.userID, GoalID: goal.ID, Amount: 1000, ContributedOn: d,
	}); err != nil {
		e.t.Fatalf("create contribution: %v", err)
	}
	if _, err := e.q.CreateNotification(ctx, db.CreateNotificationParams{
		UserID: c.userID, Type: "bill_due", Title: "Bill due", DedupeKey: "test",
	}); err != nil {
		e.t.Fatalf("create notification: %v", err)
	}
}

// userOwnedTables lists every table with a user_id column that the cascade
// must empty on account deletion.
var userOwnedTables = []string{
	"sessions", "password_reset_tokens", "accounts", "category_groups",
	"categories", "budget_periods", "budget_allocations", "allocation_history",
	"transactions", "transaction_splits", "tags", "transaction_tags",
	"recurring_rules", "goals", "goal_contributions", "import_batches",
	"notifications", "notification_preferences",
}

func (e *testEnv) countRows(table string, userID uuid.UUID) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM "+table+" WHERE user_id = $1", userID).Scan(&n); err != nil {
		e.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestDeleteAccountRequiresCorrectPassword(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("delete-wrong-pass@example.com")
	env.seed(c)

	rec := c.do(http.MethodPost, "/api/account/delete", map[string]string{"password": "not-the-password"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want 401 (body: %s)", rec.Code, rec.Body.String())
	}

	// Nothing was deleted and the session still works.
	if _, err := env.q.GetUserByID(context.Background(), c.userID); err != nil {
		t.Errorf("user should still exist after failed re-auth: %v", err)
	}
	if n := env.countRows("transactions", c.userID); n != 1 {
		t.Errorf("transactions rows = %d, want 1 after failed deletion", n)
	}
	if rec := c.do(http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusOK {
		t.Errorf("session invalidated by failed deletion attempt: /me status = %d", rec.Code)
	}
}

func TestDeleteAccountRemovesAllData(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("delete-me@example.com")
	env.seed(c)

	rec := c.do(http.MethodPost, "/api/account/delete", map[string]string{"password": password})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 (body: %s)", rec.Code, rec.Body.String())
	}

	ctx := context.Background()
	if _, err := env.q.GetUserByID(ctx, c.userID); err == nil {
		t.Error("user row still exists after deletion")
	}
	for _, table := range userOwnedTables {
		if n := env.countRows(table, c.userID); n != 0 {
			t.Errorf("%s still has %d rows for the deleted user", table, n)
		}
	}

	// The final audit event survives with its user reference nulled by the
	// cascade; the payload still identifies the account.
	var n int
	err := env.pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_events WHERE event_type = $1 AND user_id IS NULL AND payload->>'email' = $2`,
		users.EventAccountDeleted, "delete-me@example.com").Scan(&n)
	if err != nil {
		t.Fatalf("query audit events: %v", err)
	}
	if n != 1 {
		t.Errorf("found %d account_deleted audit events, want 1", n)
	}

	// The old session token is dead.
	if rec := c.do(http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("/me after deletion status = %d, want 401", rec.Code)
	}
}

func TestDeleteAccountRequiresAuth(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	anon := &client{env: env, cookies: map[string]*http.Cookie{}}
	rec := anon.do(http.MethodPost, "/api/account/delete", map[string]string{"password": password})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated delete status = %d, want 401", rec.Code)
	}
}
