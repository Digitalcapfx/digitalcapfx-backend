-- name: CreateVirtualCard :one
INSERT INTO virtual_cards (
    user_id, funding_account_id, funding_wallet_id, card_name, color_theme, card_art_id, billing_address, masked_pan, expiry, cvv_encrypted, status, provider_card_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
) RETURNING *;

-- name: ListVirtualCardsByUserID :many
SELECT * FROM virtual_cards
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetVirtualCardByID :one
SELECT * FROM virtual_cards
WHERE id = $1 AND user_id = $2;

-- name: CountActiveVirtualCards :one
SELECT COUNT(*) FROM virtual_cards
WHERE user_id = $1 AND status != 'closed';

-- name: UpdateVirtualCard :one
UPDATE virtual_cards
SET card_name = COALESCE(sqlc.narg('card_name'), card_name),
    color_theme = COALESCE(sqlc.narg('color_theme'), color_theme),
    card_art_id = COALESCE(sqlc.narg('card_art_id'), card_art_id),
    status = COALESCE(sqlc.narg('status'), status),
    updated_at = NOW()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: CreateCardTransaction :one
INSERT INTO card_transactions (
    card_id, amount, currency, merchant_name, merchant_city, merchant_country, status, provider_tx_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
) RETURNING *;

-- name: ListCardTransactions :many
SELECT * FROM card_transactions
WHERE card_id = $1
ORDER BY created_at DESC
LIMIT 50;
