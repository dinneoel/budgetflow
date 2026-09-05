// Package transactions implements the transaction ledger: create/edit/
// duplicate/soft-delete/restore for income, expense, refund, and adjustment
// transactions; transfers as paired ledger entries; category splits that must
// sum exactly to the parent amount; filtered listing with text search and
// pagination; bulk operations audited as single events; and duplicate
// detection on entry.
//
// Sign convention: amounts are stored signed in minor units. The API accepts
// positive magnitudes for income, expense, refund, and transfer and derives
// the ledger sign from the type (expense negative; income and refund
// positive; transfer negative on the source leg, positive on the destination
// leg). Adjustments accept any non-zero signed amount. A transaction with
// splits carries no category of its own — the splits are the categorization.
package transactions

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgetflow/internal/audit"
	"budgetflow/internal/db"
)

// Audit event types recorded by this package. Each bulk operation is one
// event carrying every affected transaction id.
const (
	EventBulkCategorized = "transactions_bulk_categorized"
	EventBulkTagged      = "transactions_bulk_tagged"
	EventBulkDeleted     = "transactions_bulk_deleted"
	EventBulkReviewed    = "transactions_bulk_reviewed"
)

var (
	ErrNotFound         = errors.New("transaction not found")
	ErrAccountNotFound  = errors.New("account not found")
	ErrCategoryNotFound = errors.New("category not found")
)

// ValidationError marks user-input problems that map to HTTP 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

var validTypes = map[string]bool{"income": true, "expense": true, "refund": true, "adjustment": true}
var validStatuses = map[string]bool{"uncleared": true, "cleared": true, "reconciled": true}

const maxBulkIDs = 500

// SplitInput is one category share of a split transaction. Amount is a
// positive magnitude; the stored sign follows the parent.
type SplitInput struct {
	CategoryID uuid.UUID `json:"categoryId"`
	Amount     int64     `json:"amount"`
	Memo       string    `json:"memo"`
}

// Input carries the client-editable fields for non-transfer transactions.
type Input struct {
	AccountID  uuid.UUID    `json:"accountId"`
	CategoryID *uuid.UUID   `json:"categoryId"`
	Type       string       `json:"type"`
	Status     string       `json:"status"`
	Amount     int64        `json:"amount"`
	Date       string       `json:"date"`
	Payee      string       `json:"payee"`
	Notes      string       `json:"notes"`
	Reviewed   bool         `json:"reviewed"`
	Splits     []SplitInput `json:"splits"`
	Tags       []string     `json:"tags"`

	date time.Time
}

func (in *Input) validate() error {
	if !validTypes[in.Type] {
		return ValidationError("type must be one of: income, expense, refund, adjustment")
	}
	if in.Status == "" {
		in.Status = "uncleared"
	}
	if !validStatuses[in.Status] {
		return ValidationError("status must be one of: uncleared, cleared, reconciled")
	}
	if in.Type == "adjustment" {
		if in.Amount == 0 {
			return ValidationError("adjustment amount must not be zero")
		}
	} else if in.Amount <= 0 {
		return ValidationError("amount must be a positive number of minor units; the sign is derived from the type")
	}
	d, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		return ValidationError("date must be in YYYY-MM-DD format")
	}
	in.date = d
	in.Payee = strings.TrimSpace(in.Payee)
	if len(in.Splits) == 1 {
		return ValidationError("a split transaction needs at least 2 splits")
	}
	if len(in.Splits) > 0 {
		if in.CategoryID != nil {
			return ValidationError("a split transaction is categorized by its splits; leave categoryId empty")
		}
		var sum int64
		for _, sp := range in.Splits {
			if sp.Amount <= 0 {
				return ValidationError("split amounts must be positive")
			}
			if sp.CategoryID == uuid.Nil {
				return ValidationError("every split needs a categoryId")
			}
			sum += sp.Amount
		}
		if sum != magnitude(in.Amount) {
			return ValidationError(fmt.Sprintf("split amounts must sum to the transaction amount (%d != %d)", sum, magnitude(in.Amount)))
		}
	} else if in.CategoryID == nil {
		return ValidationError("categoryId is required except for transfers")
	}
	for i, tg := range in.Tags {
		in.Tags[i] = strings.TrimSpace(tg)
		if in.Tags[i] == "" {
			return ValidationError("tags must not be empty")
		}
	}
	return nil
}

// signedAmount converts the validated input amount to the stored ledger sign.
func (in *Input) signedAmount() int64 {
	if in.Type == "expense" {
		return -in.Amount
	}
	return in.Amount
}

func magnitude(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// TransferInput carries the fields for creating a transfer pair.
type TransferInput struct {
	FromAccountID uuid.UUID `json:"fromAccountId"`
	ToAccountID   uuid.UUID `json:"toAccountId"`
	Amount        int64     `json:"amount"`
	Date          string    `json:"date"`
	Notes         string    `json:"notes"`
	Status        string    `json:"status"`

	date time.Time
}

func (in *TransferInput) validate() error {
	if in.Amount <= 0 {
		return ValidationError("transfer amount must be positive")
	}
	if in.FromAccountID == in.ToAccountID {
		return ValidationError("transfer accounts must differ")
	}
	if in.Status == "" {
		in.Status = "uncleared"
	}
	if !validStatuses[in.Status] {
		return ValidationError("status must be one of: uncleared, cleared, reconciled")
	}
	d, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		return ValidationError("date must be in YYYY-MM-DD format")
	}
	in.date = d
	return nil
}

// Tag is a tag attached to a transaction.
type Tag struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Detail is a transaction with its splits, tags, and (on entry) the ids of
// possible duplicates it resembles.
type Detail struct {
	Transaction db.Transaction
	Splits      []db.TransactionSplit
	Tags        []Tag
	DuplicateOf []uuid.UUID
}

// ListResult is one page of filtered transactions plus the total match count.
type ListResult struct {
	Transactions []Detail
	Total        int64
	Limit        int32
	Offset       int32
}

// Filter carries the optional list filters; nil pointer means "not filtered".
type Filter struct {
	AccountID  *uuid.UUID
	CategoryID *uuid.UUID
	DateFrom   *time.Time
	DateTo     *time.Time
	Type       *string
	Status     *string
	Reviewed   *bool
	Payee      *string
	AmountMin  *int64
	AmountMax  *int64
	Tag        *string
	Search     *string
	Deleted    bool
	Limit      int32
	Offset     int32
}

// BulkInput selects a bulk action over a set of transaction ids.
type BulkInput struct {
	Action     string      `json:"action"`
	IDs        []uuid.UUID `json:"ids"`
	CategoryID *uuid.UUID  `json:"categoryId"`
	Tag        string      `json:"tag"`
	Reviewed   *bool       `json:"reviewed"`
}

// BulkResult reports which transactions a bulk action touched.
type BulkResult struct {
	Action      string      `json:"action"`
	AffectedIDs []uuid.UUID `json:"affectedIds"`
}

// Service implements transaction business logic. It holds the pool because
// creates, edits, and bulk operations are multi-statement transactions.
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: db.New(pool)}
}

// Create inserts a non-transfer transaction with optional splits and tags and
// flags potential duplicates (same account and signed amount, date within one
// day, similar payee).
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input) (Detail, error) {
	if err := in.validate(); err != nil {
		return Detail{}, err
	}
	if err := s.checkRefs(ctx, userID, in); err != nil {
		return Detail{}, err
	}
	dupes, err := s.findDuplicates(ctx, userID, in.AccountID, in.signedAmount(), in.date, in.Payee, uuid.Nil)
	if err != nil {
		return Detail{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Detail{}, fmt.Errorf("begin create transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	row, err := q.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: userID, AccountID: in.AccountID, CategoryID: in.CategoryID,
		Type: in.Type, Status: in.Status, Amount: in.signedAmount(),
		Date: in.date, Payee: in.Payee, Notes: in.Notes,
	})
	if err != nil {
		return Detail{}, fmt.Errorf("create transaction: %w", err)
	}
	if err := s.writeSplitsAndTags(ctx, q, userID, row, in); err != nil {
		return Detail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, fmt.Errorf("commit create transaction: %w", err)
	}
	d, err := s.detail(ctx, row)
	d.DuplicateOf = dupes
	return d, err
}

// CreateTransfer creates the paired ledger entries for a transfer: a negative
// leg on the source account and a positive leg on the destination, linked via
// transfer_pair_id. Transfers carry no category and are excluded from
// spending analytics.
func (s *Service) CreateTransfer(ctx context.Context, userID uuid.UUID, in TransferInput) (Detail, Detail, error) {
	if err := in.validate(); err != nil {
		return Detail{}, Detail{}, err
	}
	from, err := s.q.GetAccount(ctx, db.GetAccountParams{ID: in.FromAccountID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, Detail{}, ErrAccountNotFound
	}
	if err != nil {
		return Detail{}, Detail{}, fmt.Errorf("load source account: %w", err)
	}
	to, err := s.q.GetAccount(ctx, db.GetAccountParams{ID: in.ToAccountID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, Detail{}, ErrAccountNotFound
	}
	if err != nil {
		return Detail{}, Detail{}, fmt.Errorf("load destination account: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Detail{}, Detail{}, fmt.Errorf("begin create transfer: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	outLeg, err := q.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: userID, AccountID: in.FromAccountID, Type: "transfer", Status: in.Status,
		Amount: -in.Amount, Date: in.date, Payee: "Transfer to " + to.Name, Notes: in.Notes,
	})
	if err != nil {
		return Detail{}, Detail{}, fmt.Errorf("create transfer out leg: %w", err)
	}
	inLeg, err := q.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: userID, AccountID: in.ToAccountID, Type: "transfer", Status: in.Status,
		Amount: in.Amount, Date: in.date, Payee: "Transfer from " + from.Name, Notes: in.Notes,
		TransferPairID: &outLeg.ID,
	})
	if err != nil {
		return Detail{}, Detail{}, fmt.Errorf("create transfer in leg: %w", err)
	}
	if err := q.SetTransferPair(ctx, db.SetTransferPairParams{ID: outLeg.ID, UserID: userID, TransferPairID: &inLeg.ID}); err != nil {
		return Detail{}, Detail{}, fmt.Errorf("link transfer pair: %w", err)
	}
	outLeg.TransferPairID = &inLeg.ID
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, Detail{}, fmt.Errorf("commit create transfer: %w", err)
	}
	return Detail{Transaction: outLeg, Splits: []db.TransactionSplit{}, Tags: []Tag{}},
		Detail{Transaction: inLeg, Splits: []db.TransactionSplit{}, Tags: []Tag{}}, nil
}

// Get returns one transaction (including soft-deleted) with splits and tags.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (Detail, error) {
	row, err := s.q.GetTransaction(ctx, db.GetTransactionParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, fmt.Errorf("load transaction: %w", err)
	}
	return s.detail(ctx, row)
}

// List returns one filtered, paginated page of the user's transactions with
// their splits and tags, plus the total match count.
func (s *Service) List(ctx context.Context, userID uuid.UUID, f Filter) (ListResult, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	params := db.ListTransactionsParams{
		UserID: userID, Deleted: f.Deleted,
		AccountID: f.AccountID, CategoryID: f.CategoryID,
		DateFrom: f.DateFrom, DateTo: f.DateTo,
		Type: f.Type, Status: f.Status, Reviewed: f.Reviewed,
		Payee: f.Payee, AmountMin: f.AmountMin, AmountMax: f.AmountMax,
		Tag: f.Tag, RowLimit: f.Limit, RowOffset: f.Offset,
	}
	if f.Search != nil {
		params.Search = f.Search
		if cents, ok := parseAmountSearch(*f.Search); ok {
			params.SearchAmount = &cents
		}
	}
	rows, err := s.q.ListTransactions(ctx, params)
	if err != nil {
		return ListResult{}, fmt.Errorf("list transactions: %w", err)
	}

	out := ListResult{Transactions: []Detail{}, Limit: f.Limit, Offset: f.Offset}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		out.Total = r.TotalCount
		ids = append(ids, r.Transaction.ID)
	}
	splits, tags, err := s.splitsAndTags(ctx, userID, ids)
	if err != nil {
		return ListResult{}, err
	}
	for _, r := range rows {
		out.Transactions = append(out.Transactions, Detail{
			Transaction: r.Transaction,
			Splits:      splits[r.Transaction.ID],
			Tags:        tags[r.Transaction.ID],
		})
	}
	return out, nil
}

// Update replaces the editable fields, splits, and tags of a non-transfer
// transaction. For a transfer leg only status, date, notes, amount magnitude,
// and tags may change; amount and date changes are mirrored onto the pair leg
// so the two legs always cancel out.
func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, in Input) (Detail, error) {
	existing, err := s.q.GetTransaction(ctx, db.GetTransactionParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, fmt.Errorf("load transaction: %w", err)
	}
	if existing.DeletedAt != nil {
		return Detail{}, ErrNotFound
	}
	if existing.Type == "transfer" {
		return s.updateTransferLeg(ctx, userID, existing, in)
	}
	if in.Type == "transfer" {
		return Detail{}, ValidationError("cannot change a transaction into a transfer; delete it and create a transfer instead")
	}
	if err := in.validate(); err != nil {
		return Detail{}, err
	}
	if err := s.checkRefs(ctx, userID, in); err != nil {
		return Detail{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Detail{}, fmt.Errorf("begin update transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	row, err := q.UpdateTransaction(ctx, db.UpdateTransactionParams{
		ID: id, UserID: userID, AccountID: in.AccountID, CategoryID: in.CategoryID,
		Type: in.Type, Status: in.Status, Amount: in.signedAmount(),
		Date: in.date, Payee: in.Payee, Notes: in.Notes, Reviewed: in.Reviewed,
	})
	if err != nil {
		return Detail{}, fmt.Errorf("update transaction: %w", err)
	}
	if err := q.DeleteSplitsByTransaction(ctx, db.DeleteSplitsByTransactionParams{TransactionID: id, UserID: userID}); err != nil {
		return Detail{}, fmt.Errorf("clear splits: %w", err)
	}
	if err := q.DeleteTagsByTransaction(ctx, db.DeleteTagsByTransactionParams{TransactionID: id, UserID: userID}); err != nil {
		return Detail{}, fmt.Errorf("clear tags: %w", err)
	}
	if err := s.writeSplitsAndTags(ctx, q, userID, row, in); err != nil {
		return Detail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, fmt.Errorf("commit update transaction: %w", err)
	}
	return s.detail(ctx, row)
}

func (s *Service) updateTransferLeg(ctx context.Context, userID uuid.UUID, leg db.Transaction, in Input) (Detail, error) {
	if (in.AccountID != uuid.Nil && in.AccountID != leg.AccountID) || in.CategoryID != nil || len(in.Splits) > 0 {
		return Detail{}, ValidationError("a transfer leg's accounts and category cannot be edited; delete the transfer and create a new one")
	}
	if in.Type != "" && in.Type != "transfer" {
		return Detail{}, ValidationError("a transfer cannot change type; delete it and create a new transaction")
	}
	if in.Amount <= 0 {
		return Detail{}, ValidationError("transfer amount must be positive")
	}
	if in.Status == "" {
		in.Status = "uncleared"
	}
	if !validStatuses[in.Status] {
		return Detail{}, ValidationError("status must be one of: uncleared, cleared, reconciled")
	}
	d, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		return Detail{}, ValidationError("date must be in YYYY-MM-DD format")
	}
	in.date = d

	sign := int64(1)
	if leg.Amount < 0 {
		sign = -1
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Detail{}, fmt.Errorf("begin update transfer: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	row, err := q.UpdateTransaction(ctx, db.UpdateTransactionParams{
		ID: leg.ID, UserID: userID, AccountID: leg.AccountID, CategoryID: nil,
		Type: "transfer", Status: in.Status, Amount: sign * in.Amount,
		Date: in.date, Payee: leg.Payee, Notes: in.Notes, Reviewed: in.Reviewed,
	})
	if err != nil {
		return Detail{}, fmt.Errorf("update transfer leg: %w", err)
	}
	if leg.TransferPairID != nil {
		pair, err := q.GetTransaction(ctx, db.GetTransactionParams{ID: *leg.TransferPairID, UserID: userID})
		if err != nil {
			return Detail{}, fmt.Errorf("load transfer pair: %w", err)
		}
		if _, err := q.UpdateTransaction(ctx, db.UpdateTransactionParams{
			ID: pair.ID, UserID: userID, AccountID: pair.AccountID, CategoryID: nil,
			Type: "transfer", Status: in.Status, Amount: -sign * in.Amount,
			Date: in.date, Payee: pair.Payee, Notes: in.Notes, Reviewed: pair.Reviewed,
		}); err != nil {
			return Detail{}, fmt.Errorf("mirror transfer pair: %w", err)
		}
	}
	if err := q.DeleteTagsByTransaction(ctx, db.DeleteTagsByTransactionParams{TransactionID: leg.ID, UserID: userID}); err != nil {
		return Detail{}, fmt.Errorf("clear tags: %w", err)
	}
	if err := s.writeTags(ctx, q, userID, row.ID, in.Tags); err != nil {
		return Detail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, fmt.Errorf("commit update transfer: %w", err)
	}
	return s.detail(ctx, row)
}

// Duplicate copies a non-transfer transaction (with splits and tags) as a new
// unreviewed transaction and flags the duplicates it resembles — which
// includes at least the original.
func (s *Service) Duplicate(ctx context.Context, userID, id uuid.UUID) (Detail, error) {
	src, err := s.Get(ctx, userID, id)
	if err != nil {
		return Detail{}, err
	}
	t := src.Transaction
	if t.DeletedAt != nil {
		return Detail{}, ErrNotFound
	}
	if t.Type == "transfer" {
		return Detail{}, ValidationError("a transfer cannot be duplicated; create a new transfer instead")
	}
	dupes, err := s.findDuplicates(ctx, userID, t.AccountID, t.Amount, t.Date, t.Payee, uuid.Nil)
	if err != nil {
		return Detail{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Detail{}, fmt.Errorf("begin duplicate transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	row, err := q.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: userID, AccountID: t.AccountID, CategoryID: t.CategoryID,
		Type: t.Type, Status: "uncleared", Amount: t.Amount,
		Date: t.Date, Payee: t.Payee, Notes: t.Notes,
	})
	if err != nil {
		return Detail{}, fmt.Errorf("duplicate transaction: %w", err)
	}
	for _, sp := range src.Splits {
		if _, err := q.CreateTransactionSplit(ctx, db.CreateTransactionSplitParams{
			UserID: userID, TransactionID: row.ID, CategoryID: sp.CategoryID, Amount: sp.Amount, Memo: sp.Memo,
		}); err != nil {
			return Detail{}, fmt.Errorf("duplicate split: %w", err)
		}
	}
	for _, tg := range src.Tags {
		if err := q.TagTransaction(ctx, db.TagTransactionParams{TransactionID: row.ID, TagID: tg.ID, UserID: userID}); err != nil {
			return Detail{}, fmt.Errorf("duplicate tag: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, fmt.Errorf("commit duplicate transaction: %w", err)
	}
	d, err := s.detail(ctx, row)
	d.DuplicateOf = dupes
	return d, err
}

// Delete soft-deletes a transaction; a transfer leg takes its pair with it.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	existing, err := s.q.GetTransaction(ctx, db.GetTransactionParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load transaction: %w", err)
	}
	if existing.DeletedAt != nil {
		return ErrNotFound
	}
	if err := s.q.SoftDeleteTransaction(ctx, db.SoftDeleteTransactionParams{ID: id, UserID: userID}); err != nil {
		return fmt.Errorf("soft delete transaction: %w", err)
	}
	return nil
}

// Restore un-deletes a soft-deleted transaction; a transfer leg brings its
// pair back with it.
func (s *Service) Restore(ctx context.Context, userID, id uuid.UUID) (Detail, error) {
	existing, err := s.q.GetTransaction(ctx, db.GetTransactionParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, fmt.Errorf("load transaction: %w", err)
	}
	if existing.DeletedAt == nil {
		return Detail{}, ValidationError("transaction is not deleted")
	}
	if err := s.q.RestoreTransaction(ctx, db.RestoreTransactionParams{ID: id, UserID: userID}); err != nil {
		return Detail{}, fmt.Errorf("restore transaction: %w", err)
	}
	return s.Get(ctx, userID, id)
}

// Bulk runs one bulk action (categorize, tag, delete, mark_reviewed) over a
// set of transaction ids and records it as a single audit event carrying the
// affected ids.
func (s *Service) Bulk(ctx context.Context, userID uuid.UUID, in BulkInput) (BulkResult, error) {
	if len(in.IDs) == 0 {
		return BulkResult{}, ValidationError("ids must not be empty")
	}
	if len(in.IDs) > maxBulkIDs {
		return BulkResult{}, ValidationError(fmt.Sprintf("at most %d ids per bulk operation", maxBulkIDs))
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BulkResult{}, fmt.Errorf("begin bulk %s: %w", in.Action, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	var affected []uuid.UUID
	var event string
	payload := map[string]any{}

	switch in.Action {
	case "categorize":
		if in.CategoryID == nil {
			return BulkResult{}, ValidationError("categoryId is required for bulk categorize")
		}
		if _, err := s.q.GetCategory(ctx, db.GetCategoryParams{ID: *in.CategoryID, UserID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return BulkResult{}, ErrCategoryNotFound
			}
			return BulkResult{}, fmt.Errorf("load category: %w", err)
		}
		affected, err = q.BulkSetCategory(ctx, db.BulkSetCategoryParams{UserID: userID, Ids: in.IDs, CategoryID: in.CategoryID})
		if err != nil {
			return BulkResult{}, fmt.Errorf("bulk categorize: %w", err)
		}
		// the new single category replaces any split categorization
		if err := q.DeleteSplitsByTransactions(ctx, db.DeleteSplitsByTransactionsParams{UserID: userID, Ids: affected}); err != nil {
			return BulkResult{}, fmt.Errorf("clear splits: %w", err)
		}
		event = EventBulkCategorized
		payload["category_id"] = *in.CategoryID
	case "tag":
		in.Tag = strings.TrimSpace(in.Tag)
		if in.Tag == "" {
			return BulkResult{}, ValidationError("tag is required for bulk tag")
		}
		tag, err := q.CreateTag(ctx, db.CreateTagParams{UserID: userID, Name: in.Tag})
		if err != nil {
			return BulkResult{}, fmt.Errorf("upsert tag: %w", err)
		}
		affected, err = q.FilterTransactionIDs(ctx, db.FilterTransactionIDsParams{UserID: userID, Ids: in.IDs})
		if err != nil {
			return BulkResult{}, fmt.Errorf("scope bulk tag: %w", err)
		}
		if err := q.BulkTag(ctx, db.BulkTagParams{UserID: userID, Ids: in.IDs, TagID: tag.ID}); err != nil {
			return BulkResult{}, fmt.Errorf("bulk tag: %w", err)
		}
		event = EventBulkTagged
		payload["tag"] = in.Tag
	case "delete":
		affected, err = q.BulkSoftDelete(ctx, db.BulkSoftDeleteParams{UserID: userID, Ids: in.IDs})
		if err != nil {
			return BulkResult{}, fmt.Errorf("bulk delete: %w", err)
		}
		event = EventBulkDeleted
	case "mark_reviewed":
		reviewed := true
		if in.Reviewed != nil {
			reviewed = *in.Reviewed
		}
		affected, err = q.BulkSetReviewed(ctx, db.BulkSetReviewedParams{UserID: userID, Ids: in.IDs, Reviewed: reviewed})
		if err != nil {
			return BulkResult{}, fmt.Errorf("bulk mark reviewed: %w", err)
		}
		event = EventBulkReviewed
		payload["reviewed"] = reviewed
	default:
		return BulkResult{}, ValidationError("action must be one of: categorize, tag, delete, mark_reviewed")
	}

	if affected == nil {
		affected = []uuid.UUID{}
	}
	if err := audit.Record(ctx, q, &userID, event, "transaction", affected, payload); err != nil {
		return BulkResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BulkResult{}, fmt.Errorf("commit bulk %s: %w", in.Action, err)
	}
	return BulkResult{Action: in.Action, AffectedIDs: affected}, nil
}

// checkRefs verifies the account and every referenced category belong to the
// user.
func (s *Service) checkRefs(ctx context.Context, userID uuid.UUID, in Input) error {
	if _, err := s.q.GetAccount(ctx, db.GetAccountParams{ID: in.AccountID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAccountNotFound
		}
		return fmt.Errorf("load account: %w", err)
	}
	catIDs := make([]uuid.UUID, 0, len(in.Splits)+1)
	if in.CategoryID != nil {
		catIDs = append(catIDs, *in.CategoryID)
	}
	for _, sp := range in.Splits {
		catIDs = append(catIDs, sp.CategoryID)
	}
	for _, id := range catIDs {
		if _, err := s.q.GetCategory(ctx, db.GetCategoryParams{ID: id, UserID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrCategoryNotFound
			}
			return fmt.Errorf("load category: %w", err)
		}
	}
	return nil
}

// writeSplitsAndTags inserts the input's splits (signed like the parent) and
// tag links for a freshly written transaction row.
func (s *Service) writeSplitsAndTags(ctx context.Context, q *db.Queries, userID uuid.UUID, row db.Transaction, in Input) error {
	sign := int64(1)
	if row.Amount < 0 {
		sign = -1
	}
	for _, sp := range in.Splits {
		if _, err := q.CreateTransactionSplit(ctx, db.CreateTransactionSplitParams{
			UserID: userID, TransactionID: row.ID, CategoryID: sp.CategoryID,
			Amount: sign * sp.Amount, Memo: sp.Memo,
		}); err != nil {
			return fmt.Errorf("create split: %w", err)
		}
	}
	return s.writeTags(ctx, q, userID, row.ID, in.Tags)
}

func (s *Service) writeTags(ctx context.Context, q *db.Queries, userID, transactionID uuid.UUID, tags []string) error {
	for _, name := range tags {
		tag, err := q.CreateTag(ctx, db.CreateTagParams{UserID: userID, Name: name})
		if err != nil {
			return fmt.Errorf("upsert tag: %w", err)
		}
		if err := q.TagTransaction(ctx, db.TagTransactionParams{TransactionID: transactionID, TagID: tag.ID, UserID: userID}); err != nil {
			return fmt.Errorf("tag transaction: %w", err)
		}
	}
	return nil
}

// findDuplicates returns ids of live transactions on the same account with
// the same signed amount, a date within one day, and a similar payee.
func (s *Service) findDuplicates(ctx context.Context, userID, accountID uuid.UUID, amount int64, date time.Time, payee string, exclude uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.q.FindDuplicateCandidates(ctx, db.FindDuplicateCandidatesParams{
		UserID: userID, AccountID: accountID, Amount: amount,
		DateFrom: date.AddDate(0, 0, -1), DateTo: date.AddDate(0, 0, 1),
		ExcludeID: exclude,
	})
	if err != nil {
		return nil, fmt.Errorf("find duplicate candidates: %w", err)
	}
	ids := []uuid.UUID{}
	for _, r := range rows {
		if SimilarPayee(payee, r.Payee) {
			ids = append(ids, r.ID)
		}
	}
	return ids, nil
}

// SimilarPayee normalizes both payees (lowercase, alphanumerics only) and
// treats them as similar when either contains the other. This catches
// variants like "STARBUCKS #1234" vs "Starbucks". Exported so the CSV
// importer applies the same duplicate heuristic.
func SimilarPayee(a, b string) bool {
	na, nb := NormalizePayee(a), NormalizePayee(b)
	if na == "" || nb == "" {
		return na == nb
	}
	return strings.Contains(na, nb) || strings.Contains(nb, na)
}

// NormalizePayee lowercases a payee and strips everything but alphanumerics.
func NormalizePayee(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parseAmountSearch interprets a search string as a decimal money amount and
// returns it in minor units, e.g. "12.50" -> 1250 and "13" -> 1300.
func parseAmountSearch(s string) (int64, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "-"))
	if s == "" {
		return 0, false
	}
	whole, frac, hasFrac := strings.Cut(strings.ReplaceAll(s, ",", "."), ".")
	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, false
	}
	cents := int64(0)
	if hasFrac {
		if len(frac) != 2 {
			return 0, false
		}
		cents, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, false
		}
	}
	return units*100 + cents, true
}

// splitsAndTags batch-loads splits and tags for a set of transaction ids.
func (s *Service) splitsAndTags(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]db.TransactionSplit, map[uuid.UUID][]Tag, error) {
	splits := map[uuid.UUID][]db.TransactionSplit{}
	tags := map[uuid.UUID][]Tag{}
	if len(ids) == 0 {
		return splits, tags, nil
	}
	splitRows, err := s.q.ListSplitsByTransactions(ctx, db.ListSplitsByTransactionsParams{UserID: userID, Ids: ids})
	if err != nil {
		return nil, nil, fmt.Errorf("list splits: %w", err)
	}
	for _, sp := range splitRows {
		splits[sp.TransactionID] = append(splits[sp.TransactionID], sp)
	}
	tagRows, err := s.q.ListTagsByTransactions(ctx, db.ListTagsByTransactionsParams{UserID: userID, Ids: ids})
	if err != nil {
		return nil, nil, fmt.Errorf("list tags: %w", err)
	}
	for _, tg := range tagRows {
		tags[tg.TransactionID] = append(tags[tg.TransactionID], Tag{ID: tg.ID, Name: tg.Name})
	}
	return splits, tags, nil
}

func (s *Service) detail(ctx context.Context, row db.Transaction) (Detail, error) {
	splits, tags, err := s.splitsAndTags(ctx, row.UserID, []uuid.UUID{row.ID})
	if err != nil {
		return Detail{}, err
	}
	d := Detail{Transaction: row, Splits: splits[row.ID], Tags: tags[row.ID]}
	if d.Splits == nil {
		d.Splits = []db.TransactionSplit{}
	}
	if d.Tags == nil {
		d.Tags = []Tag{}
	}
	return d, nil
}
