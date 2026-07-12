-- Migration 000016 (down)
ALTER TABLE users
    DROP COLUMN IF EXISTS kyc_provider_status,
    DROP COLUMN IF EXISTS kyc_manual_override;
