-- Add social auth, role, and auth provider fields to users

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS role VARCHAR(50) NOT NULL DEFAULT 'user',
    ADD COLUMN IF NOT EXISTS auth_provider VARCHAR(50) NOT NULL DEFAULT 'phone',
    ADD COLUMN IF NOT EXISTS google_sub VARCHAR(255) UNIQUE;

-- pin_hash becomes optional for social-auth users
ALTER TABLE users ALTER COLUMN pin_hash DROP NOT NULL;

-- Index for fast Google sub lookups
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_google_sub ON users(google_sub) WHERE google_sub IS NOT NULL;

-- Add admin audit log for KYC decisions
CREATE TABLE IF NOT EXISTS kyc_admin_actions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    admin_id    UUID NOT NULL REFERENCES users(id),
    action      VARCHAR(50) NOT NULL, -- 'approved' | 'rejected'
    reason      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_kyc_admin_actions_user ON kyc_admin_actions(user_id);
