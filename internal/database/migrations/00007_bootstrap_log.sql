-- Every request to the bootstrap endpoint: when, from which address, and what
-- happened. Shown by the CLI's `bootstrap status`.

-- +goose Up
CREATE TABLE IF NOT EXISTS cove.bootstrap_log (
    id          bigserial   NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    remote_addr text        NOT NULL,
    outcome     text        NOT NULL,
    CONSTRAINT bootstrap_log_pkey PRIMARY KEY (id),
    CONSTRAINT bootstrap_log_outcome_check
        CHECK (outcome IN ('granted', 'redelivered', 'locked', 'expired', 'forbidden', 'error'))
);

-- Append-only for the app, like event_log. (The default privileges from
-- migration 00002 gave cove_app full read/write on new tables.)
REVOKE UPDATE, DELETE, TRUNCATE ON cove.bootstrap_log FROM cove_app;
