-- The bootstrap owner has no admin_staff row, so their audited actions cannot
-- reference admin_staff(id). Allow a NULL staff_id (the denormalized staff_name /
-- staff_email still identify the actor) so owner actions are recorded instead of
-- silently dropped on the FK violation.
ALTER TABLE admin_audit_logs ALTER COLUMN staff_id DROP NOT NULL;
