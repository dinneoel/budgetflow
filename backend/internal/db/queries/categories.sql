-- name: CreateCategoryGroup :one
INSERT INTO category_groups (user_id, name, sort_order)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListCategoryGroupsByUser :many
SELECT * FROM category_groups WHERE user_id = $1 ORDER BY sort_order, created_at;

-- name: UpdateCategoryGroup :one
UPDATE category_groups SET name = $3, sort_order = $4, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: SetCategoryGroupArchived :exec
UPDATE category_groups SET archived_at = $3, updated_at = now() WHERE id = $1 AND user_id = $2;

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

-- name: SetCategoryArchived :exec
UPDATE categories SET archived_at = $3, updated_at = now() WHERE id = $1 AND user_id = $2;

-- name: DeleteCategory :exec
DELETE FROM categories WHERE id = $1 AND user_id = $2;
