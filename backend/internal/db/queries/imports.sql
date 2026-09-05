-- name: CreateImportBatch :one
INSERT INTO import_batches (user_id, account_id, file_name, status, row_count)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetImportBatch :one
SELECT * FROM import_batches WHERE id = $1 AND user_id = $2;

-- name: ListImportBatchesByUser :many
SELECT * FROM import_batches WHERE user_id = $1 ORDER BY created_at DESC;

-- name: SetImportBatchStatus :one
UPDATE import_batches SET status = $3, committed_at = $4, row_count = $5
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteTransactionsByImportBatch :exec
DELETE FROM transactions WHERE import_batch_id = $1 AND user_id = $2;

-- name: CommitImportBatch :one
UPDATE import_batches
SET account_id = $3, status = 'committed', committed_at = now(), row_count = $4
WHERE id = $1 AND user_id = $2 AND status = 'pending'
RETURNING *;

-- name: MarkImportBatchDeleted :one
UPDATE import_batches SET status = 'deleted'
WHERE id = $1 AND user_id = $2 AND status <> 'deleted'
RETURNING *;

-- name: ListTransactionIDsByImportBatch :many
SELECT id FROM transactions WHERE import_batch_id = $1 AND user_id = $2;

-- name: CreateImportUploadData :exec
INSERT INTO import_upload_data (batch_id, user_id, header, has_header, rows)
VALUES ($1, $2, $3, $4, $5);

-- name: GetImportUploadData :one
SELECT * FROM import_upload_data WHERE batch_id = $1 AND user_id = $2;

-- name: SetImportUploadMapping :exec
UPDATE import_upload_data SET mapping = $3 WHERE batch_id = $1 AND user_id = $2;

-- name: DeleteImportUploadData :exec
DELETE FROM import_upload_data WHERE batch_id = $1 AND user_id = $2;

-- Live transactions on one account within a date window, for duplicate
-- detection during import preview (matched in memory by amount/date/payee).
-- name: ListAccountTransactionsForDedup :many
SELECT id, amount, date, payee FROM transactions
WHERE user_id = $1 AND account_id = $2 AND deleted_at IS NULL
  AND date BETWEEN sqlc.arg('date_from') AND sqlc.arg('date_to');
