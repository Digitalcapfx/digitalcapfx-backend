-- name: GetSupportTicketWithMessages :one
SELECT
    t.id, t.user_id, t.subject, t.status, t.created_at,
    COALESCE(json_agg(m.*), '[]')::jsonb as messages
FROM support_tickets t
LEFT JOIN support_messages m ON m.ticket_id = t.id
WHERE t.id = $1
GROUP BY t.id;
