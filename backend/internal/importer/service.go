package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgetflow/internal/audit"
	"budgetflow/internal/db"
	"budgetflow/internal/transactions"
)

// Audit event types recorded by this package.
const (
	EventImportCommitted    = "import_committed"
	EventImportBatchDeleted = "import_batch_deleted"
)

var (
	ErrNotFound        = errors.New("import batch not found")
	ErrAccountNotFound = errors.New("account not found")
)

// ValidationError marks user-input problems that map to HTTP 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// Mapping assigns CSV columns (by index into the detected header) to
// transaction fields and fixes the target account and the date and amount
// formats. Payee, notes, and category columns are optional.
type Mapping struct {
	AccountID      uuid.UUID `json:"accountId"`
	DateColumn     *int      `json:"dateColumn"`
	AmountColumn   *int      `json:"amountColumn"`
	PayeeColumn    *int      `json:"payeeColumn"`
	NotesColumn    *int      `json:"notesColumn"`
	CategoryColumn *int      `json:"categoryColumn"`
	DateFormat     string    `json:"dateFormat"`
	AmountFormat   string    `json:"amountFormat"`
}

func (m *Mapping) validate(width int) error {
	if m.AccountID == uuid.Nil {
		return ValidationError("accountId is required")
	}
	if m.DateColumn == nil {
		return ValidationError("dateColumn is required")
	}
	if m.AmountColumn == nil {
		return ValidationError("amountColumn is required")
	}
	for name, col := range map[string]*int{
		"dateColumn": m.DateColumn, "amountColumn": m.AmountColumn,
		"payeeColumn": m.PayeeColumn, "notesColumn": m.NotesColumn, "categoryColumn": m.CategoryColumn,
	} {
		if col != nil && (*col < 0 || *col >= width) {
			return ValidationError(fmt.Sprintf("%s %d is out of range (the file has %d columns)", name, *col, width))
		}
	}
	if m.DateFormat == "" {
		m.DateFormat = "auto"
	}
	if _, ok := dateFormats[m.DateFormat]; !ok {
		return ValidationError("dateFormat must be one of: " + strings.Join(DateFormatOptions(), ", "))
	}
	if m.AmountFormat == "" {
		m.AmountFormat = "auto"
	}
	switch m.AmountFormat {
	case "auto", "dot_decimal", "comma_decimal":
	default:
		return ValidationError("amountFormat must be one of: " + strings.Join(AmountFormatOptions(), ", "))
	}
	return nil
}

// PreviewRow is one validated data row. A row with Errors is skipped on
// commit; Warnings (e.g. an unmatched category name) do not block the row.
// DuplicateOf lists existing transaction ids the row resembles, and
// DuplicateOfRow points at an identical earlier row in the same file.
type PreviewRow struct {
	Index          int         `json:"index"`
	Raw            []string    `json:"raw"`
	Date           string      `json:"date,omitempty"`
	Amount         *int64      `json:"amount,omitempty"`
	Type           string      `json:"type,omitempty"`
	Payee          string      `json:"payee"`
	Notes          string      `json:"notes"`
	CategoryName   string      `json:"categoryName,omitempty"`
	CategoryID     *uuid.UUID  `json:"categoryId,omitempty"`
	Errors         []string    `json:"errors"`
	Warnings       []string    `json:"warnings"`
	DuplicateOf    []uuid.UUID `json:"duplicateOf"`
	DuplicateOfRow *int        `json:"duplicateOfRow,omitempty"`

	date   time.Time
	amount int64
}

func (r *PreviewRow) isDuplicate() bool {
	return len(r.DuplicateOf) > 0 || r.DuplicateOfRow != nil
}

// Preview is the validated view of a pending batch under its stored mapping.
type Preview struct {
	Rows       []PreviewRow
	Valid      int
	Errored    int
	Duplicates int
}

// UploadResult reports a freshly parsed upload: the pending batch plus what
// the parser detected.
type UploadResult struct {
	Batch      db.ImportBatch
	Columns    []string
	HasHeader  bool
	RowCount   int
	SampleRows [][]string
}

// BatchDetail is a batch with its upload state (nil once committed).
type BatchDetail struct {
	Batch      db.ImportBatch
	Columns    []string
	HasHeader  bool
	RowCount   int
	SampleRows [][]string
	Mapping    *Mapping
}

// CommitInput selects how flagged duplicates are handled and allows explicit
// per-row exclusion by preview index.
type CommitInput struct {
	IncludeDuplicates bool  `json:"includeDuplicates"`
	SkipRows          []int `json:"skipRows"`
}

// CommitResult reports what a commit created and skipped.
type CommitResult struct {
	Batch             db.ImportBatch
	CreatedIDs        []uuid.UUID
	SkippedErrors     int
	SkippedDuplicates int
	SkippedManually   int
}

// DeleteResult reports a batch undo: the batch and the removed transactions.
type DeleteResult struct {
	Batch                 db.ImportBatch
	DeletedTransactionIDs []uuid.UUID
}

// Service implements the import flow. It holds the pool because commit and
// batch delete are multi-statement transactions.
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: db.New(pool)}
}

// Upload parses the file and stores it as a pending batch awaiting mapping.
func (s *Service) Upload(ctx context.Context, userID uuid.UUID, fileName string, data []byte) (UploadResult, error) {
	if len(data) > MaxUploadBytes {
		return UploadResult{}, ValidationError("the file is too large (limit 5 MB)")
	}
	pf, err := parseCSV(data)
	if err != nil {
		return UploadResult{}, err
	}

	headerJSON, err := json.Marshal(pf.Header)
	if err != nil {
		return UploadResult{}, fmt.Errorf("marshal header: %w", err)
	}
	rowsJSON, err := json.Marshal(pf.Rows)
	if err != nil {
		return UploadResult{}, fmt.Errorf("marshal rows: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UploadResult{}, fmt.Errorf("begin upload: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	batch, err := q.CreateImportBatch(ctx, db.CreateImportBatchParams{
		UserID: userID, FileName: strings.TrimSpace(fileName), Status: "pending", RowCount: int32(len(pf.Rows)),
	})
	if err != nil {
		return UploadResult{}, fmt.Errorf("create import batch: %w", err)
	}
	if err := q.CreateImportUploadData(ctx, db.CreateImportUploadDataParams{
		BatchID: batch.ID, UserID: userID, Header: headerJSON, HasHeader: pf.HasHeader, Rows: rowsJSON,
	}); err != nil {
		return UploadResult{}, fmt.Errorf("store upload data: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return UploadResult{}, fmt.Errorf("commit upload: %w", err)
	}
	return UploadResult{
		Batch: batch, Columns: pf.Header, HasHeader: pf.HasHeader,
		RowCount: len(pf.Rows), SampleRows: sample(pf.Rows),
	}, nil
}

// List returns all of the user's import batches, newest first.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]db.ImportBatch, error) {
	batches, err := s.q.ListImportBatchesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list import batches: %w", err)
	}
	if batches == nil {
		batches = []db.ImportBatch{}
	}
	return batches, nil
}

// Get returns one batch with its upload state while it is still pending.
func (s *Service) Get(ctx context.Context, userID, batchID uuid.UUID) (BatchDetail, error) {
	batch, err := s.getBatch(ctx, userID, batchID)
	if err != nil {
		return BatchDetail{}, err
	}
	out := BatchDetail{Batch: batch}
	upload, err := s.q.GetImportUploadData(ctx, db.GetImportUploadDataParams{BatchID: batchID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return BatchDetail{}, fmt.Errorf("load upload data: %w", err)
	}
	header, rows, mapping, err := decodeUpload(upload)
	if err != nil {
		return BatchDetail{}, err
	}
	out.Columns, out.HasHeader, out.RowCount, out.SampleRows, out.Mapping =
		header, upload.HasHeader, len(rows), sample(rows), mapping
	return out, nil
}

// SetMapping validates and stores the column mapping for a pending batch.
func (s *Service) SetMapping(ctx context.Context, userID, batchID uuid.UUID, m Mapping) (BatchDetail, error) {
	batch, err := s.getBatch(ctx, userID, batchID)
	if err != nil {
		return BatchDetail{}, err
	}
	if batch.Status != "pending" {
		return BatchDetail{}, ValidationError("only a pending batch can be mapped")
	}
	upload, err := s.getUpload(ctx, userID, batchID)
	if err != nil {
		return BatchDetail{}, err
	}
	header, _, _, err := decodeUpload(upload)
	if err != nil {
		return BatchDetail{}, err
	}
	if err := m.validate(len(header)); err != nil {
		return BatchDetail{}, err
	}
	if _, err := s.q.GetAccount(ctx, db.GetAccountParams{ID: m.AccountID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BatchDetail{}, ErrAccountNotFound
		}
		return BatchDetail{}, fmt.Errorf("load account: %w", err)
	}
	mappingJSON, err := json.Marshal(m)
	if err != nil {
		return BatchDetail{}, fmt.Errorf("marshal mapping: %w", err)
	}
	if err := s.q.SetImportUploadMapping(ctx, db.SetImportUploadMappingParams{
		BatchID: batchID, UserID: userID, Mapping: mappingJSON,
	}); err != nil {
		return BatchDetail{}, fmt.Errorf("store mapping: %w", err)
	}
	return s.Get(ctx, userID, batchID)
}

// Preview validates every row of a pending, mapped batch and flags
// duplicates against existing transactions and within the file.
func (s *Service) Preview(ctx context.Context, userID, batchID uuid.UUID) (Preview, error) {
	batch, err := s.getBatch(ctx, userID, batchID)
	if err != nil {
		return Preview{}, err
	}
	if batch.Status != "pending" {
		return Preview{}, ValidationError("only a pending batch can be previewed")
	}
	upload, err := s.getUpload(ctx, userID, batchID)
	if err != nil {
		return Preview{}, err
	}
	_, rows, mapping, err := decodeUpload(upload)
	if err != nil {
		return Preview{}, err
	}
	if mapping == nil {
		return Preview{}, ValidationError("submit a column mapping before previewing")
	}
	return s.buildPreview(ctx, userID, rows, *mapping)
}

// Commit atomically creates the batch's transactions from the current
// mapping, skipping rows with errors, rows in SkipRows, and — unless
// IncludeDuplicates is set — rows flagged as duplicates.
func (s *Service) Commit(ctx context.Context, userID, batchID uuid.UUID, in CommitInput) (CommitResult, error) {
	batch, err := s.getBatch(ctx, userID, batchID)
	if err != nil {
		return CommitResult{}, err
	}
	if batch.Status != "pending" {
		return CommitResult{}, ValidationError("only a pending batch can be committed")
	}
	upload, err := s.getUpload(ctx, userID, batchID)
	if err != nil {
		return CommitResult{}, err
	}
	_, rows, mapping, err := decodeUpload(upload)
	if err != nil {
		return CommitResult{}, err
	}
	if mapping == nil {
		return CommitResult{}, ValidationError("submit a column mapping before committing")
	}
	preview, err := s.buildPreview(ctx, userID, rows, *mapping)
	if err != nil {
		return CommitResult{}, err
	}

	skip := make(map[int]bool, len(in.SkipRows))
	for _, i := range in.SkipRows {
		skip[i] = true
	}
	res := CommitResult{CreatedIDs: []uuid.UUID{}}
	var toCreate []PreviewRow
	for _, row := range preview.Rows {
		switch {
		case skip[row.Index]:
			res.SkippedManually++
		case len(row.Errors) > 0:
			res.SkippedErrors++
		case row.isDuplicate() && !in.IncludeDuplicates:
			res.SkippedDuplicates++
		default:
			toCreate = append(toCreate, row)
		}
	}
	if len(toCreate) == 0 {
		return CommitResult{}, ValidationError("no importable rows: every row was skipped as an error or duplicate")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CommitResult{}, fmt.Errorf("begin import commit: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	for _, row := range toCreate {
		txn, err := q.CreateTransaction(ctx, db.CreateTransactionParams{
			UserID: userID, AccountID: mapping.AccountID, CategoryID: row.CategoryID,
			Type: row.Type, Status: "uncleared", Amount: row.amount,
			Date: row.date, Payee: row.Payee, Notes: row.Notes, ImportBatchID: &batchID,
		})
		if err != nil {
			return CommitResult{}, fmt.Errorf("create imported transaction: %w", err)
		}
		res.CreatedIDs = append(res.CreatedIDs, txn.ID)
	}
	committed, err := q.CommitImportBatch(ctx, db.CommitImportBatchParams{
		ID: batchID, UserID: userID, AccountID: &mapping.AccountID, RowCount: int32(len(res.CreatedIDs)),
	})
	if err != nil {
		return CommitResult{}, fmt.Errorf("commit import batch: %w", err)
	}
	if err := q.DeleteImportUploadData(ctx, db.DeleteImportUploadDataParams{BatchID: batchID, UserID: userID}); err != nil {
		return CommitResult{}, fmt.Errorf("drop upload data: %w", err)
	}
	if err := audit.Record(ctx, q, &userID, EventImportCommitted, "import_batch", []uuid.UUID{batchID}, map[string]any{
		"file_name": batch.FileName, "account_id": mapping.AccountID,
		"created": len(res.CreatedIDs), "skipped_errors": res.SkippedErrors,
		"skipped_duplicates": res.SkippedDuplicates, "skipped_manually": res.SkippedManually,
		"transaction_ids": res.CreatedIDs,
	}); err != nil {
		return CommitResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommitResult{}, fmt.Errorf("commit import: %w", err)
	}
	res.Batch = committed
	return res, nil
}

// Delete undoes a batch: it removes the batch's transactions (none exist for
// a still-pending batch), marks the batch deleted, and records one audit
// event with the removed transaction ids.
func (s *Service) Delete(ctx context.Context, userID, batchID uuid.UUID) (DeleteResult, error) {
	batch, err := s.getBatch(ctx, userID, batchID)
	if err != nil {
		return DeleteResult{}, err
	}
	if batch.Status == "deleted" {
		return DeleteResult{}, ValidationError("the batch is already deleted")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("begin batch delete: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	ids, err := q.ListTransactionIDsByImportBatch(ctx, db.ListTransactionIDsByImportBatchParams{ImportBatchID: &batchID, UserID: userID})
	if err != nil {
		return DeleteResult{}, fmt.Errorf("list batch transactions: %w", err)
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	if err := q.DeleteTransactionsByImportBatch(ctx, db.DeleteTransactionsByImportBatchParams{ImportBatchID: &batchID, UserID: userID}); err != nil {
		return DeleteResult{}, fmt.Errorf("delete batch transactions: %w", err)
	}
	if err := q.DeleteImportUploadData(ctx, db.DeleteImportUploadDataParams{BatchID: batchID, UserID: userID}); err != nil {
		return DeleteResult{}, fmt.Errorf("drop upload data: %w", err)
	}
	deleted, err := q.MarkImportBatchDeleted(ctx, db.MarkImportBatchDeletedParams{ID: batchID, UserID: userID})
	if err != nil {
		return DeleteResult{}, fmt.Errorf("mark batch deleted: %w", err)
	}
	if err := audit.Record(ctx, q, &userID, EventImportBatchDeleted, "import_batch", []uuid.UUID{batchID}, map[string]any{
		"file_name": batch.FileName, "was_status": batch.Status, "transaction_ids": ids,
	}); err != nil {
		return DeleteResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeleteResult{}, fmt.Errorf("commit batch delete: %w", err)
	}
	return DeleteResult{Batch: deleted, DeletedTransactionIDs: ids}, nil
}

// buildPreview parses and validates every row under the mapping, resolves
// category names, and flags duplicates.
func (s *Service) buildPreview(ctx context.Context, userID uuid.UUID, rows [][]string, m Mapping) (Preview, error) {
	cats, err := s.q.ListCategoriesByUser(ctx, userID)
	if err != nil {
		return Preview{}, fmt.Errorf("list categories: %w", err)
	}
	catByName := make(map[string]uuid.UUID, len(cats))
	for _, c := range cats {
		if c.ArchivedAt == nil {
			catByName[strings.ToLower(strings.TrimSpace(c.Name))] = c.ID
		}
	}

	out := Preview{Rows: make([]PreviewRow, 0, len(rows))}
	var minDate, maxDate time.Time
	for i, raw := range rows {
		row := PreviewRow{
			Index: i, Raw: raw, Errors: []string{}, Warnings: []string{}, DuplicateOf: []uuid.UUID{},
		}
		if d, err := parseDate(cell(raw, *m.DateColumn), m.DateFormat); err != nil {
			row.Errors = append(row.Errors, err.Error())
		} else {
			row.date = d
			row.Date = d.Format("2006-01-02")
		}
		if a, err := parseAmount(cell(raw, *m.AmountColumn), m.AmountFormat); err != nil {
			row.Errors = append(row.Errors, err.Error())
		} else if a == 0 {
			row.Errors = append(row.Errors, "amount must not be zero")
		} else {
			row.amount = a
			v := a
			row.Amount = &v
			if a < 0 {
				row.Type = "expense"
			} else {
				row.Type = "income"
			}
		}
		if m.PayeeColumn != nil {
			row.Payee = cell(raw, *m.PayeeColumn)
		}
		if m.NotesColumn != nil {
			row.Notes = cell(raw, *m.NotesColumn)
		}
		if m.CategoryColumn != nil {
			row.CategoryName = cell(raw, *m.CategoryColumn)
			if row.CategoryName != "" {
				if id, ok := catByName[strings.ToLower(row.CategoryName)]; ok {
					row.CategoryID = &id
				} else {
					row.Warnings = append(row.Warnings, fmt.Sprintf("no category named %q; the row will be imported uncategorized", row.CategoryName))
				}
			}
		}
		if len(row.Errors) == 0 {
			if minDate.IsZero() || row.date.Before(minDate) {
				minDate = row.date
			}
			if maxDate.IsZero() || row.date.After(maxDate) {
				maxDate = row.date
			}
		}
		out.Rows = append(out.Rows, row)
	}

	if err := s.flagExistingDuplicates(ctx, userID, m.AccountID, out.Rows, minDate, maxDate); err != nil {
		return Preview{}, err
	}
	flagInFileDuplicates(out.Rows)

	for i := range out.Rows {
		switch {
		case len(out.Rows[i].Errors) > 0:
			out.Errored++
		case out.Rows[i].isDuplicate():
			out.Duplicates++
		default:
			out.Valid++
		}
	}
	return out, nil
}

// flagExistingDuplicates matches rows against live transactions on the target
// account: same signed amount, date within one day, similar payee — the same
// heuristic the transactions API applies on manual entry.
func (s *Service) flagExistingDuplicates(ctx context.Context, userID, accountID uuid.UUID, rows []PreviewRow, minDate, maxDate time.Time) error {
	if minDate.IsZero() {
		return nil
	}
	existing, err := s.q.ListAccountTransactionsForDedup(ctx, db.ListAccountTransactionsForDedupParams{
		UserID: userID, AccountID: accountID,
		DateFrom: minDate.AddDate(0, 0, -1), DateTo: maxDate.AddDate(0, 0, 1),
	})
	if err != nil {
		return fmt.Errorf("list dedup candidates: %w", err)
	}
	for i := range rows {
		row := &rows[i]
		if len(row.Errors) > 0 {
			continue
		}
		for _, t := range existing {
			dayDiff := row.date.Sub(t.Date).Hours() / 24
			if t.Amount == row.amount && dayDiff >= -1 && dayDiff <= 1 && transactions.SimilarPayee(row.Payee, t.Payee) {
				row.DuplicateOf = append(row.DuplicateOf, t.ID)
			}
		}
	}
	return nil
}

// flagInFileDuplicates marks repeats of the same date + amount + normalized
// payee within the file, pointing each repeat at the first occurrence.
func flagInFileDuplicates(rows []PreviewRow) {
	seen := map[string]int{}
	for i := range rows {
		row := &rows[i]
		if len(row.Errors) > 0 {
			continue
		}
		key := fmt.Sprintf("%s|%d|%s", row.Date, row.amount, transactions.NormalizePayee(row.Payee))
		if first, ok := seen[key]; ok {
			firstIdx := rows[first].Index
			row.DuplicateOfRow = &firstIdx
		} else {
			seen[key] = i
		}
	}
}

func (s *Service) getBatch(ctx context.Context, userID, batchID uuid.UUID) (db.ImportBatch, error) {
	batch, err := s.q.GetImportBatch(ctx, db.GetImportBatchParams{ID: batchID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ImportBatch{}, ErrNotFound
	}
	if err != nil {
		return db.ImportBatch{}, fmt.Errorf("load import batch: %w", err)
	}
	return batch, nil
}

func (s *Service) getUpload(ctx context.Context, userID, batchID uuid.UUID) (db.ImportUploadDatum, error) {
	upload, err := s.q.GetImportUploadData(ctx, db.GetImportUploadDataParams{BatchID: batchID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ImportUploadDatum{}, ErrNotFound
	}
	if err != nil {
		return db.ImportUploadDatum{}, fmt.Errorf("load upload data: %w", err)
	}
	return upload, nil
}

func decodeUpload(u db.ImportUploadDatum) (header []string, rows [][]string, mapping *Mapping, err error) {
	if err = json.Unmarshal(u.Header, &header); err != nil {
		return nil, nil, nil, fmt.Errorf("decode stored header: %w", err)
	}
	if err = json.Unmarshal(u.Rows, &rows); err != nil {
		return nil, nil, nil, fmt.Errorf("decode stored rows: %w", err)
	}
	if len(u.Mapping) > 0 {
		mapping = &Mapping{}
		if err = json.Unmarshal(u.Mapping, mapping); err != nil {
			return nil, nil, nil, fmt.Errorf("decode stored mapping: %w", err)
		}
	}
	return header, rows, mapping, nil
}

func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func sample(rows [][]string) [][]string {
	if len(rows) > sampleRowCount {
		rows = rows[:sampleRowCount]
	}
	return rows
}
