// Integration tests for the accounts API: real HTTP router (handlers +
// auth/CSRF middleware) against a real PostgreSQL database per test.
package accounts_test

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

func (c *client) createAccount(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	rec := c.do(http.MethodPost, "/api/accounts", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create account status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(t, rec)["account"].(map[string]any)
}

// addTransaction inserts a transaction directly through the query layer (the
// transactions API is a later task).
func (e *testEnv) addTransaction(userID, accountID uuid.UUID, txType string, amount int64) db.Transaction {
	e.t.Helper()
	tx, err := e.q.CreateTransaction(context.Background(), db.CreateTransactionParams{
		UserID:    userID,
		AccountID: accountID,
		Type:      txType,
		Status:    "cleared",
		Amount:    amount,
		Date:      time.Now().UTC(),
		Payee:     "test payee",
	})
	if err != nil {
		e.t.Fatalf("insert %s transaction: %v", txType, err)
	}
	return tx
}

func accountID(t *testing.T, acc map[string]any) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(acc["id"].(string))
	if err != nil {
		t.Fatalf("bad account id %v: %v", acc["id"], err)
	}
	return id
}

func (c *client) getBalance(t *testing.T, id uuid.UUID) int64 {
	t.Helper()
	rec := c.do(http.MethodGet, "/api/accounts/"+id.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get account status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	return int64(decode(t, rec)["account"].(map[string]any)["balance"].(float64))
}

func TestCreateListUpdateAccount(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("acc@example.com")

	acc := c.createAccount(t, map[string]any{
		"name": "Main Checking", "institution": "MonoBank", "type": "checking",
		"currency": "uah", "openingBalance": 150_000,
	})
	if acc["name"] != "Main Checking" || acc["type"] != "checking" {
		t.Errorf("created account = %v", acc)
	}
	if acc["currency"] != "UAH" {
		t.Errorf("currency = %v, want normalized UAH", acc["currency"])
	}
	if acc["balance"].(float64) != 150_000 {
		t.Errorf("initial balance = %v, want opening balance 150000", acc["balance"])
	}
	if acc["includeInNetWorth"] != true {
		t.Error("includeInNetWorth should default to true")
	}

	// Validation failures.
	for name, body := range map[string]map[string]any{
		"empty name":   {"name": "  ", "type": "cash", "currency": "USD"},
		"bad type":     {"name": "X", "type": "yacht", "currency": "USD"},
		"bad currency": {"name": "X", "type": "cash", "currency": "DOLLARS"},
	} {
		if rec := c.do(http.MethodPost, "/api/accounts", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}

	id := accountID(t, acc)
	rec := c.do(http.MethodPut, "/api/accounts/"+id.String(), map[string]any{
		"name": "Renamed", "type": "savings", "currency": "UAH",
		"openingBalance": 200_000, "includeInNetWorth": false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	updated := decode(t, rec)["account"].(map[string]any)
	if updated["name"] != "Renamed" || updated["includeInNetWorth"] != false {
		t.Errorf("updated account = %v", updated)
	}
	if updated["balance"].(float64) != 200_000 {
		t.Errorf("balance after opening-balance change = %v, want 200000", updated["balance"])
	}

	rec = c.do(http.MethodGet, "/api/accounts", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	list := decode(t, rec)["accounts"].([]any)
	if len(list) != 1 {
		t.Fatalf("list has %d accounts, want 1", len(list))
	}

	// Audit trail for create and update.
	var n int
	if err := env.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM audit_events WHERE event_type IN ('account_created','account_updated')").Scan(&n); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if n != 2 {
		t.Errorf("audit events = %d, want 2 (create + update)", n)
	}
}

func TestBalanceComputedAcrossTransactionTypes(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("balance@example.com")
	acc := c.createAccount(t, map[string]any{"name": "Wallet", "type": "cash", "currency": "USD", "openingBalance": 10_000})
	id := accountID(t, acc)

	env.addTransaction(c.userID, id, "income", 50_000)      // +500.00
	env.addTransaction(c.userID, id, "expense", -12_345)    // -123.45
	env.addTransaction(c.userID, id, "refund", 2_345)       // +23.45
	env.addTransaction(c.userID, id, "adjustment", -1_000)  // -10.00
	env.addTransaction(c.userID, id, "transfer", -5_000)    // -50.00 outgoing leg
	del := env.addTransaction(c.userID, id, "expense", -99_999)

	// Soft-deleted transactions must not count.
	if err := env.q.SoftDeleteTransaction(context.Background(), db.SoftDeleteTransactionParams{ID: del.ID, UserID: c.userID}); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	want := int64(10_000 + 50_000 - 12_345 + 2_345 - 1_000 - 5_000)
	if got := c.getBalance(t, id); got != want {
		t.Errorf("balance = %d, want %d", got, want)
	}

	// The list endpoint reports the same computed balance.
	rec := c.do(http.MethodGet, "/api/accounts", nil)
	listed := decode(t, rec)["accounts"].([]any)[0].(map[string]any)
	if int64(listed["balance"].(float64)) != want {
		t.Errorf("list balance = %v, want %d", listed["balance"], want)
	}
}

func TestArchiveLeavesHistoryIntact(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("archive@example.com")
	acc := c.createAccount(t, map[string]any{"name": "Old Card", "type": "credit_card", "currency": "USD"})
	id := accountID(t, acc)
	env.addTransaction(c.userID, id, "expense", -7_500)

	rec := c.do(http.MethodPost, "/api/accounts/"+id.String()+"/archive", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("archive status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	archived := decode(t, rec)["account"].(map[string]any)
	if archived["archivedAt"] == nil {
		t.Error("archivedAt not set after archive")
	}

	// Historical transactions untouched, balance still computable.
	var txCount int
	if err := env.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM transactions WHERE account_id = $1 AND deleted_at IS NULL", id).Scan(&txCount); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if txCount != 1 {
		t.Errorf("transactions after archive = %d, want 1", txCount)
	}
	if got := c.getBalance(t, id); got != -7_500 {
		t.Errorf("balance after archive = %d, want -7500", got)
	}

	rec = c.do(http.MethodPost, "/api/accounts/"+id.String()+"/unarchive", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive status = %d", rec.Code)
	}
	if unarchived := decode(t, rec)["account"].(map[string]any); unarchived["archivedAt"] != nil {
		t.Errorf("archivedAt = %v after unarchive, want null", unarchived["archivedAt"])
	}
}

func TestReconciliationCreatesAdjustment(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("reconcile@example.com")
	acc := c.createAccount(t, map[string]any{"name": "Checking", "type": "checking", "currency": "USD", "openingBalance": 100_000})
	id := accountID(t, acc)
	env.addTransaction(c.userID, id, "expense", -30_000) // computed balance: 70000

	// Statement says 65000 — reconciliation must record a -5000 adjustment.
	rec := c.do(http.MethodPost, "/api/accounts/"+id.String()+"/reconcile", map[string]any{"statementBalance": 65_000})
	if rec.Code != http.StatusOK {
		t.Fatalf("reconcile status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	resp := decode(t, rec)
	adj, ok := resp["adjustment"].(map[string]any)
	if !ok {
		t.Fatalf("no adjustment in response: %v", resp)
	}
	if adj["type"] != "adjustment" || int64(adj["amount"].(float64)) != -5_000 {
		t.Errorf("adjustment = %v, want type=adjustment amount=-5000", adj)
	}
	if got := int64(resp["account"].(map[string]any)["balance"].(float64)); got != 65_000 {
		t.Errorf("post-reconcile balance = %d, want 65000", got)
	}
	if got := c.getBalance(t, id); got != 65_000 {
		t.Errorf("recomputed balance = %d, want 65000", got)
	}

	// Matching statement is a no-op: no second adjustment.
	rec = c.do(http.MethodPost, "/api/accounts/"+id.String()+"/reconcile", map[string]any{"statementBalance": 65_000})
	if rec.Code != http.StatusOK {
		t.Fatalf("second reconcile status = %d", rec.Code)
	}
	if adj := decode(t, rec)["adjustment"]; adj != nil {
		t.Errorf("no-op reconcile created adjustment: %v", adj)
	}
	var adjCount int
	if err := env.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM transactions WHERE account_id = $1 AND type = 'adjustment'", id).Scan(&adjCount); err != nil {
		t.Fatalf("count adjustments: %v", err)
	}
	if adjCount != 1 {
		t.Errorf("adjustment transactions = %d, want 1", adjCount)
	}
}

func TestCrossUserAccessDenied(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	owner := env.signUp("owner@example.com")
	intruder := env.signUp("intruder@example.com")

	acc := owner.createAccount(t, map[string]any{"name": "Private", "type": "savings", "currency": "USD", "openingBalance": 42})
	id := accountID(t, acc)

	attempts := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/accounts/" + id.String(), nil},
		{http.MethodPut, "/api/accounts/" + id.String(), map[string]any{"name": "Stolen", "type": "cash", "currency": "USD"}},
		{http.MethodPost, "/api/accounts/" + id.String() + "/archive", map[string]any{}},
		{http.MethodPost, "/api/accounts/" + id.String() + "/reconcile", map[string]any{"statementBalance": 0}},
	}
	for _, a := range attempts {
		if rec := intruder.do(a.method, a.path, a.body); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s as intruder: status = %d, want 404", a.method, a.path, rec.Code)
		}
	}

	// The intruder's list must not include the owner's account.
	rec := intruder.do(http.MethodGet, "/api/accounts", nil)
	if list := decode(t, rec)["accounts"].([]any); len(list) != 0 {
		t.Errorf("intruder sees %d foreign accounts", len(list))
	}

	// And unauthenticated requests are rejected outright.
	anon := &client{env: env, cookies: map[string]*http.Cookie{}}
	if rec := anon.do(http.MethodGet, "/api/accounts", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated list: status = %d, want 401", rec.Code)
	}

	// Owner still has full access.
	if got := owner.getBalance(t, id); got != 42 {
		t.Errorf("owner balance = %d, want 42", got)
	}
}

func TestGetUnknownAccount(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.signUp("unknown@example.com")

	for _, path := range []string{
		"/api/accounts/" + uuid.NewString(), // valid UUID, no such row
		"/api/accounts/not-a-uuid",
	} {
		if rec := c.do(http.MethodGet, path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404 (body: %s)", path, rec.Code, rec.Body.String())
		}
	}
}
