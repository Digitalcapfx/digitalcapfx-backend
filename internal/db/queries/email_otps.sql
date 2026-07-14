-- name: CreateEmailOTP :one
INSERT INTO email_otps (email, code, purpose, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetValidEmailOTP :one
SELECT * FROM email_otps
WHERE email      = $1
  AND code       = $2
  AND purpose    = $3
  AND expires_at > NOW()
  AND used       = false
LIMIT 1;

-- name: MarkEmailOTPUsed :exec
UPDATE email_otps SET used = true WHERE id = $1;

-- name: GetLatestEmailOTPSentAt :one
-- Most recent time a code of this purpose was sent to an email — powers the
-- resend cooldown.
SELECT created_at FROM email_otps
WHERE email = $1 AND purpose = $2
ORDER BY created_at DESC
LIMIT 1;

-- name: DeleteExpiredEmailOTPs :exec
DELETE FROM email_otps WHERE expires_at < NOW() OR used = true;

-- MetaMap verification queries

-- name: CreateMetamapVerification :one
INSERT INTO metamap_verifications
    (user_id, applicant_id, flow_id, identity_access)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetMetamapVerificationByUserID :one
SELECT * FROM metamap_verifications WHERE user_id = $1 LIMIT 1;

-- name: GetMetamapVerificationByApplicantID :one
SELECT * FROM metamap_verifications WHERE applicant_id = $1 LIMIT 1;

-- name: UpdateMetamapVerificationStatus :one
UPDATE metamap_verifications
SET status = $2, result_data = $3, updated_at = NOW()
WHERE applicant_id = $1
RETURNING *;
