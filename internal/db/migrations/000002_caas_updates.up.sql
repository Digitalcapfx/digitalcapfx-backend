-- ─────────────────────────────────────────────────────────────────────────────
-- Migration 000002: align local schema with CaaS API swagger
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. caas_wallets — add blind_index (the CaaS user identifier returned from provision)
ALTER TABLE caas_wallets ADD COLUMN blind_index VARCHAR(255) UNIQUE;

-- 2. crypto_transactions — add CaaS transfer fields
ALTER TABLE crypto_transactions
    ADD COLUMN quote_id            VARCHAR(255),          -- FX quote ID passed to transfers/send
    ADD COLUMN caas_transfer_id    VARCHAR(255) UNIQUE,   -- transfer_id returned by CaaS
    ADD COLUMN idempotency_key     VARCHAR(255) UNIQUE,   -- caller-supplied idempotency key
    ADD COLUMN local_fiat_amount   NUMERIC(18,6),         -- fiat amount in local currency (XOF/XAF)
    ADD COLUMN local_currency      VARCHAR(10);           -- XOF | XAF | USD | GBP | EUR

-- 3. fx_quotes — store CaaS FX quotes before committing a transfer
CREATE TABLE fx_quotes (
    id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID         NOT NULL REFERENCES users(id),
    quote_id       VARCHAR(255) UNIQUE NOT NULL,   -- quote ID from CaaS /v1/fx/quote
    fiat_amount    NUMERIC(18,6) NOT NULL,
    local_currency VARCHAR(10)  NOT NULL,          -- XOF | XAF | USD | ...
    target_token   VARCHAR(10)  NOT NULL,          -- USDC | USDT
    rate           NUMERIC(18,8) NOT NULL,         -- applied FX rate
    expires_at     TIMESTAMPTZ  NOT NULL,          -- quote expiry from CaaS
    used           BOOLEAN      NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- 4. caas_withdrawals — track off-ramp requests sent to CaaS
CREATE TABLE caas_withdrawals (
    id               UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID          NOT NULL REFERENCES users(id),
    caas_withdrawal_id VARCHAR(255) UNIQUE NOT NULL,   -- withdrawal_id from CaaS response
    phone            VARCHAR(20)   NOT NULL,
    amount           NUMERIC(18,6) NOT NULL,
    token            VARCHAR(10)   NOT NULL,           -- USDC | USDT
    payout_mobile    VARCHAR(20)   NOT NULL,
    payout_network   VARCHAR(50)   NOT NULL,           -- e.g. Orange | MTN | Wave
    idempotency_key  VARCHAR(255)  UNIQUE NOT NULL,
    status           VARCHAR(20)   NOT NULL DEFAULT 'pending', -- pending|processing|completed|failed
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_withdrawal_token CHECK (token IN ('USDC','USDT'))
);

-- 5. hub2_payments — mark when a deposit has been forwarded to CaaS fund endpoint
ALTER TABLE hub2_payments ADD COLUMN caas_funded_at TIMESTAMPTZ;

-- Indexes
CREATE INDEX idx_fx_quotes_user_id       ON fx_quotes(user_id);
CREATE INDEX idx_fx_quotes_expires_at    ON fx_quotes(expires_at) WHERE used = false;
CREATE INDEX idx_caas_withdrawals_user   ON caas_withdrawals(user_id);
CREATE INDEX idx_caas_withdrawals_status ON caas_withdrawals(status);
CREATE INDEX idx_crypto_tx_quote_id      ON crypto_transactions(quote_id);
CREATE INDEX idx_caas_wallets_blind      ON caas_wallets(blind_index);
