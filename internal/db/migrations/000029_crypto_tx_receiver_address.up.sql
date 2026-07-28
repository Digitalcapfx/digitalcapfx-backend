-- CaaS Phone Send now also accepts a raw 0x SCW wallet address as the recipient
-- (Rach CaaS auto-detects phone vs. address on the same field). A wallet address
-- is 42 chars and has no phone, so give crypto_transactions a dedicated column and
-- relax receiver_phone to nullable — an address-addressed transfer stores the
-- address in receiver_address with receiver_phone left NULL.
ALTER TABLE crypto_transactions ADD COLUMN IF NOT EXISTS receiver_address VARCHAR(42);
ALTER TABLE crypto_transactions ALTER COLUMN receiver_phone DROP NOT NULL;

CREATE INDEX IF NOT EXISTS idx_crypto_tx_receiver_address ON crypto_transactions(receiver_address);
