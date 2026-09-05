-- name: CreateTransaction :one
INSERT INTO transactions (user_id, account_id, category_id, type, status, amount, date, payee, notes, transfer_pair_id, recurring_rule_id, import_batch_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetTransaction :one
SELECT * FROM transactions WHERE id = $1 AND user_id = $2;

-- name: ListTransactionsByUser :many
SELECT * FROM transactions
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY date DESC, created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateTransaction :one
UPDATE transactions
SET account_id = $3, category_id = $4, type = $5, status = $6, amount = $7, date = $8, payee = $9, notes = $10, reviewed = $11, updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteTransaction :exec
UPDATE transactions SET deleted_at = now(), updated_at = now() WHERE id = $1 AND user_id = $2;

-- name: RestoreTransaction :exec
UPDATE transactions SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND user_id = $2;

-- name: CreateTransactionSplit :one
INSERT INTO transaction_splits (user_id, transaction_id, category_id, amount, memo)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListSplitsByTransaction :many
SELECT * FROM transaction_splits WHERE transaction_id = $1 AND user_id = $2 ORDER BY created_at;

-- name: DeleteSplitsByTransaction :exec
DELETE FROM transaction_splits WHERE transaction_id = $1 AND user_id = $2;

-- name: CreateTag :one
INSERT INTO tags (user_id, name) VALUES ($1, $2)
ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: ListTagsByUser :many
SELECT * FROM tags WHERE user_id = $1 ORDER BY name;

-- name: TagTransaction :exec
INSERT INTO transaction_tags (transaction_id, tag_id, user_id)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: UntagTransaction :exec
DELETE FROM transaction_tags WHERE transaction_id = $1 AND tag_id = $2;
