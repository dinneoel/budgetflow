// Integration tests for the export API: real HTTP router against a real
// PostgreSQL database, verifying export → parse → matches-DB round trips.
package exporter_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgetflow/internal/auth"
	"budgetflow/internal/budgets"
	"budgetflow/internal/config"
	"budgetflow/internal/db"
	"budgetflow/internal/exporter"
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

// fixture is a seeded data set: one account, two categories, a budget month
// with allocations, four transactions (one soft-deleted, one tagged), and a
// goal with a contribution.
type fixture struct {
	accountID    uuid.UUID
	groceriesID  uuid.UUID
	rentID       uuid.UUID
	periodID     uuid.UUID
	goalID       uuid.UUID
	softDeleted  uuid.UUID
	transactions []uuid.UUID
}

func date(s string) time.Time {
	d, _ := time.Parse("2006-01-02", s)
	return d
}

func (e *testEnv) seed(c *client) fixture {
	e.t.Helper()
	ctx := context.Background()
	var fx fixture

	acc, err := e.q.CreateAccount(ctx, db.CreateAccountParams{
		UserID: c.userID, Name: "Checking", Type: "checking", Currency: "USD", OpeningBalance: 10000, IncludeInNetWorth: true,
	})
	if err != nil {
		e.t.Fatalf("create account: %v", err)
	}
	fx.accountID = acc.ID

	grp, err := e.q.CreateCategoryGroup(ctx, db.CreateCategoryGroupParams{UserID: c.userID, Name: "Essentials"})
	if err != nil {
		e.t.Fatalf("create group: %v", err)
	}
	groceries, err := e.q.CreateCategory(ctx, db.CreateCategoryParams{
		UserID: c.userID, GroupID: grp.ID, Name: "Groceries", BudgetType: "variable", RolloverRule: "none",
	})
	if err != nil {
		e.t.Fatalf("create category: %v", err)
	}
	rent, err := e.q.CreateCategory(ctx, db.CreateCategoryParams{
		UserID: c.userID, GroupID: grp.ID, Name: "Rent", BudgetType: "fixed", RolloverRule: "none", SortOrder: 1,
	})
	if err != nil {
		e.t.Fatalf("create category: %v", err)
	}
	fx.groceriesID, fx.rentID = groceries.ID, rent.ID

	period, err := e.q.CreateBudgetPeriod(ctx, db.CreateBudgetPeriodParams{
		UserID: c.userID, Year: 2026, Month: 9, Currency: "USD", PlannedIncome: 500000,
	})
	if err != nil {
		e.t.Fatalf("create period: %v", err)
	}
	fx.periodID = period.ID
	for cat, amount := range map[uuid.UUID]int64{groceries.ID: 40000, rent.ID: 100000} {
		if _, err := e.q.UpsertBudgetAllocation(ctx, db.UpsertBudgetAllocationParams{
			UserID: c.userID, PeriodID: period.ID, CategoryID: cat, Amount: amount,
		}); err != nil {
			e.t.Fatalf("create allocation: %v", err)
		}
	}

	mkTx := func(catID *uuid.UUID, typ string, amount int64, day, payee string) db.Transaction {
		tx, err := e.q.CreateTransaction(ctx, db.CreateTransactionParams{
			UserID: c.userID, AccountID: acc.ID, CategoryID: catID,
			Type: typ, Status: "cleared", Amount: amount, Date: date(day), Payee: payee,
		})
		if err != nil {
			e.t.Fatalf("create transaction: %v", err)
		}
		fx.transactions = append(fx.transactions, tx.ID)
		return tx
	}
	mkTx(nil, "income", 100000, "2026-09-01", "Employer")
	groceriesTx := mkTx(&groceries.ID, "expense", -2500, "2026-09-02", "Silpo")
	mkTx(&rent.ID, "expense", -100000, "2026-09-03", "Landlord")
	deleted := mkTx(&groceries.ID, "expense", -700, "2026-09-04", "Mistake")
	if err := e.q.SoftDeleteTransaction(ctx, db.SoftDeleteTransactionParams{ID: deleted.ID, UserID: c.userID}); err != nil {
		e.t.Fatalf("soft delete transaction: %v", err)
	}
	fx.softDeleted = deleted.ID

	tag, err := e.q.CreateTag(ctx, db.CreateTagParams{UserID: c.userID, Name: "weekly"})
	if err != nil {
		e.t.Fatalf("create tag: %v", err)
	}
	if err := e.q.TagTransaction(ctx, db.TagTransactionParams{TransactionID: groceriesTx.ID, TagID: tag.ID, UserID: c.userID}); err != nil {
		e.t.Fatalf("tag transaction: %v", err)
	}

	targetDate := date("2027-09-01")
	goal, err := e.q.CreateGoal(ctx, db.CreateGoalParams{
		UserID: c.userID, Name: "Vacation", Type: "savings", TargetAmount: 120000, TargetDate: &targetDate,
	})
	if err != nil {
		e.t.Fatalf("create goal: %v", err)
	}
	fx.goalID = goal.ID
	if _, err := e.q.CreateGoalContribution(ctx, db.CreateGoalContributionParams{
		UserID: c.userID, GoalID: goal.ID, Amount: 10000, ContributedOn: date("2026-09-01"),
	}); err != nil {
		e.t.Fatalf("create contribution: %v", err)
	}
	return fx
}

// parseCSV parses a CSV body into a header slice and one map per data row.
func parseCSV(t *testing.T, data []byte) ([]string, []map[string]string) {
	t.Helper()
	records, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("export is not valid CSV: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("export CSV has no header row")
	}
	header := records[0]
	rows := make([]map[string]string, 0, len(records)-1)
	for _, rec := range records[1:] {
		row := map[string]string{}
		for i, col := range header {
			row[col] = rec[i]
		}
		rows = append(rows, row)
	}
	return header, rows
}

func (e *testEnv) countAuditEvents(userID uuid.UUID, eventType, exportKind string) int {
	e.t.Helper()
	var n int
	err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE user_id = $1 AND event_type = $2 AND payload->>'export' = $3`,
		userID, eventType, exportKind).Scan(&n)
	if err != nil {
		e.t.Fatalf("count audit events: %v", err)
	}
	return n
}

func TestTransactionsCSVExport(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("export-tx@example.com")
	fx := env.seed(c)

	rec := c.do(http.MethodGet, "/api/export/transactions.csv", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}

	_, rows := parseCSV(t, rec.Body.Bytes())
	if len(rows) != 3 {
		t.Fatalf("exported %d rows, want 3 (soft-deleted row must be excluded)", len(rows))
	}
	byID := map[string]map[string]string{}
	for _, r := range rows {
		byID[r["id"]] = r
		if r["id"] == fx.softDeleted.String() {
			t.Error("soft-deleted transaction appears in export")
		}
	}
	groceries := byID[fx.transactions[1].String()]
	if groceries == nil {
		t.Fatal("groceries transaction missing from export")
	}
	if groceries["amount_minor"] != "-2500" || groceries["payee"] != "Silpo" ||
		groceries["category"] != "Groceries" || groceries["account"] != "Checking" ||
		groceries["date"] != "2026-09-02" || groceries["tags"] != "weekly" {
		t.Errorf("groceries row exported wrong: %v", groceries)
	}

	// The same filters as the list endpoint apply to the export.
	rec = c.do(http.MethodGet, "/api/export/transactions.csv?type=expense", nil)
	_, rows = parseCSV(t, rec.Body.Bytes())
	if len(rows) != 2 {
		t.Fatalf("type=expense exported %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if r["type"] != "expense" {
			t.Errorf("filtered export contains non-expense row: %v", r)
		}
	}

	if n := env.countAuditEvents(c.userID, exporter.EventDataExported, "transactions"); n != 2 {
		t.Errorf("recorded %d transactions-export audit events, want 2", n)
	}

	rec = c.do(http.MethodGet, "/api/export/transactions.csv?amountMin=abc", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad filter status = %d, want 400", rec.Code)
	}
}

func TestBudgetCSVExport(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("export-budget@example.com")
	fx := env.seed(c)

	rec := c.do(http.MethodGet, "/api/export/budget.csv?year=2026&month=9", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	_, rows := parseCSV(t, rec.Body.Bytes())
	if len(rows) != 2 {
		t.Fatalf("exported %d rows, want 2 allocated categories", len(rows))
	}

	// The CSV must agree with the budget service's own math.
	detail, err := budgets.NewService(env.pool).Get(context.Background(), c.userID, 2026, 9)
	if err != nil {
		t.Fatalf("load budget detail: %v", err)
	}
	wantByCat := map[string]budgetRow{}
	for _, cat := range detail.Categories {
		wantByCat[cat.CategoryID.String()] = budgetRow{
			budgeted: cat.Amount, activity: cat.Spending, remaining: cat.Remaining, status: string(cat.Status),
		}
	}
	for _, r := range rows {
		want, ok := wantByCat[r["category_id"]]
		if !ok {
			t.Errorf("unexpected category row %v", r)
			continue
		}
		if r["budgeted_minor"] != strconv.FormatInt(want.budgeted, 10) ||
			r["activity_minor"] != strconv.FormatInt(want.activity, 10) ||
			r["remaining_minor"] != strconv.FormatInt(want.remaining, 10) ||
			r["status"] != want.status {
			t.Errorf("budget row %v disagrees with service (want %+v)", r, want)
		}
		if r["planned_income_minor"] != "500000" ||
			r["unallocated_minor"] != strconv.FormatInt(detail.Unallocated, 10) {
			t.Errorf("period figures wrong in row %v (want unallocated %d)", r, detail.Unallocated)
		}
	}
	grocery := findRow(rows, "category_id", fx.groceriesID.String())
	if grocery == nil || grocery["activity_minor"] != "2500" || grocery["remaining_minor"] != "37500" {
		t.Errorf("groceries row = %v, want activity 2500 and remaining 37500", grocery)
	}

	rec = c.do(http.MethodGet, "/api/export/budget.csv?year=2031&month=1", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing period status = %d, want 404", rec.Code)
	}
	rec = c.do(http.MethodGet, "/api/export/budget.csv", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing params status = %d, want 400", rec.Code)
	}

	if n := env.countAuditEvents(c.userID, exporter.EventDataExported, "budget"); n != 1 {
		t.Errorf("recorded %d budget-export audit events, want 1", n)
	}
}

type budgetRow struct {
	budgeted, activity, remaining int64
	status                        string
}

func findRow(rows []map[string]string, key, value string) map[string]string {
	for _, r := range rows {
		if r[key] == value {
			return r
		}
	}
	return nil
}

func TestCategoriesCSVExport(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("export-cats@example.com")
	env.seed(c)

	rec := c.do(http.MethodGet, "/api/export/categories.csv", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	_, rows := parseCSV(t, rec.Body.Bytes())
	if len(rows) != 2 {
		t.Fatalf("exported %d rows, want 2", len(rows))
	}
	groceries := findRow(rows, "name", "Groceries")
	if groceries == nil || groceries["group"] != "Essentials" || groceries["budget_type"] != "variable" ||
		groceries["rollover_rule"] != "none" || groceries["archived"] != "false" {
		t.Errorf("groceries category exported wrong: %v", groceries)
	}
	if n := env.countAuditEvents(c.userID, exporter.EventDataExported, "categories"); n != 1 {
		t.Errorf("recorded %d categories-export audit events, want 1", n)
	}
}

func TestGoalsCSVExport(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("export-goals@example.com")
	fx := env.seed(c)

	rec := c.do(http.MethodGet, "/api/export/goals.csv", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	_, rows := parseCSV(t, rec.Body.Bytes())
	if len(rows) != 1 {
		t.Fatalf("exported %d rows, want 1", len(rows))
	}
	g := rows[0]
	if g["id"] != fx.goalID.String() || g["name"] != "Vacation" ||
		g["target_amount_minor"] != "120000" || g["target_date"] != "2027-09-01" ||
		g["current_balance_minor"] != "10000" || g["amount_remaining_minor"] != "110000" {
		t.Errorf("goal exported wrong: %v", g)
	}
	if n := env.countAuditEvents(c.userID, exporter.EventDataExported, "goals"); n != 1 {
		t.Errorf("recorded %d goals-export audit events, want 1", n)
	}
}

func TestFullExportZIP(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("export-full@example.com")
	fx := env.seed(c)

	rec := c.do(http.MethodGet, "/api/export/all.zip", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("export is not a valid ZIP: %v", err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		files[f.Name] = data
	}

	want := []string{
		"profile.csv", "accounts.csv", "category_groups.csv", "categories.csv",
		"budget_periods.csv", "budget_allocations.csv", "transactions.csv",
		"transaction_splits.csv", "tags.csv", "transaction_tags.csv",
		"recurring_rules.csv", "goals.csv", "goal_contributions.csv",
		"import_batches.csv", "notifications.csv", "audit_events.csv",
	}
	for _, name := range want {
		if _, ok := files[name]; !ok {
			t.Errorf("ZIP is missing %s", name)
		}
	}

	_, profile := parseCSV(t, files["profile.csv"])
	if len(profile) != 1 || profile[0]["email"] != "export-full@example.com" {
		t.Errorf("profile.csv wrong: %v", profile)
	}

	// The full export includes soft-deleted transactions: all 4 rows.
	_, txRows := parseCSV(t, files["transactions.csv"])
	if len(txRows) != 4 {
		t.Fatalf("transactions.csv has %d rows, want 4 (including soft-deleted)", len(txRows))
	}
	deletedRow := findRow(txRows, "id", fx.softDeleted.String())
	if deletedRow == nil || deletedRow["deleted_at"] == "" {
		t.Errorf("soft-deleted transaction missing or lacks deleted_at: %v", deletedRow)
	}

	_, allocRows := parseCSV(t, files["budget_allocations.csv"])
	if len(allocRows) != 2 {
		t.Errorf("budget_allocations.csv has %d rows, want 2", len(allocRows))
	}
	_, contribRows := parseCSV(t, files["goal_contributions.csv"])
	if len(contribRows) != 1 || contribRows[0]["amount_minor"] != "10000" {
		t.Errorf("goal_contributions.csv wrong: %v", contribRows)
	}
	_, tagRows := parseCSV(t, files["transaction_tags.csv"])
	if len(tagRows) != 1 || tagRows[0]["tag"] != "weekly" {
		t.Errorf("transaction_tags.csv wrong: %v", tagRows)
	}

	if n := env.countAuditEvents(c.userID, exporter.EventDataExported, "full"); n != 1 {
		t.Errorf("recorded %d full-export audit events, want 1", n)
	}
}

func TestExportRequiresAuth(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	anon := &client{env: env, cookies: map[string]*http.Cookie{}}
	for _, path := range []string{"/api/export/transactions.csv", "/api/export/all.zip"} {
		rec := anon.do(http.MethodGet, path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s unauthenticated status = %d, want 401", path, rec.Code)
		}
	}
}
