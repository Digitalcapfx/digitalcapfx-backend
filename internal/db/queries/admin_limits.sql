-- name: ListPlatformLimits :many
SELECT tier, daily_withdrawal_usd, per_transaction_usd, monthly_volume_usd,
       max_holding_balance_usd, daily_transaction_count, updated_at, updated_by
FROM platform_limits
ORDER BY tier;

-- name: GetPlatformLimit :one
SELECT tier, daily_withdrawal_usd, per_transaction_usd, monthly_volume_usd,
       max_holding_balance_usd, daily_transaction_count, updated_at, updated_by
FROM platform_limits
WHERE tier = $1;

-- name: UpsertPlatformLimit :one
INSERT INTO platform_limits
    (tier, daily_withdrawal_usd, per_transaction_usd, monthly_volume_usd,
     max_holding_balance_usd, daily_transaction_count, updated_by, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now())
ON CONFLICT (tier) DO UPDATE SET
    daily_withdrawal_usd    = EXCLUDED.daily_withdrawal_usd,
    per_transaction_usd     = EXCLUDED.per_transaction_usd,
    monthly_volume_usd      = EXCLUDED.monthly_volume_usd,
    max_holding_balance_usd = EXCLUDED.max_holding_balance_usd,
    daily_transaction_count = EXCLUDED.daily_transaction_count,
    updated_by              = EXCLUDED.updated_by,
    updated_at              = now()
RETURNING tier, daily_withdrawal_usd, per_transaction_usd, monthly_volume_usd,
          max_holding_balance_usd, daily_transaction_count, updated_at, updated_by;

-- name: GetUserLimitOverride :one
SELECT user_id, daily_withdrawal_usd, per_transaction_usd, monthly_volume_usd,
       max_holding_balance_usd, daily_transaction_count, note, updated_at, updated_by
FROM user_limit_overrides
WHERE user_id = $1;

-- name: UpsertUserLimitOverride :one
INSERT INTO user_limit_overrides
    (user_id, daily_withdrawal_usd, per_transaction_usd, monthly_volume_usd,
     max_holding_balance_usd, daily_transaction_count, note, updated_by, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
ON CONFLICT (user_id) DO UPDATE SET
    daily_withdrawal_usd    = EXCLUDED.daily_withdrawal_usd,
    per_transaction_usd     = EXCLUDED.per_transaction_usd,
    monthly_volume_usd      = EXCLUDED.monthly_volume_usd,
    max_holding_balance_usd = EXCLUDED.max_holding_balance_usd,
    daily_transaction_count = EXCLUDED.daily_transaction_count,
    note                    = EXCLUDED.note,
    updated_by              = EXCLUDED.updated_by,
    updated_at              = now()
RETURNING user_id, daily_withdrawal_usd, per_transaction_usd, monthly_volume_usd,
          max_holding_balance_usd, daily_transaction_count, note, updated_at, updated_by;

-- name: DeleteUserLimitOverride :exec
DELETE FROM user_limit_overrides WHERE user_id = $1;

-- name: SetUserAccountType :one
UPDATE users
SET account_type = $2, updated_at = now()
WHERE id = $1
RETURNING id, account_type;
