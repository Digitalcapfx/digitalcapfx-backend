-- name: CreateNotification :one
INSERT INTO notifications (user_id, type, title, body, metadata)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListNotifications :many
SELECT * FROM notifications
WHERE user_id = $1
  AND (sqlc.arg(unread_only)::boolean = false OR is_read = false)
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountNotifications :one
SELECT COUNT(*) FROM notifications
WHERE user_id = $1
  AND (sqlc.arg(unread_only)::boolean = false OR is_read = false);

-- name: CountUnreadNotifications :one
SELECT COUNT(*) FROM notifications
WHERE user_id = $1 AND is_read = false;

-- name: MarkNotificationRead :one
UPDATE notifications
SET is_read = true
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: MarkAllNotificationsRead :exec
UPDATE notifications
SET is_read = true
WHERE user_id = $1;
