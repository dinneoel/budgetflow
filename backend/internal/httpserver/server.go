// Package httpserver wires the chi router and HTTP server lifecycle.
package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgetflow/internal/accounts"
	"budgetflow/internal/auth"
	"budgetflow/internal/budgets"
	"budgetflow/internal/categories"
	"budgetflow/internal/config"
	"budgetflow/internal/db"
	appmw "budgetflow/internal/httpserver/middleware"
)

// Server wraps the HTTP server and its dependencies.
type Server struct {
	cfg    config.Config
	log    *slog.Logger
	pool   *pgxpool.Pool
	mailer auth.Mailer
	http   *http.Server
}

// Option customizes Server construction (used by tests to inject fakes).
type Option func(*Server)

// WithMailer overrides the default log-only mailer.
func WithMailer(m auth.Mailer) Option {
	return func(s *Server) { s.mailer = m }
}

// New builds a Server with the API router mounted. pool may be nil for
// handlers that need no database (health checks only).
func New(cfg config.Config, log *slog.Logger, pool *pgxpool.Pool, opts ...Option) *Server {
	s := &Server{cfg: cfg, log: log, pool: pool, mailer: auth.LogMailer{Log: log}}
	for _, opt := range opts {
		opt(s)
	}
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
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(appmw.RequestLogger(s.log))
	r.Use(chimw.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		if s.pool != nil {
			s.mountAuth(r)
		}
	})

	return r
}

func (s *Server) mountAuth(r chi.Router) {
	svc := auth.NewService(db.New(s.pool), s.mailer, s.log, s.cfg.SessionSecret)
	h := auth.NewHandler(svc, s.log, s.cfg.Env == "prod", appmw.NewRateLimiter(20, time.Minute))
	ipLimiter := appmw.NewRateLimiter(60, time.Minute)

	r.Route("/auth", func(r chi.Router) {
		r.Use(appmw.RateLimitByIP(ipLimiter))
		r.Post("/sign-up", h.SignUp)
		r.Post("/sign-in", h.SignIn)
		r.Post("/password-reset/request", h.RequestPasswordReset)
		r.Post("/password-reset/confirm", h.ConfirmPasswordReset)
		r.Group(func(r chi.Router) {
			r.Use(appmw.Authenticate(svc), appmw.CSRF(svc))
			r.Get("/me", h.Me)
			r.Get("/sessions", h.Sessions)
			r.Post("/sign-out", h.SignOut)
			r.Post("/sign-out-all", h.SignOutAll)
		})
	})
	r.Group(func(r chi.Router) {
		r.Use(appmw.Authenticate(svc), appmw.CSRF(svc))
		r.Put("/profile", h.UpdateProfile)
		accounts.NewHandler(accounts.NewService(db.New(s.pool)), s.log).Mount(r)
		categories.NewHandler(categories.NewService(s.pool), s.log).Mount(r)
		budgets.NewHandler(budgets.NewService(s.pool), s.log).Mount(r)
	})
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
