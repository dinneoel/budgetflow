package importer

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budgetflow/internal/auth"
	"budgetflow/internal/db"
)

// Handler exposes the import service over HTTP.
type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Mount attaches import routes to an authenticated router group.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/imports", func(r chi.Router) {
		r.Post("/", h.Upload)
		r.Get("/", h.List)
		r.Route("/{batchID}", func(r chi.Router) {
			r.Get("/", h.Get)
			r.Put("/mapping", h.SetMapping)
			r.Get("/preview", h.Preview)
			r.Post("/commit", h.Commit)
			r.Delete("/", h.Delete)
		})
	})
}

// Upload accepts the CSV either as a multipart form (field "file") or as a
// raw request body with the file name in the fileName query parameter.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes)

	var data []byte
	fileName := r.URL.Query().Get("fileName")
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(MaxUploadBytes); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read the upload (5 MB limit)"})
			return
		}
		f, fh, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": `multipart field "file" is required`})
			return
		}
		defer f.Close() //nolint:errcheck // read-only temp file
		if data, err = io.ReadAll(f); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read the upload"})
			return
		}
		fileName = fh.Filename
	} else {
		var err error
		if data, err = io.ReadAll(r.Body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read the upload (5 MB limit)"})
			return
		}
	}

	res, err := h.svc.Upload(r.Context(), user.ID, fileName, data)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"batch":         batchJSON(res.Batch),
		"columns":       res.Columns,
		"hasHeader":     res.HasHeader,
		"rowCount":      res.RowCount,
		"sampleRows":    res.SampleRows,
		"dateFormats":   DateFormatOptions(),
		"amountFormats": AmountFormatOptions(),
	})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	batches, err := h.svc.List(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(batches))
	for _, b := range batches {
		out = append(out, batchJSON(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"batches": out})
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
	out := map[string]any{"batch": batchJSON(d.Batch)}
	if d.Columns != nil {
		out["columns"] = d.Columns
		out["hasHeader"] = d.HasHeader
		out["rowCount"] = d.RowCount
		out["sampleRows"] = d.SampleRows
		out["mapping"] = d.Mapping
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) SetMapping(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	var m Mapping
	if !h.decode(w, r, &m) {
		return
	}
	d, err := h.svc.SetMapping(r.Context(), user.ID, id, m)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"batch": batchJSON(d.Batch), "mapping": d.Mapping})
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	p, err := h.svc.Preview(r.Context(), user.ID, id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rows": p.Rows, "valid": p.Valid, "errored": p.Errored, "duplicates": p.Duplicates,
	})
}

func (h *Handler) Commit(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	in := CommitInput{}
	if r.ContentLength != 0 && !h.decode(w, r, &in) {
		return
	}
	res, err := h.svc.Commit(r.Context(), user.ID, id, in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"batch":             batchJSON(res.Batch),
		"createdIds":        res.CreatedIDs,
		"skippedErrors":     res.SkippedErrors,
		"skippedDuplicates": res.SkippedDuplicates,
		"skippedManually":   res.SkippedManually,
	})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFrom(r.Context())
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	res, err := h.svc.Delete(r.Context(), user.ID, id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"batch":                 batchJSON(res.Batch),
		"deletedTransactionIds": res.DeletedTransactionIDs,
	})
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "batchID"))
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
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrAccountNotFound):
		status, msg = http.StatusNotFound, err.Error()
	default:
		h.log.Error("importer handler error", "error", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func batchJSON(b db.ImportBatch) map[string]any {
	return map[string]any{
		"id":          b.ID,
		"fileName":    b.FileName,
		"status":      b.Status,
		"rowCount":    b.RowCount,
		"accountId":   b.AccountID,
		"createdAt":   b.CreatedAt,
		"committedAt": b.CommittedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
