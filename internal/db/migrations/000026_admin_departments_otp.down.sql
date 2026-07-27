DROP INDEX IF EXISTS idx_admin_staff_department;
ALTER TABLE admin_staff
    DROP COLUMN IF EXISTS department_id,
    DROP COLUMN IF EXISTS invite_otp_hash,
    DROP COLUMN IF EXISTS invite_otp_expires_at;
DROP TABLE IF EXISTS admin_departments;
