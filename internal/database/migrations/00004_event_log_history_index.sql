-- Fast lookup of a single secret's history, newest first.

-- +goose Up
CREATE INDEX IF NOT EXISTS event_log_secret_key_occurred_at_idx
    ON cove.event_log (secret_key, occurred_at DESC);
