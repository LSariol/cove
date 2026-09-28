-- Keys must match the API's rules: 1-256 characters from [A-Za-z0-9._-].
--
-- NOT VALID: enforced for every new or changed row, but existing rows are not
-- checked, so a non-conforming key already in prod can't stop Cove from starting.
-- Once prod is confirmed clean, a later migration can run
-- ALTER TABLE cove.secrets VALIDATE CONSTRAINT secrets_key_format_check;
--
-- (Postgres regex repetition counts max out at 255, so the length is checked
-- separately instead of with {1,256}.)

-- +goose Up
ALTER TABLE cove.secrets
    ADD CONSTRAINT secrets_key_format_check
    CHECK (key ~ '^[A-Za-z0-9._-]+$' AND char_length(key) <= 256)
    NOT VALID;
