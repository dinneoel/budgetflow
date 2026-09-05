-- name: CreateRecurringRule :one
INSERT INTO recurring_rules (user_id, name, account_id, category_id, amount, frequency, custom_interval_days, next_due_date, reminder_lead_days)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetRecurringRule :one
SELECT * FROM recurring_rules WHERE id = $1 AND user_id = $2;

-- name: ListRecurringRulesByUser :many
SELECT * FROM recurring_rules WHERE user_id = $1 AND archived_at IS NULL ORDER BY next_due_date;

-- name: UpdateRecurringRule :one
UPDATE recurring_rules
SET name = $3, account_id = $4, category_id = $5, amount = $6, frequency = $7, custom_interval_days = $8, next_due_date = $9, reminder_lead_days = $10, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: SetRecurringRuleArchived :exec
UPDATE recurring_rules SET archived_at = $3, updated_at = now() WHERE id = $1 AND user_id = $2;

-- name: ListRulesDueBy :many
SELECT * FROM recurring_rules
WHERE user_id = $1 AND archived_at IS NULL AND next_due_date <= $2
ORDER BY next_due_date;
