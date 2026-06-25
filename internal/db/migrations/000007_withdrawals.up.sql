-- Migration 000007: fiat withdrawal system
-- Business-controlled FX rates (Raenest-style: earn spread on conversions)
CREATE TABLE business_fx_rates (
    id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    source_currency VARCHAR(10)   NOT NULL,       -- e.g. "USD"
    target_currency VARCHAR(10)   NOT NULL,       -- e.g. "XAF"
    rate            NUMERIC(20,8) NOT NULL,       -- units of target per 1 source (e.g. 595)
    fee_percent     NUMERIC(5,4)  NOT NULL DEFAULT 0, -- e.g. 0.0100 = 1%
    flat_fee        NUMERIC(20,8) NOT NULL DEFAULT 0, -- flat fee in source currency
    is_active       BOOLEAN       NOT NULL DEFAULT true,
    set_by          UUID          REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (source_currency, target_currency)
);

-- Saved beneficiaries for quick repeat withdrawals
CREATE TABLE beneficiaries (
    id                   UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label                VARCHAR(255) NOT NULL,  -- user-given name e.g. "My MTN Line"
    type                 VARCHAR(20)  NOT NULL,  -- "mobile_money" | "bank"
    destination_currency VARCHAR(10)  NOT NULL,  -- "XAF" | "XOF" | "EUR" | "USD" | "GBP"
    country              VARCHAR(10)  NOT NULL,  -- ISO 3166-1 alpha-2
    -- Mobile money
    phone_number         VARCHAR(50),
    operator             VARCHAR(50),            -- "MTN" | "Orange" | "Wave" | "Moov"
    -- Bank
    bank_name            VARCHAR(255),
    account_number       VARCHAR(100),
    iban                 VARCHAR(100),
    swift_code           VARCHAR(20),
    sort_code            VARCHAR(20),
    routing_number       VARCHAR(20),
    -- Nilos recipient ID if registered (cached to avoid re-creating)
    nilos_recipient_id   VARCHAR(255),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_beneficiary_type CHECK (type IN ('mobile_money', 'bank'))
);

CREATE INDEX idx_beneficiaries_user ON beneficiaries (user_id);

-- Fiat withdrawal requests (USD/EUR/GBP out of Nilos-backed accounts)
CREATE TABLE fiat_withdrawals (
    id                   UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID          NOT NULL REFERENCES users(id),

    -- Source: user's Nilos-backed fiat account
    source_currency      VARCHAR(10)   NOT NULL,   -- "USD" | "EUR" | "GBP"
    source_amount        NUMERIC(20,8) NOT NULL,
    fee                  NUMERIC(20,8) NOT NULL DEFAULT 0,
    fee_currency         VARCHAR(10)   NOT NULL,

    -- FX conversion (NULL when source == destination currency)
    fx_rate              NUMERIC(20,8),

    -- Destination
    destination_type     VARCHAR(20)   NOT NULL,
    -- "mobile_money" | "bank_sepa" | "bank_swift" | "bank_fps" | "bank_cemac" | "bank_uemoa"
    destination_currency VARCHAR(10)   NOT NULL,
    destination_amount   NUMERIC(20,8) NOT NULL,
    destination_country  VARCHAR(10)   NOT NULL,  -- ISO alpha-2
    recipient_name       VARCHAR(255)  NOT NULL,

    -- Mobile money destination
    phone_number         VARCHAR(50),
    operator             VARCHAR(50),

    -- Bank destination
    bank_name            VARCHAR(255),
    account_number       VARCHAR(100),
    iban                 VARCHAR(100),
    swift_code           VARCHAR(20),
    sort_code            VARCHAR(20),
    routing_number       VARCHAR(20),

    -- Processing state
    status               VARCHAR(20)   NOT NULL DEFAULT 'pending',
    -- "pending" → "processing" → "completed" | "failed" | "cancelled"
    nilos_payout_id      VARCHAR(255),
    nilos_recipient_id   VARCHAR(255),
    hub2_reference       VARCHAR(255),
    failure_reason       TEXT,
    reference            VARCHAR(255)  NOT NULL UNIQUE,

    beneficiary_id       UUID REFERENCES beneficiaries(id) ON DELETE SET NULL,

    created_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),

    CONSTRAINT chk_fiat_withdrawal_status
        CHECK (status IN ('pending', 'processing', 'completed', 'failed', 'cancelled'))
);

CREATE INDEX idx_fiat_withdrawals_user   ON fiat_withdrawals (user_id, created_at DESC);
CREATE INDEX idx_fiat_withdrawals_active ON fiat_withdrawals (status)
    WHERE status IN ('pending', 'processing');
CREATE INDEX idx_fiat_withdrawals_hub2   ON fiat_withdrawals (hub2_reference)
    WHERE hub2_reference IS NOT NULL;
