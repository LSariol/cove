-- Baseline: the cove_db schema exactly as it existed in prod before migrations
-- were introduced (Cove v0.2.0). Every statement only creates what is missing, so
-- on an existing database this migration changes nothing and is only recorded as
-- applied. Fixes to this schema belong in later migrations, never here.

-- +goose Up
CREATE SCHEMA IF NOT EXISTS cove;

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_type t
        JOIN pg_namespace n ON n.oid = t.typnamespace
        WHERE n.nspname = 'cove' AND t.typname = 'secret_event_kind'
    ) THEN
        CREATE TYPE cove.secret_event_kind AS ENUM ('create', 'read', 'update', 'delete');
    END IF;
END
$$;
-- +goose StatementEnd

CREATE TABLE IF NOT EXISTS cove.secrets (
    id            uuid        NOT NULL DEFAULT gen_random_uuid(),
    secret_key    text        NOT NULL,
    secret_value  text        NOT NULL,
    version       integer     NOT NULL DEFAULT 1,
    times_pulled  integer     NOT NULL DEFAULT 0,
    date_added    timestamptz NOT NULL DEFAULT now(),
    last_modified timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT secrets_pkey PRIMARY KEY (id),
    CONSTRAINT secrets_secret_key_key UNIQUE (secret_key)
);

CREATE TABLE IF NOT EXISTS cove.event_log (
    id           bigserial              NOT NULL,
    secret_id    uuid,
    secret_key   text                   NOT NULL,
    version      integer                NOT NULL,
    modification cove.secret_event_kind NOT NULL,
    source       text                   NOT NULL,
    old_value    text,
    new_value    text,
    occurred_at  timestamptz            NOT NULL DEFAULT now(),
    CONSTRAINT event_log_pkey PRIMARY KEY (id)
);

-- Reproduced exactly as in prod, including the bug where the assignment is part
-- of the comment line. Migration 00003 replaces this function.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cove.set_last_modified() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  -- If the only change is to times_pulled, do nothing
  IF to_jsonb(NEW) - 'times_pulled' = to_jsonb(OLD) - 'times_pulled' THEN
    RETURN NEW;
  END IF;

  -- Otherwise, some other column changed   NEW.last_modified := now();
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE OR REPLACE TRIGGER secrets_set_last_modified
    BEFORE UPDATE ON cove.secrets
    FOR EACH ROW EXECUTE FUNCTION cove.set_last_modified();
