-- ─── WaaS (Payments API custody wallets) ──────────────────────────────────

-- name: CreateWaasWallet :one
INSERT INTO waas_wallets (user_id, waas_wallet_id, network, address, is_default)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetWaasWalletsByUserID :many
SELECT * FROM waas_wallets WHERE user_id = $1 ORDER BY created_at DESC;

-- name: GetWaasWalletByNetwork :one
SELECT * FROM waas_wallets
WHERE user_id = $1 AND network = $2
LIMIT 1;

-- name: GetDefaultWaasWallet :one
SELECT * FROM waas_wallets
WHERE user_id = $1 AND is_default = true
LIMIT 1;

-- name: GetWaasWalletByIDAndUser :one
SELECT * FROM waas_wallets WHERE id = $1 AND user_id = $2 LIMIT 1;

-- name: GetWaasWalletByAddress :one
SELECT * FROM waas_wallets WHERE address = $1 LIMIT 1;


