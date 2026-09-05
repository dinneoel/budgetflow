// Integration tests for the categories API: real HTTP router (handlers +
// auth/CSRF middleware) against a real PostgreSQL database per test.
package categories_test

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
	"budgetflow/internal/categories"
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

func parseID(t *testing.T, m map[string]any) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(m["id"].(string))
	if err != nil {
		t.Fatalf("bad id %v: %v", m["id"], err)
	}
	return id
}

func (c *client) createGroup(t *testing.T, name string) map[string]any {
	t.Helper()
	rec := c.do(http.MethodPost, "/api/category-groups", map[string]any{"name": name})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(t, rec)["group"].(map[string]any)
}

func (c *client) createCategory(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	rec := c.do(http.MethodPost, "/api/categories", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create category status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(t, rec)["category"].(map[string]any)
}

func (c *client) listGroups(t *testing.T) []any {
	t.Helper()
	rec := c.do(http.MethodGet, "/api/categories", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list categories status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(t, rec)["groups"].([]any)
}

// addAccount and addExpense build fixtures directly through the query layer
// (the transactions API is a later task).
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

func (e *testEnv) addExpense(userID, accountID uuid.UUID, categoryID *uuid.UUID, amount int64) db.Transaction {
	e.t.Helper()
	tx, err := e.q.CreateTransaction(context.Background(), db.CreateTransactionParams{
		UserID: userID, AccountID: accountID, CategoryID: categoryID, Type: "expense",
		Status: "cleared", Amount: amount, Date: time.Now().UTC(), Payee: "fixture payee",
	})
	if err != nil {
		e.t.Fatalf("insert expense: %v", err)
	}
	return tx
}

func TestGroupAndCategoryCRUD(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("crud@example.com")

	g := c.createGroup(t, "Bills")
	gID := parseID(t, g)

	cat := c.createCategory(t, map[string]any{
		"groupId": gID, "name": "Rent", "icon": "home", "color": "#0ea5e9",
		"budgetType": "fixed", "rolloverRule": "none",
	})
	if cat["budgetType"] != "fixed" || cat["icon"] != "home" || cat["color"] != "#0ea5e9" {
		t.Errorf("created category = %v", cat)
	}

	// Defaults apply when type/rule are omitted.
	cat2 := c.createCategory(t, map[string]any{"groupId": gID, "name": "Misc"})
	if cat2["budgetType"] != "variable" || cat2["rolloverRule"] != "none" {
		t.Errorf("defaulted category = %v, want variable/none", cat2)
	}

	// Validation failures.
	for name, body := range map[string]map[string]any{
		"empty name":    {"groupId": gID, "name": "  "},
		"bad type":      {"groupId": gID, "name": "X", "budgetType": "yolo"},
		"bad rollover":  {"groupId": gID, "name": "X", "rolloverRule": "sometimes"},
		"missing group": {"name": "X"},
	} {
		if rec := c.do(http.MethodPost, "/api/categories", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body: %s)", name, rec.Code, rec.Body.String())
		}
	}
	// Unknown group is a 404.
	if rec := c.do(http.MethodPost, "/api/categories", map[string]any{"groupId": uuid.NewString(), "name": "X"}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown group: status = %d, want 404", rec.Code)
	}

	// Update: rename, change type/rule, move to another group.
	g2 := c.createGroup(t, "Housing")
	catID := parseID(t, cat)
	rec := c.do(http.MethodPut, "/api/categories/"+catID.String(), map[string]any{
		"groupId": parseID(t, g2), "name": "Mortgage", "icon": "bank", "color": "#111111",
		"budgetType": "debt", "rolloverRule": "reset_to_target",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update category status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	updated := decode(t, rec)["category"].(map[string]any)
	if updated["name"] != "Mortgage" || updated["budgetType"] != "debt" ||
		updated["rolloverRule"] != "reset_to_target" || updated["groupId"] != g2["id"] {
		t.Errorf("updated category = %v", updated)
	}

	// Rename group.
	rec = c.do(http.MethodPut, "/api/category-groups/"+gID.String(), map[string]any{"name": "Monthly Bills"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update group status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := decode(t, rec)["group"].(map[string]any)["name"]; got != "Monthly Bills" {
		t.Errorf("group name = %v, want Monthly Bills", got)
	}

	// List returns the tree with categories nested under their groups.
	groups := c.listGroups(t)
	if len(groups) != 2 {
		t.Fatalf("list has %d groups, want 2", len(groups))
	}
	first := groups[0].(map[string]any)
	if first["name"] != "Monthly Bills" {
		t.Errorf("first group = %v, want Monthly Bills first (sort order)", first["name"])
	}
	if cats := first["categories"].([]any); len(cats) != 1 { // Misc stayed; Mortgage moved to Housing
		t.Errorf("Monthly Bills has %d categories, want 1", len(cats))
	}

	// Deleting an unused category works; deleting a non-empty group is refused
	// until its categories are gone.
	if rec := c.do(http.MethodDelete, "/api/category-groups/"+g2["id"].(string), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("delete non-empty group: status = %d, want 400", rec.Code)
	}
	if rec := c.do(http.MethodDelete, "/api/categories/"+catID.String(), nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete category: status = %d, want 204 (body: %s)", rec.Code, rec.Body.String())
	}
	if rec := c.do(http.MethodDelete, "/api/category-groups/"+g2["id"].(string), nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete emptied group: status = %d, want 204 (body: %s)", rec.Code, rec.Body.String())
	}

	// A category referenced by a transaction cannot be deleted.
	accID := env.addAccount(c.userID)
	miscID := parseID(t, cat2)
	env.addExpense(c.userID, accID, &miscID, -1_000)
	if rec := c.do(http.MethodDelete, "/api/categories/"+miscID.String(), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("delete used category: status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestReorderPersistence(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("reorder@example.com")

	gA, gB, gC := c.createGroup(t, "A"), c.createGroup(t, "B"), c.createGroup(t, "C")
	gID := parseID(t, gA)
	var catIDs []string
	for _, name := range []string{"one", "two", "three"} {
		catIDs = append(catIDs, c.createCategory(t, map[string]any{"groupId": gID, "name": name})["id"].(string))
	}

	// Reverse the group order and the category order.
	rec := c.do(http.MethodPut, "/api/category-groups/reorder", map[string]any{
		"ids": []string{gC["id"].(string), gB["id"].(string), gA["id"].(string)},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reorder groups status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	rec = c.do(http.MethodPut, "/api/categories/reorder", map[string]any{
		"ids": []string{catIDs[2], catIDs[0], catIDs[1]},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reorder categories status = %d (body: %s)", rec.Code, rec.Body.String())
	}

	// The new order is persisted and visible on a fresh list.
	groups := c.listGroups(t)
	gotGroups := []string{}
	for _, g := range groups {
		gotGroups = append(gotGroups, g.(map[string]any)["name"].(string))
	}
	if gotGroups[0] != "C" || gotGroups[1] != "B" || gotGroups[2] != "A" {
		t.Errorf("group order = %v, want [C B A]", gotGroups)
	}
	last := groups[2].(map[string]any)["categories"].([]any)
	gotCats := []string{}
	for _, cc := range last {
		gotCats = append(gotCats, cc.(map[string]any)["name"].(string))
	}
	if len(gotCats) != 3 || gotCats[0] != "three" || gotCats[1] != "one" || gotCats[2] != "two" {
		t.Errorf("category order = %v, want [three one two]", gotCats)
	}

	// Unknown or foreign ids fail the whole reorder and change nothing.
	rec = c.do(http.MethodPut, "/api/category-groups/reorder", map[string]any{
		"ids": []string{gA["id"].(string), uuid.NewString()},
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("reorder with unknown id: status = %d, want 404", rec.Code)
	}
	if got := c.listGroups(t)[0].(map[string]any)["name"]; got != "C" {
		t.Errorf("order changed after failed reorder: first = %v, want C", got)
	}
	if rec := c.do(http.MethodPut, "/api/categories/reorder", map[string]any{"ids": []string{}}); rec.Code != http.StatusBadRequest {
		t.Errorf("empty reorder: status = %d, want 400", rec.Code)
	}
}

func TestMergePreservesTotals(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	ctx := context.Background()
	c := env.signUp("merge@example.com")

	gID := parseID(t, c.createGroup(t, "Food"))
	source := parseID(t, c.createCategory(t, map[string]any{"groupId": gID, "name": "Takeout"}))
	target := parseID(t, c.createCategory(t, map[string]any{"groupId": gID, "name": "Dining Out"}))

	accID := env.addAccount(c.userID)
	env.addExpense(c.userID, accID, &source, -4_000)
	env.addExpense(c.userID, accID, &source, -6_000)
	env.addExpense(c.userID, accID, &target, -10_000)

	// A split transaction pointing at the source category.
	splitParent := env.addExpense(c.userID, accID, nil, -3_000)
	if _, err := env.q.CreateTransactionSplit(ctx, db.CreateTransactionSplitParams{
		UserID: c.userID, TransactionID: splitParent.ID, CategoryID: source, Amount: -3_000,
	}); err != nil {
		t.Fatalf("create split: %v", err)
	}

	// Period 1: both categories funded (must be summed). Period 2: source only
	// (must be reassigned).
	p1, err := env.q.CreateBudgetPeriod(ctx, db.CreateBudgetPeriodParams{UserID: c.userID, Year: 2026, Month: 8, Currency: "USD"})
	if err != nil {
		t.Fatalf("create period 1: %v", err)
	}
	p2, err := env.q.CreateBudgetPeriod(ctx, db.CreateBudgetPeriodParams{UserID: c.userID, Year: 2026, Month: 9, Currency: "USD"})
	if err != nil {
		t.Fatalf("create period 2: %v", err)
	}
	for _, a := range []db.UpsertBudgetAllocationParams{
		{UserID: c.userID, PeriodID: p1.ID, CategoryID: source, Amount: 5_000, Rollover: 500},
		{UserID: c.userID, PeriodID: p1.ID, CategoryID: target, Amount: 7_000, Rollover: 200},
		{UserID: c.userID, PeriodID: p2.ID, CategoryID: source, Amount: 9_000},
	} {
		if _, err := env.q.UpsertBudgetAllocation(ctx, a); err != nil {
			t.Fatalf("seed allocation: %v", err)
		}
	}

	// Guard rails first: self-merge and unknown target.
	if rec := c.do(http.MethodPost, "/api/categories/"+source.String()+"/merge", map[string]any{"targetId": source}); rec.Code != http.StatusBadRequest {
		t.Errorf("self-merge: status = %d, want 400", rec.Code)
	}
	if rec := c.do(http.MethodPost, "/api/categories/"+source.String()+"/merge", map[string]any{"targetId": uuid.New()}); rec.Code != http.StatusNotFound {
		t.Errorf("merge into unknown target: status = %d, want 404", rec.Code)
	}

	rec := c.do(http.MethodPost, "/api/categories/"+source.String()+"/merge", map[string]any{"targetId": target})
	if rec.Code != http.StatusOK {
		t.Fatalf("merge status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	resp := decode(t, rec)
	if got := resp["transactionsMoved"].(float64); got != 2 {
		t.Errorf("transactionsMoved = %v, want 2", got)
	}
	if got := resp["splitsMoved"].(float64); got != 1 {
		t.Errorf("splitsMoved = %v, want 1", got)
	}

	// Nothing references the source category any more, and it is gone.
	for _, q := range []string{
		"SELECT count(*) FROM transactions WHERE category_id = $1",
		"SELECT count(*) FROM transaction_splits WHERE category_id = $1",
		"SELECT count(*) FROM budget_allocations WHERE category_id = $1",
		"SELECT count(*) FROM categories WHERE id = $1",
	} {
		var n int
		if err := env.pool.QueryRow(ctx, q, source).Scan(&n); err != nil {
			t.Fatalf("query %q: %v", q, err)
		}
		if n != 0 {
			t.Errorf("%q = %d, want 0", q, n)
		}
	}

	// Spending totals moved to the target: -4000 -6000 -10000 on transactions.
	var txTotal int64
	if err := env.pool.QueryRow(ctx,
		"SELECT coalesce(sum(amount), 0) FROM transactions WHERE category_id = $1", target).Scan(&txTotal); err != nil {
		t.Fatalf("sum target transactions: %v", err)
	}
	if txTotal != -20_000 {
		t.Errorf("target transaction total = %d, want -20000", txTotal)
	}

	// Period 1 allocation was summed (5000+7000, rollover 500+200); period 2
	// allocation was reassigned intact.
	var amount, rollover int64
	if err := env.pool.QueryRow(ctx,
		"SELECT amount, rollover FROM budget_allocations WHERE period_id = $1 AND category_id = $2",
		p1.ID, target).Scan(&amount, &rollover); err != nil {
		t.Fatalf("load combined allocation: %v", err)
	}
	if amount != 12_000 || rollover != 700 {
		t.Errorf("combined allocation = %d/%d, want 12000/700", amount, rollover)
	}
	if err := env.pool.QueryRow(ctx,
		"SELECT amount FROM budget_allocations WHERE period_id = $1 AND category_id = $2",
		p2.ID, target).Scan(&amount); err != nil {
		t.Fatalf("load reassigned allocation: %v", err)
	}
	if amount != 9_000 {
		t.Errorf("reassigned allocation = %d, want 9000", amount)
	}

	// The merge recorded one audit event naming both categories.
	var auditCount int
	if err := env.pool.QueryRow(ctx,
		"SELECT count(*) FROM audit_events WHERE event_type = 'categories_merged' AND entity_ids @> ARRAY[$1::uuid, $2::uuid]",
		source, target).Scan(&auditCount); err != nil {
		t.Fatalf("count merge audit events: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("merge audit events = %d, want 1", auditCount)
	}
}

func TestArchiveWithExistingTransactions(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	ctx := context.Background()
	c := env.signUp("archive@example.com")

	gID := parseID(t, c.createGroup(t, "Fun"))
	catID := parseID(t, c.createCategory(t, map[string]any{"groupId": gID, "name": "Games"}))
	accID := env.addAccount(c.userID)
	env.addExpense(c.userID, accID, &catID, -2_500)

	rec := c.do(http.MethodPost, "/api/categories/"+catID.String()+"/archive", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("archive status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if archived := decode(t, rec)["category"].(map[string]any); archived["archivedAt"] == nil {
		t.Error("archivedAt not set after archive")
	}

	// The transaction still points at the archived category.
	var n int
	if err := env.pool.QueryRow(ctx,
		"SELECT count(*) FROM transactions WHERE category_id = $1", catID).Scan(&n); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if n != 1 {
		t.Errorf("transactions after archive = %d, want 1", n)
	}

	// Archived categories still appear in the list (clients filter).
	groups := c.listGroups(t)
	cats := groups[0].(map[string]any)["categories"].([]any)
	if len(cats) != 1 || cats[0].(map[string]any)["archivedAt"] == nil {
		t.Errorf("archived category missing from list: %v", cats)
	}

	rec = c.do(http.MethodPost, "/api/categories/"+catID.String()+"/unarchive", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive status = %d", rec.Code)
	}
	if got := decode(t, rec)["category"].(map[string]any); got["archivedAt"] != nil {
		t.Errorf("archivedAt = %v after unarchive, want null", got["archivedAt"])
	}

	// Group archive works the same way.
	rec = c.do(http.MethodPost, "/api/category-groups/"+gID.String()+"/archive", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("archive group status = %d", rec.Code)
	}
	if g := decode(t, rec)["group"].(map[string]any); g["archivedAt"] == nil {
		t.Error("group archivedAt not set after archive")
	}
}

func TestSeedDefaults(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("seed@example.com")

	rec := c.do(http.MethodPost, "/api/categories/seed-defaults", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	groups := decode(t, rec)["groups"].([]any)
	if len(groups) != len(categories.DefaultGroups) {
		t.Fatalf("seeded %d groups, want %d", len(groups), len(categories.DefaultGroups))
	}
	for i, g := range groups {
		gm := g.(map[string]any)
		want := categories.DefaultGroups[i]
		if gm["name"] != want.Name {
			t.Errorf("group %d = %v, want %v", i, gm["name"], want.Name)
		}
		if cats := gm["categories"].([]any); len(cats) != len(want.Categories) {
			t.Errorf("group %q has %d categories, want %d", want.Name, len(cats), len(want.Categories))
		}
	}

	// Seeding twice, or after any group exists, is refused.
	if rec := c.do(http.MethodPost, "/api/categories/seed-defaults", nil); rec.Code != http.StatusConflict {
		t.Errorf("second seed: status = %d, want 409 (body: %s)", rec.Code, rec.Body.String())
	}

	other := env.signUp("seed2@example.com")
	other.createGroup(t, "Custom")
	if rec := other.do(http.MethodPost, "/api/categories/seed-defaults", nil); rec.Code != http.StatusConflict {
		t.Errorf("seed with existing groups: status = %d, want 409", rec.Code)
	}
}

func TestCrossUserAccessDenied(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	owner := env.signUp("owner@example.com")
	intruder := env.signUp("intruder@example.com")

	gID := parseID(t, owner.createGroup(t, "Private"))
	catID := parseID(t, owner.createCategory(t, map[string]any{"groupId": gID, "name": "Secret"}))
	targetID := parseID(t, owner.createCategory(t, map[string]any{"groupId": gID, "name": "Other"}))

	attempts := []struct {
		method, path string
		body         any
	}{
		{http.MethodPut, "/api/category-groups/" + gID.String(), map[string]any{"name": "Stolen"}},
		{http.MethodDelete, "/api/category-groups/" + gID.String(), nil},
		{http.MethodPost, "/api/category-groups/" + gID.String() + "/archive", map[string]any{}},
		{http.MethodPut, "/api/categories/" + catID.String(), map[string]any{"groupId": gID, "name": "Stolen"}},
		{http.MethodDelete, "/api/categories/" + catID.String(), nil},
		{http.MethodPost, "/api/categories/" + catID.String() + "/archive", map[string]any{}},
		{http.MethodPost, "/api/categories/" + catID.String() + "/merge", map[string]any{"targetId": targetID}},
	}
	for _, a := range attempts {
		if rec := intruder.do(a.method, a.path, a.body); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s as intruder: status = %d, want 404 (body: %s)", a.method, a.path, rec.Code, rec.Body.String())
		}
	}

	// The intruder cannot create categories inside the owner's group, cannot
	// reorder the owner's groups, and sees an empty list.
	if rec := intruder.do(http.MethodPost, "/api/categories", map[string]any{"groupId": gID, "name": "Sneaky"}); rec.Code != http.StatusNotFound {
		t.Errorf("create in foreign group: status = %d, want 404", rec.Code)
	}
	if rec := intruder.do(http.MethodPut, "/api/category-groups/reorder", map[string]any{"ids": []string{gID.String()}}); rec.Code != http.StatusNotFound {
		t.Errorf("reorder foreign group: status = %d, want 404", rec.Code)
	}
	if groups := intruder.listGroups(t); len(groups) != 0 {
		t.Errorf("intruder sees %d foreign groups", len(groups))
	}

	// Unauthenticated requests are rejected outright.
	anon := &client{env: env, cookies: map[string]*http.Cookie{}}
	if rec := anon.do(http.MethodGet, "/api/categories", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated list: status = %d, want 401", rec.Code)
	}
}
