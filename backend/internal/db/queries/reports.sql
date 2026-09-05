-- Aggregation queries for the dashboard and reports API. All aggregates are
-- scoped to accounts in a single currency (the user's budget currency) so
-- foreign-currency accounts are never silently converted, and all exclude
-- soft-deleted transactions.

-- Income and spending per calendar month. Income is the sum of income-type
-- transactions; spending is the negated sum of expense and refund transactions
-- (refunds reduce spending). Transfers and adjustments are excluded from both.
-- name: MonthlyIncomeSpending :many
SELECT extract(YEAR FROM t.date)::int AS year, extract(MONTH FROM t.date)::int AS month,
       coalesce(sum(t.amount) FILTER (WHERE t.type = 'income'), 0)::bigint AS income,
       coalesce(sum(-t.amount) FILTER (WHERE t.type IN ('expense', 'refund')), 0)::bigint AS spending
FROM transactions t
JOIN accounts a ON a.id = t.account_id
WHERE t.user_id = sqlc.arg('user_id') AND t.deleted_at IS NULL
  AND a.currency = sqlc.arg('currency')
  AND t.date BETWEEN sqlc.arg('date_from') AND sqlc.arg('date_to')
GROUP BY 1, 2
ORDER BY 1, 2;

-- Cash movement per calendar month across all transaction types, transfers
-- and adjustments included: a transfer between two same-currency accounts
-- nets to zero, while one crossing the currency boundary shows as real
-- in/outflow for this currency.
-- name: MonthlyCashFlow :many
SELECT extract(YEAR FROM t.date)::int AS year, extract(MONTH FROM t.date)::int AS month,
       coalesce(sum(t.amount) FILTER (WHERE t.amount > 0), 0)::bigint AS inflow,
       coalesce(sum(-t.amount) FILTER (WHERE t.amount < 0), 0)::bigint AS outflow,
       coalesce(sum(t.amount), 0)::bigint AS net
FROM transactions t
JOIN accounts a ON a.id = t.account_id
WHERE t.user_id = sqlc.arg('user_id') AND t.deleted_at IS NULL
  AND a.currency = sqlc.arg('currency')
  AND t.date BETWEEN sqlc.arg('date_from') AND sqlc.arg('date_to')
GROUP BY 1, 2
ORDER BY 1, 2;

-- Net spending per category over a date range: categorized parent
-- transactions plus split lines, transfers excluded. Same sign convention as
-- CategoryMonthlySpending (spending = -amount), so refunds reduce a
-- category's total.
-- name: SpendingByCategoryRange :many
SELECT x.category_id, sum(x.spend)::bigint AS spending
FROM (
    SELECT t.category_id::uuid AS category_id, -t.amount AS spend
    FROM transactions t
    JOIN accounts a ON a.id = t.account_id
    WHERE t.user_id = sqlc.arg('user_id') AND t.deleted_at IS NULL
      AND t.category_id IS NOT NULL AND t.type <> 'transfer'
      AND a.currency = sqlc.arg('currency')
      AND t.date BETWEEN sqlc.arg('date_from') AND sqlc.arg('date_to')
    UNION ALL
    SELECT s.category_id, -s.amount
    FROM transaction_splits s
    JOIN transactions t ON t.id = s.transaction_id
    JOIN accounts a ON a.id = t.account_id
    WHERE s.user_id = sqlc.arg('user_id') AND t.deleted_at IS NULL AND t.type <> 'transfer'
      AND a.currency = sqlc.arg('currency')
      AND t.date BETWEEN sqlc.arg('date_from') AND sqlc.arg('date_to')
) x
GROUP BY x.category_id
ORDER BY sum(x.spend) DESC;

-- Payees ranked by net spending (expenses less refunds) over a date range.
-- name: TopPayeesRange :many
SELECT t.payee, count(*)::bigint AS transaction_count, sum(-t.amount)::bigint AS spending
FROM transactions t
JOIN accounts a ON a.id = t.account_id
WHERE t.user_id = sqlc.arg('user_id') AND t.deleted_at IS NULL
  AND t.type IN ('expense', 'refund') AND t.payee <> ''
  AND a.currency = sqlc.arg('currency')
  AND t.date BETWEEN sqlc.arg('date_from') AND sqlc.arg('date_to')
GROUP BY t.payee
ORDER BY sum(-t.amount) DESC, t.payee
LIMIT sqlc.arg('max_payees');

-- Most recent non-deleted transactions for the dashboard.
-- name: RecentTransactions :many
SELECT * FROM transactions
WHERE user_id = sqlc.arg('user_id') AND deleted_at IS NULL
ORDER BY date DESC, created_at DESC
LIMIT sqlc.arg('max_rows');
