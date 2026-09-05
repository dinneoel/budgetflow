package exporter

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"budgetflow/internal/auth"
	"budgetflow/internal/budgets"
	"budgetflow/internal/transactions"
)

// Handler exposes the export service over HTTP.
type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Mount attaches export routes to an authenticated router group.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/export", func(r chi.Router) {
		r.Get("/transactions.csv", h.Transactions)
		r.Get("/budget.csv", h.Budget)
		r.Get("/categories.csv", h.Categories)
		r.Get("/goals.csv", h.Goals)
		r.Get("/all.zip", h.Full)
	})
}

func (h *Handler) Transactions(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	f, err := transactions.ParseFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := h.svc.TransactionsCSV(r.Context(), user.ID, f)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeAttachment(w, "transactions.csv", "text/csv; charset=utf-8", data)
}

func (h *Handler) Budget(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	year, err1 := strconv.Atoi(r.URL.Query().Get("year"))
	month, err2 := strconv.Atoi(r.URL.Query().Get("month"))
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "year and month query parameters are required integers")
		return
	}
	data, err := h.svc.BudgetCSV(r.Context(), user.ID, year, month)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeAttachment(w, fmt.Sprintf("budget-%04d-%02d.csv", year, month), "text/csv; charset=utf-8", data)
}

func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	data, err := h.svc.CategoriesCSV(r.Context(), user.ID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeAttachment(w, "categories.csv", "text/csv; charset=utf-8", data)
}

func (h *Handler) Goals(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	data, err := h.svc.GoalsCSV(r.Context(), user.ID, time.Now().UTC())
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeAttachment(w, "goals.csv", "text/csv; charset=utf-8", data)
}

func (h *Handler) Full(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	data, err := h.svc.FullExportZIP(r.Context(), user.ID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeAttachment(w, "budgetflow-export.zip", "application/zip", data)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	var txVE transactions.ValidationError
	switch {
	case errors.As(err, &txVE):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, budgets.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		h.log.Error("exporter handler error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeAttachment(w http.ResponseWriter, filename, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":` + strconv.Quote(msg) + `}`))
}
