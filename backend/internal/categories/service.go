// Package categories implements category groups and categories: CRUD,
// reordering, archiving, merging (reassigning transactions and allocations),
// and seeding the default set for new budgets.
package categories

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgetflow/internal/audit"
	"budgetflow/internal/db"
)

// Audit event types recorded by this package.
const (
	EventGroupCreated        = "category_group_created"
	EventGroupUpdated        = "category_group_updated"
	EventGroupArchived       = "category_group_archived"
	EventGroupUnarchived     = "category_group_unarchived"
	EventGroupDeleted        = "category_group_deleted"
	EventGroupsReordered     = "category_groups_reordered"
	EventCategoryCreated     = "category_created"
	EventCategoryUpdated     = "category_updated"
	EventCategoryArchived    = "category_archived"
	EventCategoryUnarchived  = "category_unarchived"
	EventCategoryDeleted     = "category_deleted"
	EventCategoriesReordered = "categories_reordered"
	EventCategoriesMerged    = "categories_merged"
	EventDefaultsSeeded      = "category_defaults_seeded"
)

var (
	ErrNotFound      = errors.New("category not found")
	ErrGroupNotFound = errors.New("category group not found")
	ErrAlreadySeeded = errors.New("categories already exist")
)

// ValidationError marks user-input problems that map to HTTP 400.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

var validBudgetTypes = map[string]bool{
	"fixed": true, "variable": true, "sinking_fund": true, "debt": true, "savings_goal": true,
}

var validRolloverRules = map[string]bool{
	"none": true, "rollover": true, "reset_to_target": true,
}

// GroupInput carries the client-editable group fields.
type GroupInput struct {
	Name string `json:"name"`
}

func (in *GroupInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return ValidationError("group name is required")
	}
	if len(in.Name) > 100 {
		return ValidationError("group name must be at most 100 characters")
	}
	return nil
}

// CategoryInput carries the client-editable category fields.
type CategoryInput struct {
	GroupID      uuid.UUID `json:"groupId"`
	Name         string    `json:"name"`
	Icon         string    `json:"icon"`
	Color        string    `json:"color"`
	BudgetType   string    `json:"budgetType"`
	RolloverRule string    `json:"rolloverRule"`
}

func (in *CategoryInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return ValidationError("category name is required")
	}
	if len(in.Name) > 100 {
		return ValidationError("category name must be at most 100 characters")
	}
	if in.GroupID == uuid.Nil {
		return ValidationError("groupId is required")
	}
	if in.BudgetType == "" {
		in.BudgetType = "variable"
	}
	if !validBudgetTypes[in.BudgetType] {
		return ValidationError("budgetType must be one of: fixed, variable, sinking_fund, debt, savings_goal")
	}
	if in.RolloverRule == "" {
		in.RolloverRule = "none"
	}
	if !validRolloverRules[in.RolloverRule] {
		return ValidationError("rolloverRule must be one of: none, rollover, reset_to_target")
	}
	if len(in.Icon) > 64 {
		return ValidationError("icon must be at most 64 characters")
	}
	if len(in.Color) > 32 {
		return ValidationError("color must be at most 32 characters")
	}
	return nil
}

// GroupWithCategories is one node of the category tree returned by List.
type GroupWithCategories struct {
	db.CategoryGroup
	Categories []db.Category
}

// MergeResult reports what a merge moved from the source to the target.
type MergeResult struct {
	Target                db.Category
	TransactionsMoved     int64
	SplitsMoved           int64
	AllocationsCombined   int64
	AllocationsReassigned int64
}

// Service implements category business logic. It holds the pool (not just the
// queries) because merge, reorder, and seeding are multi-statement and must
// run in one database transaction.
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: db.New(pool)}
}

// List returns all groups with their categories nested, ordered by sort_order.
// Archived entities are included; clients filter on archivedAt.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]GroupWithCategories, error) {
	groups, err := s.q.ListCategoryGroupsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list category groups: %w", err)
	}
	cats, err := s.q.ListCategoriesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	byGroup := make(map[uuid.UUID][]db.Category, len(groups))
	for _, c := range cats {
		byGroup[c.GroupID] = append(byGroup[c.GroupID], c)
	}
	out := make([]GroupWithCategories, 0, len(groups))
	for _, g := range groups {
		cs := byGroup[g.ID]
		if cs == nil {
			cs = []db.Category{}
		}
		out = append(out, GroupWithCategories{CategoryGroup: g, Categories: cs})
	}
	return out, nil
}

func (s *Service) CreateGroup(ctx context.Context, userID uuid.UUID, in GroupInput) (db.CategoryGroup, error) {
	if err := in.validate(); err != nil {
		return db.CategoryGroup{}, err
	}
	order, err := s.q.NextCategoryGroupSortOrder(ctx, userID)
	if err != nil {
		return db.CategoryGroup{}, fmt.Errorf("next group sort order: %w", err)
	}
	g, err := s.q.CreateCategoryGroup(ctx, db.CreateCategoryGroupParams{UserID: userID, Name: in.Name, SortOrder: order})
	if err != nil {
		return db.CategoryGroup{}, fmt.Errorf("create category group: %w", err)
	}
	if err := audit.Record(ctx, s.q, &userID, EventGroupCreated, "category_group", []uuid.UUID{g.ID}, in); err != nil {
		return db.CategoryGroup{}, err
	}
	return g, nil
}

func (s *Service) UpdateGroup(ctx context.Context, userID, groupID uuid.UUID, in GroupInput) (db.CategoryGroup, error) {
	if err := in.validate(); err != nil {
		return db.CategoryGroup{}, err
	}
	current, err := s.q.GetCategoryGroup(ctx, db.GetCategoryGroupParams{ID: groupID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.CategoryGroup{}, ErrGroupNotFound
	}
	if err != nil {
		return db.CategoryGroup{}, fmt.Errorf("load category group: %w", err)
	}
	g, err := s.q.UpdateCategoryGroup(ctx, db.UpdateCategoryGroupParams{
		ID: groupID, UserID: userID, Name: in.Name, SortOrder: current.SortOrder,
	})
	if err != nil {
		return db.CategoryGroup{}, fmt.Errorf("update category group: %w", err)
	}
	if err := audit.Record(ctx, s.q, &userID, EventGroupUpdated, "category_group", []uuid.UUID{g.ID}, in); err != nil {
		return db.CategoryGroup{}, err
	}
	return g, nil
}

// DeleteGroup removes an empty group. Groups that still contain categories
// cannot be deleted: archive them, or move/merge their categories first.
func (s *Service) DeleteGroup(ctx context.Context, userID, groupID uuid.UUID) error {
	if _, err := s.q.GetCategoryGroup(ctx, db.GetCategoryGroupParams{ID: groupID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrGroupNotFound
		}
		return fmt.Errorf("load category group: %w", err)
	}
	n, err := s.q.CountCategoriesInGroup(ctx, db.CountCategoriesInGroupParams{GroupID: groupID, UserID: userID})
	if err != nil {
		return fmt.Errorf("count categories in group: %w", err)
	}
	if n > 0 {
		return ValidationError("group still contains categories; move or delete them first")
	}
	if err := s.q.DeleteCategoryGroup(ctx, db.DeleteCategoryGroupParams{ID: groupID, UserID: userID}); err != nil {
		return fmt.Errorf("delete category group: %w", err)
	}
	return audit.Record(ctx, s.q, &userID, EventGroupDeleted, "category_group", []uuid.UUID{groupID}, nil)
}

func (s *Service) SetGroupArchived(ctx context.Context, userID, groupID uuid.UUID, archived bool) (db.CategoryGroup, error) {
	at := archivedAt(archived)
	g, err := s.q.SetCategoryGroupArchived(ctx, db.SetCategoryGroupArchivedParams{ID: groupID, UserID: userID, ArchivedAt: at})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.CategoryGroup{}, ErrGroupNotFound
	}
	if err != nil {
		return db.CategoryGroup{}, fmt.Errorf("set group archived: %w", err)
	}
	event := EventGroupUnarchived
	if archived {
		event = EventGroupArchived
	}
	if err := audit.Record(ctx, s.q, &userID, event, "category_group", []uuid.UUID{g.ID}, nil); err != nil {
		return db.CategoryGroup{}, err
	}
	return g, nil
}

// ReorderGroups persists the given order: sort_order becomes the position in
// ids. Every group of the user must be owned by them; unknown ids fail the
// whole operation.
func (s *Service) ReorderGroups(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) error {
	return s.reorder(ctx, userID, ids, EventGroupsReordered, "category_group", func(q *db.Queries, id uuid.UUID, pos int32) (int64, error) {
		return q.SetCategoryGroupSortOrder(ctx, db.SetCategoryGroupSortOrderParams{ID: id, UserID: userID, SortOrder: pos})
	}, ErrGroupNotFound)
}

// ReorderCategories persists the given category order across all groups.
func (s *Service) ReorderCategories(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) error {
	return s.reorder(ctx, userID, ids, EventCategoriesReordered, "category", func(q *db.Queries, id uuid.UUID, pos int32) (int64, error) {
		return q.SetCategorySortOrder(ctx, db.SetCategorySortOrderParams{ID: id, UserID: userID, SortOrder: pos})
	}, ErrNotFound)
}

func (s *Service) reorder(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, event, entityType string,
	set func(*db.Queries, uuid.UUID, int32) (int64, error), notFound error) error {
	if len(ids) == 0 {
		return ValidationError("ids is required")
	}
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return ValidationError("ids contains duplicates")
		}
		seen[id] = true
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin reorder: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)
	for i, id := range ids {
		n, err := set(q, id, int32(i))
		if err != nil {
			return fmt.Errorf("set sort order: %w", err)
		}
		if n == 0 {
			return notFound
		}
	}
	if err := audit.Record(ctx, q, &userID, event, entityType, ids, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) CreateCategory(ctx context.Context, userID uuid.UUID, in CategoryInput) (db.Category, error) {
	if err := in.validate(); err != nil {
		return db.Category{}, err
	}
	if _, err := s.q.GetCategoryGroup(ctx, db.GetCategoryGroupParams{ID: in.GroupID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Category{}, ErrGroupNotFound
		}
		return db.Category{}, fmt.Errorf("load category group: %w", err)
	}
	order, err := s.q.NextCategorySortOrder(ctx, db.NextCategorySortOrderParams{UserID: userID, GroupID: in.GroupID})
	if err != nil {
		return db.Category{}, fmt.Errorf("next category sort order: %w", err)
	}
	c, err := s.q.CreateCategory(ctx, db.CreateCategoryParams{
		UserID: userID, GroupID: in.GroupID, Name: in.Name, Icon: in.Icon, Color: in.Color,
		BudgetType: in.BudgetType, RolloverRule: in.RolloverRule, SortOrder: order,
	})
	if err != nil {
		return db.Category{}, fmt.Errorf("create category: %w", err)
	}
	if err := audit.Record(ctx, s.q, &userID, EventCategoryCreated, "category", []uuid.UUID{c.ID}, in); err != nil {
		return db.Category{}, err
	}
	return c, nil
}

func (s *Service) UpdateCategory(ctx context.Context, userID, categoryID uuid.UUID, in CategoryInput) (db.Category, error) {
	if err := in.validate(); err != nil {
		return db.Category{}, err
	}
	current, err := s.q.GetCategory(ctx, db.GetCategoryParams{ID: categoryID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Category{}, ErrNotFound
	}
	if err != nil {
		return db.Category{}, fmt.Errorf("load category: %w", err)
	}
	if in.GroupID != current.GroupID {
		if _, err := s.q.GetCategoryGroup(ctx, db.GetCategoryGroupParams{ID: in.GroupID, UserID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return db.Category{}, ErrGroupNotFound
			}
			return db.Category{}, fmt.Errorf("load category group: %w", err)
		}
	}
	c, err := s.q.UpdateCategory(ctx, db.UpdateCategoryParams{
		ID: categoryID, UserID: userID, GroupID: in.GroupID, Name: in.Name, Icon: in.Icon,
		Color: in.Color, BudgetType: in.BudgetType, RolloverRule: in.RolloverRule, SortOrder: current.SortOrder,
	})
	if err != nil {
		return db.Category{}, fmt.Errorf("update category: %w", err)
	}
	if err := audit.Record(ctx, s.q, &userID, EventCategoryUpdated, "category", []uuid.UUID{c.ID}, in); err != nil {
		return db.Category{}, err
	}
	return c, nil
}

// DeleteCategory removes a category that no transaction or split references.
// Used categories must be archived or merged instead, so history stays intact.
func (s *Service) DeleteCategory(ctx context.Context, userID, categoryID uuid.UUID) error {
	if _, err := s.q.GetCategory(ctx, db.GetCategoryParams{ID: categoryID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("load category: %w", err)
	}
	txs, err := s.q.CountCategoryTransactions(ctx, db.CountCategoryTransactionsParams{CategoryID: &categoryID, UserID: userID})
	if err != nil {
		return fmt.Errorf("count category transactions: %w", err)
	}
	splits, err := s.q.CountCategorySplits(ctx, db.CountCategorySplitsParams{CategoryID: categoryID, UserID: userID})
	if err != nil {
		return fmt.Errorf("count category splits: %w", err)
	}
	if txs+splits > 0 {
		return ValidationError("category has transactions; archive or merge it instead")
	}
	if err := s.q.DeleteCategory(ctx, db.DeleteCategoryParams{ID: categoryID, UserID: userID}); err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	return audit.Record(ctx, s.q, &userID, EventCategoryDeleted, "category", []uuid.UUID{categoryID}, nil)
}

func (s *Service) SetCategoryArchived(ctx context.Context, userID, categoryID uuid.UUID, archived bool) (db.Category, error) {
	at := archivedAt(archived)
	c, err := s.q.SetCategoryArchived(ctx, db.SetCategoryArchivedParams{ID: categoryID, UserID: userID, ArchivedAt: at})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Category{}, ErrNotFound
	}
	if err != nil {
		return db.Category{}, fmt.Errorf("set category archived: %w", err)
	}
	event := EventCategoryUnarchived
	if archived {
		event = EventCategoryArchived
	}
	if err := audit.Record(ctx, s.q, &userID, event, "category", []uuid.UUID{c.ID}, nil); err != nil {
		return db.Category{}, err
	}
	return c, nil
}

// Merge moves everything that references the source category — transactions,
// splits, budget allocations (summed where the target is already funded),
// allocation history, recurring rules, and goals — onto the target category,
// then deletes the source. Runs in one database transaction.
func (s *Service) Merge(ctx context.Context, userID, sourceID, targetID uuid.UUID) (MergeResult, error) {
	if sourceID == targetID {
		return MergeResult{}, ValidationError("cannot merge a category into itself")
	}
	if _, err := s.q.GetCategory(ctx, db.GetCategoryParams{ID: sourceID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MergeResult{}, ErrNotFound
		}
		return MergeResult{}, fmt.Errorf("load source category: %w", err)
	}
	target, err := s.q.GetCategory(ctx, db.GetCategoryParams{ID: targetID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return MergeResult{}, ErrNotFound
	}
	if err != nil {
		return MergeResult{}, fmt.Errorf("load target category: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MergeResult{}, fmt.Errorf("begin merge: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	res := MergeResult{Target: target}
	args := struct{ user, source, target uuid.UUID }{userID, sourceID, targetID}
	if res.TransactionsMoved, err = q.MergeReassignTransactions(ctx, db.MergeReassignTransactionsParams{
		TargetID: &args.target, SourceID: &args.source, UserID: args.user,
	}); err != nil {
		return MergeResult{}, fmt.Errorf("reassign transactions: %w", err)
	}
	if res.SplitsMoved, err = q.MergeReassignSplits(ctx, db.MergeReassignSplitsParams{
		TargetID: args.target, SourceID: args.source, UserID: args.user,
	}); err != nil {
		return MergeResult{}, fmt.Errorf("reassign splits: %w", err)
	}
	if res.AllocationsCombined, err = q.MergeCombineAllocations(ctx, db.MergeCombineAllocationsParams{
		UserID: args.user, SourceID: args.source, TargetID: args.target,
	}); err != nil {
		return MergeResult{}, fmt.Errorf("combine allocations: %w", err)
	}
	if _, err = q.MergeDeleteCombinedAllocations(ctx, db.MergeDeleteCombinedAllocationsParams{
		SourceID: args.source, UserID: args.user, TargetID: args.target,
	}); err != nil {
		return MergeResult{}, fmt.Errorf("delete combined allocations: %w", err)
	}
	if res.AllocationsReassigned, err = q.MergeReassignAllocations(ctx, db.MergeReassignAllocationsParams{
		TargetID: args.target, SourceID: args.source, UserID: args.user,
	}); err != nil {
		return MergeResult{}, fmt.Errorf("reassign allocations: %w", err)
	}
	if _, err = q.MergeReassignAllocationHistory(ctx, db.MergeReassignAllocationHistoryParams{
		TargetID: &args.target, SourceID: &args.source, UserID: args.user,
	}); err != nil {
		return MergeResult{}, fmt.Errorf("reassign allocation history: %w", err)
	}
	if _, err = q.MergeReassignRecurringRules(ctx, db.MergeReassignRecurringRulesParams{
		TargetID: args.target, SourceID: args.source, UserID: args.user,
	}); err != nil {
		return MergeResult{}, fmt.Errorf("reassign recurring rules: %w", err)
	}
	if _, err = q.MergeReassignGoals(ctx, db.MergeReassignGoalsParams{
		TargetID: &args.target, SourceID: &args.source, UserID: args.user,
	}); err != nil {
		return MergeResult{}, fmt.Errorf("reassign goals: %w", err)
	}
	if err = q.DeleteCategory(ctx, db.DeleteCategoryParams{ID: sourceID, UserID: userID}); err != nil {
		return MergeResult{}, fmt.Errorf("delete source category: %w", err)
	}
	if err = audit.Record(ctx, q, &userID, EventCategoriesMerged, "category", []uuid.UUID{sourceID, targetID}, map[string]any{
		"source_id":              sourceID,
		"target_id":              targetID,
		"transactions_moved":     res.TransactionsMoved,
		"splits_moved":           res.SplitsMoved,
		"allocations_combined":   res.AllocationsCombined,
		"allocations_reassigned": res.AllocationsReassigned,
	}); err != nil {
		return MergeResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return MergeResult{}, fmt.Errorf("commit merge: %w", err)
	}
	return res, nil
}

// SeedDefaults creates the default group/category set for a user with no
// groups yet. Called via the explicit endpoint and by first budget creation.
func (s *Service) SeedDefaults(ctx context.Context, userID uuid.UUID) ([]GroupWithCategories, error) {
	n, err := s.q.CountCategoryGroupsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("count category groups: %w", err)
	}
	if n > 0 {
		return nil, ErrAlreadySeeded
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin seed: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	q := s.q.WithTx(tx)

	created := make([]uuid.UUID, 0)
	for gi, dg := range DefaultGroups {
		g, err := q.CreateCategoryGroup(ctx, db.CreateCategoryGroupParams{UserID: userID, Name: dg.Name, SortOrder: int32(gi)})
		if err != nil {
			return nil, fmt.Errorf("seed group %q: %w", dg.Name, err)
		}
		created = append(created, g.ID)
		for ci, dc := range dg.Categories {
			c, err := q.CreateCategory(ctx, db.CreateCategoryParams{
				UserID: userID, GroupID: g.ID, Name: dc.Name, Icon: dc.Icon, Color: dc.Color,
				BudgetType: dc.BudgetType, RolloverRule: dc.RolloverRule, SortOrder: int32(ci),
			})
			if err != nil {
				return nil, fmt.Errorf("seed category %q: %w", dc.Name, err)
			}
			created = append(created, c.ID)
		}
	}
	if err := audit.Record(ctx, q, &userID, EventDefaultsSeeded, "category_group", created, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit seed: %w", err)
	}
	return s.List(ctx, userID)
}

func archivedAt(archived bool) *time.Time {
	if !archived {
		return nil
	}
	now := time.Now()
	return &now
}
