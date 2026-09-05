// Integration tests for the goals API: real HTTP router (handlers + auth/CSRF
// middleware) against a real PostgreSQL database per test.
package goals_test

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
)

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

// client carries cookies and the CSRF token across requests, like a browser.
type client struct {
	env     *testEnv
	cookies map[string]*http.Cookie
	csrf    string
	userID  uuid.UUID
}

func (e *testEnv) signUp(email string) *client {
	e.t.Helper()
	c := &client{env: e, cookies: map[string]*http.Cookie{}}
	rec := c.do(http.MethodPost, "/api/auth/sign-up", map[string]string{"email": email, "password": "s3cret-password"})
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("sign-up status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	resp := decode(e.t, rec)
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

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not valid JSON: %v (body: %s)", err, rec.Body.String())
	}
	return v
}

func (e *testEnv) addCategory(userID uuid.UUID, name string) uuid.UUID {
	e.t.Helper()
	ctx := context.Background()
	g, err := e.q.CreateCategoryGroup(ctx, db.CreateCategoryGroupParams{UserID: userID, Name: name + " Group"})
	if err != nil {
		e.t.Fatalf("create fixture group: %v", err)
	}
	cat, err := e.q.CreateCategory(ctx, db.CreateCategoryParams{
		UserID: userID, GroupID: g.ID, Name: name, BudgetType: "savings_goal", RolloverRule: "none",
	})
	if err != nil {
		e.t.Fatalf("create fixture category: %v", err)
	}
	return cat.ID
}

func (e *testEnv) addAccount(userID uuid.UUID) uuid.UUID {
	e.t.Helper()
	acc, err := e.q.CreateAccount(context.Background(), db.CreateAccountParams{
		UserID: userID, Name: "Fixture", Type: "savings", Currency: "USD", IncludeInNetWorth: true,
	})
	if err != nil {
		e.t.Fatalf("create fixture account: %v", err)
	}
	return acc.ID
}

func (c *client) createGoal(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	rec := c.do(http.MethodPost, "/api/goals", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create goal status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(t, rec)["goal"].(map[string]any)
}

func (c *client) getGoal(t *testing.T, id string) map[string]any {
	t.Helper()
	rec := c.do(http.MethodGet, "/api/goals/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get goal status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(t, rec)["goal"].(map[string]any)
}

func goalBody(overrides map[string]any) map[string]any {
	body := map[string]any{
		"name":         "Emergency Fund",
		"type":         "savings",
		"targetAmount": 100000,
	}
	for k, v := range overrides {
		body[k] = v
	}
	return body
}

func asInt(t *testing.T, v any) int64 {
	t.Helper()
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("value %v (%T) is not a number", v, v)
	}
	return int64(f)
}

func TestGoalsCRUDAndArchive(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("goals@example.com")
	categoryID := env.addCategory(c.userID, "Savings")
	accountID := env.addAccount(c.userID)

	goal := c.createGoal(t, goalBody(map[string]any{
		"targetDate": "2035-06-01", "categoryId": categoryID, "accountId": accountID,
	}))
	if goal["name"] != "Emergency Fund" || goal["type"] != "savings" ||
		asInt(t, goal["targetAmount"]) != 100000 || goal["targetDate"] != "2035-06-01" ||
		goal["categoryId"].(string) != categoryID.String() ||
		goal["accountId"].(string) != accountID.String() {
		t.Fatalf("created goal fields wrong: %v", goal)
	}
	if asInt(t, goal["currentBalance"]) != 0 || asInt(t, goal["amountRemaining"]) != 100000 {
		t.Fatalf("new goal balance fields wrong: %v", goal)
	}
	goalID := goal["id"].(string)

	rec := c.do(http.MethodGet, "/api/goals", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if n := len(decode(t, rec)["goals"].([]any)); n != 1 {
		t.Fatalf("list returned %d goals, want 1", n)
	}

	// update: retype as purchase, drop the links, raise the target
	rec = c.do(http.MethodPut, "/api/goals/"+goalID, goalBody(map[string]any{
		"name": "New Laptop", "type": "purchase", "targetAmount": 250000,
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	updated := decode(t, rec)["goal"].(map[string]any)
	if updated["name"] != "New Laptop" || updated["type"] != "purchase" ||
		asInt(t, updated["targetAmount"]) != 250000 || updated["targetDate"] != nil ||
		updated["categoryId"] != nil || updated["accountId"] != nil {
		t.Fatalf("updated goal fields wrong: %v", updated)
	}

	rec = c.do(http.MethodPost, "/api/goals/"+goalID+"/archive", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("archive status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	rec = c.do(http.MethodGet, "/api/goals", nil)
	if n := len(decode(t, rec)["goals"].([]any)); n != 0 {
		t.Fatalf("list after archive returned %d goals, want 0", n)
	}
	if got := c.getGoal(t, goalID); got["archived"] != true {
		t.Fatalf("archived goal still readable but archived = %v", got["archived"])
	}
	rec = c.do(http.MethodPost, "/api/goals/"+goalID+"/unarchive", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	rec = c.do(http.MethodGet, "/api/goals", nil)
	if n := len(decode(t, rec)["goals"].([]any)); n != 1 {
		t.Fatalf("list after unarchive returned %d goals, want 1", n)
	}
}

func TestGoalsValidationAndOwnership(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("owner@example.com")

	for name, overrides := range map[string]map[string]any{
		"empty name":      {"name": "  "},
		"bad type":        {"type": "retirement"},
		"zero target":     {"targetAmount": 0},
		"negative target": {"targetAmount": -5000},
		"bad target date": {"targetDate": "06/01/2035"},
	} {
		rec := c.do(http.MethodPost, "/api/goals", goalBody(overrides))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body: %s)", name, rec.Code, rec.Body.String())
		}
	}

	// linked references must belong to the caller
	other := env.signUp("intruder@example.com")
	rec := other.do(http.MethodPost, "/api/goals", goalBody(map[string]any{"categoryId": env.addCategory(c.userID, "Mine")}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign category: status = %d, want 404", rec.Code)
	}
	rec = other.do(http.MethodPost, "/api/goals", goalBody(map[string]any{"accountId": env.addAccount(c.userID)}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign account: status = %d, want 404", rec.Code)
	}

	goal := c.createGoal(t, goalBody(nil))
	goalID := goal["id"].(string)
	if rec = other.do(http.MethodGet, "/api/goals/"+goalID, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign goal get: status = %d, want 404", rec.Code)
	}
	if rec = other.do(http.MethodPut, "/api/goals/"+goalID, goalBody(nil)); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign goal update: status = %d, want 404", rec.Code)
	}
	rec = other.do(http.MethodPost, "/api/goals/"+goalID+"/contributions", map[string]any{"amount": 100})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign goal contribution: status = %d, want 404", rec.Code)
	}
}

// TestContributionAccounting checks that the goal balance is derived purely
// from its contributions: manual entries, transaction-linked entries, and
// negative withdrawals all sum; deleting a contribution reverses it.
func TestContributionAccounting(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("saver@example.com")
	accountID := env.addAccount(c.userID)
	goalID := c.createGoal(t, goalBody(nil))["id"].(string)

	rec := c.do(http.MethodPost, "/api/goals/"+goalID+"/contributions", map[string]any{
		"amount": 30000, "date": "2030-01-15", "notes": "January deposit",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("contribution status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	resp := decode(t, rec)
	contrib := resp["contribution"].(map[string]any)
	if asInt(t, contrib["amount"]) != 30000 || contrib["contributedOn"] != "2030-01-15" || contrib["notes"] != "January deposit" {
		t.Fatalf("contribution fields wrong: %v", contrib)
	}
	if got := asInt(t, resp["goal"].(map[string]any)["currentBalance"]); got != 30000 {
		t.Fatalf("balance after first contribution = %d, want 30000", got)
	}

	// a transaction-linked contribution
	txn, err := env.q.CreateTransaction(context.Background(), db.CreateTransactionParams{
		UserID: c.userID, AccountID: accountID, CategoryID: nil, Type: "income",
		Status: "cleared", Amount: 20000, Date: time.Date(2030, 2, 1, 0, 0, 0, 0, time.UTC), Payee: "Bonus",
	})
	if err != nil {
		t.Fatalf("create fixture transaction: %v", err)
	}
	rec = c.do(http.MethodPost, "/api/goals/"+goalID+"/contributions", map[string]any{
		"amount": 20000, "date": "2030-02-01", "transactionId": txn.ID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("linked contribution status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	resp = decode(t, rec)
	if got := resp["contribution"].(map[string]any)["transactionId"]; got != txn.ID.String() {
		t.Fatalf("linked contribution transactionId = %v, want %s", got, txn.ID)
	}

	// a withdrawal
	rec = c.do(http.MethodPost, "/api/goals/"+goalID+"/contributions", map[string]any{
		"amount": -5000, "date": "2030-02-10",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("withdrawal status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	withdrawalID := decode(t, rec)["contribution"].(map[string]any)["id"].(string)

	goal := c.getGoal(t, goalID)
	if got := asInt(t, goal["currentBalance"]); got != 45000 {
		t.Fatalf("balance = %d, want 30000 + 20000 − 5000 = 45000", got)
	}
	if got := asInt(t, goal["amountRemaining"]); got != 55000 {
		t.Fatalf("amountRemaining = %d, want 100000 − 45000 = 55000", got)
	}
	if n := len(goal["contributions"].([]any)); n != 3 {
		t.Fatalf("goal detail lists %d contributions, want 3", n)
	}

	// the list endpoint reports the same balance
	rec = c.do(http.MethodGet, "/api/goals", nil)
	listed := decode(t, rec)["goals"].([]any)[0].(map[string]any)
	if got := asInt(t, listed["currentBalance"]); got != 45000 {
		t.Fatalf("listed balance = %d, want 45000", got)
	}

	// deleting the withdrawal restores its amount
	rec = c.do(http.MethodDelete, "/api/goals/"+goalID+"/contributions/"+withdrawalID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete contribution status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := asInt(t, decode(t, rec)["goal"].(map[string]any)["currentBalance"]); got != 50000 {
		t.Fatalf("balance after delete = %d, want 50000", got)
	}
	rec = c.do(http.MethodDelete, "/api/goals/"+goalID+"/contributions/"+withdrawalID, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("re-delete contribution status = %d, want 404", rec.Code)
	}

	// invalid contributions are rejected
	rec = c.do(http.MethodPost, "/api/goals/"+goalID+"/contributions", map[string]any{"amount": 0})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zero contribution status = %d, want 400", rec.Code)
	}
	rec = c.do(http.MethodPost, "/api/goals/"+goalID+"/contributions", map[string]any{"amount": 100, "date": "Feb 1"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad date contribution status = %d, want 400", rec.Code)
	}

	// unknown and soft-deleted transactions cannot be linked
	rec = c.do(http.MethodPost, "/api/goals/"+goalID+"/contributions", map[string]any{"amount": 100, "transactionId": uuid.New()})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown transaction link status = %d, want 404", rec.Code)
	}
	if err := env.q.SoftDeleteTransaction(context.Background(), db.SoftDeleteTransactionParams{ID: txn.ID, UserID: c.userID}); err != nil {
		t.Fatalf("soft delete fixture: %v", err)
	}
	rec = c.do(http.MethodPost, "/api/goals/"+goalID+"/contributions", map[string]any{"amount": 100, "transactionId": txn.ID})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("deleted transaction link status = %d, want 404", rec.Code)
	}
}

// TestProjectionMath checks required-monthly-contribution against a target
// date a known number of months ahead, including the round-up on division.
func TestProjectionMath(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("planner@example.com")

	// the 1st of (current month + 5) is always in the future, spanning exactly
	// 6 contribution months (this one through the target's, inclusive)
	now := time.Now().UTC()
	target := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 5, 0)
	goal := c.createGoal(t, goalBody(map[string]any{
		"targetAmount": 60000, "targetDate": target.Format("2006-01-02"),
	}))
	goalID := goal["id"].(string)
	if got := asInt(t, goal["monthsRemaining"]); got != 6 {
		t.Fatalf("monthsRemaining = %d, want 6", got)
	}
	if got := asInt(t, goal["requiredMonthlyContribution"]); got != 10000 {
		t.Fatalf("requiredMonthlyContribution = %d, want 60000 / 6 = 10000", got)
	}

	// a contribution of 1 leaves 59999 over 6 months: must round up, not down
	rec := c.do(http.MethodPost, "/api/goals/"+goalID+"/contributions", map[string]any{"amount": 1})
	if rec.Code != http.StatusCreated {
		t.Fatalf("contribution status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	goal = c.getGoal(t, goalID)
	if got := asInt(t, goal["requiredMonthlyContribution"]); got != 10000 {
		t.Fatalf("requiredMonthlyContribution = %d, want ceil(59999 / 6) = 10000", got)
	}

	// no target date → no projection
	open := c.createGoal(t, goalBody(map[string]any{"name": "Someday"}))
	if open["monthsRemaining"] != nil || open["requiredMonthlyContribution"] != nil || open["behindSchedule"] != false {
		t.Fatalf("goal without target date should have no projection: %v", open)
	}
}

// TestBehindScheduleBoundaries checks the boundary cases: an unmet goal past
// its target date is behind; a met goal is never behind, even past the date;
// a brand-new goal with a future date is on pace.
func TestBehindScheduleBoundaries(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("boundaries@example.com")

	// past target date, unmet → behind, whole remainder due now
	overdue := c.createGoal(t, goalBody(map[string]any{"name": "Overdue", "targetDate": "2020-01-01"}))
	if overdue["behindSchedule"] != true {
		t.Fatalf("unmet goal past target date should be behind: %v", overdue)
	}
	if got := asInt(t, overdue["monthsRemaining"]); got != 0 {
		t.Fatalf("monthsRemaining past target = %d, want 0", got)
	}
	if got := asInt(t, overdue["requiredMonthlyContribution"]); got != 100000 {
		t.Fatalf("required past target = %d, want the full 100000 due now", got)
	}

	// meeting the target clears behind-schedule even past the date
	overdueID := overdue["id"].(string)
	rec := c.do(http.MethodPost, "/api/goals/"+overdueID+"/contributions", map[string]any{"amount": 100000})
	if rec.Code != http.StatusCreated {
		t.Fatalf("contribution status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	met := c.getGoal(t, overdueID)
	if met["behindSchedule"] != false || asInt(t, met["amountRemaining"]) != 0 ||
		asInt(t, met["requiredMonthlyContribution"]) != 0 {
		t.Fatalf("met goal should not be behind and needs nothing monthly: %v", met)
	}

	// a brand-new goal with a future target date starts on pace
	fresh := c.createGoal(t, goalBody(map[string]any{"name": "Fresh", "targetDate": "2035-01-01"}))
	if fresh["behindSchedule"] != false {
		t.Fatalf("new goal with future target should not be behind: %v", fresh)
	}

	// overfunding never reports negative remaining
	freshID := fresh["id"].(string)
	rec = c.do(http.MethodPost, "/api/goals/"+freshID+"/contributions", map[string]any{"amount": 150000})
	if rec.Code != http.StatusCreated {
		t.Fatalf("overfund status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	over := c.getGoal(t, freshID)
	if asInt(t, over["amountRemaining"]) != 0 || over["behindSchedule"] != false {
		t.Fatalf("overfunded goal fields wrong: %v", over)
	}
}
