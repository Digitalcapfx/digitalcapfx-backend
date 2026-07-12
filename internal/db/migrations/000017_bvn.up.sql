-- Migration 000017: Nigerian Bank Verification Number (BVN)
-- The 11-digit BVN is required by the account service provider to create a
-- Nigerian bank account for the customer. Stored nullable — only NG customers
-- provide it.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS bvn VARCHAR(11);
