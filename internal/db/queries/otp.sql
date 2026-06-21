-- name: CreateOTP :one
INSERT INTO otps (phone_number, code, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetValidOTP :one
SELECT * FROM otps
WHERE phone_number = $1
  AND code         = $2
  AND expires_at  > NOW()
  AND used         = false
LIMIT 1;

-- name: MarkOTPUsed :exec
UPDATE otps SET used = true WHERE id = $1;

-- name: DeleteExpiredOTPs :exec
DELETE FROM otps WHERE expires_at < NOW() OR used = true;
