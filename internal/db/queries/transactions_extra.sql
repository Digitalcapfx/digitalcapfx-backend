-- Activity feed = a unified view over EVERY money movement for a user:
--   • fiat ledger      → transactions            (internal transfers, deposits, exchanges, withdrawals)
--   • CaaS stablecoin  → crypto_transactions     (USDC/USDT sends & receives, direction relative to the user)
--   • CaaS swaps       → caas_swaps              (USDC ⇄ USDT)
-- Column set is fixed so the generated row struct stays stable for activity_service.go.

-- name: ListActivity :many
SELECT id, type, title, amount, currency, status, source, asset, amount_sign, description, counter_name, created_at
FROM (
    SELECT
        ('tx-' || t.id::text)::text        AS id,
        t.type::text                       AS type,
        COALESCE(t.description, '')::text  AS title,
        t.amount::float8                   AS amount,
        t.currency::text                   AS currency,
        t.status::text                     AS status,
        'fiat'::text                       AS source,
        t.currency::text                   AS asset,
        ''::text                           AS amount_sign,
        COALESCE(t.description, '')::text  AS description,
        ''::text                           AS counter_name,
        t.created_at                       AS created_at
    FROM transactions t
    WHERE t.account_id IN (SELECT id FROM accounts WHERE accounts.user_id = $1)

    UNION ALL

    SELECT
        ('ct-' || c.id::text)::text,
        (CASE WHEN c.sender_user_id = $1 THEN 'transfer_out' ELSE 'transfer_in' END)::text,
        ''::text,
        c.amount::float8,
        c.token::text,
        (CASE WHEN lower(c.status) IN ('completed','settled','success') THEN 'completed'
              WHEN lower(c.status) = 'failed' THEN 'failed'
              ELSE 'pending' END)::text,
        'caas'::text,
        c.token::text,
        (CASE WHEN c.sender_user_id = $1 THEN '-' ELSE '+' END)::text,
        ''::text,
        (CASE WHEN c.sender_user_id = $1 THEN COALESCE(c.receiver_phone, c.receiver_address, '') ELSE '' END)::text,
        c.created_at
    FROM crypto_transactions c
    WHERE c.sender_user_id = $1 OR c.receiver_user_id = $1

    UNION ALL

    SELECT
        ('sw-' || s.id::text)::text,
        'exchange'::text,
        (s.token_in || ' → ' || s.token_out)::text,
        s.amount_in::float8,
        s.token_in::text,
        (CASE WHEN lower(s.status) = 'settled' THEN 'completed'
              WHEN lower(s.status) = 'failed' THEN 'failed'
              ELSE 'pending' END)::text,
        'caas'::text,
        s.token_in::text,
        '-'::text,
        (s.token_in || ' → ' || s.token_out)::text,
        ''::text,
        s.created_at
    FROM caas_swaps s
    WHERE s.user_id = $1
) act
-- type_filter accepts the UI vocabulary (received/sent/exchanged/deposited/withdrawn)
-- OR the raw type (credit/debit/transfer_in/transfer_out/exchange/deposit/withdrawal).
-- The CASE mirrors resolveType() so the tab filters actually match rows.
WHERE (sqlc.arg('type_filter')::text = '' OR act.type = sqlc.arg('type_filter')
       OR (CASE act.type WHEN 'credit' THEN 'received' WHEN 'transfer_in' THEN 'received' WHEN 'debit' THEN 'sent' WHEN 'transfer_out' THEN 'sent' WHEN 'exchange' THEN 'exchanged' WHEN 'deposit' THEN 'deposited' WHEN 'withdrawal' THEN 'withdrawn' ELSE act.type END) = sqlc.arg('type_filter'))
  AND (sqlc.arg('search')::text = '' OR act.description ILIKE '%' || sqlc.arg('search') || '%')
ORDER BY act.created_at DESC
LIMIT $3 OFFSET $2;

-- name: CountActivity :one
SELECT count(*)
FROM (
    SELECT t.type::text AS type, COALESCE(t.description, '')::text AS description, t.created_at
    FROM transactions t
    WHERE t.account_id IN (SELECT id FROM accounts WHERE accounts.user_id = $1)
    UNION ALL
    SELECT (CASE WHEN c.sender_user_id = $1 THEN 'transfer_out' ELSE 'transfer_in' END)::text, ''::text, c.created_at
    FROM crypto_transactions c
    WHERE c.sender_user_id = $1 OR c.receiver_user_id = $1
    UNION ALL
    SELECT 'exchange'::text, (s.token_in || ' → ' || s.token_out)::text, s.created_at
    FROM caas_swaps s
    WHERE s.user_id = $1
) act
WHERE (sqlc.arg('type_filter')::text = '' OR act.type = sqlc.arg('type_filter')
       OR (CASE act.type WHEN 'credit' THEN 'received' WHEN 'transfer_in' THEN 'received' WHEN 'debit' THEN 'sent' WHEN 'transfer_out' THEN 'sent' WHEN 'exchange' THEN 'exchanged' WHEN 'deposit' THEN 'deposited' WHEN 'withdrawal' THEN 'withdrawn' ELSE act.type END) = sqlc.arg('type_filter'))
  AND (sqlc.arg('search')::text = '' OR act.description ILIKE '%' || sqlc.arg('search') || '%');

-- name: ListRecentActivity :many
SELECT id, type, title, amount, currency, status, source, asset, amount_sign, description, counter_name, created_at
FROM (
    SELECT
        ('tx-' || t.id::text)::text        AS id,
        t.type::text                       AS type,
        COALESCE(t.description, '')::text  AS title,
        t.amount::float8                   AS amount,
        t.currency::text                   AS currency,
        t.status::text                     AS status,
        'fiat'::text                       AS source,
        t.currency::text                   AS asset,
        ''::text                           AS amount_sign,
        COALESCE(t.description, '')::text  AS description,
        ''::text                           AS counter_name,
        t.created_at                       AS created_at
    FROM transactions t
    WHERE t.account_id IN (SELECT id FROM accounts WHERE accounts.user_id = $1)

    UNION ALL

    SELECT
        ('ct-' || c.id::text)::text,
        (CASE WHEN c.sender_user_id = $1 THEN 'transfer_out' ELSE 'transfer_in' END)::text,
        ''::text,
        c.amount::float8,
        c.token::text,
        (CASE WHEN lower(c.status) IN ('completed','settled','success') THEN 'completed'
              WHEN lower(c.status) = 'failed' THEN 'failed'
              ELSE 'pending' END)::text,
        'caas'::text,
        c.token::text,
        (CASE WHEN c.sender_user_id = $1 THEN '-' ELSE '+' END)::text,
        ''::text,
        (CASE WHEN c.sender_user_id = $1 THEN COALESCE(c.receiver_phone, c.receiver_address, '') ELSE '' END)::text,
        c.created_at
    FROM crypto_transactions c
    WHERE c.sender_user_id = $1 OR c.receiver_user_id = $1

    UNION ALL

    SELECT
        ('sw-' || s.id::text)::text,
        'exchange'::text,
        (s.token_in || ' → ' || s.token_out)::text,
        s.amount_in::float8,
        s.token_in::text,
        (CASE WHEN lower(s.status) = 'settled' THEN 'completed'
              WHEN lower(s.status) = 'failed' THEN 'failed'
              ELSE 'pending' END)::text,
        'caas'::text,
        s.token_in::text,
        '-'::text,
        (s.token_in || ' → ' || s.token_out)::text,
        ''::text,
        s.created_at
    FROM caas_swaps s
    WHERE s.user_id = $1
) act
ORDER BY act.created_at DESC
LIMIT $2 OFFSET $3;
