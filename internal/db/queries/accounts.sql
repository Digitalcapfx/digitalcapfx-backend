-- name: CreateAccount :one
INSERT INTO accounts (user_id, currency, account_number)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetAccountByID :one
SELECT * FROM accounts WHERE id = $1 LIMIT 1;

-- name: GetAccountByUserAndCurrency :one
SELECT * FROM accounts
WHERE user_id = $1 AND currency = $2
LIMIT 1;

-- name: GetAccountsByUserID :many
SELECT * FROM accounts
WHERE user_id = $1
ORDER BY currency;

-- name: GetAccountForUpdate :one
SELECT * FROM accounts
WHERE id = $1
LIMIT 1
FOR UPDATE;

-- name: CreditAccount :one
UPDATE accounts
SET balance           = balance + $2,
    available_balance = available_balance + $2,
    updated_at        = NOW()
WHERE id = $1
RETURNING *;

-- name: DebitAccount :one
UPDATE accounts
SET balance           = balance - $2,
    available_balance = available_balance - $2,
    updated_at        = NOW()
WHERE id = $1 AND available_balance >= $2
RETURNING *;

-- name: FreezeAccount :exec
UPDATE accounts
SET status = 'frozen', updated_at = NOW()
WHERE id = $1;

-- name: UpdateNilosAccountDetails :exec
UPDATE accounts
SET nilos_account_id = $2,
    iban = $3,
    bic = $4,
    updated_at = NOW()
WHERE id = $1;

