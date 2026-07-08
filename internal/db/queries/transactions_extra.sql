-- name: ListActivity :many
SELECT
    ('tx-' || id::text)::text as id,
    type as type,
    COALESCE(description, '')::text as title,
    amount::float8 as amount,
    currency as currency,
    status as status,
    ''::text as source,
    ''::text as asset,
    ''::text as amount_sign,
    COALESCE(description, '')::text as description,
    ''::text as counter_name,
    created_at as created_at
FROM transactions
WHERE account_id IN (SELECT id FROM accounts WHERE user_id = $1)
  AND (sqlc.arg('type_filter')::text = '' OR type = sqlc.arg('type_filter'))
  AND (sqlc.arg('search')::text = '' OR description ILIKE '%' || sqlc.arg('search') || '%')
ORDER BY created_at DESC
LIMIT $3 OFFSET $2;

-- name: CountActivity :one
SELECT count(*)
FROM transactions
WHERE account_id IN (SELECT id FROM accounts WHERE user_id = $1)
  AND (sqlc.arg('type_filter')::text = '' OR type = sqlc.arg('type_filter'))
  AND (sqlc.arg('search')::text = '' OR description ILIKE '%' || sqlc.arg('search') || '%');

-- name: ListRecentActivity :many
SELECT
    ('tx-' || id::text)::text as id,
    type as type,
    COALESCE(description, '')::text as title,
    amount::float8 as amount,
    currency as currency,
    status as status,
    ''::text as source,
    ''::text as asset,
    ''::text as amount_sign,
    COALESCE(description, '')::text as description,
    ''::text as counter_name,
    created_at as created_at
FROM transactions
WHERE account_id IN (SELECT id FROM accounts WHERE user_id = $1)
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;
