-- name: CreateUserSession :one
INSERT INTO user_sessions
    (user_id, refresh_token_hash, device_name, device_ip, device_ua, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetUserSessionByID :one
SELECT * FROM user_sessions WHERE id = $1 LIMIT 1;

-- name: GetUserSessionByRefreshTokenHash :one
SELECT * FROM user_sessions
WHERE refresh_token_hash = $1 AND is_active = true AND expires_at > NOW()
LIMIT 1;

-- name: ListActiveSessionsByUserID :many
SELECT * FROM user_sessions
WHERE user_id = $1 AND is_active = true AND expires_at > NOW()
ORDER BY last_used_at DESC;

-- name: RevokeUserSessionByID :exec
UPDATE user_sessions SET is_active = false WHERE id = $1 AND user_id = $2;

-- name: RevokeAllUserSessions :exec
UPDATE user_sessions SET is_active = false WHERE user_id = $1;

-- name: RevokeAllOtherSessions :exec
UPDATE user_sessions SET is_active = false WHERE user_id = $1 AND id != sqlc.arg(exclude_id);

-- name: UpdateSessionLastUsed :exec
UPDATE user_sessions SET last_used_at = NOW() WHERE id = $1;

-- name: UpdateSessionRefreshTokenHash :exec
UPDATE user_sessions
SET refresh_token_hash = $2, last_used_at = NOW()
WHERE id = $1;
