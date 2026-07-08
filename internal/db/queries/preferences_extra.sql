-- name: GetUserPreferences :one
SELECT * FROM user_preferences WHERE user_id = $1;

-- name: UpdateUserPreferences :exec
UPDATE user_preferences
SET language = $2, dark_mode = $3, biometrics_enabled = $4, updated_at = now()
WHERE user_id = $1;

-- name: CreateUserPreferences :exec
INSERT INTO user_preferences (user_id, language, dark_mode, biometrics_enabled)
VALUES ($1, $2, $3, $4);
