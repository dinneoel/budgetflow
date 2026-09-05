-- Full-export queries. Unlike the feature list queries these include archived
-- and soft-deleted rows: a data export must contain everything the user owns.

-- name: ExportTransactionsByUser :many
SELECT * FROM transactions WHERE user_id = $1 ORDER BY date, created_at;

-- name: ExportSplitsByUser :many
SELECT * FROM transaction_splits WHERE user_id = $1 ORDER BY created_at;

-- name: ExportTransactionTagsByUser :many
SELECT tt.transaction_id, t.name
FROM transaction_tags tt
JOIN tags t ON t.id = tt.tag_id
WHERE tt.user_id = $1
ORDER BY tt.transaction_id, t.name;

-- name: ExportAllocationsByUser :many
SELECT a.*, p.year, p.month
FROM budget_allocations a
JOIN budget_periods p ON p.id = a.period_id
WHERE a.user_id = $1
ORDER BY p.year, p.month, a.created_at;

-- name: ExportRecurringRulesByUser :many
SELECT * FROM recurring_rules WHERE user_id = $1 ORDER BY created_at;

-- name: ExportGoalsByUser :many
SELECT * FROM goals WHERE user_id = $1 ORDER BY created_at;

-- name: ExportGoalContributionsByUser :many
SELECT * FROM goal_contributions WHERE user_id = $1 ORDER BY contributed_on, created_at;

-- name: ExportNotificationsByUser :many
SELECT * FROM notifications WHERE user_id = $1 ORDER BY created_at;

-- name: ExportAuditEventsByUser :many
SELECT * FROM audit_events WHERE user_id = $1 ORDER BY created_at;
