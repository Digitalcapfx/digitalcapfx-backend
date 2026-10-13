-- Durable idempotency for inbound KYC provider webhooks.
--
-- Didit retries a delivery twice on 5xx/404, and a captured delivery can also be
-- replayed from the provider console. Without a dedupe record the same decision
-- would be applied repeatedly — re-sending approval emails and re-triggering
-- fiat account provisioning.
--
-- event_id is the PRIMARY KEY so the uniqueness check is atomic at the database
-- level: two concurrent retries cannot both pass it.

CREATE TABLE IF NOT EXISTS kyc_webhook_events (
    event_id     VARCHAR(120) PRIMARY KEY,
    provider     VARCHAR(30)  NOT NULL,
    session_id   VARCHAR(120),
    vendor_data  VARCHAR(120),
    status       VARCHAR(40),
    processed_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_kyc_webhook_events_processed_at
    ON kyc_webhook_events (processed_at DESC);
