// Package httpserver wires the chi router and HTTP server lifecycle.
package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"budgetflow/internal/config"
)

// Server wraps the HTTP server and its dependencies.
type Server struct {
	cfg  config.Config
	log  *slog.Logger
	http *http.Server
}

// New builds a Server with the API router mounted.
func New(cfg config.Config, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, log: log}
	s.http = &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           s.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Router returns the chi router with all routes mounted. Exposed separately
// so tests can exercise handlers without binding a port.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
	})

	return r
}

// ListenAndServe runs the server until ctx is cancelled, then shuts down
// gracefully within the configured timeout.
func (s *Server) ListenAndServe(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("server listening", "addr", s.http.Addr, "env", s.cfg.Env)
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
	defer cancel()
	s.log.Info("shutting down")
	return s.http.Shutdown(shutdownCtx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
