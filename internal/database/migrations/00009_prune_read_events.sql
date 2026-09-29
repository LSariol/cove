-- Optional retention for read events (COVE_EVENT_LOG_RETENTION_DAYS).
--
-- cove_app can't delete from event_log (it's append-only, see 00002). This
-- function runs as its owner, cove_owner, and can only remove read events
-- older than the given age (at least 1 day). Creates, updates, deletes and
-- renames are never removed.

-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cove.prune_read_events(older_than interval)
RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    removed bigint;
BEGIN
    IF older_than < interval '1 day' THEN
        RAISE EXCEPTION 'older_than must be at least 1 day, got %', older_than;
    END IF;

    DELETE FROM cove.event_log
    WHERE kind = 'read'
      AND occurred_at < now() - older_than;

    GET DIAGNOSTICS removed = ROW_COUNT;
    RETURN removed;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION cove.prune_read_events(interval) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cove.prune_read_events(interval) TO cove_app;
