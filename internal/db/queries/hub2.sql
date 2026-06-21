-- name: CreateHub2Payment :one
INSERT INTO hub2_payments (transaction_id, hub2_reference, payment_method, operator, phone_number, metadata)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetHub2PaymentByReference :one
SELECT * FROM hub2_payments WHERE hub2_reference = $1 LIMIT 1;

-- name: UpdateHub2PaymentStatus :one
UPDATE hub2_payments
SET status = $2, hub2_status = $3, updated_at = NOW()
WHERE id = $1
RETURNING *;
