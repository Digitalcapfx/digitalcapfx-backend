-- Nilos account linkage
ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS nilos_account_id VARCHAR(100) UNIQUE,
    ADD COLUMN IF NOT EXISTS nilos_customer_id VARCHAR(100),
    ADD COLUMN IF NOT EXISTS iban VARCHAR(50),
    ADD COLUMN IF NOT EXISTS bic VARCHAR(20);

CREATE INDEX IF NOT EXISTS idx_accounts_nilos ON accounts(nilos_account_id) WHERE nilos_account_id IS NOT NULL;

-- Virtual debit cards
CREATE TABLE IF NOT EXISTS virtual_cards (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_name    VARCHAR(100) NOT NULL DEFAULT 'Daily Spending',
    last_four    VARCHAR(4)   NOT NULL,
    currency     VARCHAR(10)  NOT NULL DEFAULT 'USD',
    card_network VARCHAR(20)  NOT NULL DEFAULT 'mastercard',
    is_active    BOOLEAN      NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_virtual_cards_user_active
    ON virtual_cards(user_id) WHERE is_active = true;

-- FX rates cache (refreshed periodically by a background job)
CREATE TABLE IF NOT EXISTS fx_rates (
    base_currency  VARCHAR(10)  NOT NULL,
    quote_currency VARCHAR(10)  NOT NULL,
    rate           NUMERIC(24,8) NOT NULL,
    source         VARCHAR(50),
    fetched_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (base_currency, quote_currency)
);

-- Seed approximate rates (USD base)
INSERT INTO fx_rates (base_currency, quote_currency, rate, source) VALUES
    ('USD', 'USD', 1.0,      'seed'),
    ('USD', 'EUR', 0.91,     'seed'),
    ('USD', 'GBP', 0.79,     'seed'),
    ('USD', 'XAF', 609.0,    'seed'),
    ('USD', 'XOF', 609.0,    'seed'),
    ('USD', 'USDC', 1.0,     'seed'),
    ('USD', 'USDT', 1.0,     'seed'),
    ('USD', 'BTC', 0.000015, 'seed'),
    ('USD', 'ETH', 0.00033,  'seed')
ON CONFLICT (base_currency, quote_currency) DO NOTHING;
