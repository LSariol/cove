# Changelog

All notable changes to Cove. Versions follow [semantic versioning](https://semver.org); the HTTP API is `/v0` and stays backwards compatible.

## v1.0.0

The first stable release: a rework of v0.2.0 for security, reliability and day-to-day use. The `/v0` routes and responses are unchanged, but **every client now needs its own token, and deploying it needs the upgrade steps below**. Use CoveClient v1.0.0.

### Upgrading from v0.2.0

- **Database migrations.** Cove now manages its schema with built-in migrations (11 of them) and refuses to start on an unmigrated database. Create the `cove_owner` / `cove_migrator` / `cove_app` / `cove_reader` roles and set `COVE_MIGRATE_DATABASE_URL` ([DOCUMENTATION.md §5](DOCUMENTATION.md#5-database)). Migration 00003 renames columns, so v0.2.0 can't run against a migrated database: take a backup first.
- **No published port.** Clients must use `http://cove:2100` on the `spark` network; `http://<server>:2100` stops working.
- **Non-root container.** Run `sudo chown -R 10001:10001 /srv/server/storage/cove` before the first start.
- **The container runs `cove serve`** with no TTY. The CLI is `docker exec -it cove /cove shell`; `docker attach` no longer gives a prompt.
- **The master token is gone.** `COVE_CLIENT_SECRET` is no longer read: remove it from the `.env`. Every client needs a project token (`token create`), and requests with the old shared token get `401`. The `X-Cove-Source` header is ignored and `400 missing_source` no longer exists: a request is recorded under its token's name.
- **The bootstrap endpoint is closed by default:** run `bootstrap open <project>` when onboarding a client. It hands out that project's own token.
- **Precise error statuses:** a duplicate create is `409`, updating a missing key `404`, decrypt and database failures `500`. Success codes are unchanged.
- **The first start records the vault key.** After that, Cove refuses to start with a different `VAULT_ENCRYPTION_KEY`; change it only with `cove rotate-key`.
- `vault.json` and `APP_VAULT_PATH` are gone; `APP_MARKER_DIR` is now only `APP_MARKER_PATH`.
- Building needs **Go 1.27.1** or newer.

### Security

- **Per-project tokens**, each limited to key patterns, read-only or read/write, managed with the new `token` command. Only a hash is stored. No token has access to everything unless you create one; emergency access is the CLI on the server.
- **Trustworthy audit:** the event log and request log record the token's name, which the caller can't fake.
- **The event log is append-only** for the running app; every operation and its audit record are saved in one transaction.
- **Vault key fingerprint:** Cove refuses to start with the wrong key, and values encrypted with two keys can never be mixed.
- **`cove rotate-key`** re-encrypts the whole vault, history included, in one transaction.
- **Rate limiting:** 10 failed attempts a minute from an address block it for 5 minutes.
- **Bootstrap gate:** closed by default, one handout per opening, time-limited, optional network allowlist, every attempt logged; it hands out a project's own token.
- **No port published**; container runs as a non-root user with a read-only filesystem, no capabilities and no-new-privileges; base image pinned.
- **Nothing sensitive in logs:** the CLI is no longer attached to `docker logs`; request logs never include values or tokens; read events no longer store a copy of the value.
- **Built with Go 1.27.1** and updated dependencies, fixing 26 known vulnerabilities in Go 1.25.1, pgx and x/text.
- HTTP server timeouts; strict key validation; the `.env` is written owner-only; `.env` and local state kept out of the Docker build.

### Added

- **CLI:** `cove shell` (line editing, history, Tab completion), one-shot commands, and new commands `generate`, `rename`, `restore`, `search`, `info`, `history`, `status`, `token`, `bootstrap open/lock/status`. Grouped `help`, `help <command>` with examples, and guides `help setup` and `help patterns`. Messages on stderr with symbols; `NO_COLOR` respected.
- **`POST /v0/batch`:** read up to 100 secrets in one request, all or nothing.
- **`GET /v0/ready`** (checks the database; used by the Docker healthcheck) and **`GET /v0/version`**.
- **Optional event-log retention** for read events (`COVE_EVENT_LOG_RETENTION_DAYS`).
- **The key naming standard** `PROJECT_PLATFORM_TYPE`, with a CLI warning for non-standard names.
- `cove migrate status|up`, `cove rotate-key`, `cove version`.

### Fixed

- The `updated_at` trigger (it never fired correctly in v0.2.0).
- Concurrent updates could interleave; each change now locks its row and is logged in order.
- A failed decrypt no longer counts as a read.
- `restore` finds a secret's values from before a rename.
- Many clearer error and startup messages.

## v0.2.0

The version in production before v1.0.0: the `/v0` API with a JSON envelope and a single shared token.
