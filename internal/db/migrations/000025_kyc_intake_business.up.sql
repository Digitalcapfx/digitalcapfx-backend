-- Business (KYB) intake extras that are structured data (not document uploads):
-- importer flag (drives the Proof of Imports requirement), the top-3
-- counterparties (EUR/GBP NRE), and UBO/director contact details.
ALTER TABLE kyc_intake
    ADD COLUMN IF NOT EXISTS is_importer    BOOLEAN,
    ADD COLUMN IF NOT EXISTS counterparties JSONB,   -- [{country, relationship, purpose}, ...]
    ADD COLUMN IF NOT EXISTS contact_email  VARCHAR(255),
    ADD COLUMN IF NOT EXISTS contact_phone  VARCHAR(50);
