// Package middleware provides HTTP middleware: request logging, rate
// limiting, session authentication, and CSRF protection.
package middleware

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	"budgetflow/internal/auth"
)

// RequestLogger logs one line per request with method, path, status, and
// duration.
func RequestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start),
				"request_id", chimw.GetReqID(r.Context()),
			)
		})
	}
}

// RateLimiter is a fixed-window in-memory limiter keyed by arbitrary strings
// (IP addresses, account identifiers). Good enough for a single-process MVP.
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*windowCount
}

type windowCount struct {
	start time.Time
	count int
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, hits: make(map[string]*windowCount)}
}

// Allow records a hit for key and reports whether it is within the limit.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// Bound memory: drop expired windows once the map grows large.
	if len(l.hits) > 10_000 {
		for k, w := range l.hits {
			if now.Sub(w.start) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	w := l.hits[key]
	if w == nil || now.Sub(w.start) >= l.window {
		l.hits[key] = &windowCount{start: now, count: 1}
		return true
	}
	w.count++
	return w.count <= l.limit
}

// RateLimitByIP rejects requests over the per-IP limit with 429.
func RateLimitByIP(l *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				ip = host
			}
			if !l.Allow("ip:" + ip) {
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded, try again later"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Authenticate resolves the session cookie and attaches the user, session,
// and raw token to the request context; requests without a valid session get
// 401.
func Authenticate(svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(auth.SessionCookie)
			if err != nil || cookie.Value == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
				return
			}
			user, sess, err := svc.Authenticate(r.Context(), cookie.Value)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), user, sess, cookie.Value)))
		})
	}
}

// CSRF rejects mutating requests whose X-CSRF-Token header does not match the
// token derived from the authenticated session. Must run after Authenticate.
func CSRF(svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			token, ok := auth.TokenFrom(r.Context())
			provided := r.Header.Get(auth.CSRFHeader)
			if !ok || provided == "" || !svc.VerifyCSRF(token, provided) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid or missing CSRF token"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
