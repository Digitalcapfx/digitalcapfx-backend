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

-- name: GetMonthlyTransactionSummary :one
-- Current-month income vs spending across BOTH fiat ledger and CaaS stablecoin
-- movements, classified by type/direction (not by sign). USDC/USDT are treated
-- as USD 1:1; fiat amounts are taken at face value (same simplification the rest
-- of insights uses).
SELECT
    COALESCE(SUM(CASE WHEN direction = 'in'  THEN amt ELSE 0 END), 0)::float8 AS income_usd,
    COALESCE(SUM(CASE WHEN direction = 'out' THEN amt ELSE 0 END), 0)::float8 AS spending_usd,
    COUNT(*)::bigint AS tx_count
FROM (
    SELECT
        CASE WHEN t.type IN ('credit','deposit','transfer_in') THEN 'in'
             WHEN t.type IN ('debit','withdrawal','transfer_out','exchange') THEN 'out'
             ELSE 'other' END AS direction,
        t.amount::float8 AS amt
    FROM transactions t
    WHERE t.account_id IN (SELECT id FROM accounts WHERE accounts.user_id = $1)
      AND t.created_at >= date_trunc('month', now())
    UNION ALL
    SELECT
        CASE WHEN c.receiver_user_id = $1 THEN 'in' ELSE 'out' END AS direction,
        c.amount::float8 AS amt
    FROM crypto_transactions c
    WHERE (c.sender_user_id = $1 OR c.receiver_user_id = $1)
      AND lower(c.status) IN ('completed','settled','success')
      AND c.created_at >= date_trunc('month', now())
) m;

-- name: GetMonthlyFlow :many
SELECT month, year,
       SUM(income)::float8   AS income,
       SUM(spending)::float8 AS spending
FROM (
    SELECT
        to_char(date_trunc('month', t.created_at), 'Mon')::text AS month,
        extract(YEAR FROM date_trunc('month', t.created_at))::int AS year,
        date_trunc('month', t.created_at) AS m,
        CASE WHEN t.type IN ('credit','deposit','transfer_in') THEN t.amount::float8 ELSE 0 END AS income,
        CASE WHEN t.type IN ('debit','withdrawal','transfer_out','exchange') THEN t.amount::float8 ELSE 0 END AS spending
    FROM transactions t
    WHERE t.account_id IN (SELECT id FROM accounts WHERE accounts.user_id = $1)
      AND t.created_at >= now() - make_interval(months => sqlc.arg(months)::int)
    UNION ALL
    SELECT
        to_char(date_trunc('month', c.created_at), 'Mon')::text,
        extract(YEAR FROM date_trunc('month', c.created_at))::int,
        date_trunc('month', c.created_at),
        CASE WHEN c.receiver_user_id = $1 THEN c.amount::float8 ELSE 0 END,
        CASE WHEN c.sender_user_id = $1 THEN c.amount::float8 ELSE 0 END
    FROM crypto_transactions c
    WHERE (c.sender_user_id = $1 OR c.receiver_user_id = $1)
      AND lower(c.status) IN ('completed','settled','success')
      AND c.created_at >= now() - make_interval(months => sqlc.arg(months)::int)
) f
GROUP BY month, year, m
ORDER BY m;

-- name: GetSpendingByType :many
SELECT tx_type, source, SUM(total)::float8 AS total
FROM (
    SELECT t.type::text AS tx_type, 'fiat'::text AS source, t.amount::float8 AS total
    FROM transactions t
    WHERE t.account_id IN (SELECT id FROM accounts WHERE accounts.user_id = $1)
      AND t.type IN ('debit','withdrawal','transfer_out','exchange')
      AND t.created_at >= $2
    UNION ALL
    SELECT 'transfer_out'::text, 'caas'::text, c.amount::float8
    FROM crypto_transactions c
    WHERE c.sender_user_id = $1
      AND lower(c.status) IN ('completed','settled','success')
      AND c.created_at >= $2
    UNION ALL
    SELECT 'exchange'::text, 'caas'::text, s.amount_in::float8
    FROM caas_swaps s
    WHERE s.user_id = $1
      AND lower(s.status) = 'settled'
      AND s.created_at >= $2
) g
GROUP BY tx_type, source
ORDER BY total DESC;
