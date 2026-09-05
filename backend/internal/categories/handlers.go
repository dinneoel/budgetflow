package categories

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budgetflow/internal/auth"
	"budgetflow/internal/db"
)

// Handler exposes the categories service over HTTP.
type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Mount attaches category and group routes to an authenticated router group.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/category-groups", func(r chi.Router) {
		r.Post("/", h.CreateGroup)
		r.Put("/reorder", h.ReorderGroups)
		r.Route("/{groupID}", func(r chi.Router) {
			r.Put("/", h.UpdateGroup)
			r.Delete("/", h.DeleteGroup)
			r.Post("/archive", h.archiveGroup(true))
			r.Post("/unarchive", h.archiveGroup(false))
		})
	})
	r.Route("/categories", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.CreateCategory)
		r.Put("/reorder", h.ReorderCategories)
		r.Post("/seed-defaults", h.SeedDefaults)
		r.Route("/{categoryID}", func(r chi.Router) {
			r.Put("/", h.UpdateCategory)
			r.Delete("/", h.DeleteCategory)
			r.Post("/archive", h.archiveCategory(true))
			r.Post("/unarchive", h.archiveCategory(false))
			r.Post("/merge", h.Merge)
		})
	})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	groups, err := h.svc.List(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groupsJSON(groups)})
}

func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	var in GroupInput
	if !h.decode(w, r, &in) {
		return
	}
	g, err := h.svc.CreateGroup(r.Context(), user.ID, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"group": groupJSON(g, nil)})
}

func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r, "groupID", ErrGroupNotFound)
	if !ok {
		return
	}
	var in GroupInput
	if !h.decode(w, r, &in) {
		return
	}
	g, err := h.svc.UpdateGroup(r.Context(), user.ID, id, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": groupJSON(g, nil)})
}

func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r, "groupID", ErrGroupNotFound)
	if !ok {
		return
	}
	if err := h.svc.DeleteGroup(r.Context(), user.ID, id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) archiveGroup(archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.UserFrom(r.Context())
		id, ok := h.pathID(w, r, "groupID", ErrGroupNotFound)
		if !ok {
			return
		}
		g, err := h.svc.SetGroupArchived(r.Context(), user.ID, id, archived)
		if err != nil {
			h.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"group": groupJSON(g, nil)})
	}
}

func (h *Handler) ReorderGroups(w http.ResponseWriter, r *http.Request) {
	h.reorder(w, r, h.svc.ReorderGroups)
}

func (h *Handler) ReorderCategories(w http.ResponseWriter, r *http.Request) {
	h.reorder(w, r, h.svc.ReorderCategories)
}

func (h *Handler) reorder(w http.ResponseWriter, r *http.Request,
	apply func(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) error) {
	user, _ := auth.UserFrom(r.Context())
	var req struct {
		IDs []uuid.UUID `json:"ids"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	if err := apply(r.Context(), user.ID, req.IDs); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	var in CategoryInput
	if !h.decode(w, r, &in) {
		return
	}
	c, err := h.svc.CreateCategory(r.Context(), user.ID, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"category": categoryJSON(c)})
}

func (h *Handler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r, "categoryID", ErrNotFound)
	if !ok {
		return
	}
	var in CategoryInput
	if !h.decode(w, r, &in) {
		return
	}
	c, err := h.svc.UpdateCategory(r.Context(), user.ID, id, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"category": categoryJSON(c)})
}

func (h *Handler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r, "categoryID", ErrNotFound)
	if !ok {
		return
	}
	if err := h.svc.DeleteCategory(r.Context(), user.ID, id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) archiveCategory(archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.UserFrom(r.Context())
		id, ok := h.pathID(w, r, "categoryID", ErrNotFound)
		if !ok {
			return
		}
		c, err := h.svc.SetCategoryArchived(r.Context(), user.ID, id, archived)
		if err != nil {
			h.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"category": categoryJSON(c)})
	}
}

func (h *Handler) Merge(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	sourceID, ok := h.pathID(w, r, "categoryID", ErrNotFound)
	if !ok {
		return
	}
	var req struct {
		TargetID uuid.UUID `json:"targetId"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	if req.TargetID == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "targetId is required"})
		return
	}
	res, err := h.svc.Merge(r.Context(), user.ID, sourceID, req.TargetID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"category":              categoryJSON(res.Target),
		"transactionsMoved":     res.TransactionsMoved,
		"splitsMoved":           res.SplitsMoved,
		"allocationsCombined":   res.AllocationsCombined,
		"allocationsReassigned": res.AllocationsReassigned,
	})
}

func (h *Handler) SeedDefaults(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	groups, err := h.svc.SeedDefaults(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"groups": groupsJSON(groups)})
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
	case errors.Is(err, ErrAlreadySeeded):
		status, msg = http.StatusConflict, err.Error()
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrGroupNotFound):
		status, msg = http.StatusNotFound, err.Error()
	default:
		h.log.Error("categories handler error", "error", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func groupsJSON(groups []GroupWithCategories) []map[string]any {
	out := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		out = append(out, groupJSON(g.CategoryGroup, g.Categories))
	}
	return out
}

func groupJSON(g db.CategoryGroup, cats []db.Category) map[string]any {
	m := map[string]any{
		"id":         g.ID,
		"name":       g.Name,
		"sortOrder":  g.SortOrder,
		"archivedAt": g.ArchivedAt,
		"createdAt":  g.CreatedAt,
		"updatedAt":  g.UpdatedAt,
	}
	if cats != nil {
		cs := make([]map[string]any, 0, len(cats))
		for _, c := range cats {
			cs = append(cs, categoryJSON(c))
		}
		m["categories"] = cs
	}
	return m
}

func categoryJSON(c db.Category) map[string]any {
	return map[string]any{
		"id":           c.ID,
		"groupId":      c.GroupID,
		"name":         c.Name,
		"icon":         c.Icon,
		"color":        c.Color,
		"budgetType":   c.BudgetType,
		"rolloverRule": c.RolloverRule,
		"sortOrder":    c.SortOrder,
		"archivedAt":   c.ArchivedAt,
		"createdAt":    c.CreatedAt,
		"updatedAt":    c.UpdatedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
