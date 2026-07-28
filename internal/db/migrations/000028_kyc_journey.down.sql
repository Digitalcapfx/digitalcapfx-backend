DROP TABLE IF EXISTS kyc_identity;

ALTER TABLE kyc_intake DROP CONSTRAINT IF EXISTS chk_kyc_intake_status;
UPDATE kyc_intake SET status = 'completed' WHERE status = 'submitted';
UPDATE kyc_intake SET status = 'pending'   WHERE status = 'draft';
ALTER TABLE kyc_intake ALTER COLUMN status SET DEFAULT 'pending';
ALTER TABLE kyc_intake ADD CONSTRAINT chk_kyc_intake_status CHECK (status IN ('pending','completed'));
ALTER TABLE kyc_intake DROP COLUMN IF EXISTS saved_values;
