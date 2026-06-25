DROP TABLE IF EXISTS fx_rates;
DROP TABLE IF EXISTS virtual_cards;

ALTER TABLE accounts
    DROP COLUMN IF EXISTS bic,
    DROP COLUMN IF EXISTS iban,
    DROP COLUMN IF EXISTS nilos_customer_id,
    DROP COLUMN IF EXISTS nilos_account_id;
