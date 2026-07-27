-- name: CreateDepartment :one
INSERT INTO admin_departments (name, description)
VALUES ($1, $2)
RETURNING *;

-- name: GetDepartmentByID :one
SELECT * FROM admin_departments WHERE id = $1;

-- name: ListDepartments :many
SELECT d.*, (SELECT count(*) FROM admin_staff s WHERE s.department_id = d.id)::bigint AS staff_count
FROM admin_departments d
ORDER BY d.name;

-- name: UpdateDepartment :one
UPDATE admin_departments
SET name        = coalesce(sqlc.narg('name'), name),
    description = coalesce(sqlc.narg('description'), description),
    updated_at  = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteDepartment :execrows
DELETE FROM admin_departments WHERE id = $1;

-- name: CountStaffInDepartment :one
SELECT count(*) FROM admin_staff WHERE department_id = $1;
