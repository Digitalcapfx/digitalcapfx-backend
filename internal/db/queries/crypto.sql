-- name: CreateCryptoTransaction :one
INSERT INTO crypto_transactions
    (reference, sender_user_id, receiver_phone, receiver_user_id, token, amount)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetCryptoTransactionByID :one
SELECT * FROM crypto_transactions WHERE id = $1 LIMIT 1;

-- name: GetCryptoTransactionByReference :one
SELECT * FROM crypto_transactions WHERE reference = $1 LIMIT 1;

-- name: ListCryptoTransactionsByUser :many
SELECT * FROM crypto_transactions
WHERE sender_user_id = $1 OR receiver_user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateCryptoTransactionStatus :one
UPDATE crypto_transactions
SET status = $2, tx_hash = $3, updated_at = NOW()
WHERE id = $1
RETURNING *;
