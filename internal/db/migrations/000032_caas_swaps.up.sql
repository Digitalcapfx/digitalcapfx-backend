-- CaaS stablecoin swaps (USDC ⇄ USDT) executed on a user's smart-contract
-- wallet via the Rach CaaS /v1/swaps endpoint (FiatSwapV2 DEX router). The swap
-- settles asynchronously on-chain: we record it as `pending` and finalise it
-- from the swap.settled / swap.failed webhook (or a status poll).

CREATE TABLE IF NOT EXISTS caas_swaps (
    id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    reference       VARCHAR(60)   NOT NULL UNIQUE,          -- local reference (SWAP-...)
    user_id         UUID          NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_in        VARCHAR(10)   NOT NULL,                 -- USDC | USDT
    token_out       VARCHAR(10)   NOT NULL,                 -- USDC | USDT
    amount_in       NUMERIC(18,6) NOT NULL,                 -- input token amount
    amount_out      NUMERIC(18,6),                          -- output token amount (set on settle)
    status          VARCHAR(20)   NOT NULL DEFAULT 'pending', -- pending|processing|settled|failed
    caas_swap_id    VARCHAR(80),                            -- swap_id returned by CaaS (rach_swap_...)
    tx_hash         VARCHAR(120),                           -- on-chain EVM tx hash once settled
    idempotency_key VARCHAR(80),
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_caas_swap_tokens CHECK (token_in IN ('USDC','USDT') AND token_out IN ('USDC','USDT'))
);
CREATE INDEX IF NOT EXISTS idx_caas_swaps_user ON caas_swaps(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_caas_swaps_caas_id ON caas_swaps(caas_swap_id);
