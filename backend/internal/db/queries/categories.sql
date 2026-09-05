-- name: CreateCategoryGroup :one
INSERT INTO category_groups (user_id, name, sort_order)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetCategoryGroup :one
SELECT * FROM category_groups WHERE id = $1 AND user_id = $2;

-- name: ListCategoryGroupsByUser :many
SELECT * FROM category_groups WHERE user_id = $1 ORDER BY sort_order, created_at;

-- name: UpdateCategoryGroup :one
UPDATE category_groups SET name = $3, sort_order = $4, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: SetCategoryGroupArchived :one
UPDATE category_groups SET archived_at = $3, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: SetCategoryGroupSortOrder :execrows
UPDATE category_groups SET sort_order = $3, updated_at = now() WHERE id = $1 AND user_id = $2;

-- name: NextCategoryGroupSortOrder :one
SELECT coalesce(max(sort_order) + 1, 0)::int FROM category_groups WHERE user_id = $1;

-- name: CountCategoryGroupsByUser :one
SELECT count(*) FROM category_groups WHERE user_id = $1;

-- name: CountCategoriesInGroup :one
SELECT count(*) FROM categories WHERE group_id = $1 AND user_id = $2;

-- name: DeleteCategoryGroup :exec
DELETE FROM category_groups WHERE id = $1 AND user_id = $2;

-- name: CreateCategory :one
INSERT INTO categories (user_id, group_id, name, icon, color, budget_type, rollover_rule, sort_order)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetCategory :one
SELECT * FROM categories WHERE id = $1 AND user_id = $2;

-- name: ListCategoriesByUser :many
SELECT * FROM categories WHERE user_id = $1 ORDER BY sort_order, created_at;

-- name: UpdateCategory :one
UPDATE categories
SET group_id = $3, name = $4, icon = $5, color = $6, budget_type = $7, rollover_rule = $8, sort_order = $9, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: SetCategoryArchived :one
UPDATE categories SET archived_at = $3, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: SetCategorySortOrder :execrows
UPDATE categories SET sort_order = $3, updated_at = now() WHERE id = $1 AND user_id = $2;

-- name: NextCategorySortOrder :one
SELECT coalesce(max(sort_order) + 1, 0)::int FROM categories WHERE user_id = $1 AND group_id = $2;

-- name: CountCategoryTransactions :one
SELECT count(*) FROM transactions WHERE category_id = $1 AND user_id = $2;

-- name: CountCategorySplits :one
SELECT count(*) FROM transaction_splits WHERE category_id = $1 AND user_id = $2;

-- name: DeleteCategory :exec
DELETE FROM categories WHERE id = $1 AND user_id = $2;

-- Merge queries: each reassigns one kind of reference from the source
-- category to the target. The service runs them inside one transaction and
-- deletes the source category afterwards.

-- name: MergeReassignTransactions :execrows
UPDATE transactions SET category_id = @target_id, updated_at = now()
WHERE category_id = @source_id AND user_id = @user_id;

-- name: MergeReassignSplits :execrows
UPDATE transaction_splits SET category_id = @target_id, updated_at = now()
WHERE category_id = @source_id AND user_id = @user_id;

-- Fold source allocations into existing target allocations for periods where
-- both categories are funded, so (period_id, category_id) stays unique.
-- name: MergeCombineAllocations :execrows
UPDATE budget_allocations AS t
SET amount = t.amount + s.amount, rollover = t.rollover + s.rollover, updated_at = now()
FROM budget_allocations AS s
WHERE t.user_id = @user_id AND s.user_id = @user_id
  AND s.category_id = @source_id AND t.category_id = @target_id
  AND t.period_id = s.period_id;

-- name: MergeDeleteCombinedAllocations :execrows
DELETE FROM budget_allocations AS s
WHERE s.category_id = @source_id AND s.user_id = @user_id
  AND EXISTS (
    SELECT 1 FROM budget_allocations AS t
    WHERE t.period_id = s.period_id AND t.category_id = @target_id AND t.user_id = @user_id
  );

-- name: MergeReassignAllocations :execrows
UPDATE budget_allocations SET category_id = @target_id, updated_at = now()
WHERE category_id = @source_id AND user_id = @user_id;

-- name: MergeReassignAllocationHistory :execrows
UPDATE allocation_history SET category_id = @target_id
WHERE category_id = @source_id AND user_id = @user_id;

-- name: MergeReassignRecurringRules :execrows
UPDATE recurring_rules SET category_id = @target_id, updated_at = now()
WHERE category_id = @source_id AND user_id = @user_id;

-- name: MergeReassignGoals :execrows
UPDATE goals SET category_id = @target_id, updated_at = now()
WHERE category_id = @source_id AND user_id = @user_id;
