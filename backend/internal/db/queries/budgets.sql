-- name: CreateBudgetPeriod :one
INSERT INTO budget_periods (user_id, year, month, currency, planned_income, notes)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetBudgetPeriod :one
SELECT * FROM budget_periods WHERE user_id = $1 AND year = $2 AND month = $3;

-- name: ListBudgetPeriodsByUser :many
SELECT * FROM budget_periods WHERE user_id = $1 ORDER BY year, month;

-- name: UpdateBudgetPeriod :one
UPDATE budget_periods SET planned_income = $3, notes = $4, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: UpsertBudgetAllocation :one
INSERT INTO budget_allocations (user_id, period_id, category_id, amount, rollover)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (period_id, category_id)
DO UPDATE SET amount = EXCLUDED.amount, rollover = EXCLUDED.rollover, updated_at = now()
RETURNING *;

-- name: ListAllocationsByPeriod :many
SELECT * FROM budget_allocations WHERE period_id = $1 AND user_id = $2 ORDER BY created_at;

-- name: CreateAllocationHistory :one
INSERT INTO allocation_history (user_id, period_id, category_id, field, old_amount, new_amount)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListAllocationHistoryByPeriod :many
SELECT * FROM allocation_history WHERE period_id = $1 AND user_id = $2 ORDER BY created_at;
