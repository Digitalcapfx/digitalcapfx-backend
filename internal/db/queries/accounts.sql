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

-- name: SetAccountStatusByID :one
-- Admin enable/disable of a single currency account. status is 'active' |
-- 'inactive' (the API keeps returning all accounts; the frontend hides
-- 'inactive' ones).
UPDATE accounts
SET status = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetAccountBalanceByID :one
-- Admin absolute balance set. amount applies to both ledger and available.
UPDATE accounts
SET balance = $2, available_balance = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: AdjustAccountBalanceByID :one
-- Admin delta adjustment (may be negative). No overdraft guard — an admin
-- override can intentionally take a balance negative for reconciliation.
UPDATE accounts
SET balance = balance + $2, available_balance = available_balance + $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: AdminUpdateAccountBanking :one
-- Partial update of a customer account's banking details. NULL args leave the
-- existing value untouched.
UPDATE accounts SET
    account_number            = COALESCE(sqlc.narg('account_number'), account_number),
    iban                      = COALESCE(sqlc.narg('iban'), iban),
    bic                       = COALESCE(sqlc.narg('bic'), bic),
    sort_code                 = COALESCE(sqlc.narg('sort_code'), sort_code),
    account_number_uk         = COALESCE(sqlc.narg('account_number_uk'), account_number_uk),
    nomba_bank_name           = COALESCE(sqlc.narg('nomba_bank_name'), nomba_bank_name),
    nomba_bank_account_number = COALESCE(sqlc.narg('nomba_bank_account_number'), nomba_bank_account_number),
    nomba_bank_account_name   = COALESCE(sqlc.narg('nomba_bank_account_name'), nomba_bank_account_name),
    updated_at                = NOW()
WHERE id = $1
RETURNING *;

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

