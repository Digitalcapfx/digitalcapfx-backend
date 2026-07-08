-- Migration 000012: Merchant Teams (Business Staff)

CREATE TABLE IF NOT EXISTS merchant_staff (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    business_user_id UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    staff_user_id    UUID         REFERENCES users(id) ON DELETE SET NULL, -- Null until accepted
    email            VARCHAR(255) NOT NULL,
    role             VARCHAR(50)  NOT NULL, -- manager, developer, viewer
    status           VARCHAR(20)  NOT NULL DEFAULT 'pending', -- pending, active, disabled
    invite_token     VARCHAR(255) UNIQUE,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (business_user_id, email) -- a staff email can only be invited once per business
);

CREATE INDEX IF NOT EXISTS idx_merchant_staff_business ON merchant_staff(business_user_id);
CREATE INDEX IF NOT EXISTS idx_merchant_staff_token ON merchant_staff(invite_token);
CREATE INDEX IF NOT EXISTS idx_merchant_staff_user ON merchant_staff(staff_user_id);
