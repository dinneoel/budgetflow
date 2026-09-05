package accounts

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budgetflow/internal/auth"
)

// Handler exposes the accounts service over HTTP.
type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Mount attaches the account routes to an authenticated router group.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/accounts", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Route("/{accountID}", func(r chi.Router) {
			r.Get("/", h.Get)
			r.Put("/", h.Update)
			r.Post("/archive", h.Archive)
			r.Post("/unarchive", h.Unarchive)
			r.Post("/reconcile", h.Reconcile)
		})
	})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	accs, err := h.svc.List(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(accs))
	for _, a := range accs {
		out = append(out, accountJSON(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	var in Input
	if !h.decode(w, r, &in) {
		return
	}
	acc, err := h.svc.Create(r.Context(), user.ID, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"account": accountJSON(acc)})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.accountID(w, r)
	if !ok {
		return
	}
	acc, err := h.svc.Get(r.Context(), user.ID, id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": accountJSON(acc)})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.accountID(w, r)
	if !ok {
		return
	}
	var in Input
	if !h.decode(w, r, &in) {
		return
	}
	acc, err := h.svc.Update(r.Context(), user.ID, id, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": accountJSON(acc)})
}

func (h *Handler) Archive(w http.ResponseWriter, r *http.Request)   { h.setArchived(w, r, true) }
func (h *Handler) Unarchive(w http.ResponseWriter, r *http.Request) { h.setArchived(w, r, false) }

func (h *Handler) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.accountID(w, r)
	if !ok {
		return
	}
	acc, err := h.svc.SetArchived(r.Context(), user.ID, id, archived)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": accountJSON(acc)})
}

func (h *Handler) Reconcile(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.accountID(w, r)
	if !ok {
		return
	}
	var req struct {
		StatementBalance int64 `json:"statementBalance"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	res, err := h.svc.Reconcile(r.Context(), user.ID, id, req.StatementBalance)
	if err != nil {
		h.writeError(w, err)
		return
	}
	body := map[string]any{"account": accountJSON(res.Account), "adjustment": nil}
	if res.Adjustment != nil {
		body["adjustment"] = map[string]any{
			"id":     res.Adjustment.ID,
			"type":   res.Adjustment.Type,
			"amount": res.Adjustment.Amount,
			"date":   res.Adjustment.Date,
			"payee":  res.Adjustment.Payee,
			"notes":  res.Adjustment.Notes,
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *Handler) accountID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "accountID"))
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
	case errors.Is(err, ErrNotFound):
		status, msg = http.StatusNotFound, err.Error()
	default:
		h.log.Error("accounts handler error", "error", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func accountJSON(a AccountWithBalance) map[string]any {
	return map[string]any{
		"id":                a.ID,
		"name":              a.Name,
		"institution":       a.Institution,
		"type":              a.Type,
		"currency":          a.Currency,
		"openingBalance":    a.OpeningBalance,
		"balance":           a.Balance,
		"includeInNetWorth": a.IncludeInNetWorth,
		"archivedAt":        a.ArchivedAt,
		"createdAt":         a.CreatedAt,
		"updatedAt":         a.UpdatedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
