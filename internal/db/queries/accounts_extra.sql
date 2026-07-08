-- name: GetAccountWithNilos :one
SELECT a.id, a.user_id, a.currency, a.account_number, a.balance, COALESCE(n.nilos_id, '')::text as nilos_id
FROM accounts a
LEFT JOIN virtual_cards n ON n.account_id = a.id
WHERE a.id = $1;
