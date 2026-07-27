-- Nomba NGN rails: allow the NGN currency and store the provisioned virtual
-- bank account details returned by Nomba's create-virtual-account API.

-- 1. Allow NGN on accounts (the original CHECK omitted it).
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS chk_currency;
ALTER TABLE accounts
    ADD CONSTRAINT chk_currency CHECK (currency IN ('XAF','XOF','USD','GBP','EUR','NGN'));

-- 2. Nomba virtual-account linkage. account_ref is our stable per-account
--    reference (Nomba requires 16-64 chars); the bank_account_number is the real
--    NGN account customers receive money into.
ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS nomba_account_ref         VARCHAR(64) UNIQUE,
    ADD COLUMN IF NOT EXISTS nomba_account_holder_id   VARCHAR(100),
    ADD COLUMN IF NOT EXISTS nomba_bank_name           VARCHAR(100),
    ADD COLUMN IF NOT EXISTS nomba_bank_account_number VARCHAR(20),
    ADD COLUMN IF NOT EXISTS nomba_bank_account_name   VARCHAR(100);

-- Map an inbound webhook credit (by alias/virtual account number) back to the account.
CREATE INDEX IF NOT EXISTS idx_accounts_nomba_acctno
    ON accounts(nomba_bank_account_number) WHERE nomba_bank_account_number IS NOT NULL;
