-- Manual mobile-money rails (parallel to Hub2, which stays in place). The
-- business publishes its own collection numbers; customers pay out-of-band and
-- claim the payment, then an admin confirms manually and credits the ledger
-- (after taking a charge). Withdrawals mirror this in reverse. Scoped to XOF/XAF.

CREATE TABLE IF NOT EXISTS manual_momo_accounts (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    provider     VARCHAR(30)  NOT NULL,            -- wave | orange | mtn | moov | kbine
    display_name VARCHAR(60)  NOT NULL,            -- shown to the customer, e.g. "Wave"
    phone_number VARCHAR(30)  NOT NULL,            -- the number customers pay to
    account_name VARCHAR(100),                     -- registered holder name on the momo account
    currency     VARCHAR(3)   NOT NULL DEFAULT 'XOF',
    country      VARCHAR(2)   NOT NULL DEFAULT 'CI',
    instructions TEXT,
    is_active    BOOLEAN      NOT NULL DEFAULT true,
    sort_order   INT          NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_momo_currency CHECK (currency IN ('XOF','XAF'))
);
CREATE INDEX IF NOT EXISTS idx_momo_accounts_active ON manual_momo_accounts(is_active, sort_order);

-- Seed the Ivory Coast (XOF) commercial numbers.
INSERT INTO manual_momo_accounts (provider, display_name, phone_number, currency, country, sort_order) VALUES
    ('wave',   'Wave',         '+225 01 04 88 56 42', 'XOF', 'CI', 1),
    ('orange', 'Orange Money', '+225 07 05 56 45 22', 'XOF', 'CI', 2),
    ('mtn',    'MTN MoMo',     '+225 05 01 35 04 38', 'XOF', 'CI', 3),
    ('moov',   'Moov Money',   '+225 01 73 21 93 77', 'XOF', 'CI', 4),
    ('kbine',  'KBINE',        '+225 07 47 95 88 74', 'XOF', 'CI', 5)
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS manual_deposits (
    id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID          NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id      UUID          NOT NULL REFERENCES accounts(id),
    momo_account_id UUID          REFERENCES manual_momo_accounts(id) ON DELETE SET NULL,
    provider        VARCHAR(30)   NOT NULL,
    currency        VARCHAR(3)    NOT NULL,
    claimed_amount  NUMERIC(18,6) NOT NULL,         -- what the customer says they paid
    credited_amount NUMERIC(18,6),                  -- net credited to the ledger (set on confirm)
    charge          NUMERIC(18,6),                  -- claimed_amount - credited_amount (set on confirm)
    sender_phone    VARCHAR(30),                    -- customer's own momo number
    sender_name     VARCHAR(100),
    reference       VARCHAR(120),                   -- customer's momo transaction id / reference
    note            TEXT,
    status          VARCHAR(20)   NOT NULL DEFAULT 'pending', -- pending|confirmed|rejected
    admin_note      TEXT,
    reviewed_by     UUID          REFERENCES admin_staff(id) ON DELETE SET NULL,
    reviewed_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_manual_deposit_status CHECK (status IN ('pending','confirmed','rejected'))
);
CREATE INDEX IF NOT EXISTS idx_manual_deposits_user ON manual_deposits(user_id);
CREATE INDEX IF NOT EXISTS idx_manual_deposits_status ON manual_deposits(status, created_at);

CREATE TABLE IF NOT EXISTS manual_withdrawals (
    id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID          NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id      UUID          NOT NULL REFERENCES accounts(id),
    provider        VARCHAR(30)   NOT NULL,
    currency        VARCHAR(3)    NOT NULL,
    amount          NUMERIC(18,6) NOT NULL,         -- debited/held from the customer at request
    charge          NUMERIC(18,6),                  -- business charge (set on completion)
    payout_amount   NUMERIC(18,6),                  -- amount - charge, actually sent to recipient
    recipient_phone VARCHAR(30)   NOT NULL,         -- momo number to pay out to
    recipient_name  VARCHAR(100),
    note            TEXT,
    status          VARCHAR(20)   NOT NULL DEFAULT 'pending', -- pending|completed|rejected
    admin_note      TEXT,
    reviewed_by     UUID          REFERENCES admin_staff(id) ON DELETE SET NULL,
    reviewed_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_manual_withdrawal_status CHECK (status IN ('pending','completed','rejected'))
);
CREATE INDEX IF NOT EXISTS idx_manual_withdrawals_user ON manual_withdrawals(user_id);
CREATE INDEX IF NOT EXISTS idx_manual_withdrawals_status ON manual_withdrawals(status, created_at);
