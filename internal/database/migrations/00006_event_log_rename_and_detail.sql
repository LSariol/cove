-- Support for the CLI's rename and restore commands:
--   - a 'rename' event kind, logged under both the old and the new key;
--   - a free-text detail column for context such as "renamed from X" or
--     "restored version 3".
-- Existing rows get detail = NULL.

-- +goose Up
ALTER TYPE cove.secret_event_kind ADD VALUE IF NOT EXISTS 'rename';

ALTER TABLE cove.event_log ADD COLUMN IF NOT EXISTS detail text;
