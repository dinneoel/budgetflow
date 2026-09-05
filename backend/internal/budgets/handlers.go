package budgets

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budgetflow/internal/auth"
	"budgetflow/internal/db"
)

// Handler exposes the budgets service over HTTP.
type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Mount attaches budget routes to an authenticated router group.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/budgets", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		// literal prefix so this cannot shadow /{periodID}/history
		r.Get("/month/{year}/{month}", h.Get)
		r.Route("/{periodID}", func(r chi.Router) {
			r.Put("/", h.Update)
			r.Get("/history", h.History)
			r.Put("/allocations/{categoryID}", h.SetAllocation)
		})
	})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	periods, err := h.svc.List(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(periods))
	for _, p := range periods {
		out = append(out, periodJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"periods": out})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	var in CreateInput
	if !h.decode(w, r, &in) {
		return
	}
	d, err := h.svc.Create(r.Context(), user.ID, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"period": detailJSON(d)})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	year, err1 := strconv.Atoi(chi.URLParam(r, "year"))
	month, err2 := strconv.Atoi(chi.URLParam(r, "month"))
	if err1 != nil || err2 != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": ErrNotFound.Error()})
		return
	}
	d, err := h.svc.Get(r.Context(), user.ID, year, month)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"period": detailJSON(d)})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r, "periodID", ErrNotFound)
	if !ok {
		return
	}
	var in UpdateInput
	if !h.decode(w, r, &in) {
		return
	}
	d, err := h.svc.Update(r.Context(), user.ID, id, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"period": detailJSON(d)})
}

func (h *Handler) SetAllocation(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	periodID, ok := h.pathID(w, r, "periodID", ErrNotFound)
	if !ok {
		return
	}
	categoryID, ok := h.pathID(w, r, "categoryID", ErrCategoryNotFound)
	if !ok {
		return
	}
	var req struct {
		Amount int64 `json:"amount"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	d, err := h.svc.SetAllocation(r.Context(), user.ID, periodID, categoryID, req.Amount)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"period": detailJSON(d)})
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r, "periodID", ErrNotFound)
	if !ok {
		return
	}
	rows, err := h.svc.History(r.Context(), user.ID, id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, e := range rows {
		out = append(out, map[string]any{
			"id":         e.ID,
			"periodId":   e.PeriodID,
			"categoryId": e.CategoryID,
			"field":      e.Field,
			"oldAmount":  e.OldAmount,
			"newAmount":  e.NewAmount,
			"createdAt":  e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": out})
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request, param string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": notFound.Error()})
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
	case errors.Is(err, ErrExists):
		status, msg = http.StatusConflict, err.Error()
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrCategoryNotFound):
		status, msg = http.StatusNotFound, err.Error()
	default:
		h.log.Error("budgets handler error", "error", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func periodJSON(p db.BudgetPeriod) map[string]any {
	return map[string]any{
		"id":            p.ID,
		"year":          p.Year,
		"month":         p.Month,
		"currency":      p.Currency,
		"plannedIncome": p.PlannedIncome,
		"notes":         p.Notes,
		"createdAt":     p.CreatedAt,
		"updatedAt":     p.UpdatedAt,
	}
}

func detailJSON(d PeriodDetail) map[string]any {
	m := periodJSON(d.Period)
	m["unallocated"] = d.Unallocated
	cats := make([]map[string]any, 0, len(d.Categories))
	for _, c := range d.Categories {
		cats = append(cats, map[string]any{
			"categoryId": c.CategoryID,
			"amount":     c.Amount,
			"rollover":   c.Rollover,
			"spending":   c.Spending,
			"reserved":   c.Reserved,
			"remaining":  c.Remaining,
			"status":     c.Status,
		})
	}
	m["categories"] = cats
	return m
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
