-- name: CreateStaffMember :one
INSERT INTO admin_staff (
    email, name, role, custom_permissions, revoked_permissions, invited_by, invite_token
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: GetStaffMemberByID :one
SELECT * FROM admin_staff WHERE id = $1;

-- name: GetStaffMemberByUserID :one
SELECT * FROM admin_staff WHERE user_id = $1;

-- name: GetStaffMemberByEmail :one
SELECT * FROM admin_staff WHERE email = $1;

-- name: GetStaffMemberByInviteToken :one
SELECT * FROM admin_staff WHERE invite_token = $1;

-- name: ListStaffMembers :many
SELECT * FROM admin_staff
WHERE (sqlc.arg(include_inactive)::boolean = true OR is_active = true)
ORDER BY created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountStaffMembers :one
SELECT count(*) FROM admin_staff
WHERE (sqlc.arg(include_inactive)::boolean = true OR is_active = true);

-- name: UpdateStaffMember :one
UPDATE admin_staff
SET role = coalesce(sqlc.narg('role'), role),
    custom_permissions = coalesce(sqlc.narg('custom_permissions'), custom_permissions),
    revoked_permissions = coalesce(sqlc.narg('revoked_permissions'), revoked_permissions),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: AcceptStaffInvite :exec
UPDATE admin_staff
SET user_id = $1, invite_token = NULL, invite_accepted_at = now(), updated_at = now(), is_active = true
WHERE invite_token = $2;

-- name: DisableStaffMember :exec
UPDATE admin_staff SET is_active = false, updated_at = now() WHERE id = $1;

-- name: EnableStaffMember :exec
UPDATE admin_staff SET is_active = true, updated_at = now() WHERE id = $1;

-- name: UpdateStaffLastLogin :exec
UPDATE admin_staff SET last_login_at = now(), updated_at = now() WHERE id = $1;


-- name: CreateAdminAuditLog :one
INSERT INTO admin_audit_logs (
    staff_id, staff_name, staff_email, action, resource, resource_id, details, ip_address
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
) RETURNING *;

-- name: ListAdminAuditLogs :many
SELECT * FROM admin_audit_logs
WHERE (sqlc.narg('staff_id')::uuid IS NULL OR staff_id = sqlc.narg('staff_id'))
  AND (sqlc.arg('resource')::varchar = '' OR resource = sqlc.arg('resource'))
  AND (sqlc.arg('resource_id')::varchar = '' OR resource_id = sqlc.arg('resource_id'))
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountAdminAuditLogs :one
SELECT count(*) FROM admin_audit_logs
WHERE (sqlc.narg('staff_id')::uuid IS NULL OR staff_id = sqlc.narg('staff_id'))
  AND (sqlc.arg('resource')::varchar = '' OR resource = sqlc.arg('resource'))
  AND (sqlc.arg('resource_id')::varchar = '' OR resource_id = sqlc.arg('resource_id'));