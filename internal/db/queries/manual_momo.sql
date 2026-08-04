-- ─── Business mobile-money collection accounts ───────────────────────────────

-- name: ListActiveMomoAccounts :many
SELECT * FROM manual_momo_accounts WHERE is_active = true ORDER BY sort_order, display_name;

-- name: ListAllMomoAccounts :many
SELECT * FROM manual_momo_accounts ORDER BY sort_order, display_name;

-- name: GetMomoAccount :one
SELECT * FROM manual_momo_accounts WHERE id = $1;

-- name: CreateMomoAccount :one
INSERT INTO manual_momo_accounts
    (provider, display_name, phone_number, account_name, currency, country, instructions, is_active, sort_order)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdateMomoAccount :one
UPDATE manual_momo_accounts
SET provider = $2, display_name = $3, phone_number = $4, account_name = $5,
    currency = $6, country = $7, instructions = $8, is_active = $9, sort_order = $10,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteMomoAccount :exec
DELETE FROM manual_momo_accounts WHERE id = $1;

-- ─── Manual deposits (customer top-up claims) ────────────────────────────────

-- name: CreateManualDeposit :one
INSERT INTO manual_deposits
    (user_id, account_id, momo_account_id, provider, currency, claimed_amount,
     sender_phone, sender_name, reference, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetManualDeposit :one
SELECT * FROM manual_deposits WHERE id = $1;

-- name: ListManualDepositsByUser :many
SELECT * FROM manual_deposits WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;

-- name: ListManualDepositsByStatus :many
SELECT * FROM manual_deposits
WHERE (sqlc.arg('status')::varchar = '' OR status = sqlc.arg('status'))
ORDER BY created_at DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: ConfirmManualDeposit :one
UPDATE manual_deposits
SET status = 'confirmed', credited_amount = $2, charge = $3, admin_note = $4,
    reviewed_by = $5, reviewed_at = NOW(), updated_at = NOW()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: RejectManualDeposit :one
UPDATE manual_deposits
SET status = 'rejected', admin_note = $2, reviewed_by = $3, reviewed_at = NOW(), updated_at = NOW()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- ─── Manual withdrawals (customer cash-out requests) ─────────────────────────

-- name: CreateManualWithdrawal :one
INSERT INTO manual_withdrawals
    (user_id, account_id, provider, currency, amount, recipient_phone, recipient_name, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetManualWithdrawal :one
SELECT * FROM manual_withdrawals WHERE id = $1;

-- name: ListManualWithdrawalsByUser :many
SELECT * FROM manual_withdrawals WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;

-- name: ListManualWithdrawalsByStatus :many
SELECT * FROM manual_withdrawals
WHERE (sqlc.arg('status')::varchar = '' OR status = sqlc.arg('status'))
ORDER BY created_at DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CompleteManualWithdrawal :one
UPDATE manual_withdrawals
SET status = 'completed', charge = $2, payout_amount = $3, admin_note = $4,
    reviewed_by = $5, reviewed_at = NOW(), updated_at = NOW()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: RejectManualWithdrawal :one
UPDATE manual_withdrawals
SET status = 'rejected', admin_note = $2, reviewed_by = $3, reviewed_at = NOW(), updated_at = NOW()
WHERE id = $1 AND status = 'pending'
RETURNING *;
