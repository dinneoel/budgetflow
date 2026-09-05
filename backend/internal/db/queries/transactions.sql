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

-- ListTransactions applies every optional filter; NULL means "not filtered".
-- Amount filters and amount search compare magnitudes (abs) so users can type
-- positive numbers regardless of ledger sign. Category filter matches the
-- transaction's own category or any of its split categories.
-- name: ListTransactions :many
SELECT sqlc.embed(t), count(*) OVER ()::bigint AS total_count
FROM transactions t
WHERE t.user_id = sqlc.arg('user_id')
  AND (CASE WHEN sqlc.arg('deleted')::bool THEN t.deleted_at IS NOT NULL ELSE t.deleted_at IS NULL END)
  AND (sqlc.narg('account_id')::uuid IS NULL OR t.account_id = sqlc.narg('account_id'))
  AND (sqlc.narg('category_id')::uuid IS NULL
       OR t.category_id = sqlc.narg('category_id')
       OR EXISTS (SELECT 1 FROM transaction_splits s
                  WHERE s.transaction_id = t.id AND s.category_id = sqlc.narg('category_id')))
  AND (sqlc.narg('date_from')::date IS NULL OR t.date >= sqlc.narg('date_from'))
  AND (sqlc.narg('date_to')::date IS NULL OR t.date <= sqlc.narg('date_to'))
  AND (sqlc.narg('type')::text IS NULL OR t.type = sqlc.narg('type'))
  AND (sqlc.narg('status')::text IS NULL OR t.status = sqlc.narg('status'))
  AND (sqlc.narg('reviewed')::bool IS NULL OR t.reviewed = sqlc.narg('reviewed'))
  AND (sqlc.narg('payee')::text IS NULL OR t.payee ILIKE '%' || sqlc.narg('payee') || '%')
  AND (sqlc.narg('amount_min')::bigint IS NULL OR abs(t.amount) >= sqlc.narg('amount_min'))
  AND (sqlc.narg('amount_max')::bigint IS NULL OR abs(t.amount) <= sqlc.narg('amount_max'))
  AND (sqlc.narg('tag')::text IS NULL
       OR EXISTS (SELECT 1 FROM transaction_tags tt JOIN tags g ON g.id = tt.tag_id
                  WHERE tt.transaction_id = t.id AND g.name = sqlc.narg('tag')))
  AND ((sqlc.narg('search')::text IS NULL AND sqlc.narg('search_amount')::bigint IS NULL)
       OR t.payee ILIKE '%' || sqlc.narg('search') || '%'
       OR t.notes ILIKE '%' || sqlc.narg('search') || '%'
       OR (sqlc.narg('search_amount')::bigint IS NOT NULL AND abs(t.amount) = sqlc.narg('search_amount')))
ORDER BY t.date DESC, t.created_at DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateTransaction :one
UPDATE transactions
SET account_id = $3, category_id = $4, type = $5, status = $6, amount = $7, date = $8, payee = $9, notes = $10, reviewed = $11, updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: SetTransferPair :exec
UPDATE transactions SET transfer_pair_id = $3 WHERE id = $1 AND user_id = $2;

-- Soft delete / restore act on the transaction and its transfer pair (if any)
-- so a transfer never ends up with one live and one deleted leg.
-- name: SoftDeleteTransaction :exec
UPDATE transactions SET deleted_at = now(), updated_at = now()
WHERE user_id = $2 AND (id = $1 OR transfer_pair_id = $1) AND deleted_at IS NULL;

-- name: RestoreTransaction :exec
UPDATE transactions SET deleted_at = NULL, updated_at = now()
WHERE user_id = $2 AND (id = $1 OR transfer_pair_id = $1) AND deleted_at IS NOT NULL;

-- Candidates for duplicate detection: same account, same signed amount, date
-- within the window. Payee similarity is decided in Go.
-- name: FindDuplicateCandidates :many
SELECT * FROM transactions
WHERE user_id = $1 AND account_id = $2 AND amount = $3
  AND date BETWEEN sqlc.arg('date_from') AND sqlc.arg('date_to')
  AND deleted_at IS NULL AND id <> sqlc.arg('exclude_id')
ORDER BY date, created_at;

-- name: FilterTransactionIDs :many
SELECT id FROM transactions
WHERE user_id = $1 AND id = ANY(sqlc.arg('ids')::uuid[]) AND deleted_at IS NULL;

-- name: BulkSetCategory :many
UPDATE transactions SET category_id = sqlc.arg('category_id'), updated_at = now()
WHERE user_id = sqlc.arg('user_id') AND id = ANY(sqlc.arg('ids')::uuid[]) AND deleted_at IS NULL AND type <> 'transfer'
RETURNING id;

-- name: BulkSetReviewed :many
UPDATE transactions SET reviewed = sqlc.arg('reviewed'), updated_at = now()
WHERE user_id = sqlc.arg('user_id') AND id = ANY(sqlc.arg('ids')::uuid[]) AND deleted_at IS NULL
RETURNING id;

-- name: BulkSoftDelete :many
UPDATE transactions SET deleted_at = now(), updated_at = now()
WHERE user_id = $1 AND (id = ANY(sqlc.arg('ids')::uuid[]) OR transfer_pair_id = ANY(sqlc.arg('ids')::uuid[])) AND deleted_at IS NULL
RETURNING id;

-- name: BulkTag :exec
INSERT INTO transaction_tags (transaction_id, tag_id, user_id)
SELECT t.id, sqlc.arg('tag_id'), sqlc.arg('user_id') FROM transactions t
WHERE t.user_id = sqlc.arg('user_id') AND t.id = ANY(sqlc.arg('ids')::uuid[]) AND t.deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- name: CreateTransactionSplit :one
INSERT INTO transaction_splits (user_id, transaction_id, category_id, amount, memo)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListSplitsByTransaction :many
SELECT * FROM transaction_splits WHERE transaction_id = $1 AND user_id = $2 ORDER BY created_at;

-- name: ListSplitsByTransactions :many
SELECT * FROM transaction_splits
WHERE user_id = $1 AND transaction_id = ANY(sqlc.arg('ids')::uuid[])
ORDER BY created_at;

-- name: DeleteSplitsByTransaction :exec
DELETE FROM transaction_splits WHERE transaction_id = $1 AND user_id = $2;

-- name: DeleteSplitsByTransactions :exec
DELETE FROM transaction_splits WHERE user_id = $1 AND transaction_id = ANY(sqlc.arg('ids')::uuid[]);

-- name: CreateTag :one
INSERT INTO tags (user_id, name) VALUES ($1, $2)
ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: ListTagsByUser :many
SELECT * FROM tags WHERE user_id = $1 ORDER BY name;

-- name: ListTagsByTransactions :many
SELECT tt.transaction_id, g.id, g.name
FROM transaction_tags tt
JOIN tags g ON g.id = tt.tag_id
WHERE tt.user_id = $1 AND tt.transaction_id = ANY(sqlc.arg('ids')::uuid[])
ORDER BY g.name;

-- name: TagTransaction :exec
INSERT INTO transaction_tags (transaction_id, tag_id, user_id)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: UntagTransaction :exec
DELETE FROM transaction_tags WHERE transaction_id = $1 AND tag_id = $2;

-- name: DeleteTagsByTransaction :exec
DELETE FROM transaction_tags WHERE transaction_id = $1 AND user_id = $2;
