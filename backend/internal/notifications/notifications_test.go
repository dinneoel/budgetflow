// Integration tests for the notifications engine and API: real HTTP router
// (handlers + auth/CSRF middleware + after-write triggers) against a real
// PostgreSQL database per test.
package notifications_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// --- fixtures (direct DB inserts; the API paths under test are exercised
// through the router) ---

func (e *testEnv) addAccount(userID uuid.UUID, name string) uuid.UUID {
	e.t.Helper()
	acc, err := e.q.CreateAccount(context.Background(), db.CreateAccountParams{
		UserID: userID, Name: name, Type: "checking", Currency: "USD", IncludeInNetWorth: true,
	})
	if err != nil {
		e.t.Fatalf("create fixture account: %v", err)
	}
	return acc.ID
}

func (e *testEnv) addCategory(userID uuid.UUID, name string) uuid.UUID {
	e.t.Helper()
	ctx := context.Background()
	g, err := e.q.CreateCategoryGroup(ctx, db.CreateCategoryGroupParams{UserID: userID, Name: name + " Group"})
	if err != nil {
		e.t.Fatalf("create fixture group: %v", err)
	}
	cat, err := e.q.CreateCategory(ctx, db.CreateCategoryParams{
		UserID: userID, GroupID: g.ID, Name: name, BudgetType: "variable", RolloverRule: "none",
	})
	if err != nil {
		e.t.Fatalf("create fixture category: %v", err)
	}
	return cat.ID
}

func (e *testEnv) addPeriod(userID uuid.UUID, year, month int) uuid.UUID {
	e.t.Helper()
	p, err := e.q.CreateBudgetPeriod(context.Background(), db.CreateBudgetPeriodParams{
		UserID: userID, Year: int32(year), Month: int32(month), Currency: "USD", PlannedIncome: 100000,
	})
	if err != nil {
		e.t.Fatalf("create fixture period: %v", err)
	}
	return p.ID
}

func (e *testEnv) setAllocation(userID, periodID, categoryID uuid.UUID, amount int64) {
	e.t.Helper()
	if _, err := e.q.UpsertBudgetAllocation(context.Background(), db.UpsertBudgetAllocationParams{
		UserID: userID, PeriodID: periodID, CategoryID: categoryID, Amount: amount,
	}); err != nil {
		e.t.Fatalf("set fixture allocation: %v", err)
	}
}

func (c *client) addExpense(accountID, categoryID uuid.UUID, amount int64, date time.Time) {
	c.env.t.Helper()
	rec := c.do(http.MethodPost, "/api/transactions", map[string]any{
		"accountId": accountID, "categoryId": categoryID, "type": "expense",
		"amount": amount, "date": date.Format("2006-01-02"),
	})
	if rec.Code != http.StatusCreated {
		c.env.t.Fatalf("create expense status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
}

// countByType lists the user's notifications through the API and tallies them.
func (c *client) countByType() map[string]int {
	c.env.t.Helper()
	rec := c.do(http.MethodGet, "/api/notifications?limit=200", nil)
	if rec.Code != http.StatusOK {
		c.env.t.Fatalf("list notifications status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	resp := decode(c.env.t, rec)
	items, _ := resp["notifications"].([]any)
	counts := map[string]int{}
	for _, it := range items {
		typ, _ := it.(map[string]any)["type"].(string)
		counts[typ]++
	}
	return counts
}

func (c *client) evaluate() {
	c.env.t.Helper()
	rec := c.do(http.MethodPost, "/api/notifications/evaluate", nil)
	if rec.Code != http.StatusOK {
		c.env.t.Fatalf("evaluate status = %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func currentMonth(now time.Time) (int, int) { return now.Year(), int(now.Month()) }

// --- tests ---

func TestNotificationEndpointsAndReadState(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.signUp("reader@example.com")
	other := e.signUp("other@example.com")
	ctx := context.Background()

	var firstID uuid.UUID
	for i, title := range []string{"first", "second"} {
		n, err := e.q.CreateNotification(ctx, db.CreateNotificationParams{
			UserID: c.userID, Type: "bill_due", Title: title, DedupeKey: fmt.Sprintf("test:%d", i),
		})
		if err != nil {
			t.Fatalf("seed notification: %v", err)
		}
		if i == 0 {
			firstID = n.ID
		}
	}

	rec := c.do(http.MethodGet, "/api/notifications", nil)
	resp := decode(t, rec)
	if got := len(resp["notifications"].([]any)); got != 2 {
		t.Fatalf("listed %d notifications, want 2", got)
	}
	if got := resp["unreadCount"].(float64); got != 2 {
		t.Fatalf("unreadCount = %v, want 2", got)
	}

	// the other user sees nothing, and cannot mark this user's rows read
	if got := decode(t, other.do(http.MethodGet, "/api/notifications", nil))["unreadCount"].(float64); got != 0 {
		t.Fatalf("other user's unreadCount = %v, want 0", got)
	}
	if rec := other.do(http.MethodPost, "/api/notifications/"+firstID.String()+"/read", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("cross-user mark-read status = %d, want 204", rec.Code)
	}
	if got := decode(t, c.do(http.MethodGet, "/api/notifications/unread-count", nil))["unreadCount"].(float64); got != 2 {
		t.Fatalf("unreadCount after cross-user mark-read = %v, want 2", got)
	}

	if rec := c.do(http.MethodPost, "/api/notifications/"+firstID.String()+"/read", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("mark-read status = %d, want 204", rec.Code)
	}
	if got := decode(t, c.do(http.MethodGet, "/api/notifications/unread-count", nil))["unreadCount"].(float64); got != 1 {
		t.Fatalf("unreadCount after mark-read = %v, want 1", got)
	}

	if rec := c.do(http.MethodPost, "/api/notifications/read-all", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("read-all status = %d, want 204", rec.Code)
	}
	if got := decode(t, c.do(http.MethodGet, "/api/notifications/unread-count", nil))["unreadCount"].(float64); got != 0 {
		t.Fatalf("unreadCount after read-all = %v, want 0", got)
	}
}

func TestThresholdCrossingFiresExactlyOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.signUp("threshold@example.com")
	now := time.Now().UTC()
	year, month := currentMonth(now)

	account := e.addAccount(c.userID, "Checking")
	category := e.addCategory(c.userID, "Groceries")
	period := e.addPeriod(c.userID, year, month)
	e.setAllocation(c.userID, period, category, 10000)

	// 85% spent → remaining 1500 is within the default 20% threshold
	c.addExpense(account, category, 8500, now)
	if got := c.countByType()["category_threshold"]; got != 1 {
		t.Fatalf("category_threshold notifications after crossing = %d, want 1", got)
	}

	// further writes while the condition persists must not re-notify
	c.addExpense(account, category, 100, now)
	c.addExpense(account, category, 100, now)
	counts := c.countByType()
	if got := counts["category_threshold"]; got != 1 {
		t.Fatalf("category_threshold notifications after more writes = %d, want 1 (dedupe)", got)
	}
	if got := counts["category_over_budget"]; got != 0 {
		t.Fatalf("category_over_budget notifications = %d, want 0 while not over", got)
	}

	// crossing into over-budget is a new condition and fires once
	c.addExpense(account, category, 6000, now)
	c.addExpense(account, category, 100, now)
	counts = c.countByType()
	if got := counts["category_over_budget"]; got != 1 {
		t.Fatalf("category_over_budget notifications = %d, want 1", got)
	}
	if got := counts["category_threshold"]; got != 1 {
		t.Fatalf("category_threshold notifications = %d, want still 1", got)
	}
}

func TestAllocationWriteTriggersEvaluation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.signUp("allocation@example.com")
	now := time.Now().UTC()
	year, month := currentMonth(now)

	account := e.addAccount(c.userID, "Checking")
	category := e.addCategory(c.userID, "Dining")
	period := e.addPeriod(c.userID, year, month)
	e.setAllocation(c.userID, period, category, 10000)

	// 50% spent: on track, nothing fires
	c.addExpense(account, category, 5000, now)
	if got := len(c.countByType()); got != 0 {
		t.Fatalf("notifications while on track = %d, want 0", got)
	}

	// shrinking the allocation via the API pushes the category into the
	// warning band (remaining 500 of 5500 available)
	rec := c.do(http.MethodPut, fmt.Sprintf("/api/budgets/%s/allocations/%s", period, category),
		map[string]any{"amount": 5500})
	if rec.Code != http.StatusOK {
		t.Fatalf("set allocation status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := c.countByType()["category_threshold"]; got != 1 {
		t.Fatalf("category_threshold notifications after allocation write = %d, want 1", got)
	}
}

func TestPreferenceSuppressionAndCustomThreshold(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	now := time.Now().UTC()
	year, month := currentMonth(now)

	// disabled type never fires
	muted := e.signUp("muted@example.com")
	account := e.addAccount(muted.userID, "Checking")
	category := e.addCategory(muted.userID, "Groceries")
	period := e.addPeriod(muted.userID, year, month)
	e.setAllocation(muted.userID, period, category, 10000)
	rec := muted.do(http.MethodPut, "/api/notifications/preferences",
		map[string]any{"type": "category_threshold", "enabled": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("set preference status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	muted.addExpense(account, category, 8500, now)
	if got := len(muted.countByType()); got != 0 {
		t.Fatalf("notifications with category_threshold disabled = %d, want 0", got)
	}

	// a raised threshold fires earlier than the default would
	eager := e.signUp("eager@example.com")
	account = e.addAccount(eager.userID, "Checking")
	category = e.addCategory(eager.userID, "Groceries")
	period = e.addPeriod(eager.userID, year, month)
	e.setAllocation(eager.userID, period, category, 10000)
	rec = eager.do(http.MethodPut, "/api/notifications/preferences",
		map[string]any{"type": "category_threshold", "enabled": true, "thresholdPct": 50})
	if rec.Code != http.StatusOK {
		t.Fatalf("set preference status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	// 60% spent: remaining 40% ≤ 50% custom threshold, but above the 20% default
	eager.addExpense(account, category, 6000, now)
	if got := eager.countByType()["category_threshold"]; got != 1 {
		t.Fatalf("category_threshold notifications at custom threshold = %d, want 1", got)
	}

	// preferences endpoint reports all types, with defaults merged
	prefs := decode(t, eager.do(http.MethodGet, "/api/notifications/preferences", nil))["preferences"].([]any)
	if len(prefs) != 6 {
		t.Fatalf("preferences listed = %d, want 6", len(prefs))
	}
	first := prefs[0].(map[string]any)
	if first["type"] != "category_threshold" || first["thresholdPct"].(float64) != 50 {
		t.Fatalf("category_threshold preference = %v, want thresholdPct 50", first)
	}

	// invalid input rejected
	if rec := eager.do(http.MethodPut, "/api/notifications/preferences",
		map[string]any{"type": "nope", "enabled": true}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown type status = %d, want 400", rec.Code)
	}
	if rec := eager.do(http.MethodPut, "/api/notifications/preferences",
		map[string]any{"type": "bill_due", "enabled": true, "thresholdPct": 0}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad threshold status = %d, want 400", rec.Code)
	}
}

func TestBillDueTiming(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.signUp("bills@example.com")
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	ctx := context.Background()

	account := e.addAccount(c.userID, "Checking")
	category := e.addCategory(c.userID, "Utilities")
	addRule := func(name string, due time.Time, lead int32) {
		if _, err := e.q.CreateRecurringRule(ctx, db.CreateRecurringRuleParams{
			UserID: c.userID, Name: name, AccountID: account, CategoryID: category,
			Amount: 4500, Frequency: "monthly", NextDueDate: due, AnchorDate: due, ReminderLeadDays: lead,
		}); err != nil {
			t.Fatalf("create fixture rule: %v", err)
		}
	}
	addRule("Electric", today.AddDate(0, 0, 2), 3)   // inside the reminder window
	addRule("Insurance", today.AddDate(0, 0, 10), 3) // outside the window

	c.evaluate()
	counts := c.countByType()
	if got := counts["bill_due"]; got != 1 {
		t.Fatalf("bill_due notifications = %d, want 1 (only the bill inside its lead window)", got)
	}

	// re-running the evaluator must not duplicate the reminder
	c.evaluate()
	if got := c.countByType()["bill_due"]; got != 1 {
		t.Fatalf("bill_due notifications after re-evaluate = %d, want 1 (dedupe)", got)
	}

	rec := c.do(http.MethodGet, "/api/notifications", nil)
	items := decode(t, rec)["notifications"].([]any)
	title, _ := items[0].(map[string]any)["title"].(string)
	if want := "Electric is due in 2 days"; title != want {
		t.Fatalf("bill_due title = %q, want %q", title, want)
	}
}

func TestScheduledEvaluatorConditions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.signUp("scheduled@example.com")
	now := time.Now().UTC()
	ctx := context.Background()

	// budget exists for last month but not this one
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	prev := firstOfMonth.AddDate(0, -1, 0)
	e.addPeriod(c.userID, prev.Year(), int(prev.Month()))

	// unmet goal whose target date has passed is behind schedule
	yesterday := now.AddDate(0, 0, -1)
	if _, err := e.q.CreateGoal(ctx, db.CreateGoalParams{
		UserID: c.userID, Name: "Emergency fund", Type: "savings", TargetAmount: 100000, TargetDate: &yesterday,
	}); err != nil {
		t.Fatalf("create fixture goal: %v", err)
	}

	// uploaded-but-uncommitted import batch needs review
	account := e.addAccount(c.userID, "Checking")
	if _, err := e.q.CreateImportBatch(ctx, db.CreateImportBatchParams{
		UserID: c.userID, AccountID: &account, FileName: "bank.csv", Status: "pending",
	}); err != nil {
		t.Fatalf("create fixture import batch: %v", err)
	}

	c.evaluate()
	counts := c.countByType()
	for _, typ := range []string{"budget_month_missing", "goal_behind_schedule", "import_needs_review"} {
		if counts[typ] != 1 {
			t.Fatalf("%s notifications = %d, want 1 (all: %v)", typ, counts[typ], counts)
		}
	}

	// idempotent while the conditions persist
	c.evaluate()
	after := c.countByType()
	for typ, n := range counts {
		if after[typ] != n {
			t.Fatalf("%s notifications changed on re-evaluate: %d -> %d", typ, n, after[typ])
		}
	}
}
