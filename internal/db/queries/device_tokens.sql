-- name: UpsertDeviceToken :exec
INSERT INTO device_tokens (user_id, token, platform)
VALUES ($1, $2, $3)
ON CONFLICT (token) DO UPDATE SET
    user_id    = EXCLUDED.user_id,
    platform   = EXCLUDED.platform,
    updated_at = now();

-- name: ListDeviceTokensByUser :many
SELECT token FROM device_tokens WHERE user_id = $1;

-- name: DeleteDeviceToken :exec
DELETE FROM device_tokens WHERE token = $1 AND user_id = $2;

-- name: DeleteDeviceTokens :exec
-- Removes tokens FCM reported as unregistered/invalid (any owner).
DELETE FROM device_tokens WHERE token = ANY(@tokens::text[]);
