-- Migration 000009: account type - individual and business accounts

-- 1. account_type + country on users
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS account_type VARCHAR(20) NOT NULL DEFAULT 'individual',
    ADD COLUMN IF NOT EXISTS country      VARCHAR(10);  -- ISO 3166-1 alpha-2

ALTER TABLE users
    ADD CONSTRAINT chk_account_type CHECK (account_type IN ('individual', 'business'));

-- 2. Business profile (one per business user, created at signup)
CREATE TABLE IF NOT EXISTS business_profiles (
    user_id                  UUID         PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    company_legal_name       VARCHAR(255) NOT NULL,
    company_registration_no  VARCHAR(100) NOT NULL,
    industry                 VARCHAR(100) NOT NULL,
    country_of_incorporation VARCHAR(10)  NOT NULL,  -- ISO 3166-1 alpha-2
    annual_revenue           VARCHAR(100) NOT NULL,  -- free-text range e.g. "$50k-$500k"
    business_website         VARCHAR(500),           -- optional
    -- completion flags for the deferred KYC steps
    directors_complete       BOOLEAN      NOT NULL DEFAULT false,
    documents_complete       BOOLEAN      NOT NULL DEFAULT false,
    created_at               TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_business_profiles_user ON business_profiles(user_id);

-- 3. Business directors (multiple allowed)
CREATE TABLE IF NOT EXISTS business_directors (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    first_name    VARCHAR(100) NOT NULL,
    last_name     VARCHAR(100) NOT NULL,
    job_title     VARCHAR(100) NOT NULL,
    date_of_birth TIMESTAMPTZ  NOT NULL,
    nationality   VARCHAR(100) NOT NULL,
    phone_number  VARCHAR(50)  NOT NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_business_directors_user ON business_directors(user_id);