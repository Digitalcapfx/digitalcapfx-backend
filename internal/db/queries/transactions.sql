-- name: CreateTransaction :one
INSERT INTO transactions (reference, account_id, type, amount, currency, fee, description, status, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetTransactionByID :one
SELECT * FROM transactions WHERE id = $1 LIMIT 1;

-- name: GetTransactionByReference :one
SELECT * FROM transactions WHERE reference = $1 LIMIT 1;

-- name: ListTransactionsByAccount :many
SELECT * FROM transactions
WHERE account_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountTransactionsByAccount :one
SELECT COUNT(*) FROM transactions WHERE account_id = $1;

-- name: UpdateTransactionStatus :one
UPDATE transactions
SET status = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;
