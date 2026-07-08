-- Migration 000011: Admin RBAC (Staff and Audit Logs)

-- 1. admin_staff
CREATE TABLE IF NOT EXISTS admin_staff (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID        UNIQUE REFERENCES users(id) ON DELETE SET NULL, -- Null if invite not yet accepted
    email               VARCHAR(255) UNIQUE NOT NULL,
    name                VARCHAR(100) NOT NULL,
    role                VARCHAR(50) NOT NULL, -- owner, admin, compliance, support, finance, readonly
    custom_permissions  JSONB       NOT NULL DEFAULT '[]',
    revoked_permissions JSONB       NOT NULL DEFAULT '[]',
    is_active           BOOLEAN     NOT NULL DEFAULT true,
    invited_by          UUID        REFERENCES admin_staff(id) ON DELETE SET NULL,
    invite_token        VARCHAR(255) UNIQUE,
    invite_accepted_at  TIMESTAMPTZ,
    last_login_at       TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_admin_staff_email ON admin_staff(email);
CREATE INDEX IF NOT EXISTS idx_admin_staff_token ON admin_staff(invite_token);

-- 2. admin_audit_logs
CREATE TABLE IF NOT EXISTS admin_audit_logs (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    staff_id     UUID         NOT NULL REFERENCES admin_staff(id) ON DELETE CASCADE,
    staff_name   VARCHAR(100) NOT NULL, -- denormalized for historical record
    staff_email  VARCHAR(255) NOT NULL, -- denormalized
    action       VARCHAR(100) NOT NULL,
    resource     VARCHAR(100) NOT NULL,
    resource_id  VARCHAR(255),
    details      JSONB,
    ip_address   VARCHAR(45),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_admin_audit_staff ON admin_audit_logs(staff_id);
CREATE INDEX IF NOT EXISTS idx_admin_audit_resource ON admin_audit_logs(resource, resource_id);
CREATE INDEX IF NOT EXISTS idx_admin_audit_created ON admin_audit_logs(created_at);
