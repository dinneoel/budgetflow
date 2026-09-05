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
