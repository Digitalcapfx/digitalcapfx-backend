-- Track which provider handled a verification + its external ID.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS kyc_provider VARCHAR(20); -- 'metamap' | 'sumsub'

-- Sumsub verification records (mirrors metamap_verifications structure).
CREATE TABLE sumsub_verifications (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    applicant_id    VARCHAR(255) UNIQUE NOT NULL,
    level_name      VARCHAR(255) NOT NULL,
    access_token    TEXT        NOT NULL,
    status          VARCHAR(50)  NOT NULL DEFAULT 'pending',
    result_data     JSONB,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_sumsub_verifications_applicant ON sumsub_verifications(applicant_id);
