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
    sort_code = $5,
    account_number_uk = $6,
    updated_at = NOW()
WHERE id = $1;

-- name: UpdateNombaAccountDetails :exec
UPDATE accounts
SET nomba_account_ref         = $2,
    nomba_account_holder_id   = $3,
    nomba_bank_name           = $4,
    nomba_bank_account_number = $5,
    nomba_bank_account_name   = $6,
    updated_at                = NOW()
WHERE id = $1;

-- name: GetAccountByNombaAccountNumber :one
SELECT * FROM accounts
WHERE nomba_bank_account_number = $1
LIMIT 1;

-- name: GetAccountByNilosAccountID :one
SELECT * FROM accounts
WHERE nilos_account_id = $1
LIMIT 1;

-- name: SumAccountBalanceByCurrency :one
SELECT COALESCE(SUM(balance), 0)::numeric AS total
FROM accounts
WHERE currency = $1;

-- name: RecordDepositEvent :execrows
INSERT INTO processed_deposit_events (provider, event_id, account_id, amount, currency)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (provider, event_id) DO NOTHING;

