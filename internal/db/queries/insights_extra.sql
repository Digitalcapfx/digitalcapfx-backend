-- name: GetBalanceTrend :many
-- Daily net fiat flow per day across all the user's fiat accounts.
-- The service layer accumulates these into a balance trend and fills in
-- crypto from live balances.
SELECT
    date_trunc('day', transactions.created_at)::timestamptz AS date,
    sum(transactions.amount)::float8 AS fiat_usd,
    0::float8 AS crypto_usd
FROM transactions
WHERE transactions.account_id IN (SELECT id FROM accounts WHERE user_id = $1)
  AND transactions.created_at >= $2
GROUP BY 1
ORDER BY 1;

-- name: GetMonthlyFlow :many
SELECT
    to_char(date_trunc('month', transactions.created_at), 'Mon')::text AS month,
    extract(YEAR FROM date_trunc('month', transactions.created_at))::int AS year,
    sum(CASE WHEN transactions.amount > 0 THEN transactions.amount ELSE 0 END)::float8 AS income,
    abs(sum(CASE WHEN transactions.amount < 0 THEN transactions.amount ELSE 0 END))::float8 AS spending
FROM transactions
WHERE transactions.account_id IN (SELECT id FROM accounts WHERE user_id = $1)
  AND transactions.created_at >= now() - make_interval(months => sqlc.arg(months)::int)
GROUP BY date_trunc('month', transactions.created_at)
ORDER BY date_trunc('month', transactions.created_at);

-- name: GetSpendingByType :many
SELECT
    transactions.type::text AS tx_type,
    'fiat'::text AS source,
    abs(sum(transactions.amount))::float8 AS total
FROM transactions
WHERE transactions.account_id IN (SELECT id FROM accounts WHERE user_id = $1)
  AND transactions.amount < 0
  AND transactions.created_at >= $2
GROUP BY transactions.type
ORDER BY total DESC;
