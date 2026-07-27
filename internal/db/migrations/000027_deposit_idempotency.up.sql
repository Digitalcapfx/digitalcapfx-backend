-- Idempotency for inbound deposit crediting. Provider webhooks (Nomba, Nilos)
-- can be redelivered; this table guarantees each provider event credits the
-- ledger at most once (prevents balance inflation).
CREATE TABLE IF NOT EXISTS processed_deposit_events (
    id         UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    provider   VARCHAR(20)   NOT NULL, -- nomba | nilos
    event_id   VARCHAR(200)  NOT NULL, -- provider transaction/event id
    account_id UUID          REFERENCES accounts(id) ON DELETE SET NULL,
    amount     NUMERIC(18,6) NOT NULL,
    currency   VARCHAR(10)   NOT NULL,
    created_at TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (provider, event_id)
);
