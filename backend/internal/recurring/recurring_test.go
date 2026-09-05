// Integration tests for the recurring bills API: real HTTP router (handlers +
// auth/CSRF middleware) against a real PostgreSQL database per test.
package recurring_test

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

func (e *testEnv) addCategory(userID uuid.UUID, name string) uuid.UUID {
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
	cat, err := e.q.CreateCategory(ctx, db.CreateCategoryParams{
		UserID: userID, GroupID: groupID, Name: name, BudgetType: "fixed", RolloverRule: "none",
	})
	if err != nil {
		e.t.Fatalf("create fixture category: %v", err)
	}
	return cat.ID
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

func (c *client) createRule(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	rec := c.do(http.MethodPost, "/api/recurring", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create rule status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(t, rec)["rule"].(map[string]any)
}

func ruleBody(accountID, categoryID uuid.UUID, overrides map[string]any) map[string]any {
	body := map[string]any{
		"name":        "Netflix",
		"accountId":   accountID,
		"categoryId":  categoryID,
		"amount":      1499,
		"frequency":   "monthly",
		"nextDueDate": "2030-05-10",
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

func TestRecurringCRUDAndArchive(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("bills@example.com")
	accountID := env.addAccount(c.userID)
	categoryID := env.addCategory(c.userID, "Subscriptions")

	rule := c.createRule(t, ruleBody(accountID, categoryID, nil))
	if rule["name"] != "Netflix" || asInt(t, rule["amount"]) != 1499 ||
		rule["frequency"] != "monthly" || rule["nextDueDate"] != "2030-05-10" ||
		asInt(t, rule["reminderLeadDays"]) != 3 {
		t.Fatalf("created rule fields wrong: %v", rule)
	}
	ruleID := rule["id"].(string)

	rec := c.do(http.MethodGet, "/api/recurring", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if n := len(decode(t, rec)["rules"].([]any)); n != 1 {
		t.Fatalf("list returned %d rules, want 1", n)
	}

	rec = c.do(http.MethodPut, "/api/recurring/"+ruleID, ruleBody(accountID, categoryID, map[string]any{
		"name": "Netflix Premium", "amount": 1999, "reminderLeadDays": 7,
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	updated := decode(t, rec)["rule"].(map[string]any)
	if updated["name"] != "Netflix Premium" || asInt(t, updated["amount"]) != 1999 || asInt(t, updated["reminderLeadDays"]) != 7 {
		t.Fatalf("updated rule fields wrong: %v", updated)
	}

	rec = c.do(http.MethodPost, "/api/recurring/"+ruleID+"/archive", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("archive status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	rec = c.do(http.MethodGet, "/api/recurring", nil)
	if n := len(decode(t, rec)["rules"].([]any)); n != 0 {
		t.Fatalf("list after archive returned %d rules, want 0", n)
	}
	rec = c.do(http.MethodGet, "/api/recurring/upcoming?days=365", nil)
	if n := len(decode(t, rec)["upcoming"].([]any)); n != 0 {
		t.Fatalf("upcoming after archive returned %d rules, want 0", n)
	}

	rec = c.do(http.MethodPost, "/api/recurring/"+ruleID+"/unarchive", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	rec = c.do(http.MethodGet, "/api/recurring", nil)
	if n := len(decode(t, rec)["rules"].([]any)); n != 1 {
		t.Fatalf("list after unarchive returned %d rules, want 1", n)
	}
}

func TestRecurringValidationAndOwnership(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("owner@example.com")
	accountID := env.addAccount(c.userID)
	categoryID := env.addCategory(c.userID, "Bills")

	for name, overrides := range map[string]map[string]any{
		"bad frequency":            {"frequency": "fortnightly"},
		"custom without interval":  {"frequency": "custom"},
		"zero amount":              {"amount": 0},
		"negative amount":          {"amount": -100},
		"bad date":                 {"nextDueDate": "05/10/2030"},
		"empty name":               {"name": "  "},
		"negative reminder":        {"reminderLeadDays": -1},
		"custom negative interval": {"frequency": "custom", "customIntervalDays": -5},
	} {
		rec := c.do(http.MethodPost, "/api/recurring", ruleBody(accountID, categoryID, overrides))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body: %s)", name, rec.Code, rec.Body.String())
		}
	}

	// references must belong to the caller
	other := env.signUp("intruder@example.com")
	rec := other.do(http.MethodPost, "/api/recurring", ruleBody(accountID, env.addCategory(other.userID, "Mine"), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign account: status = %d, want 404", rec.Code)
	}
	rule := c.createRule(t, ruleBody(accountID, categoryID, nil))
	rec = other.do(http.MethodGet, "/api/recurring/"+rule["id"].(string), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign rule get: status = %d, want 404", rec.Code)
	}
	rec = other.do(http.MethodPost, "/api/recurring/"+rule["id"].(string)+"/pay", map[string]any{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign rule pay: status = %d, want 404", rec.Code)
	}
}

func TestMarkPaidCreatesTransactionAndAdvances(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("payer@example.com")
	accountID := env.addAccount(c.userID)
	categoryID := env.addCategory(c.userID, "Rent")

	// due on the 31st: advancement must clamp to Apr 30 and recover May 31
	rule := c.createRule(t, ruleBody(accountID, categoryID, map[string]any{
		"name": "Rent", "amount": 120000, "nextDueDate": "2030-03-31",
	}))
	ruleID := rule["id"].(string)

	rec := c.do(http.MethodPost, "/api/recurring/"+ruleID+"/pay", map[string]any{})
	if rec.Code != http.StatusCreated {
		t.Fatalf("pay status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	resp := decode(t, rec)
	txn := resp["transaction"].(map[string]any)
	if txn["type"] != "expense" || asInt(t, txn["amount"]) != -120000 ||
		txn["payee"] != "Rent" || txn["date"] != "2030-03-31" ||
		txn["recurringRuleId"] != ruleID || txn["categoryId"].(string) != categoryID.String() {
		t.Fatalf("paid transaction fields wrong: %v", txn)
	}
	if due := resp["rule"].(map[string]any)["nextDueDate"]; due != "2030-04-30" {
		t.Fatalf("next due after pay = %v, want 2030-04-30", due)
	}

	// the transaction is real: it is on the ledger and affects the balance
	id, err := uuid.Parse(txn["id"].(string))
	if err != nil {
		t.Fatalf("bad transaction id: %v", err)
	}
	stored, err := env.q.GetTransaction(context.Background(), db.GetTransactionParams{ID: id, UserID: c.userID})
	if err != nil {
		t.Fatalf("load paid transaction: %v", err)
	}
	if stored.Amount != -120000 || stored.RecurringRuleID == nil || stored.RecurringRuleID.String() != ruleID {
		t.Fatalf("stored transaction wrong: %+v", stored)
	}

	// paying again advances past the clamped month back to the 31st, with
	// overrides applied
	rec = c.do(http.MethodPost, "/api/recurring/"+ruleID+"/pay", map[string]any{"amount": 125000, "date": "2030-04-28", "status": "cleared"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("second pay status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	resp = decode(t, rec)
	txn = resp["transaction"].(map[string]any)
	if asInt(t, txn["amount"]) != -125000 || txn["date"] != "2030-04-28" || txn["status"] != "cleared" {
		t.Fatalf("override pay transaction wrong: %v", txn)
	}
	if due := resp["rule"].(map[string]any)["nextDueDate"]; due != "2030-05-31" {
		t.Fatalf("next due after second pay = %v, want 2030-05-31 (anchor day recovered)", due)
	}
}

func TestMatchExistingTransaction(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("matcher@example.com")
	accountID := env.addAccount(c.userID)
	categoryID := env.addCategory(c.userID, "Utilities")

	rule := c.createRule(t, ruleBody(accountID, categoryID, map[string]any{
		"name": "Electric", "frequency": "weekly", "nextDueDate": "2030-05-10",
	}))
	ruleID := rule["id"].(string)

	txn, err := env.q.CreateTransaction(context.Background(), db.CreateTransactionParams{
		UserID: c.userID, AccountID: accountID, CategoryID: &categoryID, Type: "expense",
		Status: "cleared", Amount: -1520, Date: time.Date(2030, 5, 9, 0, 0, 0, 0, time.UTC), Payee: "Power Co",
	})
	if err != nil {
		t.Fatalf("create fixture transaction: %v", err)
	}

	rec := c.do(http.MethodPost, "/api/recurring/"+ruleID+"/match", map[string]any{"transactionId": txn.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("match status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	resp := decode(t, rec)
	if got := resp["transaction"].(map[string]any)["recurringRuleId"]; got != ruleID {
		t.Fatalf("matched transaction recurringRuleId = %v, want %s", got, ruleID)
	}
	if due := resp["rule"].(map[string]any)["nextDueDate"]; due != "2030-05-17" {
		t.Fatalf("next due after match = %v, want 2030-05-17", due)
	}

	// unknown and soft-deleted transactions cannot be matched
	rec = c.do(http.MethodPost, "/api/recurring/"+ruleID+"/match", map[string]any{"transactionId": uuid.New()})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("match unknown transaction status = %d, want 404", rec.Code)
	}
	if err := env.q.SoftDeleteTransaction(context.Background(), db.SoftDeleteTransactionParams{ID: txn.ID, UserID: c.userID}); err != nil {
		t.Fatalf("soft delete fixture: %v", err)
	}
	rec = c.do(http.MethodPost, "/api/recurring/"+ruleID+"/match", map[string]any{"transactionId": txn.ID})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("match deleted transaction status = %d, want 404", rec.Code)
	}
}

func TestUpcomingWindow(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("upcoming@example.com")
	accountID := env.addAccount(c.userID)
	categoryID := env.addCategory(c.userID, "Bills")

	today := time.Now().UTC()
	day := func(offset int) string { return today.AddDate(0, 0, offset).Format("2006-01-02") }
	c.createRule(t, ruleBody(accountID, categoryID, map[string]any{"name": "Overdue", "nextDueDate": day(-2)}))
	c.createRule(t, ruleBody(accountID, categoryID, map[string]any{"name": "Soon", "nextDueDate": day(5)}))
	c.createRule(t, ruleBody(accountID, categoryID, map[string]any{"name": "Later", "nextDueDate": day(40)}))

	rec := c.do(http.MethodGet, "/api/recurring/upcoming?days=30", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("upcoming status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	bills := decode(t, rec)["upcoming"].([]any)
	if len(bills) != 2 {
		t.Fatalf("upcoming returned %d bills, want 2 (overdue + soon): %v", len(bills), bills)
	}
	first := bills[0].(map[string]any)
	second := bills[1].(map[string]any)
	if first["name"] != "Overdue" || asInt(t, first["daysUntilDue"]) != -2 {
		t.Fatalf("first upcoming = %v, want Overdue at -2 days", first)
	}
	if second["name"] != "Soon" || asInt(t, second["daysUntilDue"]) != 5 {
		t.Fatalf("second upcoming = %v, want Soon at 5 days", second)
	}

	rec = c.do(http.MethodGet, "/api/recurring/upcoming?days=60", nil)
	if n := len(decode(t, rec)["upcoming"].([]any)); n != 3 {
		t.Fatalf("upcoming days=60 returned %d bills, want 3", n)
	}
	rec = c.do(http.MethodGet, "/api/recurring/upcoming?days=0", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("upcoming days=0 status = %d, want 400", rec.Code)
	}
}

// TestReservedAppearsInCategoryRemaining exercises the PRD formula
// Remaining = Budgeted + Rollover − ActualSpending − ReservedUpcomingPayments
// end to end: a bill due in the budget month reserves its amount, and paying
// it converts the reservation into actual spending.
func TestReservedAppearsInCategoryRemaining(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("reserved@example.com")
	accountID := env.addAccount(c.userID)
	categoryID := env.addCategory(c.userID, "Housing")

	rec := c.do(http.MethodPost, "/api/budgets", map[string]any{"year": 2030, "month": 5, "plannedIncome": 100000})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create period status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	periodID := decode(t, rec)["period"].(map[string]any)["id"].(string)
	rec = c.do(http.MethodPut, "/api/budgets/"+periodID+"/allocations/"+categoryID.String(), map[string]any{"amount": 20000})
	if rec.Code != http.StatusOK {
		t.Fatalf("set allocation status = %d (body: %s)", rec.Code, rec.Body.String())
	}

	// an expense of 3000 plus a 5000 bill due in May 2030
	if _, err := env.q.CreateTransaction(context.Background(), db.CreateTransactionParams{
		UserID: c.userID, AccountID: accountID, CategoryID: &categoryID, Type: "expense",
		Status: "cleared", Amount: -3000, Date: time.Date(2030, 5, 2, 0, 0, 0, 0, time.UTC), Payee: "fixture",
	}); err != nil {
		t.Fatalf("create fixture expense: %v", err)
	}
	rule := c.createRule(t, ruleBody(accountID, categoryID, map[string]any{
		"name": "Insurance", "amount": 5000, "nextDueDate": "2030-05-20",
	}))

	row := categoryRow(t, c.getPeriod(t, 2030, 5), categoryID)
	if asInt(t, row["reserved"]) != 5000 {
		t.Fatalf("reserved = %d, want 5000", asInt(t, row["reserved"]))
	}
	if got := asInt(t, row["remaining"]); got != 12000 {
		t.Fatalf("remaining = %d, want 20000 − 3000 − 5000 = 12000", got)
	}

	// paying the bill (for 4000) moves it from reserved to actual spending
	// and advances its due date out of the month
	rec = c.do(http.MethodPost, "/api/recurring/"+rule["id"].(string)+"/pay", map[string]any{"amount": 4000, "date": "2030-05-20"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("pay status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	row = categoryRow(t, c.getPeriod(t, 2030, 5), categoryID)
	if asInt(t, row["reserved"]) != 0 {
		t.Fatalf("reserved after pay = %d, want 0", asInt(t, row["reserved"]))
	}
	if got := asInt(t, row["remaining"]); got != 13000 {
		t.Fatalf("remaining after pay = %d, want 20000 − 7000 = 13000", got)
	}
}

func (c *client) getPeriod(t *testing.T, year, month int) map[string]any {
	t.Helper()
	rec := c.do(http.MethodGet, fmt.Sprintf("/api/budgets/month/%d/%d", year, month), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get period status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(t, rec)["period"].(map[string]any)
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
	t.Fatalf("category %s not in period detail: %v", categoryID, period["categories"])
	return nil
}
