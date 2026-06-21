-- name: CreateKYCDocument :one
INSERT INTO kyc_documents (user_id, doc_type, doc_url)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetKYCDocumentsByUserID :many
SELECT * FROM kyc_documents
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: UpdateKYCDocumentStatus :one
UPDATE kyc_documents
SET status = $2, rejection_reason = $3, updated_at = NOW()
WHERE id = $1
RETURNING *;
