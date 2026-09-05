-- name: CreateAccount :one
INSERT INTO accounts (user_id, name, institution, type, currency, opening_balance, include_in_net_worth)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = $1 AND user_id = $2;

-- name: ListAccountsByUser :many
SELECT * FROM accounts WHERE user_id = $1 ORDER BY created_at;

-- name: UpdateAccount :one
UPDATE accounts
SET name = $3, institution = $4, type = $5, currency = $6, opening_balance = $7, include_in_net_worth = $8, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: SetAccountArchived :one
UPDATE accounts SET archived_at = $3, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: GetAccountBalance :one
SELECT (a.opening_balance + COALESCE(sum(t.amount), 0))::bigint AS balance
FROM accounts a
LEFT JOIN transactions t ON t.account_id = a.id AND t.deleted_at IS NULL
WHERE a.id = $1 AND a.user_id = $2
GROUP BY a.opening_balance;
