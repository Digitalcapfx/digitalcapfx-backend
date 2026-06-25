-- Migration 000008: settings — security, preferences, support

-- 2FA fields on users
ALTER TABLE users
    ADD COLUMN totp_secret  VARCHAR(255),
    ADD COLUMN totp_enabled BOOLEAN NOT NULL DEFAULT false;

-- User preferences: language, dark mode, biometrics device flag
CREATE TABLE user_preferences (
    user_id             UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    language            VARCHAR(10) NOT NULL DEFAULT 'en',
    dark_mode           VARCHAR(20) NOT NULL DEFAULT 'always',
    -- 'always' | 'never' | 'system'
    biometrics_enabled  BOOLEAN     NOT NULL DEFAULT false,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Support tickets
CREATE TABLE support_tickets (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reference   VARCHAR(50)  NOT NULL UNIQUE,
    subject     VARCHAR(255) NOT NULL,
    category    VARCHAR(50)  NOT NULL DEFAULT 'general',
    status      VARCHAR(20)  NOT NULL DEFAULT 'open',
    priority    VARCHAR(20)  NOT NULL DEFAULT 'normal',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT chk_ticket_status   CHECK (status   IN ('open', 'in_progress', 'resolved', 'closed')),
    CONSTRAINT chk_ticket_priority CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    CONSTRAINT chk_ticket_category CHECK (category IN ('general', 'account', 'payment', 'kyc', 'technical', 'card'))
);

CREATE INDEX idx_support_tickets_user   ON support_tickets (user_id, created_at DESC);
CREATE INDEX idx_support_tickets_status ON support_tickets (status)
    WHERE status IN ('open', 'in_progress');

-- Support messages (threaded conversation per ticket)
CREATE TABLE support_messages (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id   UUID        NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    sender_type VARCHAR(10) NOT NULL, -- 'user' | 'agent'
    sender_id   UUID        REFERENCES users(id) ON DELETE SET NULL,
    body        TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_support_messages_ticket ON support_messages (ticket_id, created_at ASC);

-- FAQs (admin-managed)
CREATE TABLE faqs (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    question   TEXT        NOT NULL,
    answer     TEXT        NOT NULL,
    category   VARCHAR(50) NOT NULL DEFAULT 'general',
    sort_order INTEGER     NOT NULL DEFAULT 0,
    is_active  BOOLEAN     NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Default FAQ seed
INSERT INTO faqs (question, answer, category, sort_order) VALUES
    ('How do I add money to my account?',
     'Fund your account via Mobile Money (MTN, Orange, Wave, Moov) by going to the Crypto section and tapping Fund Account.',
     'account', 1),
    ('How long does a withdrawal take?',
     'Mobile money withdrawals are typically instant to 5 minutes. Bank transfers (SEPA/SWIFT) take 1–3 business days.',
     'payment', 2),
    ('How do I complete identity verification?',
     'Go to Settings → Verification to start the KYC process. You will need a government-issued ID and a selfie.',
     'kyc', 3),
    ('What currencies are supported?',
     'DigitalFX supports USD, EUR, GBP (fiat bank accounts), XAF/XOF (mobile money), and USDC/USDT (stablecoins).',
     'general', 4),
    ('How do I send crypto to another user?',
     'Go to the Crypto section, tap Send, enter the recipient''s phone number, choose the token, and confirm the amount.',
     'account', 5),
    ('What are the withdrawal fees?',
     'Mobile money fees depend on the currency pair — you can preview the exact amount before confirming. Bank transfer fees are 0.5% of the withdrawal amount.',
     'payment', 6),
    ('How do I enable two-factor authentication?',
     'Go to Settings → Security → 2FA, then scan the QR code with Google Authenticator or any TOTP app.',
     'account', 7),
    ('What is the daily withdrawal limit?',
     'After full KYC verification you can withdraw up to the equivalent of $10,000 per day.',
     'payment', 8),
    ('How do I change my PIN?',
     'Go to Settings → Security → Change PIN. You will need to enter your current PIN to set a new one.',
     'account', 9),
    ('What is an Instant USD Account?',
     'An Instant USD Account is a stablecoin wallet (USDC/USDT) on an ERC-4337 smart contract — you can fund it with mobile money and send USD to anyone by phone number at near-zero fees.',
     'general', 10);
