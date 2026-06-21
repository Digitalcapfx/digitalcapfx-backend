-- name: CreateUser :one
INSERT INTO users (phone_number, email, first_name, last_name, pin_hash)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1 LIMIT 1;

-- name: GetUserByPhone :one
SELECT * FROM users WHERE phone_number = $1 LIMIT 1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1 LIMIT 1;

-- name: UpdateUserKYCStatus :one
UPDATE users
SET kyc_status = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateUserPinHash :exec
UPDATE users
SET pin_hash = $2, updated_at = NOW()
WHERE id = $1;

-- name: DeactivateUser :exec
UPDATE users
SET is_active = false, updated_at = NOW()
WHERE id = $1;
