-- Migration 000018: device tokens for Firebase Cloud Messaging push notifications.
-- One row per (device) registration token. A token is globally unique — if it
-- re-registers under a different user, ownership is reassigned (upsert).
CREATE TABLE IF NOT EXISTS device_tokens (
    id         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token      TEXT         NOT NULL UNIQUE,
    platform   VARCHAR(20)  NOT NULL DEFAULT 'unknown', -- ios | android | web | unknown
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_device_tokens_user ON device_tokens(user_id);
