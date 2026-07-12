-- name: GetUserSecurity :one
SELECT id, totp_enabled, totp_secret, pin_hash FROM users WHERE id = $1;

-- name: GetUserByGoogleSub :one
SELECT * FROM users WHERE google_sub = $1;

-- name: CreateGoogleUser :one
INSERT INTO users (
    email, first_name, last_name, google_sub, role, auth_provider
) VALUES (
    $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: UpdateUserEmailVerified :exec
UPDATE users SET is_email_verified = true, updated_at = now() WHERE id = $1;

-- name: GetUserFullByID :one
SELECT * FROM users WHERE id = $1;

-- name: UpdateUserProfile :one
UPDATE users
SET first_name = COALESCE(sqlc.narg('first_name'), first_name),
    last_name = COALESCE(sqlc.narg('last_name'), last_name),
    bio = COALESCE(sqlc.narg('bio'), bio),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    date_of_birth = COALESCE(sqlc.narg('date_of_birth'), date_of_birth),
    nationality = COALESCE(sqlc.narg('nationality'), nationality),
    bvn = COALESCE(sqlc.narg('bvn'), bvn),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetUserBVN :one
UPDATE users SET bvn = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: CreateIndividualUser :one
INSERT INTO users (
    phone_number, email, first_name, last_name, pin_hash, country, role, auth_provider, account_type
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, 'individual'
) RETURNING *;

-- name: CreateBusinessUser :one
INSERT INTO users (
    phone_number, email, first_name, last_name, pin_hash, country, role, auth_provider, account_type
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, 'business'
) RETURNING *;
