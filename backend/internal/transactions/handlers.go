package transactions

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
)

// Handler exposes the transactions service over HTTP.
type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Mount attaches transaction routes to an authenticated router group.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/transactions", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Post("/transfer", h.CreateTransfer)
		r.Post("/bulk", h.Bulk)
		r.Route("/{transactionID}", func(r chi.Router) {
			r.Get("/", h.Get)
			r.Put("/", h.Update)
			r.Delete("/", h.Delete)
			r.Post("/duplicate", h.Duplicate)
			r.Post("/restore", h.Restore)
		})
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	var in Input
	if !h.decode(w, r, &in) {
		return
	}
	d, err := h.svc.Create(r.Context(), user.ID, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"transaction": detailJSON(d)})
}

func (h *Handler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	var in TransferInput
	if !h.decode(w, r, &in) {
		return
	}
	out, in2, err := h.svc.CreateTransfer(r.Context(), user.ID, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"outTransaction": detailJSON(out),
		"inTransaction":  detailJSON(in2),
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	d, err := h.svc.Get(r.Context(), user.ID, id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transaction": detailJSON(d)})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	f, err := parseFilter(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	res, err := h.svc.List(r.Context(), user.ID, f)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(res.Transactions))
	for _, d := range res.Transactions {
		out = append(out, detailJSON(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"transactions": out,
		"total":        res.Total,
		"limit":        res.Limit,
		"offset":       res.Offset,
	})
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
	d, err := h.svc.Update(r.Context(), user.ID, id, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transaction": detailJSON(d)})
}

func (h *Handler) Duplicate(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	d, err := h.svc.Duplicate(r.Context(), user.ID, id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"transaction": detailJSON(d)})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), user.ID, id); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	d, err := h.svc.Restore(r.Context(), user.ID, id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transaction": detailJSON(d)})
}

func (h *Handler) Bulk(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	var in BulkInput
	if !h.decode(w, r, &in) {
		return
	}
	res, err := h.svc.Bulk(r.Context(), user.ID, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// parseFilter reads list filters from query parameters. Unknown values fail
// fast with a validation error rather than silently returning everything.
func parseFilter(r *http.Request) (Filter, error) {
	q := r.URL.Query()
	f := Filter{}

	for name, dst := range map[string]**uuid.UUID{"accountId": &f.AccountID, "categoryId": &f.CategoryID} {
		if v := q.Get(name); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				return f, ValidationError(name + " must be a UUID")
			}
			*dst = &id
		}
	}
	for name, dst := range map[string]**time.Time{"from": &f.DateFrom, "to": &f.DateTo} {
		if v := q.Get(name); v != "" {
			d, err := time.Parse("2006-01-02", v)
			if err != nil {
				return f, ValidationError(name + " must be a YYYY-MM-DD date")
			}
			*dst = &d
		}
	}
	for name, dst := range map[string]**string{"type": &f.Type, "status": &f.Status, "payee": &f.Payee, "tag": &f.Tag, "q": &f.Search} {
		if v := q.Get(name); v != "" {
			s := v
			*dst = &s
		}
	}
	for name, dst := range map[string]**int64{"amountMin": &f.AmountMin, "amountMax": &f.AmountMax} {
		if v := q.Get(name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return f, ValidationError(name + " must be an integer amount in minor units")
			}
			*dst = &n
		}
	}
	if v := q.Get("reviewed"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return f, ValidationError("reviewed must be true or false")
		}
		f.Reviewed = &b
	}
	if v := q.Get("deleted"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return f, ValidationError("deleted must be true or false")
		}
		f.Deleted = b
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			return f, ValidationError("limit must be a positive integer")
		}
		f.Limit = int32(n)
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 0 {
			return f, ValidationError("offset must be a non-negative integer")
		}
		f.Offset = int32(n)
	}
	return f, nil
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "transactionID"))
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
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrAccountNotFound), errors.Is(err, ErrCategoryNotFound):
		status, msg = http.StatusNotFound, err.Error()
	default:
		h.log.Error("transactions handler error", "error", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func detailJSON(d Detail) map[string]any {
	t := d.Transaction
	splits := make([]map[string]any, 0, len(d.Splits))
	for _, sp := range d.Splits {
		splits = append(splits, map[string]any{
			"id":         sp.ID,
			"categoryId": sp.CategoryID,
			"amount":     sp.Amount,
			"memo":       sp.Memo,
		})
	}
	tags := make([]map[string]any, 0, len(d.Tags))
	for _, tg := range d.Tags {
		tags = append(tags, map[string]any{"id": tg.ID, "name": tg.Name})
	}
	m := map[string]any{
		"id":             t.ID,
		"accountId":      t.AccountID,
		"categoryId":     t.CategoryID,
		"type":           t.Type,
		"status":         t.Status,
		"amount":         t.Amount,
		"date":           t.Date.Format("2006-01-02"),
		"payee":          t.Payee,
		"notes":          t.Notes,
		"reviewed":       t.Reviewed,
		"transferPairId": t.TransferPairID,
		"deletedAt":      t.DeletedAt,
		"createdAt":      t.CreatedAt,
		"updatedAt":      t.UpdatedAt,
		"splits":         splits,
		"tags":           tags,
	}
	if d.DuplicateOf != nil {
		m["duplicateOf"] = d.DuplicateOf
		m["duplicateWarning"] = len(d.DuplicateOf) > 0
	}
	return m
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
