-- Migration 000013: Referrals and Points

ALTER TABLE users 
    ADD COLUMN IF NOT EXISTS referral_code VARCHAR(50) UNIQUE,
    ADD COLUMN IF NOT EXISTS referred_by UUID REFERENCES users(id) ON DELETE SET NULL;

-- Index on referred_by
CREATE INDEX IF NOT EXISTS idx_users_referred_by ON users(referred_by);

-- Points ledger table
CREATE TABLE IF NOT EXISTS points_ledger (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount      INTEGER     NOT NULL,
    description TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index for quick lookup of user points ledger
CREATE INDEX IF NOT EXISTS idx_points_ledger_user ON points_ledger(user_id, created_at DESC);
