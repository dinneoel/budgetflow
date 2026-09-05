-- name: CreateGoal :one
INSERT INTO goals (user_id, name, type, target_amount, target_date, category_id, account_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetGoal :one
SELECT * FROM goals WHERE id = $1 AND user_id = $2;

-- name: ListGoalsByUser :many
SELECT * FROM goals WHERE user_id = $1 AND archived_at IS NULL ORDER BY created_at;

-- name: UpdateGoal :one
UPDATE goals
SET name = $3, type = $4, target_amount = $5, target_date = $6, category_id = $7, account_id = $8, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: SetGoalArchived :exec
UPDATE goals SET archived_at = $3, updated_at = now() WHERE id = $1 AND user_id = $2;

-- name: CreateGoalContribution :one
INSERT INTO goal_contributions (user_id, goal_id, transaction_id, amount, contributed_on, notes)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListContributionsByGoal :many
SELECT * FROM goal_contributions WHERE goal_id = $1 AND user_id = $2 ORDER BY contributed_on;

-- name: GetGoalBalance :one
SELECT COALESCE(sum(amount), 0)::bigint AS balance
FROM goal_contributions
WHERE goal_id = $1 AND user_id = $2;

-- name: ListGoalBalances :many
SELECT goal_id, sum(amount)::bigint AS balance
FROM goal_contributions
WHERE user_id = $1
GROUP BY goal_id;

-- name: DeleteGoalContribution :execrows
DELETE FROM goal_contributions
WHERE id = $1 AND user_id = $2 AND goal_id = $3;
