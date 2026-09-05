// Integration tests for the budgets API: real HTTP router (handlers +
// auth/CSRF middleware) against a real PostgreSQL database per test.
package budgets_test

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

// fixtures built directly through the query layer (the transactions API is a
// later task).

func (e *testEnv) addCategory(userID uuid.UUID, name, rolloverRule string) uuid.UUID {
	e.t.Helper()
	ctx := context.Background()
	groups, err := e.q.ListCategoryGroupsByUser(ctx, userID)
	if err != nil {
		e.t.Fatalf("list groups: %v", err)
	}
	var groupID uuid.UUID
	if len(groups) > 0 {
		groupID = groups[0].ID
	} else {
		g, err := e.q.CreateCategoryGroup(ctx, db.CreateCategoryGroupParams{UserID: userID, Name: "Fixture Group"})
		if err != nil {
			e.t.Fatalf("create fixture group: %v", err)
		}
		groupID = g.ID
	}
	c, err := e.q.CreateCategory(ctx, db.CreateCategoryParams{
		UserID: userID, GroupID: groupID, Name: name, BudgetType: "variable", RolloverRule: rolloverRule,
	})
	if err != nil {
		e.t.Fatalf("create fixture category: %v", err)
	}
	return c.ID
}

func (e *testEnv) addAccount(userID uuid.UUID) uuid.UUID {
	e.t.Helper()
	acc, err := e.q.CreateAccount(context.Background(), db.CreateAccountParams{
		UserID: userID, Name: "Fixture", Type: "checking", Currency: "USD", IncludeInNetWorth: true,
	})
	if err != nil {
		e.t.Fatalf("create fixture account: %v", err)
	}
	return acc.ID
}

// addExpense records a categorized expense of `spent` minor units (a positive
// spend is stored as a negative ledger amount) on the given date.
func (e *testEnv) addExpense(userID, accountID, categoryID uuid.UUID, spent int64, date time.Time) {
	e.t.Helper()
	_, err := e.q.CreateTransaction(context.Background(), db.CreateTransactionParams{
		UserID: userID, AccountID: accountID, CategoryID: &categoryID, Type: "expense",
		Status: "cleared", Amount: -spent, Date: date, Payee: "fixture payee",
	})
	if err != nil {
		e.t.Fatalf("create fixture expense: %v", err)
	}
}

func (c *client) createPeriod(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	rec := c.do(http.MethodPost, "/api/budgets", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create period status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(t, rec)["period"].(map[string]any)
}

func (c *client) getPeriod(t *testing.T, year, month int) map[string]any {
	t.Helper()
	rec := c.do(http.MethodGet, fmt.Sprintf("/api/budgets/month/%d/%d", year, month), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get period status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(t, rec)["period"].(map[string]any)
}

func (c *client) setAllocation(t *testing.T, periodID string, categoryID uuid.UUID, amount int64) map[string]any {
	t.Helper()
	rec := c.do(http.MethodPut, "/api/budgets/"+periodID+"/allocations/"+categoryID.String(), map[string]any{"amount": amount})
	if rec.Code != http.StatusOK {
		t.Fatalf("set allocation status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(t, rec)["period"].(map[string]any)
}

func asInt(t *testing.T, v any) int64 {
	t.Helper()
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("value %v (%T) is not a number", v, v)
	}
	return int64(f)
}

// categoryRow finds one category entry in a period detail response.
func categoryRow(t *testing.T, period map[string]any, categoryID uuid.UUID) map[string]any {
	t.Helper()
	for _, raw := range period["categories"].([]any) {
		row := raw.(map[string]any)
		if row["categoryId"] == categoryID.String() {
			return row
		}
	}
	t.Fatalf("category %s not present in period detail", categoryID)
	return nil
}

func TestCreateUsesDefaultCurrencyAndRejectsDuplicates(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("budget-create@example.com")

	p := c.createPeriod(t, map[string]any{"year": 2026, "month": 1, "notes": "first month"})
	if p["currency"] != "USD" {
		t.Errorf("currency = %v, want the user's default USD", p["currency"])
	}
	if p["notes"] != "first month" {
		t.Errorf("notes = %v, want %q", p["notes"], "first month")
	}

	rec := c.do(http.MethodPost, "/api/budgets", map[string]any{"year": 2026, "month": 1})
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate period status = %d, want 409", rec.Code)
	}
	if rec := c.do(http.MethodPost, "/api/budgets", map[string]any{"year": 2026, "month": 13}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad month status = %d, want 400", rec.Code)
	}
}

func TestUnallocatedMathAndStatuses(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("budget-math@example.com")
	groceries := env.addCategory(c.userID, "Groceries", "none")
	fun := env.addCategory(c.userID, "Fun", "none")
	account := env.addAccount(c.userID)

	p := c.createPeriod(t, map[string]any{"year": 2026, "month": 1, "plannedIncome": 100000})
	if got := asInt(t, p["unallocated"]); got != 100000 {
		t.Fatalf("unallocated = %d, want 100000 before any allocation", got)
	}
	periodID := p["id"].(string)

	p = c.setAllocation(t, periodID, groceries, 60000)
	if got := asInt(t, p["unallocated"]); got != 40000 {
		t.Errorf("unallocated after first allocation = %d, want 40000", got)
	}
	p = c.setAllocation(t, periodID, fun, 15000)
	if got := asInt(t, p["unallocated"]); got != 25000 {
		t.Errorf("unallocated after second allocation = %d, want 25000", got)
	}

	// 54000 of 60000 spent → remaining 6000 (10%) → approaching_limit;
	// fun overspends → over_budget.
	env.addExpense(c.userID, account, groceries, 54000, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC))
	env.addExpense(c.userID, account, fun, 20000, time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC))

	p = c.getPeriod(t, 2026, 1)
	g := categoryRow(t, p, groceries)
	if got := asInt(t, g["spending"]); got != 54000 {
		t.Errorf("groceries spending = %d, want 54000", got)
	}
	if got := asInt(t, g["remaining"]); got != 6000 {
		t.Errorf("groceries remaining = %d, want 6000", got)
	}
	if g["status"] != "approaching_limit" {
		t.Errorf("groceries status = %v, want approaching_limit", g["status"])
	}
	f := categoryRow(t, p, fun)
	if got := asInt(t, f["remaining"]); got != -5000 {
		t.Errorf("fun remaining = %d, want -5000", got)
	}
	if f["status"] != "over_budget" {
		t.Errorf("fun status = %v, want over_budget", f["status"])
	}
}

func TestCopyPriorMonth(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("budget-copy@example.com")
	groceries := env.addCategory(c.userID, "Groceries", "none")
	fun := env.addCategory(c.userID, "Fun", "none")

	jan := c.createPeriod(t, map[string]any{"year": 2026, "month": 1, "plannedIncome": 50000})
	janID := jan["id"].(string)
	c.setAllocation(t, janID, groceries, 30000)
	c.setAllocation(t, janID, fun, 10000)

	feb := c.createPeriod(t, map[string]any{"year": 2026, "month": 2, "plannedIncome": 50000, "copyPrior": true})
	if got := asInt(t, categoryRow(t, feb, groceries)["amount"]); got != 30000 {
		t.Errorf("copied groceries allocation = %d, want 30000", got)
	}
	if got := asInt(t, categoryRow(t, feb, fun)["amount"]); got != 10000 {
		t.Errorf("copied fun allocation = %d, want 10000", got)
	}
	// income 50000 + pool carry (jan unallocated 10000 + unspent 'none'
	// allocations 40000 returned) − copied allocations 40000 = 60000
	if got := asInt(t, feb["unallocated"]); got != 60000 {
		t.Errorf("feb unallocated = %d, want 60000", got)
	}

	// copied allocations are recorded in history as 0 → amount
	rec := c.do(http.MethodGet, "/api/budgets/"+feb["id"].(string)+"/history", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("history status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	entries := decode(t, rec)["history"].([]any)
	var copied int
	for _, raw := range entries {
		e := raw.(map[string]any)
		if e["field"] == "allocation" && asInt(t, e["oldAmount"]) == 0 {
			copied++
		}
	}
	if copied != 2 {
		t.Errorf("copied-allocation history entries = %d, want 2", copied)
	}
}

func TestRolloverAcrossPeriods(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("budget-rollover@example.com")
	groceries := env.addCategory(c.userID, "Groceries", "rollover")
	fun := env.addCategory(c.userID, "Fun", "none")
	sinking := env.addCategory(c.userID, "Car Repairs", "reset_to_target")
	account := env.addAccount(c.userID)

	jan := c.createPeriod(t, map[string]any{"year": 2026, "month": 1, "plannedIncome": 100000})
	janID := jan["id"].(string)
	c.setAllocation(t, janID, groceries, 50000)
	c.setAllocation(t, janID, fun, 20000)
	p := c.setAllocation(t, janID, sinking, 30000)
	if got := asInt(t, p["unallocated"]); got != 0 {
		t.Fatalf("jan unallocated = %d, want 0 (fully allocated)", got)
	}
	env.addExpense(c.userID, account, groceries, 30000, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
	env.addExpense(c.userID, account, fun, 5000, time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC))

	// Feb copies Jan's structure. Rollover: groceries keeps its unused 20000;
	// fun ('none') returns 15000 to the pool; sinking carries 30000 (at its
	// target, the prior budgeted amount).
	feb := c.createPeriod(t, map[string]any{"year": 2026, "month": 2, "plannedIncome": 100000, "copyPrior": true})
	if got := asInt(t, categoryRow(t, feb, groceries)["rollover"]); got != 20000 {
		t.Errorf("groceries rollover = %d, want 20000", got)
	}
	if got := asInt(t, categoryRow(t, feb, fun)["rollover"]); got != 0 {
		t.Errorf("fun rollover = %d, want 0 ('none' returns to pool)", got)
	}
	if got := asInt(t, categoryRow(t, feb, sinking)["rollover"]); got != 30000 {
		t.Errorf("sinking rollover = %d, want 30000", got)
	}
	// unallocated: income 100000 + pool carry (jan unallocated 0 + fun's
	// released 15000) − copied allocations 100000 = 15000
	if got := asInt(t, feb["unallocated"]); got != 15000 {
		t.Errorf("feb unallocated = %d, want 15000", got)
	}

	// Mar without copy: groceries carries 50000+20000=70000; sinking is
	// capped at its 30000 target, releasing the excess 30000 to the pool;
	// fun releases its full 20000.
	mar := c.createPeriod(t, map[string]any{"year": 2026, "month": 3})
	if got := asInt(t, categoryRow(t, mar, groceries)["rollover"]); got != 70000 {
		t.Errorf("mar groceries rollover = %d, want 70000", got)
	}
	if got := asInt(t, categoryRow(t, mar, sinking)["rollover"]); got != 30000 {
		t.Errorf("mar sinking rollover = %d, want 30000 (capped at target)", got)
	}
	// fun carried nothing, so no allocation row for it in March
	for _, raw := range mar["categories"].([]any) {
		if raw.(map[string]any)["categoryId"] == fun.String() {
			t.Errorf("fun should have no allocation row in an uncopied period")
		}
	}
	// unallocated: income 0 + pool carry (feb unallocated 15000 + fun 20000
	// + sinking excess 30000) − 0 allocated = 65000
	if got := asInt(t, mar["unallocated"]); got != 65000 {
		t.Errorf("mar unallocated = %d, want 65000", got)
	}
}

func TestHistoryRecordedOnEveryChange(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("budget-history@example.com")
	groceries := env.addCategory(c.userID, "Groceries", "none")

	p := c.createPeriod(t, map[string]any{"year": 2026, "month": 1, "plannedIncome": 5000})
	periodID := p["id"].(string)

	rec := c.do(http.MethodPut, "/api/budgets/"+periodID, map[string]any{"plannedIncome": 8000, "notes": "raise"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update period status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := decode(t, rec)["period"].(map[string]any)["notes"]; got != "raise" {
		t.Errorf("notes = %v, want %q", got, "raise")
	}

	c.setAllocation(t, periodID, groceries, 1000)
	c.setAllocation(t, periodID, groceries, 1000) // unchanged: no history entry
	c.setAllocation(t, periodID, groceries, 2500)

	rec = c.do(http.MethodGet, "/api/budgets/"+periodID+"/history", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("history status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	entries := decode(t, rec)["history"].([]any)
	type change struct {
		field    string
		old, new int64
	}
	want := []change{
		{"planned_income", 0, 5000},
		{"planned_income", 5000, 8000},
		{"allocation", 0, 1000},
		{"allocation", 1000, 2500},
	}
	if len(entries) != len(want) {
		t.Fatalf("history has %d entries, want %d: %v", len(entries), len(want), entries)
	}
	for i, raw := range entries {
		e := raw.(map[string]any)
		got := change{e["field"].(string), asInt(t, e["oldAmount"]), asInt(t, e["newAmount"])}
		if got != want[i] {
			t.Errorf("history[%d] = %+v, want %+v", i, got, want[i])
		}
	}
}

func TestCrossUserAccessDenied(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	owner := env.signUp("budget-owner@example.com")
	other := env.signUp("budget-other@example.com")
	groceries := env.addCategory(owner.userID, "Groceries", "none")

	p := owner.createPeriod(t, map[string]any{"year": 2026, "month": 1})
	periodID := p["id"].(string)

	if rec := other.do(http.MethodPut, "/api/budgets/"+periodID, map[string]any{"plannedIncome": 1}); rec.Code != http.StatusNotFound {
		t.Errorf("foreign period update status = %d, want 404", rec.Code)
	}
	if rec := other.do(http.MethodPut, "/api/budgets/"+periodID+"/allocations/"+groceries.String(), map[string]any{"amount": 1}); rec.Code != http.StatusNotFound {
		t.Errorf("foreign allocation update status = %d, want 404", rec.Code)
	}
	if rec := other.do(http.MethodGet, "/api/budgets/"+periodID+"/history", nil); rec.Code != http.StatusNotFound {
		t.Errorf("foreign history status = %d, want 404", rec.Code)
	}
	if rec := other.do(http.MethodGet, "/api/budgets/month/2026/1", nil); rec.Code != http.StatusNotFound {
		t.Errorf("foreign period by month status = %d, want 404 (other user has no period)", rec.Code)
	}
}
