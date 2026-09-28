-- Grants for the runtime roles. Requires the roles cove_app and cove_reader to
-- exist (created by the Admin setup script, since roles are server-wide).
--
-- cove_app:    read/write secrets; event_log is append-only (SELECT + INSERT);
--              read-only on the migrations table (startup version check).
-- cove_reader: read-only on everything in the schema.
--
-- All privileges are reset first, so this ends in the same state no matter what
-- was granted by hand before.

-- +goose Up
REVOKE ALL ON SCHEMA cove FROM PUBLIC;
GRANT USAGE ON SCHEMA cove TO cove_app, cove_reader;

REVOKE ALL ON ALL TABLES IN SCHEMA cove FROM cove_app, cove_reader;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA cove FROM cove_app, cove_reader;

GRANT SELECT, INSERT, UPDATE, DELETE ON cove.secrets TO cove_app;
GRANT SELECT, INSERT ON cove.event_log TO cove_app;
GRANT USAGE, SELECT ON SEQUENCE cove.event_log_id_seq TO cove_app;

-- Lets Cove check the schema version at startup without migrator credentials.
GRANT SELECT ON cove.goose_db_version TO cove_app;

GRANT SELECT ON ALL TABLES IN SCHEMA cove TO cove_reader;

-- Tables and sequences created by later migrations (as cove_owner).
-- New tables give cove_app full read/write; tighten per table in the migration
-- that creates it when needed (as done for event_log above).
ALTER DEFAULT PRIVILEGES FOR ROLE cove_owner IN SCHEMA cove
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO cove_app;
ALTER DEFAULT PRIVILEGES FOR ROLE cove_owner IN SCHEMA cove
    GRANT USAGE, SELECT ON SEQUENCES TO cove_app;
ALTER DEFAULT PRIVILEGES FOR ROLE cove_owner IN SCHEMA cove
    GRANT SELECT ON TABLES TO cove_reader;
