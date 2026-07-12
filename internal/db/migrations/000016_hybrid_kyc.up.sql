-- Migration 000016: hybrid KYC control
-- Records the KYC provider's (e.g. Sumsub) automated decision separately from
-- the final kyc_status, and flags when an admin has taken manual control so a
-- later automated webhook cannot silently revert an admin decision.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS kyc_provider_status TEXT,
    ADD COLUMN IF NOT EXISTS kyc_manual_override BOOLEAN NOT NULL DEFAULT false;
