-- Encryption key rotation (QOL-9).
--
-- cove.vault_key holds a fingerprint of the key the vault is encrypted with
-- (never the key itself). Cove records it on its first start, refuses to
-- start with a different key, and every write checks it, so values encrypted
-- with two different keys can never be mixed. `cove rotate-key` re-encrypts
-- everything and changes the fingerprint in one transaction, as cove_owner;
-- cove_app may only read it and record it once.
--
-- set_updated_at also changes: re-encrypting a value (same version, new
-- ciphertext) isn't a modification, so it no longer bumps updated_at.

-- +goose Up
CREATE TABLE IF NOT EXISTS cove.vault_key (
    id          boolean     NOT NULL DEFAULT true,
    fingerprint text        NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    rotated_at  timestamptz,
    CONSTRAINT vault_key_pkey PRIMARY KEY (id),
    CONSTRAINT vault_key_single_row CHECK (id)
);

-- The default privileges from 00002 gave cove_app full read/write.
REVOKE UPDATE, DELETE, TRUNCATE ON cove.vault_key FROM cove_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cove.set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    -- A read (read_count) or a re-encryption (encrypted_value with the same
    -- version) isn't a modification. A real update always bumps version.
    IF to_jsonb(NEW) - 'read_count' - 'encrypted_value' = to_jsonb(OLD) - 'read_count' - 'encrypted_value' THEN
        RETURN NEW;
    END IF;

    NEW.updated_at := now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
