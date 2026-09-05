// Integration tests for the auth stack: they exercise the real HTTP router
// (handlers + middleware) against a real PostgreSQL database per test.
package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"budgetflow/internal/auth"
	"budgetflow/internal/config"
	"budgetflow/internal/httpserver"
	"budgetflow/internal/testdb"
)

type captureMailer struct {
	mu     sync.Mutex
	emails []string
	tokens []string
}

func (m *captureMailer) SendPasswordReset(_ context.Context, email, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emails = append(m.emails, email)
	m.tokens = append(m.tokens, token)
	return nil
}

func (m *captureMailer) lastToken(t *testing.T) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tokens) == 0 {
		t.Fatal("no password reset email was sent")
	}
	return m.tokens[len(m.tokens)-1]
}

type testEnv struct {
	t      *testing.T
	router http.Handler
	pool   *pgxpool.Pool
	mailer *captureMailer
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	pool := testdb.New(t)
	mailer := &captureMailer{}
	cfg := config.Config{Port: "0", Env: "test", SessionSecret: "test-secret", ShutdownTimeout: time.Second}
	srv := httpserver.New(cfg, slog.New(slog.DiscardHandler), pool, httpserver.WithMailer(mailer))
	return &testEnv{t: t, router: srv.Router(), pool: pool, mailer: mailer}
}

// auditEventTypes returns every recorded audit event type, oldest first.
func (e *testEnv) auditEventTypes() []string {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(), "SELECT event_type FROM audit_events ORDER BY created_at, id")
	if err != nil {
		e.t.Fatalf("query audit_events: %v", err)
	}
	defer rows.Close()
	var types []string
	for rows.Next() {
		var et string
		if err := rows.Scan(&et); err != nil {
			e.t.Fatalf("scan audit event: %v", err)
		}
		types = append(types, et)
	}
	return types
}

func (e *testEnv) requireAudit(eventType string) {
	e.t.Helper()
	if types := e.auditEventTypes(); !slices.Contains(types, eventType) {
		e.t.Errorf("audit_events has no %q event; recorded: %v", eventType, types)
	}
}

// client is a stateful test HTTP client: it carries cookies and the CSRF
// token across requests, like a browser + SPA would.
type client struct {
	env     *testEnv
	cookies map[string]*http.Cookie
	csrf    string
}

func (e *testEnv) client() *client {
	return &client{env: e, cookies: map[string]*http.Cookie{}}
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

func (c *client) signUp(email, password string) map[string]any {
	c.env.t.Helper()
	rec := c.do(http.MethodPost, "/api/auth/sign-up", map[string]string{"email": email, "password": password})
	if rec.Code != http.StatusCreated {
		c.env.t.Fatalf("sign-up status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	resp := decode(c.env.t, rec)
	c.csrf, _ = resp["csrfToken"].(string)
	return resp
}

func (c *client) signIn(email, password string) *httptest.ResponseRecorder {
	c.env.t.Helper()
	rec := c.do(http.MethodPost, "/api/auth/sign-in", map[string]string{"email": email, "password": password})
	if rec.Code == http.StatusOK {
		resp := decode(c.env.t, rec)
		c.csrf, _ = resp["csrfToken"].(string)
	}
	return rec
}

func TestSignUpSignInSignOutFlow(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.client()

	resp := c.signUp("leo@example.com", "s3cret-password")
	user := resp["user"].(map[string]any)
	if user["email"] != "leo@example.com" {
		t.Errorf("sign-up user email = %v", user["email"])
	}
	if c.csrf == "" {
		t.Fatal("sign-up response has no csrfToken")
	}

	sess := c.cookies[auth.SessionCookie]
	if sess == nil {
		t.Fatal("sign-up did not set a session cookie")
	}
	if !sess.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}
	if sess.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie SameSite = %v, want Lax", sess.SameSite)
	}

	rec := c.do(http.MethodGet, "/api/auth/me", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /me status = %d, want 200", rec.Code)
	}
	if me := decode(t, rec)["user"].(map[string]any); me["email"] != "leo@example.com" {
		t.Errorf("me email = %v", me["email"])
	}

	if rec := c.do(http.MethodPost, "/api/auth/sign-out", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("sign-out status = %d, want 204 (body: %s)", rec.Code, rec.Body.String())
	}
	if _, ok := c.cookies[auth.SessionCookie]; ok {
		t.Error("sign-out did not clear the session cookie")
	}

	// The old token must be dead server-side, not just cookie-cleared.
	c.cookies[auth.SessionCookie] = sess
	if rec := c.do(http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after sign-out status = %d, want 401", rec.Code)
	}

	if rec := c.signIn("leo@example.com", "s3cret-password"); rec.Code != http.StatusOK {
		t.Fatalf("sign-in status = %d, want 200", rec.Code)
	}
	if rec := c.do(http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusOK {
		t.Errorf("me after sign-in status = %d, want 200", rec.Code)
	}

	for _, et := range []string{"sign_up", "sign_out", "sign_in"} {
		env.requireAudit(et)
	}
}

func TestSignUpValidationAndDuplicates(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.client()

	cases := []struct {
		name string
		body map[string]string
		want int
	}{
		{"bad email", map[string]string{"email": "not-an-email", "password": "long-enough"}, http.StatusBadRequest},
		{"short password", map[string]string{"email": "a@example.com", "password": "short"}, http.StatusBadRequest},
		{"bad currency", map[string]string{"email": "a@example.com", "password": "long-enough", "defaultCurrency": "DOLLARS"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		if rec := c.do(http.MethodPost, "/api/auth/sign-up", tc.body); rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, rec.Code, tc.want)
		}
	}

	c.signUp("taken@example.com", "s3cret-password")
	rec := env.client().do(http.MethodPost, "/api/auth/sign-up",
		map[string]string{"email": "Taken@Example.com", "password": "s3cret-password"})
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate email (case-insensitive) status = %d, want 409", rec.Code)
	}
}

func TestSignInInvalidCredentials(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	env.client().signUp("leo@example.com", "s3cret-password")

	c := env.client()
	if rec := c.signIn("leo@example.com", "wrong-password"); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong password status = %d, want 401", rec.Code)
	}
	if rec := c.signIn("nobody@example.com", "whatever-pass"); rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown email status = %d, want 401", rec.Code)
	}
}

func TestAccountLockoutAfterRepeatedFailures(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	env.client().signUp("leo@example.com", "s3cret-password")

	c := env.client()
	// Default policy locks on the 5th consecutive failure.
	for i := 1; i <= 4; i++ {
		if rec := c.signIn("leo@example.com", "wrong-password"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: status = %d, want 401", i, rec.Code)
		}
	}
	if rec := c.signIn("leo@example.com", "wrong-password"); rec.Code != http.StatusLocked {
		t.Fatalf("5th failure: status = %d, want 423", rec.Code)
	}
	// Even the correct password is rejected while locked.
	if rec := c.signIn("leo@example.com", "s3cret-password"); rec.Code != http.StatusLocked {
		t.Errorf("correct password while locked: status = %d, want 423", rec.Code)
	}
	env.requireAudit("account_locked")
}

func TestPerAccountSignInRateLimit(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.client()
	// The per-account limiter (20/min) kicks in even for a nonexistent
	// account, keyed by email; the per-IP limit (60/min) is not reached.
	var got429 bool
	for i := 0; i < 21; i++ {
		if rec := c.signIn("victim@example.com", "guess"); rec.Code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("21 sign-in attempts for one account never hit the rate limit")
	}
}

func TestCSRFRejection(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.client()
	c.signUp("leo@example.com", "s3cret-password")

	profile := map[string]any{
		"name": "Leo", "locale": "en-US", "timeZone": "UTC", "firstDayOfWeek": 1, "defaultCurrency": "USD",
	}

	goodCSRF := c.csrf
	c.csrf = ""
	if rec := c.do(http.MethodPut, "/api/profile", profile); rec.Code != http.StatusForbidden {
		t.Errorf("mutating request without CSRF token: status = %d, want 403", rec.Code)
	}
	c.csrf = "0000000000000000000000000000000000000000000000000000000000000000"
	if rec := c.do(http.MethodPut, "/api/profile", profile); rec.Code != http.StatusForbidden {
		t.Errorf("mutating request with wrong CSRF token: status = %d, want 403", rec.Code)
	}
	// GET is exempt: reads carry no CSRF risk.
	c.csrf = ""
	if rec := c.do(http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusOK {
		t.Errorf("GET without CSRF token: status = %d, want 200", rec.Code)
	}
	c.csrf = goodCSRF
	if rec := c.do(http.MethodPut, "/api/profile", profile); rec.Code != http.StatusOK {
		t.Errorf("mutating request with valid CSRF token: status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestSignOutAllInvalidatesEverySession(t *testing.T) {
	t.Parallel()
	env := newEnv(t)

	first := env.client()
	first.signUp("leo@example.com", "s3cret-password")
	second := env.client()
	if rec := second.signIn("leo@example.com", "s3cret-password"); rec.Code != http.StatusOK {
		t.Fatalf("second device sign-in status = %d", rec.Code)
	}

	rec := second.do(http.MethodGet, "/api/auth/sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sessions status = %d", rec.Code)
	}
	if sessions := decode(t, rec)["sessions"].([]any); len(sessions) != 2 {
		t.Errorf("session count = %d, want 2", len(sessions))
	}

	if rec := second.do(http.MethodPost, "/api/auth/sign-out-all", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("sign-out-all status = %d, want 204", rec.Code)
	}
	if rec := first.do(http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("first device after sign-out-all: status = %d, want 401", rec.Code)
	}
	if rec := second.do(http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("second device after sign-out-all: status = %d, want 401", rec.Code)
	}
	env.requireAudit("sign_out_all")
}

func TestPasswordResetFlow(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.client()
	c.signUp("leo@example.com", "old-password-1")

	rec := env.client().do(http.MethodPost, "/api/auth/password-reset/request", map[string]string{"email": "leo@example.com"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("reset request status = %d, want 202", rec.Code)
	}
	token := env.mailer.lastToken(t)

	rec = env.client().do(http.MethodPost, "/api/auth/password-reset/confirm",
		map[string]string{"token": token, "password": "new-password-2"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset confirm status = %d, want 204 (body: %s)", rec.Code, rec.Body.String())
	}

	// The pre-reset session must be invalidated.
	if rec := c.do(http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("old session after reset: status = %d, want 401", rec.Code)
	}
	if rec := env.client().signIn("leo@example.com", "old-password-1"); rec.Code != http.StatusUnauthorized {
		t.Errorf("old password after reset: status = %d, want 401", rec.Code)
	}
	if rec := env.client().signIn("leo@example.com", "new-password-2"); rec.Code != http.StatusOK {
		t.Errorf("new password after reset: status = %d, want 200", rec.Code)
	}

	// Tokens are single-use.
	rec = env.client().do(http.MethodPost, "/api/auth/password-reset/confirm",
		map[string]string{"token": token, "password": "another-pass-3"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("token reuse status = %d, want 400", rec.Code)
	}

	env.requireAudit("password_reset_requested")
	env.requireAudit("password_reset")
}

func TestPasswordResetUnknownEmailIsSilent(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	rec := env.client().do(http.MethodPost, "/api/auth/password-reset/request", map[string]string{"email": "ghost@example.com"})
	if rec.Code != http.StatusAccepted {
		t.Errorf("unknown email reset request status = %d, want 202 (must not reveal registration)", rec.Code)
	}
	if len(env.mailer.tokens) != 0 {
		t.Error("a reset email was sent for an unregistered address")
	}
}

func TestProfileUpdate(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.client()
	c.signUp("leo@example.com", "s3cret-password")

	rec := c.do(http.MethodPut, "/api/profile", map[string]any{
		"name": "Leonid", "locale": "uk-UA", "timeZone": "Europe/Kyiv", "firstDayOfWeek": 1, "defaultCurrency": "uah",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("profile update status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	user := decode(t, rec)["user"].(map[string]any)
	if user["name"] != "Leonid" || user["locale"] != "uk-UA" || user["timeZone"] != "Europe/Kyiv" {
		t.Errorf("updated profile = %v", user)
	}
	if user["defaultCurrency"] != "UAH" {
		t.Errorf("defaultCurrency = %v, want normalized UAH", user["defaultCurrency"])
	}

	// Persisted, not just echoed.
	me := decode(t, c.do(http.MethodGet, "/api/auth/me", nil))["user"].(map[string]any)
	if me["timeZone"] != "Europe/Kyiv" {
		t.Errorf("persisted timeZone = %v, want Europe/Kyiv", me["timeZone"])
	}

	bad := []map[string]any{
		{"name": "L", "locale": "en-US", "timeZone": "Mars/Olympus", "firstDayOfWeek": 1, "defaultCurrency": "USD"},
		{"name": "L", "locale": "en-US", "timeZone": "UTC", "firstDayOfWeek": 7, "defaultCurrency": "USD"},
		{"name": "L", "locale": "", "timeZone": "UTC", "firstDayOfWeek": 1, "defaultCurrency": "USD"},
	}
	for i, body := range bad {
		if rec := c.do(http.MethodPut, "/api/profile", body); rec.Code != http.StatusBadRequest {
			t.Errorf("invalid profile %d: status = %d, want 400", i, rec.Code)
		}
	}
	env.requireAudit("profile_updated")
}

func TestUnauthenticatedRequestsRejected(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	c := env.client()

	for _, path := range []string{"/api/auth/me", "/api/auth/sessions"} {
		if rec := c.do(http.MethodGet, path, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without session: status = %d, want 401", path, rec.Code)
		}
	}
	c.cookies[auth.SessionCookie] = &http.Cookie{Name: auth.SessionCookie, Value: "forged-garbage-token"}
	if rec := c.do(http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /me with forged cookie: status = %d, want 401", rec.Code)
	}
}
