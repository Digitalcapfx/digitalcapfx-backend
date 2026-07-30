-- Restore NOT NULL. Any NULL rows (owner actions) would block the constraint, so
-- delete them first — they cannot be attributed to an admin_staff row anyway.
DELETE FROM admin_audit_logs WHERE staff_id IS NULL;
ALTER TABLE admin_audit_logs ALTER COLUMN staff_id SET NOT NULL;
