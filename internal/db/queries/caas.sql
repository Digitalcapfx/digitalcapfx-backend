-- ─── CaaS wallet queries ──────────────────────────────────────────────────────

-- name: CreateCaasWalletFull :one
INSERT INTO caas_wallets (user_id, caas_wallet_id, blind_index, abstraction_address)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetCaasWalletByUserID :one
SELECT * FROM caas_wallets WHERE user_id = $1 LIMIT 1;

-- name: GetCaasWalletByBlindIndex :one
SELECT * FROM caas_wallets WHERE blind_index = $1 LIMIT 1;

-- name: GetCaasWalletByAddress :one
SELECT * FROM caas_wallets WHERE abstraction_address = $1 LIMIT 1;

-- name: UpdateCaasWalletPhone :exec
UPDATE caas_wallets
SET blind_index = $2
WHERE user_id = $1;

-- ─── FX quote queries ─────────────────────────────────────────────────────────

-- name: CreateFXQuote :one
INSERT INTO fx_quotes (user_id, quote_id, fiat_amount, local_currency, target_token, rate, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetFXQuoteByQuoteID :one
SELECT * FROM fx_quotes WHERE quote_id = $1 LIMIT 1;

-- name: MarkFXQuoteUsed :exec
UPDATE fx_quotes SET used = true WHERE quote_id = $1;

-- name: DeleteExpiredFXQuotes :exec
DELETE FROM fx_quotes WHERE expires_at < NOW() AND used = false;

-- ─── CaaS withdrawal queries ──────────────────────────────────────────────────

-- name: CreateCaasWithdrawal :one
INSERT INTO caas_withdrawals
    (user_id, caas_withdrawal_id, phone, amount, token, payout_mobile, payout_network, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetCaasWithdrawalByID :one
SELECT * FROM caas_withdrawals WHERE id = $1 LIMIT 1;

-- name: GetCaasWithdrawalByIdempotencyKey :one
SELECT * FROM caas_withdrawals WHERE idempotency_key = $1 LIMIT 1;

-- name: GetCaasWithdrawalByCaasID :one
SELECT * FROM caas_withdrawals WHERE caas_withdrawal_id = $1 LIMIT 1;

-- name: UpdateCaasWithdrawalStatus :one
UPDATE caas_withdrawals
SET status = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: ListCaasWithdrawalsByUser :many
SELECT * FROM caas_withdrawals
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- ─── crypto_transactions CaaS extras ─────────────────────────────────────────
-- (CreateCryptoTransaction is defined in the existing stub; the params struct is
--  extended in query.sql.go to include the nullable CaaS fields below.)

-- name: GetCryptoTransactionByIdempotencyKey :one
SELECT * FROM crypto_transactions WHERE idempotency_key = $1 LIMIT 1;

-- name: UpdateCryptoTransactionCaasResult :one
UPDATE crypto_transactions
SET status = $2, tx_hash = $3, caas_transfer_id = $4, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- ─── hub2_payments CaaS funding ───────────────────────────────────────────────

-- name: MarkHub2PaymentCaasFunded :exec
UPDATE hub2_payments
SET caas_funded_at = NOW()
WHERE id = $1;

-- name: GetHub2PaymentByReferenceForFunding :one
SELECT * FROM hub2_payments
WHERE hub2_reference = $1 AND caas_funded_at IS NULL
LIMIT 1;
