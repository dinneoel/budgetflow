// Package e2e_test walks the 15 MVP acceptance criteria in sequence over the
// real HTTP API against a real PostgreSQL database: auth and profile,
// accounts, categories, monthly budgets with rollover, transactions of every
// type (splits, bulk ops, soft delete), recurring bills, savings goals, in-app
// notifications, CSV import with batch undo, CSV/ZIP export, dashboard and
// reports, and account deletion with full data wipe.
//
// The final criterion — accessible, responsive UI — is a frontend concern and
// is covered by the axe-core and viewport tests in frontend/src.
package e2e_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"budgetflow/internal/config"
	"budgetflow/internal/httpserver"
	"budgetflow/internal/testdb"
)

const (
	checkingOpening = int64(100_000)
	savingsOpening  = int64(50_000)
	eurOpening      = int64(25_000)

	plannedIncome = int64(500_000)

	allocGroceries = int64(30_000)
	allocCoffee    = int64(10_000)
	allocInternet  = int64(8_000)
	allocSinking   = int64(5_000)

	incomeAmount     = int64(500_000)
	groceriesExpense = int64(20_000)
	splitTotal       = int64(15_000)
	splitGroceries   = int64(10_000)
	splitCoffee      = int64(5_000)
	refundAmount     = int64(2_000)
	transferAmount   = int64(10_000)
	overBudgetCoffee = int64(6_000) // pushes coffee past its allocation
	internetBill     = int64(6_000)
)

func TestMVPAcceptanceCriteria(t *testing.T) {
	pool := testdb.New(t)
	cfg := config.Config{Port: "0", Env: "test", SessionSecret: "e2e-test-secret"}
	srv := httpserver.New(cfg, slog.New(slog.DiscardHandler), pool)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	c := newClient(t, ts)

	now := time.Now()
	day1 := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthStart := day1.Format("2006-01-02")
	monthEnd := day1.AddDate(0, 1, -1).Format("2006-01-02")
	txDate := monthStart

	// --- AC 1: sign-up, session, profile settings ---

	body := c.must(http.MethodPost, "/api/auth/sign-up", http.StatusCreated, obj{
		"email": "leonid@example.com", "password": "correct-horse-battery",
		"name": "Leonid", "defaultCurrency": "USD",
	})
	c.csrf = str(t, body, "csrfToken")
	if got := str(t, body, "user", "email"); got != "leonid@example.com" {
		t.Fatalf("sign-up user email = %q", got)
	}

	c.must(http.MethodPut, "/api/profile", http.StatusOK, obj{
		"name": "Leonid P", "locale": "uk-UA", "timeZone": "Europe/Kyiv",
		"firstDayOfWeek": 1, "defaultCurrency": "USD",
	})
	body = c.must(http.MethodGet, "/api/auth/me", http.StatusOK, nil)
	if got := str(t, body, "user", "timeZone"); got != "Europe/Kyiv" {
		t.Fatalf("profile timeZone = %q, want Europe/Kyiv", got)
	}

	// --- AC 2: manual accounts ---

	checkingID := c.createAccount("Main Checking", "checking", "USD", checkingOpening)
	savingsID := c.createAccount("Rainy Day", "savings", "USD", savingsOpening)
	c.createAccount("EU Wallet", "ewallet", "EUR", eurOpening) // informational foreign-currency balance

	body = c.must(http.MethodGet, "/api/accounts", http.StatusOK, nil)
	if got := len(arr(t, body, "accounts")); got != 3 {
		t.Fatalf("account count = %d, want 3", got)
	}

	// --- AC 3: category groups and categories (defaults + custom) ---

	body = c.must(http.MethodPost, "/api/categories/seed-defaults", http.StatusCreated, nil)
	if len(arr(t, body, "groups")) == 0 {
		t.Fatal("seed-defaults returned no groups")
	}

	essentialsID := c.createGroup("E2E Essentials")
	funID := c.createGroup("E2E Fun")
	savingsGroupID := c.createGroup("E2E Savings")

	incomeCatID := c.createCategory(essentialsID, "E2E Income", "variable", "none")
	groceriesID := c.createCategory(essentialsID, "E2E Groceries", "variable", "none")
	internetID := c.createCategory(essentialsID, "E2E Internet", "fixed", "none")
	coffeeID := c.createCategory(funID, "E2E Coffee", "variable", "rollover")
	sinkingID := c.createCategory(savingsGroupID, "E2E Sinking", "sinking_fund", "rollover")

	// --- AC 4: monthly budget period, allocations, unallocated funds ---

	body = c.must(http.MethodPost, "/api/budgets", http.StatusCreated, obj{
		"year": day1.Year(), "month": int(day1.Month()), "copyPrior": false,
		"plannedIncome": plannedIncome, "notes": "first month",
	})
	periodID := str(t, body, "period", "id")

	for cat, amount := range map[string]int64{
		groceriesID: allocGroceries, coffeeID: allocCoffee,
		internetID: allocInternet, sinkingID: allocSinking,
	} {
		body = c.must(http.MethodPut, "/api/budgets/"+periodID+"/allocations/"+cat, http.StatusOK, obj{"amount": amount})
	}
	wantUnallocated := plannedIncome - allocGroceries - allocCoffee - allocInternet - allocSinking
	if got := num(t, body, "period", "unallocated"); got != wantUnallocated {
		t.Fatalf("unallocated = %d, want %d", got, wantUnallocated)
	}

	// --- AC 5: transactions — every type, splits, duplicates, soft delete, bulk ops ---

	incomeTxID := c.createTransaction(obj{
		"accountId": checkingID, "categoryId": incomeCatID, "type": "income",
		"amount": incomeAmount, "date": txDate, "payee": "Employer",
	})
	groceriesTxID := c.createTransaction(obj{
		"accountId": checkingID, "categoryId": groceriesID, "type": "expense",
		"amount": groceriesExpense, "date": txDate, "payee": "SuperMart",
	})
	c.createTransaction(obj{
		"accountId": checkingID, "type": "expense", "amount": splitTotal,
		"date": txDate, "payee": "MegaMall",
		"splits": []obj{
			{"categoryId": groceriesID, "amount": splitGroceries},
			{"categoryId": coffeeID, "amount": splitCoffee, "memo": "beans"},
		},
	})
	c.createTransaction(obj{
		"accountId": checkingID, "categoryId": groceriesID, "type": "refund",
		"amount": refundAmount, "date": txDate, "payee": "SuperMart",
	})
	c.must(http.MethodPost, "/api/transactions/transfer", http.StatusCreated, obj{
		"fromAccountId": checkingID, "toAccountId": savingsID,
		"amount": transferAmount, "date": txDate, "notes": "monthly stash",
	})

	// Same account, amount, payee, and date as an existing transaction must be
	// flagged as a likely duplicate on entry.
	body = c.must(http.MethodPost, "/api/transactions", http.StatusCreated, obj{
		"accountId": checkingID, "categoryId": groceriesID, "type": "expense",
		"amount": groceriesExpense, "date": txDate, "payee": "SuperMart",
	})
	if !boolean(t, body, "transaction", "duplicateWarning") {
		t.Fatal("duplicate entry not flagged with duplicateWarning")
	}
	dupID := str(t, body, "transaction", "id")

	// Soft delete, inspect the deleted view, restore, then delete for good.
	c.must(http.MethodDelete, "/api/transactions/"+dupID, http.StatusOK, nil)
	body = c.must(http.MethodGet, "/api/transactions?deleted=true", http.StatusOK, nil)
	if got := len(arr(t, body, "transactions")); got != 1 {
		t.Fatalf("deleted view has %d transactions, want 1", got)
	}
	c.must(http.MethodPost, "/api/transactions/"+dupID+"/restore", http.StatusOK, nil)
	c.must(http.MethodDelete, "/api/transactions/"+dupID, http.StatusOK, nil)

	body = c.must(http.MethodPost, "/api/transactions/bulk", http.StatusOK, obj{
		"action": "mark_reviewed", "ids": []string{incomeTxID, groceriesTxID}, "reviewed": true,
	})
	if got := len(arr(t, body, "affectedIds")); got != 2 {
		t.Fatalf("bulk reviewed affected %d, want 2", got)
	}

	body = c.must(http.MethodGet, "/api/transactions?q=SuperMart", http.StatusOK, nil)
	if got := num(t, body, "total"); got != 2 { // groceries expense + refund; the deleted duplicate is excluded
		t.Fatalf("search SuperMart total = %d, want 2", got)
	}

	wantChecking := checkingOpening + incomeAmount - groceriesExpense - splitTotal + refundAmount - transferAmount
	c.assertBalance(checkingID, wantChecking)
	c.assertBalance(savingsID, savingsOpening+transferAmount)

	// Reconciliation: a statement 500 higher than the ledger produces an
	// adjustment transaction closing the gap.
	statement := savingsOpening + transferAmount + 500
	body = c.must(http.MethodPost, "/api/accounts/"+savingsID+"/reconcile", http.StatusOK, obj{"statementBalance": statement})
	if got := num(t, body, "adjustment", "amount"); got != 500 {
		t.Fatalf("reconcile adjustment = %d, want 500", got)
	}
	c.assertBalance(savingsID, statement)

	// Category math after activity: spending nets refunds, splits land on
	// their own categories, transfers stay out entirely.
	period := c.budgetMonth(day1)
	groceriesRow := categoryRow(t, period, groceriesID)
	if got := num(t, groceriesRow, "spending"); got != groceriesExpense+splitGroceries-refundAmount {
		t.Fatalf("groceries spending = %d, want %d", got, groceriesExpense+splitGroceries-refundAmount)
	}
	if got := str(t, groceriesRow, "status"); got != "approaching_limit" {
		t.Fatalf("groceries status = %q, want approaching_limit", got)
	}
	if got := str(t, categoryRow(t, period, coffeeID), "status"); got != "on_track" {
		t.Fatalf("coffee status = %q, want on_track", got)
	}

	// --- AC 6: recurring bills — upcoming list and reserved amounts ---

	due := now.AddDate(0, 0, 2)
	if due.Month() != now.Month() { // clamp to the current period so the reserve is observable
		due = day1.AddDate(0, 1, -1)
	}
	body = c.must(http.MethodPost, "/api/recurring", http.StatusCreated, obj{
		"name": "Internet", "accountId": checkingID, "categoryId": internetID,
		"amount": internetBill, "frequency": "monthly",
		"nextDueDate": due.Format("2006-01-02"), "reminderLeadDays": 7,
	})
	ruleID := str(t, body, "rule", "id")

	body = c.must(http.MethodGet, "/api/recurring/upcoming?days=30", http.StatusOK, nil)
	if got := len(arr(t, body, "upcoming")); got != 1 {
		t.Fatalf("upcoming bills = %d, want 1", got)
	}

	internetRow := categoryRow(t, c.budgetMonth(day1), internetID)
	if got := num(t, internetRow, "reserved"); got != internetBill {
		t.Fatalf("internet reserved = %d, want %d", got, internetBill)
	}
	if got := num(t, internetRow, "remaining"); got != allocInternet-internetBill {
		t.Fatalf("internet remaining = %d, want %d", got, allocInternet-internetBill)
	}

	// --- AC 7: savings goals ---

	body = c.must(http.MethodPost, "/api/goals", http.StatusCreated, obj{
		"name": "Emergency fund", "type": "savings", "targetAmount": 120_000,
		"targetDate": day1.AddDate(1, 0, 0).Format("2006-01-02"), "accountId": savingsID,
	})
	goalID := str(t, body, "goal", "id")
	body = c.must(http.MethodPost, "/api/goals/"+goalID+"/contributions", http.StatusCreated, obj{
		"amount": 10_000, "date": txDate,
	})
	if got := num(t, body, "goal", "currentBalance"); got != 10_000 {
		t.Fatalf("goal balance = %d, want 10000", got)
	}
	if got := num(t, body, "goal", "amountRemaining"); got != 110_000 {
		t.Fatalf("goal remaining = %d, want 110000", got)
	}
	body = c.must(http.MethodGet, "/api/goals/"+goalID, http.StatusOK, nil)
	if field(t, body, "goal", "requiredMonthlyContribution") == nil {
		t.Fatal("goal has no requiredMonthlyContribution despite a target date")
	}

	// --- AC 8: in-app notifications — event triggers, scheduled evaluation, read state ---

	c.must(http.MethodPut, "/api/notifications/preferences", http.StatusOK, obj{
		"type": "category_threshold", "enabled": true, "thresholdPct": 80,
	})

	// Overspending coffee through the transactions API must fire the
	// over-budget trigger without any explicit evaluate call.
	c.createTransaction(obj{
		"accountId": checkingID, "categoryId": coffeeID, "type": "expense",
		"amount": overBudgetCoffee, "date": txDate, "payee": "Third Wave",
	})
	body = c.must(http.MethodGet, "/api/notifications", http.StatusOK, nil)
	if !hasNotificationType(body, "category_over_budget") {
		t.Fatal("no category_over_budget notification after overspending write")
	}

	body = c.must(http.MethodPost, "/api/notifications/evaluate", http.StatusOK, nil)
	unread := num(t, body, "unreadCount")
	if unread == 0 {
		t.Fatal("unreadCount = 0 after evaluate")
	}
	body = c.must(http.MethodGet, "/api/notifications", http.StatusOK, nil)
	if !hasNotificationType(body, "bill_due") {
		t.Fatal("no bill_due notification for a bill inside its reminder window")
	}

	// Evaluating again with no new activity must not duplicate notifications.
	body = c.must(http.MethodPost, "/api/notifications/evaluate", http.StatusOK, nil)
	if got := num(t, body, "unreadCount"); got != unread {
		t.Fatalf("re-evaluate changed unreadCount %d -> %d (dedupe broken)", unread, got)
	}

	first := arr(t, c.must(http.MethodGet, "/api/notifications", http.StatusOK, nil), "notifications")[0].(map[string]any)
	c.must(http.MethodPost, "/api/notifications/"+str(t, first, "id")+"/read", http.StatusNoContent, nil)
	body = c.must(http.MethodGet, "/api/notifications/unread-count", http.StatusOK, nil)
	if got := num(t, body, "unreadCount"); got != unread-1 {
		t.Fatalf("unread after single read = %d, want %d", got, unread-1)
	}
	c.must(http.MethodPost, "/api/notifications/read-all", http.StatusNoContent, nil)
	body = c.must(http.MethodGet, "/api/notifications/unread-count", http.StatusOK, nil)
	if got := num(t, body, "unreadCount"); got != 0 {
		t.Fatalf("unread after read-all = %d, want 0", got)
	}

	// --- AC 6 continued: mark-as-paid creates the real transaction and advances the schedule ---

	body = c.must(http.MethodPost, "/api/recurring/"+ruleID+"/pay", http.StatusCreated, obj{"date": txDate})
	if got := num(t, body, "transaction", "amount"); got != -internetBill {
		t.Fatalf("mark-paid transaction amount = %d, want %d", got, -internetBill)
	}
	nextDue, err := time.Parse("2006-01-02", str(t, body, "rule", "nextDueDate"))
	if err != nil || !nextDue.After(due) {
		t.Fatalf("nextDueDate %q not advanced past %s (err %v)", str(t, body, "rule", "nextDueDate"), due.Format("2006-01-02"), err)
	}
	wantChecking -= overBudgetCoffee + internetBill
	c.assertBalance(checkingID, wantChecking)

	// --- AC 9: CSV import — upload, mapping, preview with errors, commit, batch undo ---

	csvData := fmt.Sprintf("Date,Amount,Payee,Notes\n%s,-12.34,Cafe Import,latte\n%s,45.00,Employer,bonus\nnot-a-date,9.99,Broken,\n", txDate, txDate)
	body = c.upload("/api/imports", "bank.csv", csvData)
	batchID := str(t, body, "batch", "id")
	if got := len(arr(t, body, "columns")); got != 4 {
		t.Fatalf("detected %d columns, want 4", got)
	}

	c.must(http.MethodPut, "/api/imports/"+batchID+"/mapping", http.StatusOK, obj{
		"accountId": savingsID, "dateColumn": 0, "amountColumn": 1,
		"payeeColumn": 2, "notesColumn": 3, "dateFormat": "auto", "amountFormat": "dot_decimal",
	})
	body = c.must(http.MethodGet, "/api/imports/"+batchID+"/preview", http.StatusOK, nil)
	if v, e := num(t, body, "valid"), num(t, body, "errored"); v != 2 || e != 1 {
		t.Fatalf("preview valid/errored = %d/%d, want 2/1", v, e)
	}

	body = c.must(http.MethodPost, "/api/imports/"+batchID+"/commit", http.StatusCreated, obj{"includeDuplicates": false})
	if got := len(arr(t, body, "createdIds")); got != 2 {
		t.Fatalf("commit created %d transactions, want 2", got)
	}
	c.assertBalance(savingsID, statement-1_234+4_500)

	// Deleting the batch removes its transactions and restores prior balances.
	c.must(http.MethodDelete, "/api/imports/"+batchID, http.StatusOK, nil)
	c.assertBalance(savingsID, statement)

	// --- AC 10: CSV export and full data export ---

	ct, data := c.download("/api/export/transactions.csv")
	if !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("transactions.csv content type = %q", ct)
	}
	if lines := strings.Count(strings.TrimSpace(string(data)), "\n"); lines < 5 {
		t.Fatalf("transactions.csv has %d data lines, want >= 5", lines)
	}

	ct, data = c.download("/api/export/all.zip")
	if !strings.HasPrefix(ct, "application/zip") {
		t.Fatalf("all.zip content type = %q", ct)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("full export is not a readable ZIP: %v", err)
	}
	if len(zr.File) < 4 {
		t.Fatalf("full export contains %d files, want >= 4", len(zr.File))
	}

	// --- AC 11: dashboard ---

	body = c.must(http.MethodGet, "/api/dashboard", http.StatusOK, nil)
	wantAvailable := wantChecking + statement
	if got := num(t, body, "availableBalance"); got != wantAvailable {
		t.Fatalf("dashboard availableBalance = %d, want %d", got, wantAvailable)
	}
	if !boolean(t, body, "budget", "exists") {
		t.Fatal("dashboard reports no budget for the current month")
	}
	wantSpending := (groceriesExpense + splitGroceries - refundAmount) + (splitCoffee + overBudgetCoffee) + internetBill
	if got := num(t, body, "mtdSpending"); got != wantSpending {
		t.Fatalf("dashboard mtdSpending = %d, want %d", got, wantSpending)
	}
	if got := len(arr(t, body, "foreignBalances")); got != 1 {
		t.Fatalf("dashboard foreignBalances = %d entries, want 1 (EUR)", got)
	}
	if got := len(arr(t, body, "goals")); got != 1 {
		t.Fatalf("dashboard goals = %d, want 1", got)
	}
	if len(arr(t, body, "recentTransactions")) == 0 {
		t.Fatal("dashboard has no recent transactions")
	}

	// --- AC 12: reports (transfers excluded, refunds netted, foreign currency informational) ---

	body = c.must(http.MethodGet, "/api/reports/spending-by-category?from="+monthStart+"&to="+monthEnd, http.StatusOK, nil)
	// The report nets every categorized non-transfer transaction, so the
	// income category appears as negative spending in the total.
	if got := num(t, body, "totalSpending"); got != wantSpending-incomeAmount {
		t.Fatalf("report totalSpending = %d, want %d", got, wantSpending-incomeAmount)
	}
	var reportGroceries int64
	for _, v := range arr(t, body, "categories") {
		if row := v.(map[string]any); row["categoryId"] == groceriesID {
			reportGroceries = num(t, row, "spending")
		}
	}
	if want := groceriesExpense + splitGroceries - refundAmount; reportGroceries != want {
		t.Fatalf("report groceries spending = %d, want %d (refund netted, transfer excluded)", reportGroceries, want)
	}
	body = c.must(http.MethodGet, "/api/reports/net-worth", http.StatusOK, nil)
	if got := num(t, body, "total"); got != wantAvailable {
		t.Fatalf("net worth total = %d, want %d", got, wantAvailable)
	}
	if got := num(t, body, "foreignTotals", "EUR"); got != eurOpening {
		t.Fatalf("net worth EUR foreign total = %d, want %d", got, eurOpening)
	}

	// --- AC 4 continued: next month created from prior, rollover rules applied ---

	next := day1.AddDate(0, 1, 0)
	body = c.must(http.MethodPost, "/api/budgets", http.StatusCreated, obj{
		"year": next.Year(), "month": int(next.Month()), "copyPrior": true, "plannedIncome": plannedIncome,
	})
	nextPeriod := field(t, body, "period").(map[string]any)
	if got := num(t, categoryRow(t, nextPeriod, sinkingID), "rollover"); got != allocSinking {
		t.Fatalf("sinking rollover = %d, want %d (unused balance carries forward)", got, allocSinking)
	}
	if got := num(t, categoryRow(t, nextPeriod, groceriesID), "rollover"); got != 0 {
		t.Fatalf("groceries rollover = %d, want 0 (rule none)", got)
	}
	if got := num(t, categoryRow(t, nextPeriod, groceriesID), "amount"); got != allocGroceries {
		t.Fatalf("copied groceries allocation = %d, want %d", got, allocGroceries)
	}

	// --- AC 13 + 14: account deletion requires re-auth and wipes everything ---

	c.must(http.MethodPost, "/api/account/delete", http.StatusUnauthorized, obj{"password": "wrong-password"})
	c.must(http.MethodPost, "/api/account/delete", http.StatusNoContent, obj{"password": "correct-horse-battery"})
	c.must(http.MethodGet, "/api/auth/me", http.StatusUnauthorized, nil)
	c.must(http.MethodPost, "/api/auth/sign-in", http.StatusUnauthorized, obj{
		"email": "leonid@example.com", "password": "correct-horse-battery",
	})

	for _, table := range []string{
		"users", "sessions", "accounts", "categories", "category_groups",
		"budget_periods", "budget_allocations", "transactions", "transaction_splits",
		"recurring_rules", "goals", "goal_contributions", "import_batches", "notifications",
	} {
		var count int
		if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s still has %d rows after account deletion", table, count)
		}
	}
}

// obj is shorthand for JSON request bodies.
type obj map[string]any

type client struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
}

func newClient(t *testing.T, ts *httptest.Server) *client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	hc := ts.Client()
	hc.Jar = jar
	return &client{t: t, base: ts.URL, http: hc}
}

// must sends a JSON request and fails the test unless the response has the
// expected status. It returns the decoded JSON body (nil for empty bodies).
func (c *client) must(method, path string, wantStatus int, body any) map[string]any {
	c.t.Helper()
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal request: %v", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, c.base+path, reqBody)
	if err != nil {
		c.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != wantStatus {
		c.t.Fatalf("%s %s = %d, want %d; body: %s", method, path, res.StatusCode, wantStatus, raw)
	}
	if len(raw) == 0 {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		c.t.Fatalf("%s %s: invalid JSON response %q: %v", method, path, raw, err)
	}
	return decoded
}

// download fetches a raw (non-JSON) resource and returns its content type and bytes.
func (c *client) download(path string) (string, []byte) {
	c.t.Helper()
	res, err := c.http.Get(c.base + path)
	if err != nil {
		c.t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = res.Body.Close() }()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		c.t.Fatalf("GET %s = %d; body: %s", path, res.StatusCode, data)
	}
	return res.Header.Get("Content-Type"), data
}

// upload posts a multipart file and returns the decoded JSON response.
func (c *client) upload(path, filename, content string) map[string]any {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		c.t.Fatalf("multipart: %v", err)
	}
	if _, err := io.WriteString(fw, content); err != nil {
		c.t.Fatalf("multipart write: %v", err)
	}
	if err := mw.Close(); err != nil {
		c.t.Fatalf("multipart close: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.base+path, &buf)
	if err != nil {
		c.t.Fatalf("build upload: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", c.csrf)
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusCreated {
		c.t.Fatalf("POST %s = %d, want 201; body: %s", path, res.StatusCode, raw)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		c.t.Fatalf("upload response not JSON: %v", err)
	}
	return decoded
}

func (c *client) createAccount(name, typ, currency string, opening int64) string {
	c.t.Helper()
	body := c.must(http.MethodPost, "/api/accounts", http.StatusCreated, obj{
		"name": name, "type": typ, "currency": currency, "openingBalance": opening,
	})
	return str(c.t, body, "account", "id")
}

func (c *client) createGroup(name string) string {
	c.t.Helper()
	body := c.must(http.MethodPost, "/api/category-groups", http.StatusCreated, obj{"name": name})
	return str(c.t, body, "group", "id")
}

func (c *client) createCategory(groupID, name, budgetType, rollover string) string {
	c.t.Helper()
	body := c.must(http.MethodPost, "/api/categories", http.StatusCreated, obj{
		"groupId": groupID, "name": name, "budgetType": budgetType, "rolloverRule": rollover,
	})
	return str(c.t, body, "category", "id")
}

func (c *client) createTransaction(in obj) string {
	c.t.Helper()
	body := c.must(http.MethodPost, "/api/transactions", http.StatusCreated, in)
	return str(c.t, body, "transaction", "id")
}

func (c *client) assertBalance(accountID string, want int64) {
	c.t.Helper()
	body := c.must(http.MethodGet, "/api/accounts/"+accountID, http.StatusOK, nil)
	if got := num(c.t, body, "account", "balance"); got != want {
		c.t.Fatalf("account %s balance = %d, want %d", accountID, got, want)
	}
}

func (c *client) budgetMonth(day1 time.Time) map[string]any {
	c.t.Helper()
	body := c.must(http.MethodGet, fmt.Sprintf("/api/budgets/month/%d/%d", day1.Year(), int(day1.Month())), http.StatusOK, nil)
	return field(c.t, body, "period").(map[string]any)
}

// categoryRow finds a category's row in a budget period response.
func categoryRow(t *testing.T, period map[string]any, categoryID string) map[string]any {
	t.Helper()
	for _, v := range arr(t, period, "categories") {
		row := v.(map[string]any)
		if row["categoryId"] == categoryID {
			return row
		}
	}
	t.Fatalf("category %s not in budget period response", categoryID)
	return nil
}

func hasNotificationType(body map[string]any, typ string) bool {
	for _, v := range body["notifications"].([]any) {
		if v.(map[string]any)["type"] == typ {
			return true
		}
	}
	return false
}

// field navigates nested JSON objects, failing the test on a missing key.
func field(t *testing.T, m map[string]any, path ...string) any {
	t.Helper()
	var cur any = m
	for i, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("field %v: %v is not an object", path[:i+1], cur)
		}
		cur, ok = mm[p]
		if !ok {
			t.Fatalf("field %v missing (have keys %v)", path[:i+1], keys(mm))
		}
	}
	return cur
}

func str(t *testing.T, m map[string]any, path ...string) string {
	t.Helper()
	v, ok := field(t, m, path...).(string)
	if !ok {
		t.Fatalf("field %v is not a string", path)
	}
	return v
}

func num(t *testing.T, m map[string]any, path ...string) int64 {
	t.Helper()
	v, ok := field(t, m, path...).(float64)
	if !ok {
		t.Fatalf("field %v is not a number", path)
	}
	return int64(v)
}

func boolean(t *testing.T, m map[string]any, path ...string) bool {
	t.Helper()
	v, ok := field(t, m, path...).(bool)
	if !ok {
		t.Fatalf("field %v is not a bool", path)
	}
	return v
}

func arr(t *testing.T, m map[string]any, path ...string) []any {
	t.Helper()
	v, ok := field(t, m, path...).([]any)
	if !ok {
		t.Fatalf("field %v is not an array", path)
	}
	return v
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
