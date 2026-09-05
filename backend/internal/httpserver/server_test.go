package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"budgetflow/internal/config"
)

func TestHealthEndpoint(t *testing.T) {
	cfg := config.Config{Port: "0", Env: "test"}
	srv := New(cfg, slog.New(slog.DiscardHandler))

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/health status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

func TestUnknownRouteReturns404(t *testing.T) {
	cfg := config.Config{Port: "0", Env: "test"}
	srv := New(cfg, slog.New(slog.DiscardHandler))

	req := httptest.NewRequest(http.MethodGet, "/api/nope", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/nope status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
