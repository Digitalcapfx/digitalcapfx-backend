DROP TABLE IF EXISTS metamap_verifications;
DROP TABLE IF EXISTS email_otps;
DROP TABLE IF EXISTS user_sessions;

ALTER TABLE users
    DROP COLUMN IF EXISTS bio,
    DROP COLUMN IF EXISTS avatar_url,
    DROP COLUMN IF EXISTS date_of_birth,
    DROP COLUMN IF EXISTS nationality,
    DROP COLUMN IF EXISTS is_email_verified;
