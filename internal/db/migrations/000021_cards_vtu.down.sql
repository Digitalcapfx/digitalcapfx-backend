DROP TABLE IF EXISTS vtu_transactions;
DROP TABLE IF EXISTS card_transactions;
ALTER TABLE virtual_cards
    DROP COLUMN IF EXISTS funding_account_id,
    DROP COLUMN IF EXISTS funding_wallet_id,
    DROP COLUMN IF EXISTS color_theme,
    DROP COLUMN IF EXISTS card_art_id,
    DROP COLUMN IF EXISTS billing_address,
    DROP COLUMN IF EXISTS masked_pan,
    DROP COLUMN IF EXISTS expiry,
    DROP COLUMN IF EXISTS cvv_encrypted,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS provider_card_id,
    DROP COLUMN IF EXISTS updated_at;
CREATE UNIQUE INDEX IF NOT EXISTS idx_virtual_cards_user_active
    ON virtual_cards(user_id) WHERE is_active = true;
