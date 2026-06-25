-- Migration 000003: sessions, pin reset, email OTPs, MetaMap KYC, user profile

-- ─── 1. User profile fields ───────────────────────────────────────────────────
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS bio              VARCHAR(500),
    ADD COLUMN IF NOT EXISTS avatar_url       TEXT,
    ADD COLUMN IF NOT EXISTS date_of_birth    DATE,
    ADD COLUMN IF NOT EXISTS nationality      VARCHAR(100),
    ADD COLUMN IF NOT EXISTS is_email_verified BOOLEAN NOT NULL DEFAULT false;

-- ─── 2. Active login sessions (device tracking + refresh token binding) ───────
CREATE TABLE user_sessions (
    id                 UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash VARCHAR(255) UNIQUE NOT NULL,
    device_name        VARCHAR(255),
    device_ip          VARCHAR(45),
    device_ua          VARCHAR(500),
    is_active          BOOLEAN      NOT NULL DEFAULT true,
    last_used_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    expires_at         TIMESTAMPTZ  NOT NULL
);

-- ─── 3. Email OTPs (email verification + PIN reset codes) ─────────────────────
CREATE TABLE email_otps (
    id         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    email      VARCHAR(255) NOT NULL,
    code       VARCHAR(10)  NOT NULL,
    purpose    VARCHAR(50)  NOT NULL,   -- verify_email | pin_reset
    expires_at TIMESTAMPTZ  NOT NULL,
    used       BOOLEAN      NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ─── 4. MetaMap KYC verification records ──────────────────────────────────────
CREATE TABLE metamap_verifications (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID         UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    applicant_id     VARCHAR(255) UNIQUE NOT NULL,
    flow_id          VARCHAR(255) NOT NULL,
    identity_access  TEXT         NOT NULL,   -- SDK token returned by MetaMap API
    status           VARCHAR(50)  NOT NULL DEFAULT 'pending',  -- pending|processing|approved|rejected
    result_data      JSONB,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ─── Indexes ──────────────────────────────────────────────────────────────────
CREATE INDEX idx_user_sessions_user_active ON user_sessions(user_id, is_active);
CREATE INDEX idx_user_sessions_token_hash  ON user_sessions(refresh_token_hash);
CREATE INDEX idx_email_otps_lookup         ON email_otps(email, purpose) WHERE used = false;
CREATE INDEX idx_metamap_user              ON metamap_verifications(user_id);
