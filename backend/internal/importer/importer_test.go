// Integration tests for the CSV import API: real HTTP router (handlers +
// auth/CSRF middleware) against a real PostgreSQL database per test.
package importer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
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
	return c.doRaw(method, path, "application/json", rd)
}

func (c *client) doRaw(method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	c.env.t.Helper()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
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

// upload posts content as a multipart CSV file and returns the response body.
func (c *client) upload(fileName, content string) map[string]any {
	c.env.t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		c.env.t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		c.env.t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		c.env.t.Fatalf("close multipart writer: %v", err)
	}
	rec := c.doRaw(http.MethodPost, "/api/imports", mw.FormDataContentType(), body)
	if rec.Code != http.StatusCreated {
		c.env.t.Fatalf("upload status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(c.env.t, rec)
}

func (c *client) batchID(uploadResp map[string]any) string {
	c.env.t.Helper()
	id, _ := uploadResp["batch"].(map[string]any)["id"].(string)
	if id == "" {
		c.env.t.Fatalf("upload response has no batch id: %v", uploadResp)
	}
	return id
}

func (c *client) setMapping(batchID string, mapping map[string]any) *httptest.ResponseRecorder {
	c.env.t.Helper()
	return c.do(http.MethodPut, "/api/imports/"+batchID+"/mapping", mapping)
}

func (c *client) preview(batchID string) map[string]any {
	c.env.t.Helper()
	rec := c.do(http.MethodGet, "/api/imports/"+batchID+"/preview", nil)
	if rec.Code != http.StatusOK {
		c.env.t.Fatalf("preview status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	return decode(c.env.t, rec)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not valid JSON: %v (body: %s)", err, rec.Body.String())
	}
	return v
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
		UserID: userID, GroupID: groupID, Name: name, BudgetType: "variable", RolloverRule: "none",
	})
	if err != nil {
		e.t.Fatalf("create fixture category: %v", err)
	}
	return cat.ID
}

func (e *testEnv) accountBalance(accountID uuid.UUID) int64 {
	e.t.Helper()
	var sum int64
	err := e.pool.QueryRow(context.Background(),
		"SELECT COALESCE(SUM(amount), 0) FROM transactions WHERE account_id = $1 AND deleted_at IS NULL", accountID).Scan(&sum)
	if err != nil {
		e.t.Fatalf("sum account balance: %v", err)
	}
	return sum
}

func (e *testEnv) auditCount(userID uuid.UUID, eventType string) int {
	e.t.Helper()
	var n int
	err := e.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM audit_events WHERE user_id = $1 AND event_type = $2", userID, eventType).Scan(&n)
	if err != nil {
		e.t.Fatalf("count audit events: %v", err)
	}
	return n
}

func mappingFor(accountID uuid.UUID, extra map[string]any) map[string]any {
	m := map[string]any{
		"accountId": accountID, "dateColumn": 0, "amountColumn": 1, "payeeColumn": 2,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestUploadDetectsColumnsAndDelimiter(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("upload@example.com")

	resp := c.upload("bank.csv", "Date;Amount;Payee\n2026-08-01;-12,50;Coffee\n2026-08-02;-8,00;Lunch\n")
	cols, _ := resp["columns"].([]any)
	if len(cols) != 3 || cols[0] != "Date" || cols[2] != "Payee" {
		t.Fatalf("columns = %v, want [Date Amount Payee]", cols)
	}
	if resp["hasHeader"] != true {
		t.Fatalf("hasHeader = %v, want true", resp["hasHeader"])
	}
	if resp["rowCount"] != float64(2) {
		t.Fatalf("rowCount = %v, want 2", resp["rowCount"])
	}
	if got := resp["batch"].(map[string]any)["status"]; got != "pending" {
		t.Fatalf("batch status = %v, want pending", got)
	}
	if sample, _ := resp["sampleRows"].([]any); len(sample) != 2 {
		t.Fatalf("sampleRows = %v, want 2 rows", resp["sampleRows"])
	}

	// the pending batch shows up in the batch list
	rec := c.do(http.MethodGet, "/api/imports", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if batches, _ := decode(t, rec)["batches"].([]any); len(batches) != 1 {
		t.Fatalf("batches = %v, want 1", batches)
	}

	// a malformed file is rejected, an empty one too
	empty := &bytes.Buffer{}
	mw := multipart.NewWriter(empty)
	fw, _ := mw.CreateFormFile("file", "empty.csv")
	fw.Write([]byte("   \n")) //nolint:errcheck
	mw.Close()               //nolint:errcheck
	if rec := c.doRaw(http.MethodPost, "/api/imports", mw.FormDataContentType(), empty); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty upload status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestMappingValidationAndPreview(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("mapping@example.com")
	account := e.addAccount(c.userID)
	groceries := e.addCategory(c.userID, "Groceries")

	// DD/MM/YYYY dates, comma-decimal amounts, one bad date, one bad amount,
	// one unknown category
	resp := c.upload("bank.csv", "Date,Amount,Payee,Category\n"+
		"31/08/2026,\"-1.234,56\",Grocery Store,Groceries\n"+
		"01/09/2026,\"2.000,00\",Employer,Salary\n"+
		"not-a-date,\"-5,00\",Cafe,\n"+
		"02/09/2026,oops,Cafe,\n")
	id := c.batchID(resp)

	// mapping with an out-of-range column is rejected
	bad := mappingFor(account, map[string]any{"amountColumn": 9})
	if rec := c.setMapping(id, bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad mapping status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	// mapping with someone else's account is rejected
	other := e.signUp("other-mapping@example.com")
	foreign := e.addAccount(other.userID)
	if rec := c.setMapping(id, mappingFor(foreign, nil)); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign account mapping status = %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
	// preview before mapping is a 400
	if rec := c.do(http.MethodGet, "/api/imports/"+id+"/preview", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("premature preview status = %d, want 400", rec.Code)
	}

	good := mappingFor(account, map[string]any{
		"categoryColumn": 3, "dateFormat": "DD/MM/YYYY", "amountFormat": "comma_decimal",
	})
	if rec := c.setMapping(id, good); rec.Code != http.StatusOK {
		t.Fatalf("set mapping status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	p := c.preview(id)
	rows, _ := p["rows"].([]any)
	if len(rows) != 4 {
		t.Fatalf("preview rows = %d, want 4", len(rows))
	}
	if p["valid"] != float64(2) || p["errored"] != float64(2) {
		t.Fatalf("preview counts = valid %v errored %v, want 2/2", p["valid"], p["errored"])
	}

	first := rows[0].(map[string]any)
	if first["date"] != "2026-08-31" || first["amount"] != float64(-123456) || first["type"] != "expense" {
		t.Fatalf("row 0 = %v, want 2026-08-31 / -123456 / expense", first)
	}
	if got, _ := first["categoryId"].(string); got != groceries.String() {
		t.Fatalf("row 0 categoryId = %v, want %s", first["categoryId"], groceries)
	}
	second := rows[1].(map[string]any)
	if second["type"] != "income" || second["amount"] != float64(200000) {
		t.Fatalf("row 1 = %v, want income of 200000", second)
	}
	// unknown category "Salary" warns but does not error
	if warns, _ := second["warnings"].([]any); len(warns) != 1 {
		t.Fatalf("row 1 warnings = %v, want 1", second["warnings"])
	}
	if errs, _ := rows[2].(map[string]any)["errors"].([]any); len(errs) == 0 {
		t.Fatalf("bad-date row has no errors")
	}
	if errs, _ := rows[3].(map[string]any)["errors"].([]any); len(errs) == 0 {
		t.Fatalf("bad-amount row has no errors")
	}
}

func TestPreviewFlagsDuplicates(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("dupes@example.com")
	account := e.addAccount(c.userID)
	cat := e.addCategory(c.userID, "Eating Out")

	// an existing transaction one day off with a similar payee
	existing, err := e.q.CreateTransaction(context.Background(), db.CreateTransactionParams{
		UserID: c.userID, AccountID: account, CategoryID: &cat, Type: "expense", Status: "cleared",
		Amount: -1250, Date: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), Payee: "STARBUCKS #1234",
	})
	if err != nil {
		t.Fatalf("create existing transaction: %v", err)
	}

	resp := c.upload("bank.csv", "Date,Amount,Payee\n"+
		"2026-08-01,-12.50,Starbucks\n"+ // dup of existing (±1 day, similar payee)
		"2026-08-10,-30.00,Cinema\n"+
		"2026-08-10,-30.00,Cinema\n") // in-file repeat of the row above
	id := c.batchID(resp)
	if rec := c.setMapping(id, mappingFor(account, nil)); rec.Code != http.StatusOK {
		t.Fatalf("set mapping status = %d (body: %s)", rec.Code, rec.Body.String())
	}

	p := c.preview(id)
	rows, _ := p["rows"].([]any)
	dupOf, _ := rows[0].(map[string]any)["duplicateOf"].([]any)
	if len(dupOf) != 1 || dupOf[0] != existing.ID.String() {
		t.Fatalf("row 0 duplicateOf = %v, want [%s]", dupOf, existing.ID)
	}
	if rows[1].(map[string]any)["duplicateOfRow"] != nil {
		t.Fatalf("row 1 unexpectedly flagged as in-file duplicate")
	}
	if got := rows[2].(map[string]any)["duplicateOfRow"]; got != float64(1) {
		t.Fatalf("row 2 duplicateOfRow = %v, want 1", got)
	}
	if p["duplicates"] != float64(2) {
		t.Fatalf("duplicates = %v, want 2", p["duplicates"])
	}
}

func TestCommitSkipsAndIncludesDuplicates(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("commit@example.com")
	account := e.addAccount(c.userID)
	cat := e.addCategory(c.userID, "Eating Out")
	if _, err := e.q.CreateTransaction(context.Background(), db.CreateTransactionParams{
		UserID: c.userID, AccountID: account, CategoryID: &cat, Type: "expense", Status: "cleared",
		Amount: -1250, Date: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Payee: "Starbucks",
	}); err != nil {
		t.Fatalf("create existing transaction: %v", err)
	}
	balanceBefore := e.accountBalance(account)

	content := "Date,Amount,Payee,Category\n" +
		"2026-08-01,-12.50,Starbucks,\n" + // duplicate of the existing transaction
		"2026-08-02,-8.00,Bakery,Eating Out\n" +
		"2026-08-03,1500.00,Employer,\n" +
		"bad-date,-1.00,X,\n" // error row
	mapping := mappingFor(account, map[string]any{"categoryColumn": 3})

	// default commit skips duplicates and error rows
	resp := c.upload("bank.csv", content)
	id := c.batchID(resp)
	if rec := c.setMapping(id, mapping); rec.Code != http.StatusOK {
		t.Fatalf("set mapping status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	rec := c.do(http.MethodPost, "/api/imports/"+id+"/commit", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	res := decode(t, rec)
	created, _ := res["createdIds"].([]any)
	if len(created) != 2 || res["skippedDuplicates"] != float64(1) || res["skippedErrors"] != float64(1) {
		t.Fatalf("commit result = %v, want 2 created / 1 dup / 1 error", res)
	}
	if got := res["batch"].(map[string]any)["status"]; got != "committed" {
		t.Fatalf("batch status = %v, want committed", got)
	}
	if got := e.accountBalance(account); got != balanceBefore-800+150000 {
		t.Fatalf("balance = %d, want %d", got, balanceBefore-800+150000)
	}
	// the bakery row picked up its category by name
	var catCount int
	if err := e.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM transactions WHERE user_id = $1 AND category_id = $2 AND payee = 'Bakery'",
		c.userID, cat).Scan(&catCount); err != nil || catCount != 1 {
		t.Fatalf("bakery categorized count = %d (%v), want 1", catCount, err)
	}
	if n := e.auditCount(c.userID, "import_committed"); n != 1 {
		t.Fatalf("import_committed audit events = %d, want 1", n)
	}
	// a committed batch cannot be committed again
	if rec := c.do(http.MethodPost, "/api/imports/"+id+"/commit", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("re-commit status = %d, want 400", rec.Code)
	}

	// includeDuplicates imports the flagged row too; skipRows excludes by index
	resp = c.upload("again.csv", content)
	id2 := c.batchID(resp)
	if rec := c.setMapping(id2, mapping); rec.Code != http.StatusOK {
		t.Fatalf("set mapping status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	rec = c.do(http.MethodPost, "/api/imports/"+id2+"/commit", map[string]any{
		"includeDuplicates": true, "skipRows": []int{2},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("second commit status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	res = decode(t, rec)
	// rows 0 (dup of existing), 1 (dup of first import), row 2 skipped manually
	if created, _ := res["createdIds"].([]any); len(created) != 2 || res["skippedManually"] != float64(1) {
		t.Fatalf("second commit result = %v, want 2 created / 1 manual skip", res)
	}
}

func TestBatchDeleteRestoresBalances(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("undo@example.com")
	account := e.addAccount(c.userID)
	balanceBefore := e.accountBalance(account)

	resp := c.upload("bank.csv", "Date,Amount,Payee\n2026-08-01,-12.50,Coffee\n2026-08-02,100.00,Refund\n")
	id := c.batchID(resp)
	if rec := c.setMapping(id, mappingFor(account, nil)); rec.Code != http.StatusOK {
		t.Fatalf("set mapping status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if rec := c.do(http.MethodPost, "/api/imports/"+id+"/commit", nil); rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := e.accountBalance(account); got != balanceBefore+8750 {
		t.Fatalf("balance after commit = %d, want %d", got, balanceBefore+8750)
	}

	// another user cannot touch the batch
	other := e.signUp("other-undo@example.com")
	if rec := other.do(http.MethodDelete, "/api/imports/"+id, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user delete status = %d, want 404", rec.Code)
	}

	rec := c.do(http.MethodDelete, "/api/imports/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	res := decode(t, rec)
	if ids, _ := res["deletedTransactionIds"].([]any); len(ids) != 2 {
		t.Fatalf("deletedTransactionIds = %v, want 2", res["deletedTransactionIds"])
	}
	if got := res["batch"].(map[string]any)["status"]; got != "deleted" {
		t.Fatalf("batch status = %v, want deleted", got)
	}
	if got := e.accountBalance(account); got != balanceBefore {
		t.Fatalf("balance after undo = %d, want %d", got, balanceBefore)
	}
	if n := e.auditCount(c.userID, "import_batch_deleted"); n != 1 {
		t.Fatalf("import_batch_deleted audit events = %d, want 1", n)
	}
	// deleting again is rejected
	if rec := c.do(http.MethodDelete, "/api/imports/"+id, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("re-delete status = %d, want 400", rec.Code)
	}
}

func TestPendingBatchDiscard(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("discard@example.com")

	resp := c.upload("bank.csv", "Date,Amount\n2026-08-01,-1.00\n")
	id := c.batchID(resp)
	rec := c.do(http.MethodDelete, "/api/imports/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("discard status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	res := decode(t, rec)
	if ids, _ := res["deletedTransactionIds"].([]any); len(ids) != 0 {
		t.Fatalf("deletedTransactionIds = %v, want none", res["deletedTransactionIds"])
	}
	if got := res["batch"].(map[string]any)["status"]; got != "deleted" {
		t.Fatalf("batch status = %v, want deleted", got)
	}
}

func TestCommitWithNothingToImport(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("nothing@example.com")
	account := e.addAccount(c.userID)

	resp := c.upload("bank.csv", "Date,Amount\nbad,also-bad\n")
	id := c.batchID(resp)
	if rec := c.setMapping(id, map[string]any{"accountId": account, "dateColumn": 0, "amountColumn": 1}); rec.Code != http.StatusOK {
		t.Fatalf("set mapping status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if rec := c.do(http.MethodPost, "/api/imports/"+id+"/commit", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("commit status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}

	sum := 0
	if err := e.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM transactions WHERE user_id = $1", c.userID).Scan(&sum); err != nil || sum != 0 {
		t.Fatalf("transactions after failed commit = %d (%v), want 0", sum, err)
	}
}

func TestCrossUserAccessDenied(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("owner@example.com")
	intruder := e.signUp("intruder@example.com")

	id := c.batchID(c.upload("bank.csv", "Date,Amount\n2026-08-01,-1.00\n"))
	paths := map[string]string{
		http.MethodGet: "/api/imports/" + id,
	}
	for method, path := range paths {
		if rec := intruder.do(method, path, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s as intruder = %d, want 404", method, path, rec.Code)
		}
	}
	if rec := intruder.do(http.MethodGet, "/api/imports/"+id+"/preview", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("intruder preview = %d, want 404", rec.Code)
	}
	if rec := intruder.do(http.MethodPost, "/api/imports/"+id+"/commit", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("intruder commit = %d, want 404", rec.Code)
	}
}
