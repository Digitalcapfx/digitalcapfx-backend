-- name: CreateVTUTransaction :one
INSERT INTO vtu_transactions (
    user_id, account_id, amount, currency, service_type, provider, target_phone, target_account, reference, provider_ref, status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) RETURNING *;

-- name: GetVTUTransactionByReference :one
SELECT * FROM vtu_transactions
WHERE reference = $1;

-- name: UpdateVTUTransactionStatus :one
UPDATE vtu_transactions
SET status = $2,
    provider_ref = COALESCE(sqlc.narg('provider_ref'), provider_ref),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: ListVTUTransactionsByUserID :many
SELECT * FROM vtu_transactions
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT 50;
