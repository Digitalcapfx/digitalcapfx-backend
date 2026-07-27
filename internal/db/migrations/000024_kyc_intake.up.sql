-- KYC intake: the fields DigitalFX collects itself BEFORE launching the Sumsub
-- SDK dialog. The set is driven by what our downstream providers require:
--   - Nomba (NGN virtual accounts / payouts) → account holder name + BVN.
--   - Nilos (EUR/GBP/… rails, recipients)    → legal name, DOB, nationality,
--     structured residential/business address, country.
-- The Sumsub token is only issued once this intake is marked completed.
CREATE TABLE IF NOT EXISTS kyc_intake (
    user_id            UUID         PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    account_type       VARCHAR(20)  NOT NULL DEFAULT 'individual', -- individual|business

    -- Identity (individual, or the business's authorised representative)
    legal_first_name   VARCHAR(100),
    legal_last_name    VARCHAR(100),
    date_of_birth      VARCHAR(20),   -- YYYY-MM-DD (TEXT, matching users.date_of_birth)
    nationality        VARCHAR(100),
    bvn                VARCHAR(11),   -- Nigerian Bank Verification Number (Nomba)

    -- Structured address (Nilos recipient / compliance)
    address_line1      VARCHAR(255),
    address_line2      VARCHAR(255),
    city               VARCHAR(100),
    state              VARCHAR(100),
    postal_code        VARCHAR(20),
    country            VARCHAR(10),   -- ISO 3166-1 alpha-2

    -- Compliance context
    occupation         VARCHAR(150),
    source_of_funds    VARCHAR(150),
    purpose_of_account VARCHAR(150),

    status             VARCHAR(20)  NOT NULL DEFAULT 'pending', -- pending|completed
    submitted_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT chk_kyc_intake_status CHECK (status IN ('pending','completed'))
);
