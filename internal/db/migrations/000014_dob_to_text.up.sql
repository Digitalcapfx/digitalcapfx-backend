-- Migration 000014: store users.date_of_birth as TEXT.
--
-- It was a DATE column but the app reads/writes it as a plain ISO string
-- ("YYYY-MM-DD"). Scanning a non-NULL Postgres DATE back into a Go *string via
-- pgx fails, which made every profile update that included a date_of_birth
-- return 500. TEXT removes the pgx date<->string mismatch entirely; the app
-- validates the format.
ALTER TABLE users
    ALTER COLUMN date_of_birth TYPE text USING date_of_birth::text;
