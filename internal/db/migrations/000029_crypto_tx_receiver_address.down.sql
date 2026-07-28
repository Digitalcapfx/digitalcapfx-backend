DROP INDEX IF EXISTS idx_crypto_tx_receiver_address;

-- Restore receiver_phone NOT NULL. Backfill address-only rows (NULL phone) with
-- '' first so the constraint can be re-applied.
UPDATE crypto_transactions SET receiver_phone = '' WHERE receiver_phone IS NULL;
ALTER TABLE crypto_transactions ALTER COLUMN receiver_phone SET NOT NULL;

ALTER TABLE crypto_transactions DROP COLUMN IF EXISTS receiver_address;
