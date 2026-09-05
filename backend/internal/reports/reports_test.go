// Integration tests for the dashboard and reports API: real HTTP router
// against a real PostgreSQL database, asserting aggregates over a seeded
// fixture set (transfers and refunds handled correctly, currency scoping,
// account-inclusion flag respected).
package reports_test

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

// getJSON GETs a path and decodes the JSON body, asserting status 200.
func (c *client) getJSON(path string) map[string]any {
	c.env.t.Helper()
	rec := c.do(http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		c.env.t.Fatalf("GET %s status = %d, want 200 (body: %s)", path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		c.env.t.Fatalf("GET %s response not JSON: %v", path, err)
	}
	return out
}

// fixture seeds a deterministic data set relative to the current month.
//
// Accounts: checking and savings (USD, included), eur (EUR, included →
// informational), hidden (USD, excluded from net worth), old (USD, archived).
//
// Current-month USD ledger (all dated the 1st): income +200000; groceries
// expenses -2500/-1000 and refund +500; rent -100000; dining -2000; a -3000
// split parent (groceries -2000, dining -1000); a -5000 transfer pair
// checking→savings; a soft-deleted -700; plus -900 on the EUR account and
// -100 on the excluded account (both uncategorized). Previous month: -4000
// groceries.
//
// Budget: current-month period, planned income 200000, allocations groceries
// 40000, rent 100000 (fully spent → approaching), dining 1000 (overspent).
// Recurring: Internet due in 3 days, Insurance due in 40 days. One goal with
// a 10000 contribution.
type fixture struct {
	checkingID  uuid.UUID
	savingsID   uuid.UUID
	eurID       uuid.UUID
	hiddenID    uuid.UUID
	archivedID  uuid.UUID
	groceriesID uuid.UUID
	rentID      uuid.UUID
	diningID    uuid.UUID
	goalID      uuid.UUID
	softDeleted uuid.UUID
	today       time.Time
	monthStart  time.Time
}

func (e *testEnv) seed(c *client) fixture {
	e.t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	fx := fixture{
		today:      time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
		monthStart: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC),
	}

	mkAccount := func(name, currency string, opening int64, include bool) db.Account {
		acc, err := e.q.CreateAccount(ctx, db.CreateAccountParams{
			UserID: c.userID, Name: name, Type: "checking", Currency: currency,
			OpeningBalance: opening, IncludeInNetWorth: include,
		})
		if err != nil {
			e.t.Fatalf("create account %s: %v", name, err)
		}
		return acc
	}
	fx.checkingID = mkAccount("Checking", "USD", 100000, true).ID
	fx.savingsID = mkAccount("Savings", "USD", 50000, true).ID
	fx.eurID = mkAccount("Euro", "EUR", 30000, true).ID
	fx.hiddenID = mkAccount("Hidden", "USD", 99999, false).ID
	fx.archivedID = mkAccount("Old", "USD", 77777, true).ID
	archivedAt := fx.today
	if _, err := e.q.SetAccountArchived(ctx, db.SetAccountArchivedParams{
		ID: fx.archivedID, UserID: c.userID, ArchivedAt: &archivedAt,
	}); err != nil {
		e.t.Fatalf("archive account: %v", err)
	}

	grp, err := e.q.CreateCategoryGroup(ctx, db.CreateCategoryGroupParams{UserID: c.userID, Name: "Essentials"})
	if err != nil {
		e.t.Fatalf("create group: %v", err)
	}
	mkCategory := func(name string) uuid.UUID {
		cat, err := e.q.CreateCategory(ctx, db.CreateCategoryParams{
			UserID: c.userID, GroupID: grp.ID, Name: name, BudgetType: "variable", RolloverRule: "none",
		})
		if err != nil {
			e.t.Fatalf("create category %s: %v", name, err)
		}
		return cat.ID
	}
	fx.groceriesID = mkCategory("Groceries")
	fx.rentID = mkCategory("Rent")
	fx.diningID = mkCategory("Dining")
	utilitiesID := mkCategory("Utilities")

	period, err := e.q.CreateBudgetPeriod(ctx, db.CreateBudgetPeriodParams{
		UserID: c.userID, Year: int32(fx.today.Year()), Month: int32(fx.today.Month()),
		Currency: "USD", PlannedIncome: 200000,
	})
	if err != nil {
		e.t.Fatalf("create period: %v", err)
	}
	for _, alloc := range []struct {
		cat    uuid.UUID
		amount int64
	}{{fx.groceriesID, 40000}, {fx.rentID, 100000}, {fx.diningID, 1000}} {
		if _, err := e.q.UpsertBudgetAllocation(ctx, db.UpsertBudgetAllocationParams{
			UserID: c.userID, PeriodID: period.ID, CategoryID: alloc.cat, Amount: alloc.amount,
		}); err != nil {
			e.t.Fatalf("create allocation: %v", err)
		}
	}

	mkTx := func(accID uuid.UUID, catID *uuid.UUID, typ string, amount int64, date time.Time, payee string, pair *uuid.UUID) db.Transaction {
		tx, err := e.q.CreateTransaction(ctx, db.CreateTransactionParams{
			UserID: c.userID, AccountID: accID, CategoryID: catID, Type: typ, Status: "cleared",
			Amount: amount, Date: date, Payee: payee, TransferPairID: pair,
		})
		if err != nil {
			e.t.Fatalf("create transaction %s: %v", payee, err)
		}
		return tx
	}
	day1 := fx.monthStart
	mkTx(fx.checkingID, nil, "income", 200000, day1, "Employer", nil)
	mkTx(fx.checkingID, &fx.groceriesID, "expense", -2500, day1, "Silpo", nil)
	mkTx(fx.checkingID, &fx.groceriesID, "expense", -1000, day1, "Silpo", nil)
	mkTx(fx.checkingID, &fx.groceriesID, "refund", 500, day1, "Silpo", nil)
	mkTx(fx.checkingID, &fx.rentID, "expense", -100000, day1, "Landlord", nil)
	mkTx(fx.checkingID, &fx.diningID, "expense", -2000, day1, "Cafe", nil)

	split := mkTx(fx.checkingID, nil, "expense", -3000, day1, "Market", nil)
	for _, sp := range []struct {
		cat    uuid.UUID
		amount int64
	}{{fx.groceriesID, -2000}, {fx.diningID, -1000}} {
		if _, err := e.q.CreateTransactionSplit(ctx, db.CreateTransactionSplitParams{
			UserID: c.userID, TransactionID: split.ID, CategoryID: sp.cat, Amount: sp.amount,
		}); err != nil {
			e.t.Fatalf("create split: %v", err)
		}
	}

	out := mkTx(fx.checkingID, nil, "transfer", -5000, day1, "Transfer to Savings", nil)
	mkTx(fx.savingsID, nil, "transfer", 5000, day1, "Transfer from Checking", &out.ID)

	mkTx(fx.eurID, nil, "expense", -900, day1, "Berlin Cafe", nil)
	mkTx(fx.hiddenID, nil, "expense", -100, day1, "Kiosk", nil)
	mkTx(fx.checkingID, &fx.groceriesID, "expense", -4000, fx.monthStart.AddDate(0, -1, 0), "LastMonth", nil)

	deleted := mkTx(fx.checkingID, &fx.groceriesID, "expense", -700, day1, "Mistake", nil)
	if err := e.q.SoftDeleteTransaction(ctx, db.SoftDeleteTransactionParams{ID: deleted.ID, UserID: c.userID}); err != nil {
		e.t.Fatalf("soft delete transaction: %v", err)
	}
	fx.softDeleted = deleted.ID

	mkRule := func(name string, amount int64, due time.Time) {
		if _, err := e.q.CreateRecurringRule(ctx, db.CreateRecurringRuleParams{
			UserID: c.userID, Name: name, AccountID: fx.checkingID, CategoryID: utilitiesID,
			Amount: amount, Frequency: "monthly", NextDueDate: due, AnchorDate: due, ReminderLeadDays: 3,
		}); err != nil {
			e.t.Fatalf("create recurring rule %s: %v", name, err)
		}
	}
	mkRule("Internet", 4500, fx.today.AddDate(0, 0, 3))
	mkRule("Insurance", 9900, fx.today.AddDate(0, 0, 40))

	targetDate := fx.today.AddDate(1, 0, 0)
	goal, err := e.q.CreateGoal(ctx, db.CreateGoalParams{
		UserID: c.userID, Name: "Vacation", Type: "savings", TargetAmount: 120000, TargetDate: &targetDate,
	})
	if err != nil {
		e.t.Fatalf("create goal: %v", err)
	}
	fx.goalID = goal.ID
	if _, err := e.q.CreateGoalContribution(ctx, db.CreateGoalContributionParams{
		UserID: c.userID, GoalID: goal.ID, Amount: 10000, ContributedOn: day1,
	}); err != nil {
		e.t.Fatalf("create contribution: %v", err)
	}
	return fx
}

func num(t *testing.T, m map[string]any, key string) int64 {
	t.Helper()
	f, ok := m[key].(float64)
	if !ok {
		t.Fatalf("field %q = %v (%T), want number", key, m[key], m[key])
	}
	return int64(f)
}

func list(t *testing.T, m map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := m[key].([]any)
	if !ok {
		t.Fatalf("field %q = %v (%T), want array", key, m[key], m[key])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("field %q contains non-object %v", key, item)
		}
		out = append(out, obj)
	}
	return out
}

func findBy(rows []map[string]any, key, value string) map[string]any {
	for _, r := range rows {
		if r[key] == value {
			return r
		}
	}
	return nil
}

func TestDashboard(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("dashboard@example.com")
	fx := env.seed(c)

	d := c.getJSON("/api/dashboard")

	if d["currency"] != "USD" {
		t.Errorf("currency = %v, want USD", d["currency"])
	}
	// Included USD accounts only: checking 183000 + savings 55000. The
	// excluded, archived, and EUR accounts must not enter this sum.
	if got := num(t, d, "availableBalance"); got != 238000 {
		t.Errorf("availableBalance = %d, want 238000", got)
	}
	foreign := list(t, d, "foreignBalances")
	if len(foreign) != 1 || foreign[0]["currency"] != "EUR" || num(t, foreign[0], "balance") != 29100 {
		t.Errorf("foreignBalances = %v, want one EUR balance of 29100", foreign)
	}

	// Transfers stay out of both totals; the refund reduces spending; the
	// EUR expense is out (currency), the excluded-account one is in.
	if got := num(t, d, "mtdIncome"); got != 200000 {
		t.Errorf("mtdIncome = %d, want 200000", got)
	}
	if got := num(t, d, "mtdSpending"); got != 108100 {
		t.Errorf("mtdSpending = %d, want 108100", got)
	}

	budget, _ := d["budget"].(map[string]any)
	if budget == nil || budget["exists"] != true {
		t.Fatalf("budget = %v, want existing budget", d["budget"])
	}
	if got := num(t, budget, "plannedIncome"); got != 200000 {
		t.Errorf("budget.plannedIncome = %d, want 200000", got)
	}
	if got := num(t, budget, "unallocated"); got != 59000 {
		t.Errorf("budget.unallocated = %d, want 59000", got)
	}
	if got := num(t, budget, "totalBudgeted"); got != 141000 {
		t.Errorf("budget.totalBudgeted = %d, want 141000", got)
	}
	if got := num(t, budget, "totalSpending"); got != 108000 {
		t.Errorf("budget.totalSpending = %d, want 108000", got)
	}
	if got := num(t, budget, "totalRemaining"); got != 33000 {
		t.Errorf("budget.totalRemaining = %d, want 33000", got)
	}
	if budget["health"] != "over_budget" {
		t.Errorf("budget.health = %v, want over_budget (dining is overspent)", budget["health"])
	}

	atRisk := list(t, d, "categoriesAtRisk")
	if len(atRisk) != 2 {
		t.Fatalf("categoriesAtRisk has %d entries, want 2 (rent, dining): %v", len(atRisk), atRisk)
	}
	rent := findBy(atRisk, "categoryId", fx.rentID.String())
	if rent == nil || rent["status"] != "approaching_limit" || num(t, rent, "remaining") != 0 {
		t.Errorf("rent at-risk entry = %v, want approaching_limit with remaining 0", rent)
	}
	dining := findBy(atRisk, "categoryId", fx.diningID.String())
	if dining == nil || dining["status"] != "over_budget" || num(t, dining, "remaining") != -2000 || dining["name"] != "Dining" {
		t.Errorf("dining at-risk entry = %v, want over_budget with remaining -2000", dining)
	}

	bills := list(t, d, "upcomingBills")
	if len(bills) != 1 {
		t.Fatalf("upcomingBills has %d entries, want 1 (Insurance is 40 days out): %v", len(bills), bills)
	}
	if bills[0]["name"] != "Internet" || num(t, bills[0], "amount") != 4500 || num(t, bills[0], "daysUntilDue") != 3 {
		t.Errorf("upcoming bill = %v, want Internet 4500 due in 3 days", bills[0])
	}

	recent := list(t, d, "recentTransactions")
	if len(recent) != 10 {
		t.Errorf("recentTransactions has %d entries, want 10 (11 live transactions capped)", len(recent))
	}
	if findBy(recent, "id", fx.softDeleted.String()) != nil {
		t.Error("soft-deleted transaction appears in recentTransactions")
	}

	goalRows := list(t, d, "goals")
	if len(goalRows) != 1 || goalRows[0]["id"] != fx.goalID.String() ||
		num(t, goalRows[0], "currentBalance") != 10000 || num(t, goalRows[0], "amountRemaining") != 110000 {
		t.Errorf("goals = %v, want Vacation with balance 10000 and 110000 remaining", goalRows)
	}
}

func TestDashboardEmptyUser(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("dashboard-empty@example.com")

	d := c.getJSON("/api/dashboard")
	if got := num(t, d, "availableBalance"); got != 0 {
		t.Errorf("availableBalance = %d, want 0", got)
	}
	budget, _ := d["budget"].(map[string]any)
	if budget == nil || budget["exists"] != false || budget["health"] != "no_budget" {
		t.Errorf("budget = %v, want exists=false with health no_budget", d["budget"])
	}
	for _, key := range []string{"foreignBalances", "categoriesAtRisk", "upcomingBills", "recentTransactions", "goals"} {
		if rows := list(t, d, key); len(rows) != 0 {
			t.Errorf("%s = %v, want empty", key, rows)
		}
	}
}

func TestSpendingByCategory(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("report-spending@example.com")
	fx := env.seed(c)

	res := c.getJSON("/api/reports/spending-by-category")
	if got := num(t, res, "totalSpending"); got != 108000 {
		t.Errorf("totalSpending = %d, want 108000", got)
	}
	cats := list(t, res, "categories")
	if len(cats) != 3 {
		t.Fatalf("categories has %d entries, want 3: %v", len(cats), cats)
	}
	// Ordered most-spent first; split lines are attributed to their own
	// categories; the refund reduces groceries.
	wantOrder := []struct {
		id       uuid.UUID
		name     string
		spending int64
	}{{fx.rentID, "Rent", 100000}, {fx.groceriesID, "Groceries", 5000}, {fx.diningID, "Dining", 3000}}
	for i, want := range wantOrder {
		got := cats[i]
		if got["categoryId"] != want.id.String() || got["name"] != want.name ||
			num(t, got, "spending") != want.spending || got["group"] != "Essentials" {
			t.Errorf("categories[%d] = %v, want %s spending %d", i, got, want.name, want.spending)
		}
	}

	// A range with no transactions yields an empty report, not an error.
	res = c.getJSON("/api/reports/spending-by-category?from=2000-01-01&to=2000-12-31")
	if got := num(t, res, "totalSpending"); got != 0 {
		t.Errorf("empty-range totalSpending = %d, want 0", got)
	}
	if cats := list(t, res, "categories"); len(cats) != 0 {
		t.Errorf("empty-range categories = %v, want none", cats)
	}
}

func TestIncomeVsExpensesAndTrend(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("report-income@example.com")
	env.seed(c)

	res := c.getJSON("/api/reports/income-vs-expenses?months=3")
	months := list(t, res, "months")
	if len(months) != 3 {
		t.Fatalf("months has %d entries, want 3 (zero-filled)", len(months))
	}
	if num(t, months[0], "income") != 0 || num(t, months[0], "expenses") != 0 {
		t.Errorf("months[0] = %v, want zero-filled month", months[0])
	}
	if num(t, months[1], "expenses") != 4000 || num(t, months[1], "income") != 0 || num(t, months[1], "net") != -4000 {
		t.Errorf("months[1] = %v, want prior-month expenses 4000", months[1])
	}
	cur := months[2]
	if num(t, cur, "income") != 200000 || num(t, cur, "expenses") != 108100 || num(t, cur, "net") != 91900 {
		t.Errorf("months[2] = %v, want income 200000 / expenses 108100 / net 91900", cur)
	}

	trend := c.getJSON("/api/reports/monthly-trend?months=2")
	tm := list(t, trend, "months")
	if len(tm) != 2 || num(t, tm[0], "spending") != 4000 || num(t, tm[1], "spending") != 108100 {
		t.Errorf("monthly trend = %v, want spending [4000, 108100]", tm)
	}
}

func TestCashFlow(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("report-cashflow@example.com")
	env.seed(c)

	res := c.getJSON("/api/reports/cash-flow?months=1")
	months := list(t, res, "months")
	if len(months) != 1 {
		t.Fatalf("months has %d entries, want 1", len(months))
	}
	// Cash flow counts every USD movement including both transfer legs
	// (which net out) and excludes the EUR expense and the deleted row.
	m := months[0]
	if num(t, m, "inflow") != 205500 || num(t, m, "outflow") != 113600 || num(t, m, "net") != 91900 {
		t.Errorf("cash flow = %v, want inflow 205500 / outflow 113600 / net 91900", m)
	}
}

func TestNetWorth(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("report-networth@example.com")
	fx := env.seed(c)

	res := c.getJSON("/api/reports/net-worth")
	if res["currency"] != "USD" {
		t.Errorf("currency = %v, want USD", res["currency"])
	}
	if got := num(t, res, "total"); got != 238000 {
		t.Errorf("total = %d, want 238000", got)
	}
	accounts := list(t, res, "accounts")
	if len(accounts) != 2 {
		t.Fatalf("accounts has %d entries, want 2 (excluded and archived filtered out): %v", len(accounts), accounts)
	}
	for _, id := range []uuid.UUID{fx.hiddenID, fx.archivedID} {
		if findBy(accounts, "accountId", id.String()) != nil {
			t.Errorf("account %s must not appear in net worth", id)
		}
	}
	checking := findBy(accounts, "accountId", fx.checkingID.String())
	if checking == nil || num(t, checking, "balance") != 183000 {
		t.Errorf("checking = %v, want balance 183000", checking)
	}

	foreign := list(t, res, "foreignBalances")
	if len(foreign) != 1 || num(t, foreign[0], "balance") != 29100 {
		t.Errorf("foreignBalances = %v, want one EUR entry of 29100", foreign)
	}
	totals, _ := res["foreignTotals"].(map[string]any)
	if totals == nil || len(totals) != 1 || int64(totals["EUR"].(float64)) != 29100 {
		t.Errorf("foreignTotals = %v, want {EUR: 29100}", res["foreignTotals"])
	}
}

func TestTopPayees(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("report-payees@example.com")
	env.seed(c)

	res := c.getJSON("/api/reports/top-payees")
	payees := list(t, res, "payees")
	if len(payees) != 5 {
		t.Fatalf("payees has %d entries, want 5: %v", len(payees), payees)
	}
	if payees[0]["payee"] != "Landlord" || num(t, payees[0], "spending") != 100000 {
		t.Errorf("payees[0] = %v, want Landlord 100000", payees[0])
	}
	silpo := findBy(payees, "payee", "Silpo")
	if silpo == nil || num(t, silpo, "spending") != 3000 || num(t, silpo, "transactionCount") != 3 {
		t.Errorf("Silpo = %v, want net spending 3000 over 3 transactions (refund included)", silpo)
	}
	if findBy(payees, "payee", "Berlin Cafe") != nil {
		t.Error("EUR-account payee appears in a USD-scoped report")
	}
	if findBy(payees, "payee", "Employer") != nil {
		t.Error("income payee appears in the spending ranking")
	}

	res = c.getJSON("/api/reports/top-payees?limit=2")
	payees = list(t, res, "payees")
	if len(payees) != 2 || payees[0]["payee"] != "Landlord" || payees[1]["payee"] != "Market" {
		t.Errorf("limit=2 payees = %v, want [Landlord, Market]", payees)
	}
}

func TestReportValidationAndAuth(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("report-validation@example.com")

	for _, path := range []string{
		"/api/reports/income-vs-expenses?months=abc",
		"/api/reports/monthly-trend?months=0",
		"/api/reports/cash-flow?months=61",
		"/api/reports/spending-by-category?from=notadate",
		"/api/reports/top-payees?to=2026-13-99",
		"/api/reports/top-payees?limit=999",
		"/api/reports/spending-by-category?from=2026-02-01&to=2026-01-01",
	} {
		if rec := c.do(http.MethodGet, path, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want 400", path, rec.Code)
		}
	}

	anon := &client{env: env, cookies: map[string]*http.Cookie{}}
	for _, path := range []string{"/api/dashboard", "/api/reports/net-worth"} {
		if rec := anon.do(http.MethodGet, path, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s unauthenticated status = %d, want 401", path, rec.Code)
		}
	}
}
