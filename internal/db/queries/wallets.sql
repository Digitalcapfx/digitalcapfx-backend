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

-- ─── CaaS (abstraction wallets) ───────────────────────────────────────────

-- name: CreateCaasWallet :one
INSERT INTO caas_wallets (user_id, caas_wallet_id, abstraction_address)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetCaasWalletByUserID :one
SELECT * FROM caas_wallets WHERE user_id = $1 LIMIT 1;

-- name: GetCaasWalletByAddress :one
SELECT * FROM caas_wallets WHERE abstraction_address = $1 LIMIT 1;
