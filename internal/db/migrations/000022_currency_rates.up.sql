-- Migration 000022: per-currency buy / sell / standard rates (admin-controlled).
--
-- Rates are expressed as "units of this currency per 1 USD" (USD is the base):
--   standard_rate — mid / display rate. Used to show a customer's balance in
--                   another currency they choose.
--   buy_rate      — the rate the business BUYS this currency at (customer sells
--                   to us / receives) — set to buy low.
--   sell_rate     — the rate the business SELLS this currency at (customer buys
--                   from us / spends) — set to sell high.
-- The spread between buy_rate and sell_rate is the business margin.
CREATE TABLE IF NOT EXISTS currency_rates (
    currency      VARCHAR(10)   PRIMARY KEY,   -- EUR, GBP, USD, XAF, XOF, NGN
    standard_rate NUMERIC(20,8) NOT NULL,
    buy_rate      NUMERIC(20,8) NOT NULL,
    sell_rate     NUMERIC(20,8) NOT NULL,
    updated_by    UUID          REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- Seed the six supported currencies (approximate rates per 1 USD).
-- Admin can adjust any of these at any time via the rates endpoints.
INSERT INTO currency_rates (currency, standard_rate, buy_rate, sell_rate) VALUES
    ('USD', 1.00000000,    1.00000000,    1.00000000),
    ('EUR', 0.92000000,    0.93000000,    0.91000000),
    ('GBP', 0.79000000,    0.80000000,    0.78000000),
    ('NGN', 1600.00000000, 1620.00000000, 1580.00000000),
    ('XAF', 609.00000000,  615.00000000,  603.00000000),
    ('XOF', 609.00000000,  615.00000000,  603.00000000)
ON CONFLICT (currency) DO NOTHING;
