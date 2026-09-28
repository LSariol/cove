-- Consistent column names for v1.0.0. The API's JSON field names are unchanged.
-- Also replaces the broken set_last_modified() trigger with set_updated_at().
--
-- After this migration, Cove v0.2.0 can no longer run against the database.

-- +goose Up
ALTER TABLE cove.secrets RENAME COLUMN secret_key    TO key;
ALTER TABLE cove.secrets RENAME COLUMN secret_value  TO encrypted_value;
ALTER TABLE cove.secrets RENAME COLUMN times_pulled  TO read_count;
ALTER TABLE cove.secrets RENAME COLUMN date_added    TO created_at;
ALTER TABLE cove.secrets RENAME COLUMN last_modified TO updated_at;
ALTER TABLE cove.secrets RENAME CONSTRAINT secrets_secret_key_key TO secrets_key_key;

ALTER TABLE cove.event_log RENAME COLUMN modification TO kind;
ALTER TABLE cove.event_log RENAME COLUMN version      TO secret_version;
ALTER TABLE cove.event_log RENAME COLUMN old_value    TO old_encrypted_value;
ALTER TABLE cove.event_log RENAME COLUMN new_value    TO new_encrypted_value;

DROP TRIGGER IF EXISTS secrets_set_last_modified ON cove.secrets;
DROP FUNCTION IF EXISTS cove.set_last_modified();

-- Sets updated_at on every update, except when read_count is the only column
-- that changed (reading a secret isn't a modification).
-- +goose StatementBegin
CREATE FUNCTION cove.set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF to_jsonb(NEW) - 'read_count' = to_jsonb(OLD) - 'read_count' THEN
        RETURN NEW;
    END IF;

    NEW.updated_at := now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER secrets_set_updated_at
    BEFORE UPDATE ON cove.secrets
    FOR EACH ROW EXECUTE FUNCTION cove.set_updated_at();
