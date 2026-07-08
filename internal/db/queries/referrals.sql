-- name: GetUserByReferralCode :one
SELECT * FROM users WHERE referral_code = $1 LIMIT 1;

-- name: GetPointsBalance :one
SELECT COALESCE(SUM(amount), 0)::bigint FROM points_ledger WHERE user_id = $1;

-- name: GetPointsHistory :many
SELECT * FROM points_ledger
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CreatePointsRecord :one
INSERT INTO points_ledger (user_id, amount, description)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetReferralsCount :one
SELECT COUNT(*)::bigint FROM users WHERE referred_by = $1;

-- name: GetReferralsList :many
SELECT id, phone_number, email, first_name, last_name, created_at FROM users
WHERE referred_by = $1
ORDER BY created_at DESC;

-- name: SetReferralCode :exec
UPDATE users SET referral_code = $2, referred_by = COALESCE($3, referred_by)
WHERE id = $1;
