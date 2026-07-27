-- Reverse Nomba NGN rails.
DROP INDEX IF EXISTS idx_accounts_nomba_acctno;

ALTER TABLE accounts
    DROP COLUMN IF EXISTS nomba_account_ref,
    DROP COLUMN IF EXISTS nomba_account_holder_id,
    DROP COLUMN IF EXISTS nomba_bank_name,
    DROP COLUMN IF EXISTS nomba_bank_account_number,
    DROP COLUMN IF EXISTS nomba_bank_account_name;

-- Restore the original currency CHECK (without NGN). Rows with currency='NGN'
-- must be removed first for this to apply.
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS chk_currency;
ALTER TABLE accounts
    ADD CONSTRAINT chk_currency CHECK (currency IN ('XAF','XOF','USD','GBP','EUR'));
