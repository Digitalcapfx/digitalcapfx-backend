-- Migration 000021: Virtual Cards and VTU Transactions

-- Alter existing virtual_cards table (created in 000005)
ALTER TABLE virtual_cards
    ADD COLUMN IF NOT EXISTS funding_account_id UUID REFERENCES accounts(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS funding_wallet_id  UUID REFERENCES waas_wallets(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS color_theme        VARCHAR(50),
    ADD COLUMN IF NOT EXISTS card_art_id        VARCHAR(50),
    ADD COLUMN IF NOT EXISTS billing_address    JSONB,
    ADD COLUMN IF NOT EXISTS masked_pan         VARCHAR(20),
    ADD COLUMN IF NOT EXISTS expiry             VARCHAR(10),
    ADD COLUMN IF NOT EXISTS cvv_encrypted      VARCHAR(255),
    ADD COLUMN IF NOT EXISTS status             VARCHAR(50) NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS provider_card_id   VARCHAR(255),
    ADD COLUMN IF NOT EXISTS updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Drop the old unique index that restricted users to 1 active card
DROP INDEX IF EXISTS idx_virtual_cards_user_active;

CREATE INDEX IF NOT EXISTS idx_virtual_cards_user_id ON virtual_cards(user_id);
CREATE INDEX IF NOT EXISTS idx_virtual_cards_provider_id ON virtual_cards(provider_card_id);

CREATE TABLE card_transactions (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id          UUID         NOT NULL REFERENCES virtual_cards(id) ON DELETE CASCADE,
    amount           NUMERIC(18,4) NOT NULL,
    currency         VARCHAR(10)  NOT NULL, -- Usually USD
    merchant_name    VARCHAR(255) NOT NULL,
    merchant_city    VARCHAR(100),
    merchant_country VARCHAR(100),
    status           VARCHAR(50)  NOT NULL DEFAULT 'pending', -- pending, settled, failed, refunded
    provider_tx_id   VARCHAR(255),
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_card_transactions_card_id ON card_transactions(card_id);

CREATE TABLE vtu_transactions (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id       UUID         NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    amount           NUMERIC(18,4) NOT NULL,
    currency         VARCHAR(10)  NOT NULL, -- XAF or XOF
    service_type     VARCHAR(50)  NOT NULL, -- airtime, data, bill
    provider         VARCHAR(100) NOT NULL, -- e.g., Hub2, MTN, Orange, ENEO
    target_phone     VARCHAR(50),           -- For airtime/data
    target_account   VARCHAR(100),          -- For bills (meter number, contract ID)
    reference        VARCHAR(255) UNIQUE,   -- Our internal ref
    provider_ref     VARCHAR(255),          -- Hub2's ref
    status           VARCHAR(50)  NOT NULL DEFAULT 'pending', -- pending, successful, failed
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_vtu_transactions_user_id ON vtu_transactions(user_id);
CREATE INDEX idx_vtu_transactions_reference ON vtu_transactions(reference);
