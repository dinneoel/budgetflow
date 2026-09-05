// Integration tests for the transactions API: real HTTP router (handlers +
// auth/CSRF middleware) against a real PostgreSQL database per test.
package transactions_test

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
	"budgetflow/internal/transactions"
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

// --- fixtures ---

func (c *client) addAccount(name string, opening int64) uuid.UUID {
	c.env.t.Helper()
	rec := c.do(http.MethodPost, "/api/accounts", map[string]any{
		"name": name, "type": "checking", "currency": "USD", "openingBalance": opening,
	})
	if rec.Code != http.StatusCreated {
		c.env.t.Fatalf("create account status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	return parseID(c.env.t, decode(c.env.t, rec)["account"].(map[string]any)["id"])
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
			e.t.Fatalf("create group: %v", err)
		}
		groupID = g.ID
	}
	cat, err := e.q.CreateCategory(ctx, db.CreateCategoryParams{
		UserID: userID, GroupID: groupID, Name: name, BudgetType: "variable", RolloverRule: "none",
	})
	if err != nil {
		e.t.Fatalf("create category: %v", err)
	}
	return cat.ID
}

func parseID(t *testing.T, v any) uuid.UUID {
	t.Helper()
	s, _ := v.(string)
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("bad uuid %v: %v", v, err)
	}
	return id
}

func (c *client) createTransaction(body map[string]any) map[string]any {
	c.env.t.Helper()
	rec := c.do(http.MethodPost, "/api/transactions", body)
	if rec.Code != http.StatusCreated {
		c.env.t.Fatalf("create transaction status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(c.env.t, rec)["transaction"].(map[string]any)
}

func (c *client) balance(accountID uuid.UUID) int64 {
	c.env.t.Helper()
	rec := c.do(http.MethodGet, "/api/accounts/"+accountID.String(), nil)
	if rec.Code != http.StatusOK {
		c.env.t.Fatalf("get account status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	return int64(decode(c.env.t, rec)["account"].(map[string]any)["balance"].(float64))
}

// spending returns net category spending summed across all months.
func (e *testEnv) spending(userID, categoryID uuid.UUID) int64 {
	e.t.Helper()
	rows, err := e.q.CategoryMonthlySpending(context.Background(), userID)
	if err != nil {
		e.t.Fatalf("category monthly spending: %v", err)
	}
	var total int64
	for _, r := range rows {
		if r.CategoryID == categoryID {
			total += r.Spending
		}
	}
	return total
}

func (c *client) listTransactions(query string) (items []map[string]any, total int64) {
	c.env.t.Helper()
	rec := c.do(http.MethodGet, "/api/transactions"+query, nil)
	if rec.Code != http.StatusOK {
		c.env.t.Fatalf("list status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	resp := decode(c.env.t, rec)
	for _, it := range resp["transactions"].([]any) {
		items = append(items, it.(map[string]any))
	}
	return items, int64(resp["total"].(float64))
}

// --- tests ---

func TestEachTypeAffectsBalanceAndSpending(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("types@example.com")
	acc := c.addAccount("Checking", 100_000)
	cat := env.addCategory(c.userID, "Groceries")
	incomeCat := env.addCategory(c.userID, "Salary")

	c.createTransaction(map[string]any{
		"accountId": acc, "categoryId": incomeCat, "type": "income", "amount": 50_000,
		"date": "2026-09-01", "payee": "Employer",
	})
	if got := c.balance(acc); got != 150_000 {
		t.Errorf("balance after income = %d, want 150000", got)
	}

	exp := c.createTransaction(map[string]any{
		"accountId": acc, "categoryId": cat, "type": "expense", "amount": 20_000,
		"date": "2026-09-02", "payee": "Supermarket",
	})
	if exp["amount"].(float64) != -20_000 {
		t.Errorf("expense stored amount = %v, want -20000", exp["amount"])
	}
	if got := c.balance(acc); got != 130_000 {
		t.Errorf("balance after expense = %d, want 130000", got)
	}
	if got := env.spending(c.userID, cat); got != 20_000 {
		t.Errorf("category spending after expense = %d, want 20000", got)
	}

	c.createTransaction(map[string]any{
		"accountId": acc, "categoryId": cat, "type": "refund", "amount": 5_000,
		"date": "2026-09-03", "payee": "Supermarket refund",
	})
	if got := c.balance(acc); got != 135_000 {
		t.Errorf("balance after refund = %d, want 135000", got)
	}
	if got := env.spending(c.userID, cat); got != 15_000 {
		t.Errorf("category spending after refund = %d, want 15000", got)
	}

	c.createTransaction(map[string]any{
		"accountId": acc, "categoryId": cat, "type": "adjustment", "amount": -1_000,
		"date": "2026-09-04", "payee": "Fix",
	})
	if got := c.balance(acc); got != 134_000 {
		t.Errorf("balance after adjustment = %d, want 134000", got)
	}

	// Validation failures.
	for name, body := range map[string]map[string]any{
		"zero amount":      {"accountId": acc, "categoryId": cat, "type": "expense", "amount": 0, "date": "2026-09-01"},
		"negative expense": {"accountId": acc, "categoryId": cat, "type": "expense", "amount": -500, "date": "2026-09-01"},
		"bad type":         {"accountId": acc, "categoryId": cat, "type": "transfer", "amount": 500, "date": "2026-09-01"},
		"bad date":         {"accountId": acc, "categoryId": cat, "type": "expense", "amount": 500, "date": "September 1"},
		"no category":      {"accountId": acc, "type": "expense", "amount": 500, "date": "2026-09-01"},
	} {
		if rec := c.do(http.MethodPost, "/api/transactions", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body: %s)", name, rec.Code, rec.Body.String())
		}
	}
}

func TestTransferPairedAndExcludedFromAnalytics(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("transfer@example.com")
	from := c.addAccount("Checking", 100_000)
	to := c.addAccount("Savings", 0)

	rec := c.do(http.MethodPost, "/api/transactions/transfer", map[string]any{
		"fromAccountId": from, "toAccountId": to, "amount": 30_000, "date": "2026-09-05",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create transfer status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	resp := decode(t, rec)
	outLeg := resp["outTransaction"].(map[string]any)
	inLeg := resp["inTransaction"].(map[string]any)
	if outLeg["amount"].(float64) != -30_000 || inLeg["amount"].(float64) != 30_000 {
		t.Errorf("legs = %v / %v, want -30000 / +30000", outLeg["amount"], inLeg["amount"])
	}
	if parseID(t, outLeg["transferPairId"]) != parseID(t, inLeg["id"]) ||
		parseID(t, inLeg["transferPairId"]) != parseID(t, outLeg["id"]) {
		t.Error("transfer legs are not linked to each other")
	}
	if got := c.balance(from); got != 70_000 {
		t.Errorf("source balance = %d, want 70000", got)
	}
	if got := c.balance(to); got != 30_000 {
		t.Errorf("destination balance = %d, want 30000", got)
	}

	// Transfers never appear in category spending analytics.
	rows, err := env.q.CategoryMonthlySpending(context.Background(), c.userID)
	if err != nil {
		t.Fatalf("category monthly spending: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("spending rows = %d, want 0 (transfers excluded)", len(rows))
	}

	// Deleting one leg soft-deletes both; restoring brings both back.
	outID := parseID(t, outLeg["id"])
	if rec := c.do(http.MethodDelete, "/api/transactions/"+outID.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("delete transfer leg status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := c.balance(from); got != 100_000 {
		t.Errorf("source balance after delete = %d, want 100000", got)
	}
	if got := c.balance(to); got != 0 {
		t.Errorf("destination balance after delete = %d, want 0", got)
	}
	if rec := c.do(http.MethodPost, "/api/transactions/"+outID.String()+"/restore", nil); rec.Code != http.StatusOK {
		t.Fatalf("restore transfer leg status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := c.balance(to); got != 30_000 {
		t.Errorf("destination balance after restore = %d, want 30000 (pair restored too)", got)
	}

	// Same-account transfer is rejected.
	if rec := c.do(http.MethodPost, "/api/transactions/transfer", map[string]any{
		"fromAccountId": from, "toAccountId": from, "amount": 100, "date": "2026-09-05",
	}); rec.Code != http.StatusBadRequest {
		t.Errorf("same-account transfer status = %d, want 400", rec.Code)
	}
}

func TestSplitsMustSumToParent(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("splits@example.com")
	acc := c.addAccount("Checking", 0)
	groceries := env.addCategory(c.userID, "Groceries")
	household := env.addCategory(c.userID, "Household")

	tx := c.createTransaction(map[string]any{
		"accountId": acc, "type": "expense", "amount": 10_000,
		"date": "2026-09-05", "payee": "Hypermarket",
		"splits": []map[string]any{
			{"categoryId": groceries, "amount": 6_000, "memo": "food"},
			{"categoryId": household, "amount": 4_000, "memo": "cleaning"},
		},
	})
	if tx["categoryId"] != nil {
		t.Errorf("split parent categoryId = %v, want null (splits are the categorization)", tx["categoryId"])
	}
	splits := tx["splits"].([]any)
	if len(splits) != 2 {
		t.Fatalf("splits = %d, want 2", len(splits))
	}
	var sum int64
	for _, sp := range splits {
		sum += int64(sp.(map[string]any)["amount"].(float64))
	}
	if sum != -10_000 {
		t.Errorf("stored splits sum = %d, want -10000 (same sign as parent)", sum)
	}
	if got := env.spending(c.userID, groceries); got != 6_000 {
		t.Errorf("groceries spending = %d, want 6000", got)
	}
	if got := env.spending(c.userID, household); got != 4_000 {
		t.Errorf("household spending = %d, want 4000", got)
	}
	if got := c.balance(acc); got != -10_000 {
		t.Errorf("balance = %d, want -10000 (splits don't double-charge)", got)
	}

	for name, splitsBody := range map[string][]map[string]any{
		"sum mismatch": {
			{"categoryId": groceries, "amount": 6_000},
			{"categoryId": household, "amount": 3_000},
		},
		"single split": {
			{"categoryId": groceries, "amount": 10_000},
		},
		"negative split": {
			{"categoryId": groceries, "amount": 11_000},
			{"categoryId": household, "amount": -1_000},
		},
	} {
		rec := c.do(http.MethodPost, "/api/transactions", map[string]any{
			"accountId": acc, "type": "expense", "amount": 10_000, "date": "2026-09-05", "splits": splitsBody,
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body: %s)", name, rec.Code, rec.Body.String())
		}
	}

	// categoryId together with splits is rejected.
	rec := c.do(http.MethodPost, "/api/transactions", map[string]any{
		"accountId": acc, "categoryId": groceries, "type": "expense", "amount": 10_000, "date": "2026-09-05",
		"splits": []map[string]any{
			{"categoryId": groceries, "amount": 6_000},
			{"categoryId": household, "amount": 4_000},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("categoryId+splits: status = %d, want 400", rec.Code)
	}
}

func TestFiltersAndSearch(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("filters@example.com")
	acc1 := c.addAccount("Checking", 0)
	acc2 := c.addAccount("Wallet", 0)
	groceries := env.addCategory(c.userID, "Groceries")
	fun := env.addCategory(c.userID, "Fun")

	c.createTransaction(map[string]any{
		"accountId": acc1, "categoryId": groceries, "type": "expense", "amount": 12_500,
		"date": "2026-09-01", "payee": "Silpo", "notes": "weekly shop", "tags": []string{"food"},
	})
	c.createTransaction(map[string]any{
		"accountId": acc1, "categoryId": fun, "type": "expense", "amount": 4_000,
		"date": "2026-09-10", "payee": "Cinema City", "status": "cleared",
	})
	c.createTransaction(map[string]any{
		"accountId": acc2, "categoryId": groceries, "type": "expense", "amount": 700,
		"date": "2026-08-20", "payee": "Kiosk",
	})
	c.createTransaction(map[string]any{
		"accountId": acc1, "categoryId": fun, "type": "income", "amount": 90_000,
		"date": "2026-09-15", "payee": "Employer Inc",
	})

	for name, tc := range map[string]struct {
		query string
		want  int64
	}{
		"all":            {"", 4},
		"by account":     {"?accountId=" + acc2.String(), 1},
		"by category":    {"?categoryId=" + groceries.String(), 2},
		"date range":     {"?from=2026-09-01&to=2026-09-30", 3},
		"by type":        {"?type=income", 1},
		"by status":      {"?status=cleared", 1},
		"payee match":    {"?payee=cinema", 1},
		"amount range":   {"?amountMin=1000&amountMax=20000", 2},
		"by tag":         {"?tag=food", 1},
		"search payee":   {"?q=silpo", 1},
		"search notes":   {"?q=weekly", 1},
		"search amount":  {"?q=125.00", 1},
		"search nothing": {"?q=zzznope", 0},
	} {
		_, total := c.listTransactions(tc.query)
		if total != tc.want {
			t.Errorf("%s (%s): total = %d, want %d", name, tc.query, total, tc.want)
		}
	}

	// Pagination: newest first, one per page.
	items, total := c.listTransactions("?limit=1&offset=0")
	if total != 4 || len(items) != 1 {
		t.Fatalf("page 1: total=%d items=%d, want 4/1", total, len(items))
	}
	if items[0]["payee"] != "Employer Inc" {
		t.Errorf("newest first = %v, want Employer Inc", items[0]["payee"])
	}
	items, _ = c.listTransactions("?limit=1&offset=1")
	if items[0]["payee"] != "Cinema City" {
		t.Errorf("second page = %v, want Cinema City", items[0]["payee"])
	}
}

func TestBulkOperations(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("bulk@example.com")
	acc := c.addAccount("Checking", 0)
	oldCat := env.addCategory(c.userID, "Misc")
	newCat := env.addCategory(c.userID, "Dining")

	var ids []string
	for i := range 3 {
		tx := c.createTransaction(map[string]any{
			"accountId": acc, "categoryId": oldCat, "type": "expense", "amount": 1_000 + i,
			"date": "2026-09-05", "payee": fmt.Sprintf("Cafe %d", i),
		})
		ids = append(ids, tx["id"].(string))
	}

	bulk := func(body map[string]any) map[string]any {
		t.Helper()
		rec := c.do(http.MethodPost, "/api/transactions/bulk", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("bulk status = %d (body: %s)", rec.Code, rec.Body.String())
		}
		return decode(t, rec)
	}
	auditEvents := func(eventType string) [][]uuid.UUID {
		t.Helper()
		rows, err := env.pool.Query(context.Background(),
			"SELECT entity_ids FROM audit_events WHERE user_id = $1 AND event_type = $2", c.userID, eventType)
		if err != nil {
			t.Fatalf("query audit events: %v", err)
		}
		defer rows.Close()
		var out [][]uuid.UUID
		for rows.Next() {
			var ids []uuid.UUID
			if err := rows.Scan(&ids); err != nil {
				t.Fatalf("scan audit event: %v", err)
			}
			out = append(out, ids)
		}
		return out
	}

	// Categorize all three: one audit event carrying all affected ids.
	res := bulk(map[string]any{"action": "categorize", "ids": ids, "categoryId": newCat})
	if got := len(res["affectedIds"].([]any)); got != 3 {
		t.Errorf("categorize affected = %d, want 3", got)
	}
	if got := env.spending(c.userID, newCat); got != 3_003 {
		t.Errorf("recategorized spending = %d, want 3003", got)
	}
	if events := auditEvents(transactions.EventBulkCategorized); len(events) != 1 || len(events[0]) != 3 {
		t.Errorf("categorize audit events = %v, want one event with 3 ids", events)
	}

	// Tag.
	bulk(map[string]any{"action": "tag", "ids": ids, "tag": "eating-out"})
	if _, total := c.listTransactions("?tag=eating-out"); total != 3 {
		t.Errorf("tagged transactions = %d, want 3", total)
	}
	if events := auditEvents(transactions.EventBulkTagged); len(events) != 1 || len(events[0]) != 3 {
		t.Errorf("tag audit events = %v, want one event with 3 ids", events)
	}

	// Mark reviewed.
	bulk(map[string]any{"action": "mark_reviewed", "ids": ids})
	if _, total := c.listTransactions("?reviewed=true"); total != 3 {
		t.Errorf("reviewed transactions = %d, want 3", total)
	}
	if events := auditEvents(transactions.EventBulkReviewed); len(events) != 1 {
		t.Errorf("reviewed audit events = %d, want 1", len(events))
	}

	// Delete.
	bulk(map[string]any{"action": "delete", "ids": ids})
	if _, total := c.listTransactions(""); total != 0 {
		t.Errorf("live transactions after bulk delete = %d, want 0", total)
	}
	if _, total := c.listTransactions("?deleted=true"); total != 3 {
		t.Errorf("deleted transactions = %d, want 3", total)
	}
	if events := auditEvents(transactions.EventBulkDeleted); len(events) != 1 || len(events[0]) != 3 {
		t.Errorf("delete audit events = %v, want one event with 3 ids", events)
	}

	// Invalid actions and inputs.
	for name, body := range map[string]map[string]any{
		"unknown action":       {"action": "explode", "ids": ids},
		"empty ids":            {"action": "delete", "ids": []string{}},
		"categorize sans cat":  {"action": "categorize", "ids": ids},
		"tag without tag name": {"action": "tag", "ids": ids},
	} {
		if rec := c.do(http.MethodPost, "/api/transactions/bulk", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body: %s)", name, rec.Code, rec.Body.String())
		}
	}
}

func TestBulkCategorizeClearsSplitsAndSkipsTransfers(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("bulk2@example.com")
	acc := c.addAccount("Checking", 0)
	acc2 := c.addAccount("Savings", 0)
	catA := env.addCategory(c.userID, "A")
	catB := env.addCategory(c.userID, "B")

	split := c.createTransaction(map[string]any{
		"accountId": acc, "type": "expense", "amount": 5_000, "date": "2026-09-05",
		"splits": []map[string]any{
			{"categoryId": catA, "amount": 2_500},
			{"categoryId": catB, "amount": 2_500},
		},
	})
	rec := c.do(http.MethodPost, "/api/transactions/transfer", map[string]any{
		"fromAccountId": acc, "toAccountId": acc2, "amount": 1_000, "date": "2026-09-05",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create transfer: %s", rec.Body.String())
	}
	transferID := decode(t, rec)["outTransaction"].(map[string]any)["id"].(string)

	rec = c.do(http.MethodPost, "/api/transactions/bulk", map[string]any{
		"action": "categorize", "ids": []string{split["id"].(string), transferID}, "categoryId": catB,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk categorize status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	affected := decode(t, rec)["affectedIds"].([]any)
	if len(affected) != 1 || affected[0].(string) != split["id"].(string) {
		t.Errorf("affected = %v, want only the split transaction (transfers skipped)", affected)
	}
	// The single category replaced the splits entirely: no double counting.
	if got := env.spending(c.userID, catB); got != 5_000 {
		t.Errorf("catB spending = %d, want 5000", got)
	}
	if got := env.spending(c.userID, catA); got != 0 {
		t.Errorf("catA spending = %d, want 0", got)
	}
}

func TestDuplicateDetectionOnEntry(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("dupes@example.com")
	acc := c.addAccount("Checking", 0)
	cat := env.addCategory(c.userID, "Coffee")

	first := c.createTransaction(map[string]any{
		"accountId": acc, "categoryId": cat, "type": "expense", "amount": 450,
		"date": "2026-09-05", "payee": "Starbucks",
	})
	if first["duplicateWarning"] != false {
		t.Errorf("first entry duplicateWarning = %v, want false", first["duplicateWarning"])
	}

	// Same amount, next day, payee variant: flagged, pointing at the first.
	second := c.createTransaction(map[string]any{
		"accountId": acc, "categoryId": cat, "type": "expense", "amount": 450,
		"date": "2026-09-06", "payee": "STARBUCKS #1234",
	})
	if second["duplicateWarning"] != true {
		t.Fatalf("duplicateWarning = %v, want true (body: %v)", second["duplicateWarning"], second)
	}
	dupes := second["duplicateOf"].([]any)
	if len(dupes) == 0 || dupes[0].(string) != first["id"].(string) {
		t.Errorf("duplicateOf = %v, want [%v]", dupes, first["id"])
	}

	// Different amount: not flagged.
	third := c.createTransaction(map[string]any{
		"accountId": acc, "categoryId": cat, "type": "expense", "amount": 999,
		"date": "2026-09-05", "payee": "Starbucks",
	})
	if third["duplicateWarning"] != false {
		t.Errorf("different amount duplicateWarning = %v, want false", third["duplicateWarning"])
	}

	// Outside the ±1 day window: not flagged.
	fourth := c.createTransaction(map[string]any{
		"accountId": acc, "categoryId": cat, "type": "expense", "amount": 450,
		"date": "2026-09-09", "payee": "Starbucks",
	})
	if fourth["duplicateWarning"] != false {
		t.Errorf("distant date duplicateWarning = %v, want false", fourth["duplicateWarning"])
	}

	// Dissimilar payee: not flagged.
	fifth := c.createTransaction(map[string]any{
		"accountId": acc, "categoryId": cat, "type": "expense", "amount": 450,
		"date": "2026-09-05", "payee": "Aroma Kava",
	})
	if fifth["duplicateWarning"] != false {
		t.Errorf("different payee duplicateWarning = %v, want false", fifth["duplicateWarning"])
	}
}

func TestDuplicateEndpointCopies(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("copy@example.com")
	acc := c.addAccount("Checking", 0)
	catA := env.addCategory(c.userID, "A")
	catB := env.addCategory(c.userID, "B")

	src := c.createTransaction(map[string]any{
		"accountId": acc, "type": "expense", "amount": 8_000, "date": "2026-09-05", "payee": "Store",
		"splits": []map[string]any{
			{"categoryId": catA, "amount": 5_000},
			{"categoryId": catB, "amount": 3_000},
		},
		"tags": []string{"trip"},
	})
	rec := c.do(http.MethodPost, "/api/transactions/"+src["id"].(string)+"/duplicate", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("duplicate status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	dup := decode(t, rec)["transaction"].(map[string]any)
	if dup["id"] == src["id"] {
		t.Error("duplicate must be a new transaction")
	}
	if dup["amount"].(float64) != -8_000 || len(dup["splits"].([]any)) != 2 || len(dup["tags"].([]any)) != 1 {
		t.Errorf("duplicate lost fields: %v", dup)
	}
	if dup["reviewed"] != false || dup["status"] != "uncleared" {
		t.Errorf("duplicate should start uncleared/unreviewed: %v", dup)
	}
	if dup["duplicateWarning"] != true {
		t.Errorf("copy should flag the original as a duplicate: %v", dup["duplicateWarning"])
	}
	if got := c.balance(acc); got != -16_000 {
		t.Errorf("balance = %d, want -16000", got)
	}
}

func TestUpdateAndRestore(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("update@example.com")
	acc := c.addAccount("Checking", 0)
	catA := env.addCategory(c.userID, "A")
	catB := env.addCategory(c.userID, "B")

	tx := c.createTransaction(map[string]any{
		"accountId": acc, "categoryId": catA, "type": "expense", "amount": 2_000,
		"date": "2026-09-05", "payee": "Shop", "tags": []string{"one"},
	})
	id := tx["id"].(string)

	rec := c.do(http.MethodPut, "/api/transactions/"+id, map[string]any{
		"accountId": acc, "categoryId": catB, "type": "expense", "amount": 3_000,
		"date": "2026-09-06", "payee": "Shop 2", "status": "cleared", "reviewed": true,
		"tags": []string{"two"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	upd := decode(t, rec)["transaction"].(map[string]any)
	if upd["amount"].(float64) != -3_000 || upd["payee"] != "Shop 2" || upd["reviewed"] != true {
		t.Errorf("updated = %v", upd)
	}
	tags := upd["tags"].([]any)
	if len(tags) != 1 || tags[0].(map[string]any)["name"] != "two" {
		t.Errorf("tags = %v, want replaced with [two]", tags)
	}
	if got := env.spending(c.userID, catA); got != 0 {
		t.Errorf("catA spending after recategorize = %d, want 0", got)
	}
	if got := env.spending(c.userID, catB); got != 3_000 {
		t.Errorf("catB spending = %d, want 3000", got)
	}

	// Soft delete removes it from balance and live lists; restore brings it back.
	if rec := c.do(http.MethodDelete, "/api/transactions/"+id, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}
	if got := c.balance(acc); got != 0 {
		t.Errorf("balance after delete = %d, want 0", got)
	}
	if _, total := c.listTransactions(""); total != 0 {
		t.Errorf("live list after delete = %d, want 0", total)
	}
	if rec := c.do(http.MethodPut, "/api/transactions/"+id, map[string]any{
		"accountId": acc, "categoryId": catB, "type": "expense", "amount": 100, "date": "2026-09-06",
	}); rec.Code != http.StatusNotFound {
		t.Errorf("update of deleted transaction = %d, want 404", rec.Code)
	}
	if rec := c.do(http.MethodPost, "/api/transactions/"+id+"/restore", nil); rec.Code != http.StatusOK {
		t.Fatalf("restore status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := c.balance(acc); got != -3_000 {
		t.Errorf("balance after restore = %d, want -3000", got)
	}
	if _, total := c.listTransactions(""); total != 1 {
		t.Errorf("live list after restore = %d, want 1", total)
	}
	// Restoring a live transaction is a validation error.
	if rec := c.do(http.MethodPost, "/api/transactions/"+id+"/restore", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("restore of live transaction = %d, want 400", rec.Code)
	}
}

func TestTransferEditRestrictions(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("transferedit@example.com")
	from := c.addAccount("Checking", 50_000)
	to := c.addAccount("Savings", 0)
	cat := env.addCategory(c.userID, "Sneaky")

	rec := c.do(http.MethodPost, "/api/transactions/transfer", map[string]any{
		"fromAccountId": from, "toAccountId": to, "amount": 10_000, "date": "2026-09-05",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create transfer: %s", rec.Body.String())
	}
	outID := decode(t, rec)["outTransaction"].(map[string]any)["id"].(string)

	// Amount change mirrors onto the pair leg.
	rec = c.do(http.MethodPut, "/api/transactions/"+outID, map[string]any{
		"amount": 15_000, "date": "2026-09-07", "status": "cleared",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update transfer leg status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := c.balance(from); got != 35_000 {
		t.Errorf("source balance = %d, want 35000", got)
	}
	if got := c.balance(to); got != 15_000 {
		t.Errorf("destination balance = %d, want 15000 (pair mirrored)", got)
	}

	// Categorizing or retyping a transfer is rejected.
	if rec := c.do(http.MethodPut, "/api/transactions/"+outID, map[string]any{
		"categoryId": cat, "amount": 15_000, "date": "2026-09-07",
	}); rec.Code != http.StatusBadRequest {
		t.Errorf("categorize transfer = %d, want 400", rec.Code)
	}
	if rec := c.do(http.MethodPut, "/api/transactions/"+outID, map[string]any{
		"type": "expense", "amount": 15_000, "date": "2026-09-07",
	}); rec.Code != http.StatusBadRequest {
		t.Errorf("retype transfer = %d, want 400", rec.Code)
	}
	// Duplicating a transfer leg is rejected.
	if rec := c.do(http.MethodPost, "/api/transactions/"+outID+"/duplicate", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("duplicate transfer = %d, want 400", rec.Code)
	}
}

func TestCrossUserAccessDenied(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	owner := env.signUp("owner@example.com")
	other := env.signUp("other@example.com")
	acc := owner.addAccount("Checking", 0)
	cat := env.addCategory(owner.userID, "Groceries")

	tx := owner.createTransaction(map[string]any{
		"accountId": acc, "categoryId": cat, "type": "expense", "amount": 1_000,
		"date": "2026-09-05", "payee": "Shop",
	})
	id := tx["id"].(string)

	if rec := other.do(http.MethodGet, "/api/transactions/"+id, nil); rec.Code != http.StatusNotFound {
		t.Errorf("cross-user get = %d, want 404", rec.Code)
	}
	if rec := other.do(http.MethodDelete, "/api/transactions/"+id, nil); rec.Code != http.StatusNotFound {
		t.Errorf("cross-user delete = %d, want 404", rec.Code)
	}
	// Creating against someone else's account or category is a 404, not a leak.
	if rec := other.do(http.MethodPost, "/api/transactions", map[string]any{
		"accountId": acc, "categoryId": cat, "type": "expense", "amount": 1_000, "date": "2026-09-05",
	}); rec.Code != http.StatusNotFound {
		t.Errorf("cross-user create = %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
	// Bulk ops silently skip foreign ids.
	rec := other.do(http.MethodPost, "/api/transactions/bulk", map[string]any{"action": "delete", "ids": []string{id}})
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk status = %d", rec.Code)
	}
	if got := len(decode(t, rec)["affectedIds"].([]any)); got != 0 {
		t.Errorf("cross-user bulk affected = %d, want 0", got)
	}
	if _, total := owner.listTransactions(""); total != 1 {
		t.Errorf("owner's transaction survived = %d rows, want 1", total)
	}
}
