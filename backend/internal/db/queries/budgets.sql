-- name: CreateBudgetPeriod :one
INSERT INTO budget_periods (user_id, year, month, currency, planned_income, notes)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetBudgetPeriod :one
SELECT * FROM budget_periods WHERE user_id = $1 AND year = $2 AND month = $3;

-- name: GetBudgetPeriodByID :one
SELECT * FROM budget_periods WHERE id = $1 AND user_id = $2;

-- name: LatestBudgetPeriodBefore :one
SELECT * FROM budget_periods
WHERE user_id = $1 AND (year < $2 OR (year = $2 AND month < $3))
ORDER BY year DESC, month DESC
LIMIT 1;

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

-- name: GetBudgetAllocation :one
SELECT * FROM budget_allocations WHERE period_id = $1 AND category_id = $2 AND user_id = $3;

-- name: ListAllocationsWithRuleByUser :many
SELECT a.period_id, a.category_id, a.amount, a.rollover, p.year, p.month, c.rollover_rule
FROM budget_allocations a
JOIN budget_periods p ON p.id = a.period_id
JOIN categories c ON c.id = a.category_id
WHERE a.user_id = $1
ORDER BY p.year, p.month, a.created_at;

-- Net spending per category per calendar month: parent transactions that carry
-- a category plus split lines. Ledger amounts are signed (expense negative), so
-- spending = -amount. Transfers are never categorized and are excluded.
-- name: CategoryMonthlySpending :many
SELECT x.year, x.month, x.category_id, sum(x.spend)::bigint AS spending
FROM (
    SELECT extract(YEAR FROM t.date)::int AS year, extract(MONTH FROM t.date)::int AS month,
           t.category_id::uuid AS category_id, -t.amount AS spend
    FROM transactions t
    WHERE t.user_id = $1 AND t.deleted_at IS NULL AND t.category_id IS NOT NULL AND t.type <> 'transfer'
    UNION ALL
    SELECT extract(YEAR FROM t.date)::int, extract(MONTH FROM t.date)::int,
           s.category_id, -s.amount
    FROM transaction_splits s
    JOIN transactions t ON t.id = s.transaction_id
    WHERE s.user_id = $1 AND t.deleted_at IS NULL AND t.type <> 'transfer'
) x
GROUP BY x.year, x.month, x.category_id;

-- name: CreateAllocationHistory :one
INSERT INTO allocation_history (user_id, period_id, category_id, field, old_amount, new_amount)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListAllocationHistoryByPeriod :many
SELECT * FROM allocation_history WHERE period_id = $1 AND user_id = $2 ORDER BY created_at;
