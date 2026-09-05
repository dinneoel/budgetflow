package users

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"budgetflow/internal/auth"
)

// Handler exposes account deletion over HTTP.
type Handler struct {
	svc           *Service
	log           *slog.Logger
	secureCookies bool
}

func NewHandler(svc *Service, log *slog.Logger, secureCookies bool) *Handler {
	return &Handler{svc: svc, log: log, secureCookies: secureCookies}
}

// Mount attaches the account routes to an authenticated router group.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/account", func(r chi.Router) {
		r.Post("/delete", h.Delete)
	})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if err := h.svc.DeleteAccount(r.Context(), user, req.Password); err != nil {
		if errors.Is(err, ErrReauthFailed) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		h.log.Error("account deletion failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	// The session rows are already gone with the cascade; clear the cookie so
	// the browser stops sending the dead token.
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
