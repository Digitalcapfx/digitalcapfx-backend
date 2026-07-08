-- name: GetAdminUserView :one
SELECT
    id, phone_number, email, first_name, last_name, kyc_status,
    is_active, role, 0::int as account_count, created_at,
    now()::timestamp as last_login_at
FROM users WHERE id = $1;

-- name: GetAdminDashboardStats :one
SELECT
    count(*)::bigint as total_users,
    count(*) FILTER (WHERE is_active = true)::bigint as active_users,
    count(*) FILTER (WHERE is_active = false)::bigint as disabled_users,
    count(*) FILTER (WHERE kyc_status = 'pending')::bigint as pending_kyc,
    count(*) FILTER (WHERE kyc_status = 'approved')::bigint as approved_kyc,
    count(*) FILTER (WHERE kyc_status = 'rejected')::bigint as rejected_kyc,
    0::bigint as total_staff,
    0::bigint as active_staff,
    0::bigint as tx_count_30d,
    0::float8 as tx_volume_30d,
    count(*) FILTER (WHERE created_at >= now() - interval '7 days')::bigint as new_users_7d,
    count(*) FILTER (WHERE created_at >= now() - interval '30 days')::bigint as new_users_30d
FROM users;
