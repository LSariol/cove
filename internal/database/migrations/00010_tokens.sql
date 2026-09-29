-- Per-project tokens (SEC-4). Each project gets its own bearer token, limited
-- to the keys its patterns cover. The master token (COVE_CLIENT_SECRET) keeps
-- working with full access.
--
-- Only a SHA-256 hash of each token is stored; the token itself is shown once,
-- when it's created or rotated. Patterns are exact keys ("shared.tmdb") or a
-- prefix ending in "*" ("lighthouse.*", or "*" for every key). Write patterns
-- also allow reading.
--
-- token_log records every change to a token (who can reach what), append-only
-- for the app like event_log.

-- +goose Up
CREATE TABLE IF NOT EXISTS cove.tokens (
    id             bigserial   NOT NULL,
    name           text        NOT NULL,
    token_hash     bytea       NOT NULL,
    read_patterns  text[]      NOT NULL DEFAULT '{}',
    write_patterns text[]      NOT NULL DEFAULT '{}',
    created_at     timestamptz NOT NULL DEFAULT now(),
    rotated_at     timestamptz,
    last_used_at   timestamptz,
    CONSTRAINT tokens_pkey PRIMARY KEY (id),
    CONSTRAINT tokens_name_key UNIQUE (name),
    CONSTRAINT tokens_token_hash_key UNIQUE (token_hash),
    CONSTRAINT tokens_name_check CHECK (name ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    CONSTRAINT tokens_token_hash_check CHECK (octet_length(token_hash) = 32)
);

CREATE TABLE IF NOT EXISTS cove.token_log (
    id          bigserial   NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    token_name  text        NOT NULL,
    action      text        NOT NULL,
    detail      text,
    source      text        NOT NULL,
    CONSTRAINT token_log_pkey PRIMARY KEY (id),
    CONSTRAINT token_log_action_check
        CHECK (action IN ('create', 'rotate', 'revoke', 'allow', 'deny', 'rename_key'))
);

CREATE INDEX IF NOT EXISTS token_log_token_name_idx
    ON cove.token_log (token_name, occurred_at DESC);

-- The default privileges from 00002 gave cove_app full read/write on both
-- tables. token_log is append-only.
REVOKE UPDATE, DELETE, TRUNCATE ON cove.token_log FROM cove_app;
