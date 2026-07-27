ALTER TABLE kyc_intake
    DROP COLUMN IF EXISTS is_importer,
    DROP COLUMN IF EXISTS counterparties,
    DROP COLUMN IF EXISTS contact_email,
    DROP COLUMN IF EXISTS contact_phone;
