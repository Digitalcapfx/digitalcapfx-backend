ALTER TABLE hub2_payments DROP COLUMN IF EXISTS caas_funded_at;
DROP TABLE IF EXISTS caas_withdrawals;
DROP TABLE IF EXISTS fx_quotes;
ALTER TABLE crypto_transactions
    DROP COLUMN IF EXISTS quote_id,
    DROP COLUMN IF EXISTS caas_transfer_id,
    DROP COLUMN IF EXISTS idempotency_key,
    DROP COLUMN IF EXISTS local_fiat_amount,
    DROP COLUMN IF EXISTS local_currency;
ALTER TABLE caas_wallets DROP COLUMN IF EXISTS blind_index;
