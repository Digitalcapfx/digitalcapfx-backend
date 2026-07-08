-- Business profile queries

-- name: CreateBusinessProfile :one
INSERT INTO business_profiles (
    user_id,
    company_legal_name,
    company_registration_no,
    industry,
    country_of_incorporation,
    annual_revenue,
    business_website
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetBusinessProfileByUserID :one
SELECT * FROM business_profiles WHERE user_id = $1 LIMIT 1;

-- name: MarkDirectorsComplete :exec
UPDATE business_profiles
SET directors_complete = true, updated_at = now()
WHERE user_id = $1;

-- name: MarkDocumentsComplete :exec
UPDATE business_profiles
SET documents_complete = true, updated_at = now()
WHERE user_id = $1;

-- Business director queries

-- name: CreateBusinessDirector :one
INSERT INTO business_directors (
    user_id,
    first_name,
    last_name,
    job_title,
    date_of_birth,
    nationality,
    phone_number
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListBusinessDirectors :many
SELECT * FROM business_directors
WHERE user_id = $1
ORDER BY created_at ASC;

-- name: DeleteBusinessDirector :exec
DELETE FROM business_directors
WHERE id = $1 AND user_id = $2;


-- Merchant staff queries

-- name: CreateMerchantStaff :one
INSERT INTO merchant_staff (
    business_user_id,
    email,
    role,
    invite_token
) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetMerchantStaffByID :one
SELECT * FROM merchant_staff WHERE id = $1;

-- name: GetMerchantStaffByEmailAndBusiness :one
SELECT * FROM merchant_staff WHERE email = $1 AND business_user_id = $2;

-- name: GetMerchantStaffByInviteToken :one
SELECT * FROM merchant_staff WHERE invite_token = $1;

-- name: ListMerchantStaff :many
SELECT * FROM merchant_staff
WHERE business_user_id = $1
ORDER BY created_at DESC;

-- name: AcceptMerchantStaffInvite :exec
UPDATE merchant_staff
SET staff_user_id = $1, status = 'active', invite_token = NULL, updated_at = now()
WHERE invite_token = $2;

-- name: UpdateMerchantStaffRole :exec
UPDATE merchant_staff
SET role = $1, updated_at = now()
WHERE id = $2 AND business_user_id = $3;

-- name: DeleteMerchantStaff :exec
DELETE FROM merchant_staff WHERE id = $1 AND business_user_id = $2;

-- name: GetActiveMerchantStaffByUser :many
SELECT * FROM merchant_staff
WHERE staff_user_id = $1 AND status = 'active';

-- name: SaveBusinessProfile :one
INSERT INTO business_profiles (
    user_id,
    company_legal_name,
    company_registration_no,
    industry,
    country_of_incorporation,
    annual_revenue,
    business_website
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id) DO UPDATE SET
    company_legal_name = EXCLUDED.company_legal_name,
    company_registration_no = EXCLUDED.company_registration_no,
    industry = EXCLUDED.industry,
    country_of_incorporation = EXCLUDED.country_of_incorporation,
    annual_revenue = EXCLUDED.annual_revenue,
    business_website = EXCLUDED.business_website,
    updated_at = now()
RETURNING *;


