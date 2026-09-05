package goals

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budgetflow/internal/auth"
	"budgetflow/internal/db"
)

// Handler exposes the goals service over HTTP.
type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Mount attaches goal routes to an authenticated router group.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/goals", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Route("/{goalID}", func(r chi.Router) {
			r.Get("/", h.Get)
			r.Put("/", h.Update)
			r.Post("/archive", h.Archive)
			r.Post("/unarchive", h.Unarchive)
			r.Post("/contributions", h.AddContribution)
			r.Delete("/contributions/{contributionID}", h.DeleteContribution)
		})
	})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	details, err := h.svc.List(r.Context(), user.ID, time.Now().UTC())
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(details))
	for _, d := range details {
		out = append(out, goalJSON(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"goals": out})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	var in Input
	if !h.decode(w, r, &in) {
		return
	}
	detail, err := h.svc.Create(r.Context(), user.ID, in, time.Now().UTC())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"goal": goalJSON(detail)})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	detail, err := h.svc.Get(r.Context(), user.ID, id, time.Now().UTC())
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := goalJSON(detail)
	contribs := make([]map[string]any, 0, len(detail.Contributions))
	for _, c := range detail.Contributions {
		contribs = append(contribs, contributionJSON(c))
	}
	out["contributions"] = contribs
	writeJSON(w, http.StatusOK, map[string]any{"goal": out})
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
	detail, err := h.svc.Update(r.Context(), user.ID, id, in, time.Now().UTC())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"goal": goalJSON(detail)})
}

func (h *Handler) Archive(w http.ResponseWriter, r *http.Request)   { h.setArchived(w, r, true) }
func (h *Handler) Unarchive(w http.ResponseWriter, r *http.Request) { h.setArchived(w, r, false) }

func (h *Handler) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	detail, err := h.svc.SetArchived(r.Context(), user.ID, id, archived, time.Now().UTC())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"goal": goalJSON(detail)})
}

func (h *Handler) AddContribution(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	var in ContributionInput
	if !h.decode(w, r, &in) {
		return
	}
	contrib, detail, err := h.svc.AddContribution(r.Context(), user.ID, id, in, time.Now().UTC())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"contribution": contributionJSON(contrib),
		"goal":         goalJSON(detail),
	})
}

func (h *Handler) DeleteContribution(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	contribID, err := uuid.Parse(chi.URLParam(r, "contributionID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": ErrContributionNotFound.Error()})
		return
	}
	detail, err := h.svc.DeleteContribution(r.Context(), user.ID, id, contribID, time.Now().UTC())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"goal": goalJSON(detail)})
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "goalID"))
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
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrContributionNotFound),
		errors.Is(err, ErrAccountNotFound), errors.Is(err, ErrCategoryNotFound),
		errors.Is(err, ErrTransactionNotFound):
		status, msg = http.StatusNotFound, err.Error()
	default:
		h.log.Error("goals handler error", "error", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func goalJSON(d Detail) map[string]any {
	goal, p := d.Goal, d.Progress
	var targetDate any
	if goal.TargetDate != nil {
		targetDate = goal.TargetDate.Format("2006-01-02")
	}
	return map[string]any{
		"id":                          goal.ID,
		"name":                        goal.Name,
		"type":                        goal.Type,
		"targetAmount":                goal.TargetAmount,
		"targetDate":                  targetDate,
		"categoryId":                  goal.CategoryID,
		"accountId":                   goal.AccountID,
		"archived":                    goal.ArchivedAt != nil,
		"currentBalance":              p.CurrentBalance,
		"amountRemaining":             p.AmountRemaining,
		"monthsRemaining":             p.MonthsRemaining,
		"requiredMonthlyContribution": p.RequiredMonthly,
		"behindSchedule":              p.BehindSchedule,
		"createdAt":                   goal.CreatedAt,
		"updatedAt":                   goal.UpdatedAt,
	}
}

func contributionJSON(c db.GoalContribution) map[string]any {
	return map[string]any{
		"id":            c.ID,
		"goalId":        c.GoalID,
		"transactionId": c.TransactionID,
		"amount":        c.Amount,
		"contributedOn": c.ContributedOn.Format("2006-01-02"),
		"notes":         c.Notes,
		"createdAt":     c.CreatedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
