package notifications

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budgetflow/internal/auth"
	"budgetflow/internal/db"
)

// Handler exposes the notification service and engine over HTTP.
type Handler struct {
	svc    *Service
	engine *Engine
	log    *slog.Logger
}

func NewHandler(svc *Service, engine *Engine, log *slog.Logger) *Handler {
	return &Handler{svc: svc, engine: engine, log: log}
}

// Mount attaches notification routes to an authenticated router group.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/notifications", func(r chi.Router) {
		r.Get("/", h.List)
		r.Get("/unread-count", h.UnreadCount)
		r.Post("/read-all", h.MarkAllRead)
		r.Post("/{notificationID}/read", h.MarkRead)
		r.Get("/preferences", h.Preferences)
		r.Put("/preferences", h.SetPreference)
		r.Post("/evaluate", h.Evaluate)
	})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	limit := queryInt(r, "limit")
	offset := queryInt(r, "offset")
	page, err := h.svc.List(r.Context(), user.ID, limit, offset)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(page.Notifications))
	for _, n := range page.Notifications {
		out = append(out, notificationJSON(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": out,
		"unreadCount":   page.UnreadCount,
		"limit":         page.Limit,
		"offset":        page.Offset,
	})
}

func (h *Handler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	n, err := h.svc.UnreadCount(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unreadCount": n})
}

func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "notificationID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": ErrNotFound.Error()})
		return
	}
	if err := h.svc.MarkRead(r.Context(), user.ID, id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	if err := h.svc.MarkAllRead(r.Context(), user.ID); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Preferences(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	prefs, err := h.svc.Preferences(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"preferences": prefs})
}

func (h *Handler) SetPreference(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	var in PreferenceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	pref, err := h.svc.SetPreference(r.Context(), user.ID, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"preference": pref})
}

// Evaluate runs the scheduled evaluator (and the current-month category
// checks) for the signed-in user, then returns the fresh unread count. A
// production deployment can call this from a ticker; clients call it on
// demand, e.g. when opening the notification center.
func (h *Handler) Evaluate(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	now := time.Now().UTC()
	if err := h.engine.RunScheduled(r.Context(), user.ID, now); err != nil {
		h.writeError(w, err)
		return
	}
	if err := h.engine.CheckBudget(r.Context(), user.ID, now); err != nil {
		h.writeError(w, err)
		return
	}
	n, err := h.svc.UnreadCount(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unreadCount": n})
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	var ve ValidationError
	status := http.StatusInternalServerError
	msg := "internal error"
	switch {
	case errors.As(err, &ve):
		status, msg = http.StatusBadRequest, ve.Error()
	case errors.Is(err, ErrNotFound):
		status, msg = http.StatusNotFound, err.Error()
	default:
		h.log.Error("notifications handler error", "error", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func notificationJSON(n db.Notification) map[string]any {
	return map[string]any{
		"id":        n.ID,
		"type":      n.Type,
		"title":     n.Title,
		"body":      n.Body,
		"actionUrl": n.ActionUrl,
		"read":      n.ReadAt != nil,
		"readAt":    n.ReadAt,
		"createdAt": n.CreatedAt,
	}
}

func queryInt(r *http.Request, key string) int32 {
	v, err := strconv.ParseInt(r.URL.Query().Get(key), 10, 32)
	if err != nil {
		return 0
	}
	return int32(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
