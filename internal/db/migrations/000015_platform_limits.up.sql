-- Migration 000015: admin-controllable limits
-- Makes the account-tier limits editable by platform owners (persisted, not
-- just hardcoded) and adds per-user overrides for granular control.

-- 1. Platform tier limits — one row per tier, editable via the admin API.
CREATE TABLE IF NOT EXISTS platform_limits (
    tier                    VARCHAR(20)      PRIMARY KEY CHECK (tier IN ('individual', 'business')),
    daily_withdrawal_usd    DOUBLE PRECISION NOT NULL,
    per_transaction_usd     DOUBLE PRECISION NOT NULL,
    monthly_volume_usd      DOUBLE PRECISION NOT NULL,
    max_holding_balance_usd DOUBLE PRECISION NOT NULL,
    daily_transaction_count INTEGER          NOT NULL,
    updated_at              TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_by              UUID
);

-- Seed with the built-in defaults (must match services.DefaultLimitsResolver).
INSERT INTO platform_limits
    (tier, daily_withdrawal_usd, per_transaction_usd, monthly_volume_usd, max_holding_balance_usd, daily_transaction_count)
VALUES
    ('individual', 10000,  10000,  100000,  50000,   50),
    ('business',   250000, 100000, 5000000, 5000000, 1000)
ON CONFLICT (tier) DO NOTHING;

-- 2. Per-user overrides — NULL column = fall back to the tier limit. Lets
-- owners grant a specific user higher (or lower) caps than their tier.
CREATE TABLE IF NOT EXISTS user_limit_overrides (
    user_id                 UUID             PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    daily_withdrawal_usd    DOUBLE PRECISION,
    per_transaction_usd     DOUBLE PRECISION,
    monthly_volume_usd      DOUBLE PRECISION,
    max_holding_balance_usd DOUBLE PRECISION,
    daily_transaction_count INTEGER,
    note                    TEXT             NOT NULL DEFAULT '',
    updated_at              TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_by              UUID
);
