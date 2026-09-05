package recurring

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

// Handler exposes the recurring service over HTTP.
type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Mount attaches recurring routes to an authenticated router group.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/recurring", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Get("/upcoming", h.Upcoming)
		r.Route("/{ruleID}", func(r chi.Router) {
			r.Get("/", h.Get)
			r.Put("/", h.Update)
			r.Post("/archive", h.Archive)
			r.Post("/unarchive", h.Unarchive)
			r.Post("/pay", h.MarkPaid)
			r.Post("/match", h.Match)
		})
	})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	rules, err := h.svc.List(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		out = append(out, ruleJSON(rule))
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": out})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	var in Input
	if !h.decode(w, r, &in) {
		return
	}
	rule, err := h.svc.Create(r.Context(), user.ID, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"rule": ruleJSON(rule)})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	rule, err := h.svc.Get(r.Context(), user.ID, id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": ruleJSON(rule)})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	var in Input
	if !h.decode(w, r, &in) {
		return
	}
	rule, err := h.svc.Update(r.Context(), user.ID, id, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": ruleJSON(rule)})
}

func (h *Handler) Archive(w http.ResponseWriter, r *http.Request)   { h.setArchived(w, r, true) }
func (h *Handler) Unarchive(w http.ResponseWriter, r *http.Request) { h.setArchived(w, r, false) }

func (h *Handler) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	rule, err := h.svc.SetArchived(r.Context(), user.ID, id, archived)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": ruleJSON(rule)})
}

func (h *Handler) MarkPaid(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	var in PayInput
	if !h.decode(w, r, &in) {
		return
	}
	res, err := h.svc.MarkPaid(r.Context(), user.ID, id, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resultJSON(res))
}

func (h *Handler) Match(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		TransactionID uuid.UUID `json:"transactionId"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	res, err := h.svc.Match(r.Context(), user.ID, id, req.TransactionID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resultJSON(res))
}

func (h *Handler) Upcoming(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 365 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "days must be between 1 and 365"})
			return
		}
		days = n
	}
	bills, err := h.svc.Upcoming(r.Context(), user.ID, days, time.Now().UTC())
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(bills))
	for _, b := range bills {
		m := ruleJSON(b.Rule)
		m["daysUntilDue"] = b.DaysUntilDue
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"upcoming": out})
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "ruleID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": ErrNotFound.Error()})
		return uuid.Nil, false
	}
	return id, true
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
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrAccountNotFound),
		errors.Is(err, ErrCategoryNotFound), errors.Is(err, ErrTransactionNotFound):
		status, msg = http.StatusNotFound, err.Error()
	default:
		h.log.Error("recurring handler error", "error", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func ruleJSON(rule db.RecurringRule) map[string]any {
	return map[string]any{
		"id":                 rule.ID,
		"name":               rule.Name,
		"accountId":          rule.AccountID,
		"categoryId":         rule.CategoryID,
		"amount":             rule.Amount,
		"frequency":          rule.Frequency,
		"customIntervalDays": rule.CustomIntervalDays,
		"nextDueDate":        rule.NextDueDate.Format("2006-01-02"),
		"reminderLeadDays":   rule.ReminderLeadDays,
		"archived":           rule.ArchivedAt != nil,
		"createdAt":          rule.CreatedAt,
		"updatedAt":          rule.UpdatedAt,
	}
}

func resultJSON(res PaidResult) map[string]any {
	return map[string]any{
		"rule": ruleJSON(res.Rule),
		"transaction": map[string]any{
			"id":              res.Transaction.ID,
			"accountId":       res.Transaction.AccountID,
			"categoryId":      res.Transaction.CategoryID,
			"type":            res.Transaction.Type,
			"status":          res.Transaction.Status,
			"amount":          res.Transaction.Amount,
			"date":            res.Transaction.Date.Format("2006-01-02"),
			"payee":           res.Transaction.Payee,
			"recurringRuleId": res.Transaction.RecurringRuleID,
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
