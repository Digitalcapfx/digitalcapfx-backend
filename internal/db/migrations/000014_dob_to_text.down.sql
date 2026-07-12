-- Revert users.date_of_birth back to DATE. Empty strings become NULL.
ALTER TABLE users
    ALTER COLUMN date_of_birth TYPE date USING NULLIF(date_of_birth, '')::date;
