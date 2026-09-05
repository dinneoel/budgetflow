package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"budgetflow/internal/db"
)

// SessionCookie is the name of the HttpOnly session cookie.
const SessionCookie = "bf_session"

// CSRFHeader is the request header carrying the CSRF token on mutating calls.
const CSRFHeader = "X-CSRF-Token"

// Limiter gates repeated attempts on a single key (e.g. an account email).
// Implemented by middleware.RateLimiter; declared here to avoid an import
// cycle.
type Limiter interface {
	Allow(key string) bool
}

// Handler exposes the auth service over HTTP.
type Handler struct {
	svc            *Service
	log            *slog.Logger
	secureCookies  bool
	accountLimiter Limiter
}

func NewHandler(svc *Service, log *slog.Logger, secureCookies bool, accountLimiter Limiter) *Handler {
	return &Handler{svc: svc, log: log, secureCookies: secureCookies, accountLimiter: accountLimiter}
}

// Service returns the underlying auth service, for middleware wiring.
func (h *Handler) Service() *Service { return h.svc }

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Currency string `json:"defaultCurrency"`
}

func (h *Handler) SignUp(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !h.decode(w, r, &req) {
		return
	}
	user, token, err := h.svc.SignUp(r.Context(), req.Email, req.Password, req.Name, req.Currency, r.UserAgent(), clientIP(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.setSessionCookie(w, token)
	writeJSON(w, http.StatusCreated, map[string]any{"user": userJSON(user), "csrfToken": h.svc.CSRFToken(token)})
}

func (h *Handler) SignIn(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !h.decode(w, r, &req) {
		return
	}
	if !h.accountLimiter.Allow("sign-in:" + req.Email) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many sign-in attempts, try again later"})
		return
	}
	user, token, err := h.svc.SignIn(r.Context(), req.Email, req.Password, r.UserAgent(), clientIP(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]any{"user": userJSON(user), "csrfToken": h.svc.CSRFToken(token)})
}

func (h *Handler) SignOut(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFrom(r.Context())
	if !ok {
		h.writeError(w, ErrUnauthenticated)
		return
	}
	if err := h.svc.SignOut(r.Context(), sess); err != nil {
		h.writeError(w, err)
		return
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SignOutAll(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFrom(r.Context())
	if !ok {
		h.writeError(w, ErrUnauthenticated)
		return
	}
	if err := h.svc.SignOutAll(r.Context(), user.ID); err != nil {
		h.writeError(w, err)
		return
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	token, _ := TokenFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"user": userJSON(user), "csrfToken": h.svc.CSRFToken(token)})
}

func (h *Handler) Sessions(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	current, _ := SessionFrom(r.Context())
	sessions, err := h.svc.ListSessions(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, map[string]any{
			"id":        s.ID,
			"userAgent": s.UserAgent,
			"ipAddress": s.IpAddress,
			"createdAt": s.CreatedAt,
			"expiresAt": s.ExpiresAt,
			"current":   s.ID == current.ID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFrom(r.Context())
	if !ok {
		h.writeError(w, ErrUnauthenticated)
		return
	}
	var req ProfileUpdate
	if !h.decode(w, r, &req) {
		return
	}
	updated, err := h.svc.UpdateProfile(r.Context(), user.ID, req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userJSON(updated)})
}

func (h *Handler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.svc.RequestPasswordReset(r.Context(), req.Email); err != nil {
		h.writeError(w, err)
		return
	}
	// Always 202: the response must not reveal whether the email exists.
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.svc.ResetPassword(r.Context(), req.Token, req.Password); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.svc.SessionTTL / time.Second),
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return false
	}
	return true
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	var ve ValidationError
	status := http.StatusInternalServerError
	msg := "internal error"
	switch {
	case errors.As(err, &ve):
		status, msg = http.StatusBadRequest, ve.Error()
	case errors.Is(err, ErrInvalidCredentials):
		status, msg = http.StatusUnauthorized, err.Error()
	case errors.Is(err, ErrUnauthenticated):
		status, msg = http.StatusUnauthorized, err.Error()
	case errors.Is(err, ErrAccountLocked):
		status, msg = http.StatusLocked, err.Error()
	case errors.Is(err, ErrEmailTaken):
		status, msg = http.StatusConflict, err.Error()
	case errors.Is(err, ErrInvalidToken):
		status, msg = http.StatusBadRequest, err.Error()
	default:
		h.log.Error("auth handler error", "error", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func userJSON(u db.User) map[string]any {
	return map[string]any{
		"id":              u.ID,
		"email":           u.Email,
		"name":            u.Name,
		"locale":          u.Locale,
		"timeZone":        u.TimeZone,
		"firstDayOfWeek":  u.FirstDayOfWeek,
		"defaultCurrency": u.DefaultCurrency,
		"createdAt":       u.CreatedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
