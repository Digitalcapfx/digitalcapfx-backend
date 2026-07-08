-- Down Migration 000013: Referrals and Points

DROP TABLE IF EXISTS points_ledger;
ALTER TABLE users DROP COLUMN IF EXISTS referral_code;
ALTER TABLE users DROP COLUMN IF EXISTS referred_by;
