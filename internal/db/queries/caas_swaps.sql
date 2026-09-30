-- name: CreateCaasSwap :one
INSERT INTO caas_swaps
    (reference, user_id, token_in, token_out, amount_in, status, caas_swap_id, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetCaasSwapByID :one
SELECT * FROM caas_swaps WHERE id = $1 LIMIT 1;

-- name: GetCaasSwapByReference :one
SELECT * FROM caas_swaps WHERE reference = $1 LIMIT 1;

-- name: GetCaasSwapByCaasID :one
SELECT * FROM caas_swaps WHERE caas_swap_id = $1 LIMIT 1;

-- name: ListCaasSwapsByUser :many
SELECT * FROM caas_swaps
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateCaasSwapCaasID :one
UPDATE caas_swaps
SET caas_swap_id = $2, status = $3, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateCaasSwapResult :one
UPDATE caas_swaps
SET status = $2, amount_out = $3, tx_hash = $4, updated_at = NOW()
WHERE id = $1
RETURNING *;
