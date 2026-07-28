-- name: GetKYCIntake :one
SELECT * FROM kyc_intake WHERE user_id = $1 LIMIT 1;

-- name: UpsertKYCIntake :one
INSERT INTO kyc_intake (
    user_id, account_type,
    legal_first_name, legal_last_name, date_of_birth, nationality, bvn,
    address_line1, address_line2, city, state, postal_code, country,
    occupation, source_of_funds, purpose_of_account,
    is_importer, counterparties, contact_email, contact_phone,
    saved_values, status, submitted_at, updated_at
) VALUES (
    $1, $2,
    $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12, $13,
    $14, $15, $16,
    $17, $18, $19, $20,
    $21, 'submitted', now(), now()
)
ON CONFLICT (user_id) DO UPDATE SET
    account_type       = EXCLUDED.account_type,
    legal_first_name   = EXCLUDED.legal_first_name,
    legal_last_name    = EXCLUDED.legal_last_name,
    date_of_birth      = EXCLUDED.date_of_birth,
    nationality        = EXCLUDED.nationality,
    bvn                = EXCLUDED.bvn,
    address_line1      = EXCLUDED.address_line1,
    address_line2      = EXCLUDED.address_line2,
    city               = EXCLUDED.city,
    state              = EXCLUDED.state,
    postal_code        = EXCLUDED.postal_code,
    country            = EXCLUDED.country,
    occupation         = EXCLUDED.occupation,
    source_of_funds    = EXCLUDED.source_of_funds,
    purpose_of_account = EXCLUDED.purpose_of_account,
    is_importer        = EXCLUDED.is_importer,
    counterparties     = EXCLUDED.counterparties,
    contact_email      = EXCLUDED.contact_email,
    contact_phone      = EXCLUDED.contact_phone,
    saved_values       = EXCLUDED.saved_values,
    status             = 'submitted',
    submitted_at       = now(),
    updated_at         = now()
RETURNING *;

-- name: SaveKYCIntakeDraft :one
-- Persist partial progress (no required-field validation). Merges the supplied
-- values into any existing draft (last-write-wins per key) and never downgrades
-- a submitted intake back to draft.
INSERT INTO kyc_intake (user_id, account_type, saved_values, status, updated_at)
VALUES ($1, $2, $3, 'draft', now())
ON CONFLICT (user_id) DO UPDATE SET
    account_type = EXCLUDED.account_type,
    saved_values = COALESCE(kyc_intake.saved_values, '{}'::jsonb) || EXCLUDED.saved_values,
    status       = CASE WHEN kyc_intake.status = 'submitted' THEN 'submitted' ELSE 'draft' END,
    updated_at   = now()
RETURNING *;
