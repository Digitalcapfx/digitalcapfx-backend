-- name: CreateSumsubVerification :one
INSERT INTO sumsub_verifications (
    user_id,
    applicant_id,
    level_name,
    access_token
) VALUES (
    $1, $2, $3, $4
) RETURNING *;

-- name: GetSumsubVerificationByUserID :one
SELECT * FROM sumsub_verifications
WHERE user_id = $1 LIMIT 1;

-- name: GetSumsubVerificationByApplicantID :one
SELECT * FROM sumsub_verifications
WHERE applicant_id = $1 LIMIT 1;

-- name: UpdateSumsubVerificationStatus :one
UPDATE sumsub_verifications
SET status = $2,
    result_data = $3,
    updated_at = now()
WHERE applicant_id = $1
RETURNING *;
