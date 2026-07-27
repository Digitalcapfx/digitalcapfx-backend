-- Admin team management: departments + OTP-based invite acceptance.

-- Departments a staff member can belong to (Compliance, Finance, Support, …).
CREATE TABLE IF NOT EXISTS admin_departments (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(100) UNIQUE NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Staff: optional department + a hashed one-time code the invitee enters to
-- accept (in addition to the emailed link token).
ALTER TABLE admin_staff
    ADD COLUMN IF NOT EXISTS department_id         UUID REFERENCES admin_departments(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS invite_otp_hash        VARCHAR(255),
    ADD COLUMN IF NOT EXISTS invite_otp_expires_at  TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_admin_staff_department ON admin_staff(department_id) WHERE department_id IS NOT NULL;
