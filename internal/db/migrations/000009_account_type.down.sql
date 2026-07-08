-- Rollback migration 000009: account type

DROP TABLE IF EXISTS business_directors;
DROP TABLE IF EXISTS business_profiles;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS chk_account_type;

ALTER TABLE users
    DROP COLUMN IF EXISTS account_type,
    DROP COLUMN IF EXISTS country;
