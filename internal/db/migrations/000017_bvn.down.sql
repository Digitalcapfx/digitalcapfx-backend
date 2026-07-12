-- Migration 000017 (down)
ALTER TABLE users DROP COLUMN IF EXISTS bvn;
