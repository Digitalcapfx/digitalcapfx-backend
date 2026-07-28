-- name: GetKYCIdentity :one
SELECT * FROM kyc_identity WHERE user_id = $1 LIMIT 1;

-- name: MarkKYCIdentityStarted :exec
-- Called on POST /kyc/init. Sets in_progress the first time; never regresses a
-- terminal/in-review state back to in_progress.
INSERT INTO kyc_identity (user_id, status, started_at, updated_at)
VALUES ($1, 'in_progress', now(), now())
ON CONFLICT (user_id) DO UPDATE SET
    status     = CASE WHEN kyc_identity.status IN ('not_started') THEN 'in_progress' ELSE kyc_identity.status END,
    started_at = COALESCE(kyc_identity.started_at, now()),
    updated_at = now();

-- name: UpsertKYCIdentityFromWebhook :exec
INSERT INTO kyc_identity (user_id, status, applicant_id, review_answer, reject_labels, reject_type, moderation_comment, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now())
ON CONFLICT (user_id) DO UPDATE SET
    status             = EXCLUDED.status,
    applicant_id       = COALESCE(EXCLUDED.applicant_id, kyc_identity.applicant_id),
    review_answer      = COALESCE(EXCLUDED.review_answer, kyc_identity.review_answer),
    reject_labels      = COALESCE(EXCLUDED.reject_labels, kyc_identity.reject_labels),
    reject_type        = COALESCE(EXCLUDED.reject_type, kyc_identity.reject_type),
    moderation_comment = COALESCE(EXCLUDED.moderation_comment, kyc_identity.moderation_comment),
    updated_at         = now();
