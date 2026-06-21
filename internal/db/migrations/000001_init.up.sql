-- ─────────────────────────────────────────────────────────────────────────────
-- Users
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE users (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    phone_number VARCHAR(20) UNIQUE NOT NULL,
    email        VARCHAR(255) UNIQUE,
    first_name   VARCHAR(100) NOT NULL,
    last_name    VARCHAR(100) NOT NULL,
    pin_hash     VARCHAR(255) NOT NULL,
    kyc_status   VARCHAR(20)  NOT NULL DEFAULT 'pending',  -- pending|submitted|approved|rejected
    is_active    BOOLEAN      NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Accounts (one per supported currency per user)
-- Supported: XAF, XOF, USD, GBP, EUR
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE accounts (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    currency          VARCHAR(3)  NOT NULL,
    balance           NUMERIC(18,6) NOT NULL DEFAULT 0,
    available_balance NUMERIC(18,6) NOT NULL DEFAULT 0,
    account_number    VARCHAR(20)   UNIQUE NOT NULL,
    status            VARCHAR(20)   NOT NULL DEFAULT 'active',  -- active|frozen|closed
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, currency),
    CONSTRAINT chk_currency CHECK (currency IN ('XAF','XOF','USD','GBP','EUR')),
    CONSTRAINT chk_balance   CHECK (balance >= 0)
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Fiat transactions
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE transactions (
    id          UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    reference   VARCHAR(100)  UNIQUE NOT NULL,
    account_id  UUID          NOT NULL REFERENCES accounts(id),
    type        VARCHAR(20)   NOT NULL,                          -- credit|debit
    amount      NUMERIC(18,6) NOT NULL,
    currency    VARCHAR(3)    NOT NULL,
    fee         NUMERIC(18,6) NOT NULL DEFAULT 0,
    description TEXT,
    status      VARCHAR(20)   NOT NULL DEFAULT 'pending',       -- pending|completed|failed|reversed
    metadata    JSONB,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_amount CHECK (amount > 0)
);

-- ─────────────────────────────────────────────────────────────────────────────
-- WaaS wallets  (custody wallets via Rach Payments API)
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE waas_wallets (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    waas_wallet_id VARCHAR(255) NOT NULL,                        -- ID from Payments API
    network        VARCHAR(50)  NOT NULL,                        -- BSC|ETH|TRON|SOLANA|BTC|XRP
    address        VARCHAR(255) NOT NULL,
    is_default     BOOLEAN      NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- CaaS abstraction wallets  (ERC-4337 smart accounts via Rach CaaS)
-- One per user — phone number is the identifier for P2P sends
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE caas_wallets (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID        UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    caas_wallet_id      VARCHAR(255) NOT NULL,                   -- ID from CaaS
    abstraction_address VARCHAR(255) UNIQUE NOT NULL,            -- ERC-4337 smart account
    is_active           BOOLEAN      NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Crypto P2P transactions (USDT / USDC between DigitalFX users by phone)
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE crypto_transactions (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    reference        VARCHAR(100) UNIQUE NOT NULL,
    sender_user_id   UUID         NOT NULL REFERENCES users(id),
    receiver_phone   VARCHAR(20)  NOT NULL,
    receiver_user_id UUID         REFERENCES users(id),
    token            VARCHAR(10)  NOT NULL,                      -- USDT|USDC
    amount           NUMERIC(18,6) NOT NULL,
    tx_hash          VARCHAR(255),
    status           VARCHAR(20)  NOT NULL DEFAULT 'pending',    -- pending|processing|completed|failed
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_token CHECK (token IN ('USDT','USDC'))
);

-- ─────────────────────────────────────────────────────────────────────────────
-- KYC documents
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE kyc_documents (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    doc_type         VARCHAR(50) NOT NULL,  -- national_id|passport|selfie|proof_of_address
    doc_url          TEXT        NOT NULL,
    status           VARCHAR(20) NOT NULL DEFAULT 'pending',  -- pending|approved|rejected
    rejection_reason TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- HUB2 payment records (Mobile Money, XAF/XOF on/off ramp)
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE hub2_payments (
    id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id UUID         REFERENCES transactions(id),
    hub2_reference VARCHAR(255) UNIQUE NOT NULL,
    payment_method VARCHAR(50)  NOT NULL,  -- mobile_money|card|bank_transfer
    operator       VARCHAR(50),            -- Orange|MTN|Wave|Moov|Airtel
    phone_number   VARCHAR(20),
    status         VARCHAR(20)  NOT NULL DEFAULT 'pending',
    hub2_status    VARCHAR(50),
    metadata       JSONB,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- OTPs (phone number verification)
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE otps (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    phone_number VARCHAR(20) NOT NULL,
    code         VARCHAR(10) NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    used         BOOLEAN     NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Indexes
-- ─────────────────────────────────────────────────────────────────────────────
CREATE INDEX idx_accounts_user_id             ON accounts(user_id);
CREATE INDEX idx_transactions_account_id      ON transactions(account_id);
CREATE INDEX idx_transactions_reference       ON transactions(reference);
CREATE INDEX idx_transactions_status          ON transactions(status);
CREATE INDEX idx_waas_wallets_user_id         ON waas_wallets(user_id);
CREATE INDEX idx_caas_wallets_user_id         ON caas_wallets(user_id);
CREATE INDEX idx_crypto_tx_sender             ON crypto_transactions(sender_user_id);
CREATE INDEX idx_crypto_tx_receiver_phone     ON crypto_transactions(receiver_phone);
CREATE INDEX idx_kyc_documents_user_id        ON kyc_documents(user_id);
CREATE INDEX idx_hub2_payments_reference      ON hub2_payments(hub2_reference);
CREATE INDEX idx_otps_phone_expires           ON otps(phone_number, expires_at) WHERE used = false;
