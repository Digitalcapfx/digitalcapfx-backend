DROP TABLE IF EXISTS kyc_admin_actions;
DROP INDEX IF EXISTS idx_users_google_sub;

ALTER TABLE users ALTER COLUMN pin_hash SET NOT NULL;

ALTER TABLE users
    DROP COLUMN IF EXISTS google_sub,
    DROP COLUMN IF EXISTS auth_provider,
    DROP COLUMN IF EXISTS role;
