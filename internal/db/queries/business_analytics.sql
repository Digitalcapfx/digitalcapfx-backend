-- name: GetTransactionStatsSince :one
-- Aggregate transaction stats across all of a user's fiat accounts since a
-- given time. Powers the business analytics dashboard (count / volume / avg /
-- largest). Volume is the sum of absolute amounts (both in- and out-flows).
SELECT
    count(*)::bigint AS tx_count,
    COALESCE(sum(abs(transactions.amount)), 0)::float8 AS total_volume,
    COALESCE(avg(abs(transactions.amount)), 0)::float8 AS avg_amount,
    COALESCE(max(abs(transactions.amount)), 0)::float8 AS max_amount
FROM transactions
WHERE transactions.account_id IN (SELECT id FROM accounts WHERE user_id = $1)
  AND transactions.created_at >= $2;

-- name: GetVolumeByCurrencySince :many
-- Per-currency transaction count and volume since a given time. Gives business
-- accounts a currency-level breakdown of their activity.
SELECT
    transactions.currency::text AS currency,
    count(*)::bigint AS tx_count,
    COALESCE(sum(abs(transactions.amount)), 0)::float8 AS volume
FROM transactions
WHERE transactions.account_id IN (SELECT id FROM accounts WHERE user_id = $1)
  AND transactions.created_at >= $2
GROUP BY transactions.currency
ORDER BY volume DESC;
