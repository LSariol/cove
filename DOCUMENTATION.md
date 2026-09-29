# Cove Documentation

Full reference for Cove, a self-hosted secret vault. Version v0.2.0, API version `v0`.

The [README](README.md) is a quick overview. This document goes further: how each piece works internally, how the server is deployed, how secrets are stored, and the known issues in the current code.

Companion client library: [CoveClient](https://github.com/LSariol/CoveClient). It has its own `DOCUMENTATION.md`.

---

## Contents

1. [Overview](#1-overview)
2. [Architecture](#2-architecture)
3. [Configuration](#3-configuration)
4. [Database](#4-database)
5. [Encryption](#5-encryption)
6. [HTTP API](#6-http-api)
7. [Bootstrap flow](#7-bootstrap-flow)
8. [CLI](#8-cli)
9. [Running locally](#9-running-locally)
10. [Deploying with Docker](#10-deploying-with-docker)
11. [Operations](#11-operations)
12. [Known issues and gotchas](#12-known-issues-and-gotchas)

---

## 1. Overview

Cove stores key/value secrets for other self-hosted projects. It is a single Go binary with two parts:

- It runs an **HTTP API** on `APP_PORT`. Other apps call it (usually through CoveClient) to read and manage secrets.
- It has a **CLI** for managing secrets directly: an interactive prompt (`cove shell`, or on stdin with plain `cove`) and one-shot commands (`cove list`).

Both use the same database, so a change made in one shows up in the other immediately.

Main properties:

| Property | How it's done |
|---|---|
| Encryption at rest | AES-256-GCM. The key is SHA-256 of `VAULT_ENCRYPTION_KEY`. Only encrypted values are stored in the DB. |
| Authentication | One shared bearer token (`COVE_CLIENT_SECRET`), compared in constant time. |
| Auditing | Every create, read, update, and delete writes a row to `cove.event_log`, including the calling app (`X-Cove-Source`). |
| Versioning | Each secret has a `version` counter that goes up on every update. Old encrypted values stay in the event log. |
| First-boot setup | A one-time, unauthenticated "lighthouse" endpoint returns the bearer token to a new client. |

---

## 2. Architecture

```
cmd/cove/main.go              Entry point: picks the mode (server, serve, shell, one-shot command, migrate, version) and wires packages together
internal/
  config/config.go            Config struct: every setting, read from the environment once
  encryption/
    cipher.go                 Cipher: Encrypt / Decrypt (AES-256-GCM)
    random.go                 GenerateSecret (random alphanumeric)
  database/
    database.go               pgxpool connection (New, Connect, Close)
    migrate.go, migrations/   goose migrations and the startup schema check
    store.go                  SQL queries only; values in and out are always encrypted
    types.go                  Secret and Event rows, EventLogInput, event kinds
  vault/
    vault.go                  The rules for secrets: encrypt/decrypt, store, and log every operation
    keys.go                   ValidateKey
  bootstrap/gate.go           The bootstrap gate: open window, handout, grace period, allowed networks
  server/
    server.go                 Server struct; Run serves until stopped, then shuts down gracefully
    routes.go                 URL → handler table
    middleware.go             Bearer-token check
    secrets.go                /v0/secrets handlers
    system.go                 /v0/health, /v0/ready, /v0/auth, /v0/version
    bootstrap.go              /v0/bootstrap/lighthouse
    respond.go                JSON envelope helpers (writeResponse / writeError)
    api_types.go              JSON response types
  cli/
    shell.go                  CLI struct, the prompt loop, Exec (one command), confirmations
    terminal.go               Line editing, history and Tab completion for `cove shell`
    commands.go               Command table (names, aliases, usage, help, completion) and help/exit
    cmd_secrets.go            get / create / update / delete / rename / list / search
    cmd_generate.go           generate
    cmd_restore.go            restore
    cmd_history.go            info / history
    cmd_status.go             status
    cmd_bootstrap.go          bootstrap open / lock / status
    output.go                 stdout/stderr, symbols, color, and the prompt
```

Dependencies point one way: `main` → `server` / `cli` → `vault` → `database` + `encryption`, with `bootstrap` used by `server` and `cli`. Only `config` reads environment variables; every other package gets its settings passed in.

**Where things go:**
- A rule about secrets (validation, what gets logged, transactions) → `vault`. Both the API and the CLI get it automatically.
- A new SQL query → `database/store.go`. A schema change → a new migration.
- A new API route → a handler in `server/`, registered in `routes.go`.
- A new CLI command → a function in a `cli/cmd_*.go` file, plus one entry in `commandTable()` in `commands.go`. `help` picks it up automatically.
- A new setting → a field in `config.Config`, read in `FromEnv()`.

### Startup sequence (`cmd/cove/main.go`)

1. `config.Load()` loads the `.env` file and returns a `Config`. It uses `APP_ENV_PATH` if that's set in the environment (as in docker-compose), otherwise the first of `./.env` and `/app/vault/.env` that exists. A file that exists but can't be parsed stops startup with the parse error.
2. `config.Ensure()` generates any missing `COVE_CLIENT_SECRET` / `VAULT_ENCRYPTION_KEY`, saves them to `APP_ENV_PATH`, and uses them straight away. Then `cfg.Validate()` stops startup if `COVE_DATABASE_URL` or `APP_PORT` is missing, or the client secret is shorter than 24 characters (a short vault key only logs a warning), and Cove logs its version. Startup errors print one line, `cove: <message>`, and exit with status 1.
3. A context is created that is cancelled on `SIGINT` / `SIGTERM`.
4. If `COVE_MIGRATE_DATABASE_URL` is set, pending database migrations are applied (see [§4](#4-database)). A failed migration stops startup.
5. `database.New(cfg.DatabaseURL)` and `Connect()` opens a `pgxpool` and pings it with a 3-second timeout.
6. `CheckSchemaVersion()` confirms every migration built into this binary has been applied. If not, Cove stops with a message saying to set `COVE_MIGRATE_DATABASE_URL`.
7. A `vault.Vault` is built from the database and an `encryption.Cipher`, and shared by the server and CLI.
8. In plain `cove`, the CLI starts in its own goroutine on stdin (`cove serve` skips it). When stdin closes, the CLI simply returns and the API keeps serving.
9. `srv.Run(ctx)` serves the API until the context is cancelled: by `SIGINT`/`SIGTERM` (Ctrl+C, `docker stop`) or the CLI's `exit`. It then stops accepting requests, gives those in progress up to 5 seconds, closes the database pool, and logs `Cove stopped.`

`cove shell` and one-shot commands follow steps 1, 5 and 6 only: they use the server's `.env`, but never generate secrets or run migrations.

### Request flow

```
HTTP request
  → http.ServeMux (routes.go: defineRoutes)
  → requireClientSecret middleware (middleware.go)      [not for /health, /bootstrap]
  → handleSecretsCollection / handleSecretID (secrets.go) [path + key + X-Cove-Source checks]
  → getSecret / postSecret / patchSecret / deleteSecret (secrets.go)
  → vault.Get / Create / Update / Delete / List → encryption.Cipher
  → database store queries (store.go) + LogEvent
  → writeResponse / writeError (JSON envelope)
```

---

## 3. Configuration

All configuration comes from environment variables. `config.Load()` reads them from a `.env` file. godotenv **never overrides** a variable that is already set, so values from docker-compose's `environment:` section take priority over the `.env` file.

| Variable | Required | Used by | Description |
|---|---|---|---|
| `COVE_DATABASE_URL` | **Yes** | `database.New` | Connection string for the runtime role, e.g. `postgres://cove_app:pass@sparkdb:5432/cove_db`. |
| `COVE_MIGRATE_DATABASE_URL` | No | `database.Migrate` | Connection string for the migrator role, e.g. `postgres://cove_migrator:pass@sparkdb:5432/cove_db`. When set, Cove applies pending migrations on startup and `cove migrate` works. When unset, the database must already be migrated. |
| `COVE_CLIENT_SECRET` | Yes* | `server` (middleware, bootstrap) | Bearer token that clients must send. Must be at least 24 characters. *Generated (32 chars) if empty. |
| `VAULT_ENCRYPTION_KEY` | Yes* | `encryption` | Master key material. SHA-256 of this value is the AES key. Shorter than 24 characters logs a warning. *Generated (45 chars) if empty. |
| `APP_PORT` | **Yes** | `server.Start` | Listen port. The server binds `0.0.0.0:$APP_PORT`. |
| `APP_ENV_PATH` | No | `config` | The `.env` file to load first, and where generated secrets are saved. Default: the `.env` file that was loaded. |
| `APP_MARKER_PATH` | No | `bootstrap.Gate` | Directory for the bootstrap state file (`bootstrap.json`). Default: `/app/vault/markers`. The older name `APP_MARKER_DIR` also works. |
| `COVE_BOOTSTRAP_ALLOWED_CIDRS` | No | `bootstrap.Gate` | Comma-separated networks and/or addresses that may use the bootstrap endpoint, e.g. `172.18.0.0/16`. Empty allows any address. An invalid entry stops startup. |
| `APP_ENV` | No | — | `DEV` / `PROD` label. Not read by the code yet. |

Generated secrets use `encryption.GenerateSecret(n)`: `n` characters from `[a-zA-Z0-9]`, picked with `crypto/rand`.

> **Note:** when `config.Store` saves a generated secret, it rewrites the whole `.env` file: comments and formatting are lost, values are re-quoted, and the file is set to `600` (owner only). A `.env` it can't parse is left untouched.

### Dev vs. prod values

| | Dev (`.env`) | Prod (`docker-compose.yml`) |
|---|---|---|
| `APP_PORT` | `2110` | `2100` |
| `.env` location | `./.env` | `/app/vault/.env` (bind-mounted, **read-only**) |
| Marker dir | `./markers` | `/app/vault/markers` (bind-mounted) |

---

## 4. Database

Cove uses PostgreSQL through `github.com/jackc/pgx/v5/pgxpool`. The schema is managed by [goose](https://github.com/pressly/goose) migrations in `internal/database/migrations/`, which are built into the binary.

### Roles

| Role | Login | Used for |
|---|---|---|
| `cove_owner` | No | Owns the `cove` schema and everything in it |
| `cove_migrator` | Yes | Runs migrations. Member of `cove_owner` with `SET role = 'cove_owner'`, so everything it creates is owned by `cove_owner` |
| `cove_app` | Yes | Cove at runtime (`COVE_DATABASE_URL`) |
| `cove_reader` | Yes | Read-only access (pgAdmin, debugging) |

Roles, the database itself, and connect permissions are created once per environment by an admin (they're server-wide, so they can't live in Cove's migrations). The migrations need `cove_app` and `cove_reader` to exist, and `cove_owner` must own `cove_db`.

### Migrations

| Migration | What it does |
|---|---|
| `00001_baseline.sql` | The v0.2.0 schema exactly as it was in prod. Only creates what's missing, so on an existing database it changes nothing. |
| `00002_role_grants.sql` | Grants for `cove_app` / `cove_reader`, default privileges for future tables, and makes `event_log` append-only for `cove_app` |
| `00003_rename_columns.sql` | v1.0.0 column names, and replaces the broken `set_last_modified()` trigger with `set_updated_at()` |
| `00004_event_log_history_index.sql` | Index for per-secret history lookups |
| `00005_secret_key_format_check.sql` | Key format rule (`[A-Za-z0-9._-]`, 1–256 chars), `NOT VALID` so existing rows aren't checked |
| `00006_event_log_rename_and_detail.sql` | Adds the `rename` event kind and a `detail` column (e.g. "renamed from X", "restored version 3") |
| `00007_bootstrap_log.sql` | `cove.bootstrap_log`: every bootstrap request (time, address, outcome). Append-only for `cove_app` |

Running them:

- **On startup:** set `COVE_MIGRATE_DATABASE_URL`. Pending migrations are applied before Cove connects as `cove_app`. A Postgres advisory lock stops two instances migrating at once.
- **By hand:** `cove migrate status` lists each migration and whether it's applied. `cove migrate up` applies pending ones. Both use `COVE_MIGRATE_DATABASE_URL`.
- Applied migrations are recorded in `cove.goose_db_version`.

Rules for new migrations:

- **Every database change is a migration.** Never change the schema by hand.
- **Never edit a migration that has been applied in prod.** Add a new one.
- Name them `000NN_description.sql` with the next number, starting with `-- +goose Up`. Wrap statements containing `$$` (functions, `DO` blocks) in `-- +goose StatementBegin` / `-- +goose StatementEnd`.
- Forward-only: there are no down migrations. If something is wrong, the next migration fixes it.

### `cove.secrets`

| Column | Notes |
|---|---|
| `id` | UUID primary key |
| `key` | The name clients use. Unique. Must match `[A-Za-z0-9._-]`, 1–256 characters. |
| `encrypted_value` | Encrypted with AES-GCM and base64url-encoded. Never plaintext. |
| `version` | Starts at 1. `UpdateSecret` adds 1. |
| `read_count` | Goes up by 1 every time an app reads the secret through the API. The CLI's `get` doesn't count. Exposed in the API as `times_pulled`. |
| `created_at` / `updated_at` | `updated_at` is set by the `set_updated_at` trigger on any change except a `read_count`-only update. |

### `cove.event_log`

Every operation in `vault` writes one row through `LogEvent`:

| `kind` | `old_encrypted_value` | `new_encrypted_value` | Written by |
|---|---|---|---|
| `create` | `NULL` | new ciphertext | `Vault.Create` |
| `read` | current ciphertext | `NULL` | `Vault.Get` (API) / `Vault.Show` (CLI `get`) |
| `update` | previous ciphertext | new ciphertext | `Vault.Update`, `Vault.Restore` |
| `delete` | deleted ciphertext | `NULL` | `Vault.Delete` |
| `rename` | `NULL` | `NULL` | `Vault.Rename`, once under the old key and once under the new |

`kind` is the enum `cove.secret_event_kind`. `detail` holds optional context such as "renamed from X" or "restored version 3". `source` is the `X-Cove-Source` header value for API calls, or `cove_cli` for CLI calls. `secret_version` is the secret's version after the operation. `occurred_at` is set automatically.

The log holds ciphertext only. With `VAULT_ENCRYPTION_KEY` you can decrypt old values to recover a previous version or a deleted secret. Errors from `LogEvent` are ignored (`_ = d.LogEvent(...)`), so a failed log write never fails the operation. The table is **append-only for `cove_app`**: it has `SELECT` and `INSERT` only, so the running app can't rewrite history.

You don't need SQL to read it: the CLI's `history <key>` and `info <key>` show a secret's events and last read, and `restore` brings back earlier values.

---

## 5. Encryption

`internal/encryption/cipher.go`

- **Key:** `sha256.Sum256([]byte(VAULT_ENCRYPTION_KEY))`, which gives 32 bytes, so AES-256. It's computed once, when `encryption.NewCipher` is called at startup.
- **Mode:** AES-GCM with a random 12-byte nonce for each encryption.
- **Stored format:** `base64.URLEncoding( nonce || ciphertext || GCM tag )`.
- **Decrypt:** base64url-decodes the value, splits off the first 12 bytes as the nonce, then `gcm.Open`. GCM authenticates the data, so a wrong key or tampered value returns an error instead of garbage.

Consequences:

- **If you lose `VAULT_ENCRYPTION_KEY`, every secret is gone.** Back it up somewhere other than the Cove host.
- **You can't rotate the key yet.** Changing it makes every existing row undecryptable. To rotate, you'd need a script that decrypts each row with the old key and re-encrypts it with the new one (secrets **and** `event_log` values).
- The key is hashed with plain SHA-256, not a password KDF. That's fine for a long random key like the generated one. Don't use a short, human-chosen passphrase.

---

## 6. HTTP API

Base path: `/v0`. Every response is JSON with `Content-Type: application/json`.

### Response envelope

```json
{ "success": true,  "data":  { ... } }
{ "success": false, "error": { "type": "<code>", "message": "<text>" } }
```

### Authentication

Every route except `/v0/health` and `/v0/bootstrap/lighthouse` needs:

```
Authorization: Bearer <COVE_CLIENT_SECRET>
```

The middleware (`middleware.go`) checks these in order:

| Condition | Status | `error.type` |
|---|---|---|
| Header missing | 401 | `missing_token` |
| Not in the form `Bearer <token>` (case-sensitive `Bearer`) | 401 | `invalid_token_format` |
| Token doesn't match (constant-time compare) | 401 | `invalid_token` |

There is only one token. Every client gets the same access: full read/write to every secret.

### Routes

| Method | Path | Auth | `X-Cove-Source` | Purpose |
|---|---|---|---|---|
| any | `/v0/health` | no | no | Liveness: the HTTP server is up |
| any | `/v0/ready` | no | no | Readiness: the database is reachable |
| any | `/v0/version` | yes | no | The running Cove version |
| GET | `/v0/bootstrap/lighthouse` | no | no | Token handout while opened with `bootstrap open` ([§7](#7-bootstrap-flow)) |
| any | `/v0/auth` | yes | no | Checks that the token is valid |
| GET | `/v0/secrets` | yes | no | List metadata for all secrets |
| GET | `/v0/secrets/{key}` | yes | **yes** | Read (decrypted) value |
| POST | `/v0/secrets/{key}` | yes | **yes** | Create |
| PATCH | `/v0/secrets/{key}` | yes | **yes** | Update |
| DELETE | `/v0/secrets/{key}` | yes | **yes** | Delete |

### Checks on `/v0/secrets/{key}`

In `handleSecretID`, these run in order after auth:

| Check | Status | `error.type` |
|---|---|---|
| Empty key (`/v0/secrets/`) | 400 | `missing_key` |
| Key longer than 256 bytes, or has a character outside `[A-Za-z0-9._-]` (so no `/`) | 400 | `invalid_key` |
| `X-Cove-Source` missing | 400 | `missing_source` |
| Method not GET/POST/PATCH/DELETE | 405 | `method_not_allowed` |

Request bodies (POST/PATCH) can be at most **64 KB** (`maxBodyBytes`). Anything larger, or invalid JSON, returns `400 invalid_body`. A body missing `value` is accepted and stores an empty string.

### Endpoints

**`GET /v0/health`**
```json
{ "success": true, "data": { "healthy": true, "time": "2026-05-15T12:00:00Z" } }
```
This only shows that the HTTP server is up. It doesn't check the database; use `/v0/ready` for that.

**`GET /v0/ready`**: pings the database (2-second limit).
```json
{ "success": true, "data": { "ready": true, "time": "2026-05-15T12:00:00Z" } }
```
Returns `503 not_ready` when the database is unreachable. The Docker healthcheck uses this.

**`GET /v0/version`**: requires auth.
```json
{ "success": true, "data": { "version": "v1.0.0" } }
```

**`GET /v0/auth`**
```json
{ "success": true, "data": { "authenticated": true, "time": "2026-05-15T12:00:00Z" } }
```

**`GET /v0/secrets`**: metadata only, sorted by key. Values are never returned.
```json
{
  "success": true,
  "data": {
    "secrets": [
      { "key": "my-api-key", "version": 3, "times_pulled": 12,
        "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-04-10T08:30:00Z" }
    ]
  }
}
```
If the vault is empty, `secrets` is `[]`. A DB error returns `500 get_all`.

**`GET /v0/secrets/{key}`**: adds 1 to `times_pulled` (`read_count` in the database) and logs a `read` event.
```json
{ "success": true, "data": { "key": "my-api-key", "value": "abc123", "version": 3 } }
```
Every failure (missing key, decrypt error, DB error) returns `404 not_found`.

**`POST /v0/secrets/{key}`** with body `{ "value": "..." }` returns `201`:
```json
{ "success": true, "data": { "key": "my-api-key", "action": "created", "message": "my-api-key has been created." } }
```
Every failure, including a key that already exists, returns `500 create_error`.

**`PATCH /v0/secrets/{key}`** with body `{ "value": "..." }` returns `200` with `"action": "updated"`. Every failure, including a key that doesn't exist, returns `500 update_error`.

**`DELETE /v0/secrets/{key}`** returns `200` with `"action": "deleted"`. Every failure returns `404 not_found`.

### Error type reference

| `type` | Status | Where |
|---|---|---|
| `missing_token`, `invalid_token_format`, `invalid_token` | 401 | auth middleware |
| `missing_key`, `invalid_key`, `missing_source`, `invalid_body` | 400 | secret routes |
| `not_found` | 404 | unknown `/v0/secrets*` path, GET/DELETE failure |
| `method_not_allowed` | 405 | wrong method |
| `bootstrap_locked`, `bootstrap_expired`, `bootstrap_forbidden` | 403 | bootstrap endpoint closed / window ran out / address not allowed ([§7](#7-bootstrap-flow)) |
| `get_all`, `create_error`, `update_error`, `server_error`, `marker_error` | 500 | DB / config / bootstrap marker failures |
| `not_ready` | 503 | `/v0/ready` when the database is unreachable |

### curl examples

```bash
COVE=http://localhost:2110
TOKEN=...   # COVE_CLIENT_SECRET

curl $COVE/v0/health
curl -H "Authorization: Bearer $TOKEN" $COVE/v0/secrets
curl -H "Authorization: Bearer $TOKEN" -H "X-Cove-Source: curl" $COVE/v0/secrets/my-key
curl -X POST  -H "Authorization: Bearer $TOKEN" -H "X-Cove-Source: curl" -d '{"value":"s3cret"}' $COVE/v0/secrets/my-key
curl -X PATCH -H "Authorization: Bearer $TOKEN" -H "X-Cove-Source: curl" -d '{"value":"n3w"}'    $COVE/v0/secrets/my-key
curl -X DELETE -H "Authorization: Bearer $TOKEN" -H "X-Cove-Source: curl" $COVE/v0/secrets/my-key
```

---

## 7. Bootstrap flow

The bootstrap endpoint gives a new client (e.g. Lighthouse) the client token (`COVE_CLIENT_SECRET`) before it has any credentials. Because it hands out a token that unlocks every secret, it's **closed unless you open it**, and then only briefly.

### Onboarding a client

```
cove> bootstrap open           # open for 10 minutes (or e.g. `bootstrap open 30m`)
        (start the client; it fetches the token once and saves it)
cove> bootstrap status         # check it worked: shows the last handout and recent attempts
```

With CoveClient, the client side is one call on every start: `LoadOrBootstrap(path)` reads the saved token, or fetches and saves it when there's none yet.

### Rules

`GET /v0/bootstrap/lighthouse` answers:

| Situation | Response |
|---|---|
| The endpoint is open (within its window) | `200 { "secret": "..." }`, and the endpoint **closes** |
| The same address asks again within **2 minutes** of a handout (e.g. it crashed before saving the token) | `200` again (`redelivered`) |
| Closed (never opened, already used, or `bootstrap lock`) | `403 bootstrap_locked` |
| Opened, but the window ran out before anyone used it | `403 bootstrap_expired` |
| `COVE_BOOTSTRAP_ALLOWED_CIDRS` is set and the address isn't in it | `403 bootstrap_forbidden` (doesn't use up the window) |
| The state file can't be read or saved | `500 marker_error` |

- **Closed by default:** a fresh install never starts with the endpoint open.
- **Allowed networks (optional):** set `COVE_BOOTSTRAP_ALLOWED_CIDRS`, e.g. `172.18.0.0/16` for the Docker network. The caller's address comes from the connection itself, never from headers like `X-Forwarded-For`.
- **Every attempt is recorded** in `cove.bootstrap_log` (time, address, outcome) and in the server log. `bootstrap status` shows the last 5.
- **CLI:** `bootstrap open [duration]` (1m–24h; `bootstrap clear` is the older name), `bootstrap lock` (close now, including any grace period), `bootstrap status` (or just `bootstrap`).

### State

The state lives in `<APP_MARKER_PATH>/bootstrap.json` (default `/app/vault/markers/bootstrap.json`, bind-mounted in Docker so it survives restarts): when the open window ends, and the last handout (time and address). It's written atomically. A missing file means closed; a corrupt one fails closed. The v0.2.0 marker file, `bootstrap_completed`, is ignored and removed on the next `open` or `lock`.

---

## 8. CLI

The CLI works directly on the vault (not through the HTTP API), and everything it does is logged with source `cove_cli`. There are three ways to run it:

| Invocation | What it does |
|---|---|
| `cove shell` | Interactive prompt, e.g. `docker exec -it cove /cove shell`. On a terminal it has line editing, up/down history, and Tab completion of commands and secret keys. `exit`, Ctrl+D or Ctrl+C leave the shell; the server keeps running. |
| `cove <command> [args]` | Runs one command and exits: `0` on success, `1` on failure. E.g. `docker exec cove /cove list MYAPP_`. |
| `cove` (no arguments) | The server with the prompt on stdin, as in v0.2.0, for running Cove directly in a terminal. Here `exit` stops the whole server. No line editing. The Docker image runs `cove serve` instead. |

Shell and one-shot mode use the server's `.env` but never generate secrets or run migrations (that's the server's job).

The prompt shows the environment from `APP_ENV`: `cove (dev)>`, and `cove (prod)>` in red.

### Commands

| Command | Alias | Usage | Notes |
|---|---|---|---|
| `get` | `g` | `get <key>` | Prints only the value, so `value=$(cove get KEY)` works. Logged as a read, but **doesn't** add to `read_count` (that counts app reads). |
| `create` | `c` | `create <key> <value>` | Refuses keys the API can't read. |
| `update` | `u` | `update <key> <value>` | Reports the new version. |
| `generate` | | `generate <key> [length] [--yes]` | Random value (letters and digits, 32 by default, 16–256), printed once. Asks before replacing an existing value. |
| `delete` | `d` | `delete <key> [--yes]` | Asks for confirmation unless `--yes`. With no way to answer (e.g. `docker exec` without `-it`) it fails and suggests `--yes`. |
| `rename` | | `rename <key> <new-key>` | Keeps the value, version and read count. Logged under both keys. |
| `restore` | | `restore <key> [version] [--yes]` | Brings back the previous value, a specific version, or a deleted secret's last value. Saved as a new version, so nothing is lost. |
| `list` | `l` | `list [prefix]` | Table of keys (optionally starting with `prefix`): version, reads, created, updated, and a count. Never shows values. |
| `search` | `s` | `search <text>` | Keys containing `text` (not case-sensitive). The older `list <text> fuzzy` still works. |
| `info` | `i` | `info <key>` | Version, app reads, when and by whom it was last read, created and updated times. For a deleted key, says when and by whom it was deleted. |
| `history` | | `history <key> [count]` | The last `count` events (default 20): when, what, version, source, detail. Works for deleted keys. |
| `status` | | `status` | Version, environment, database, schema version, number of secrets, bootstrap state. Non-zero exit when something needs attention. |
| `bootstrap` | `b` | `bootstrap open [duration]` / `lock` / `status` | Opens the bootstrap endpoint for 10 minutes (or the given duration), closes it, or shows its state and recent attempts ([§7](#7-bootstrap-flow)). |
| `help` | `h` | `help [command]` | All commands, or one. |
| `exit` | `quit` | `exit` | Leaves the shell (or stops the server in plain `cove`). |

Keys and values are split on whitespace, so they can't contain spaces from the CLI; use the API for those.

### Output

Following the [clig.dev](https://clig.dev) conventions:

- **Data goes to stdout, uncolored:** values, tables, help. Scripts can capture it.
- **Messages go to stderr,** marked with a symbol so the meaning doesn't depend on color: `✓` success, `!` warning or wrong arguments (with the correct `Usage:`), `✗` error, `?` a question.
- **Color only on a terminal,** and never when `NO_COLOR` is set, so `docker logs` and scripts get plain text.

---

## 9. Running locally

Prerequisites: Go 1.25.1+ and a PostgreSQL instance with the schema from [§4](#4-database).

```bash
cp .env.example .env
```

Edit `.env`:

```env
COVE_DATABASE_URL=postgres://cove_app:pass@localhost:5432/cove_db
COVE_MIGRATE_DATABASE_URL=postgres://cove_migrator:pass@localhost:5432/cove_db
COVE_CLIENT_SECRET=           # empty = generate
VAULT_ENCRYPTION_KEY=         # empty = generate
APP_ENV=DEV
APP_PORT=2110
APP_ENV_PATH=.env
APP_MARKER_PATH=./markers
```

```bash
go run ./cmd/cove
# or
go build -o cove ./cmd/cove && ./cove
```

VS Code: `.vscode/launch.json` has a debug configuration for `cmd/cove`.

---

## 10. Deploying with Docker

### Image (`Dockerfile`)

Two-stage build: `golang:1.25.1-alpine` builds the binary, then it's copied to `/cove` in `alpine:latest`. The working directory is `/app`. The image exposes `2100` and runs `/cove serve`. The `VERSION` build argument is stamped into the binary (compose passes `COVE_VERSION`, default `dev`).

### Compose (`docker-compose.yml`)

| Setting | Value |
|---|---|
| Port | `2100:2100` |
| Network | `spark` (external, must already exist) |
| `.env` | `/srv/server/storage/cove/.env` → `/app/vault/.env` (**read-only**) |
| Markers | `/srv/server/storage/cove/markers` → `/app/vault/markers` |
| Command | `cove serve` (the image's default): API only, no TTY, so nothing typed into the CLI reaches `docker logs` |
| Restart | `unless-stopped` |
| Healthcheck | `wget -qO- http://localhost:2100/v0/ready` every 10s (unhealthy when the database is unreachable) |

Because `.env` is mounted read-only, **the host `.env` must already contain `COVE_CLIENT_SECRET` and `VAULT_ENCRYPTION_KEY`**. If either is empty, Cove can't save a generated value, and stops with a message naming the setting and the file. Generate the values first (for example by running Cove locally once, or with `openssl rand -base64 36 | tr -dc 'A-Za-z0-9'`).

### First deploy

```bash
# on the server
mkdir -p /srv/server/storage/cove/markers
cp .env.example /srv/server/storage/cove/.env
# edit it: COVE_DATABASE_URL, COVE_CLIENT_SECRET, VAULT_ENCRYPTION_KEY
chmod 600 /srv/server/storage/cove/.env

docker network create spark        # if it doesn't exist
COVE_VERSION=$(git describe --tags --always) docker compose up -d --build
docker compose logs -f cove
```

The Postgres host in `COVE_DATABASE_URL` must be reachable from inside the container. If Postgres is on the `spark` network, use its container name as the host.

### Updating

```bash
git pull
COVE_VERSION=$(git describe --tags --always) docker compose up -d --build
```

State lives in Postgres and the bind mounts, so rebuilding the container is safe.

### Using the CLI in the container

```bash
docker exec -it cove /cove shell       # interactive, with Tab completion; exit leaves the shell
docker exec cove /cove list MYAPP_     # one command
docker exec cove /cove status          # health overview; exits non-zero if something's wrong
```

`docker exec` sessions aren't recorded in `docker logs`, and `exit` or Ctrl+C only end the session. A handy alias on the server: `alias cove='docker exec -it cove /cove shell'`.

There's no `docker attach` CLI any more: the container runs `cove serve` without a TTY. (Plain `cove`, with the prompt on stdin, still exists for running Cove directly in a terminal.)

---

## 11. Operations

### Backups

Back up all of these. **Without the key, the DB dump is useless.**

1. PostgreSQL: `pg_dump -n cove ...`
2. `/srv/server/storage/cove/.env` (contains `VAULT_ENCRYPTION_KEY` and `COVE_CLIENT_SECRET`)

### Rotating the client token

1. Put a new `COVE_CLIENT_SECRET` (at least 24 characters) in the host `.env`.
2. `docker compose restart cove`
3. Update every client, either by hand or by running `bootstrap open` and letting a client fetch it again (with `LoadOrBootstrap`, delete its token file first).

### Recovering a deleted or overwritten secret

`restore <key>` brings back the previous value (or a deleted secret's last value); `restore <key> <version>` brings back a specific one. `history <key>` shows the versions. The restored value is saved as a new version.

### Network exposure

Cove speaks plain HTTP and has no rate limiting. Keep it on the internal Docker network or LAN. If it has to be reachable from outside, put it behind a TLS reverse proxy (Caddy, Nginx, Traefik).

---

## 12. Known issues and gotchas

These remain in the current code. They're grouped by how much they matter.

> See [IMPROVEMENTS.md](IMPROVEMENTS.md) for ratings (criticality, effort, improvement), proposed fixes, and a suggested order of work.

### API behavior

1. Status codes are coarse: a duplicate create returns `500` (not `409`), updating a missing key returns `500` (not `404`), and a decrypt failure on GET returns `404`.
2. `/v0/health`, `/v0/ready`, `/v0/auth` and `/v0/version` accept any HTTP method.
3. `read_count` goes up even when decryption then fails.
4. There's one global token. Clients can't be given read-only or per-secret access; bootstrap hands out that same token.

### Process and runtime

5. `LogEvent` errors are thrown away, so a broken `event_log` table fails silently.
6. The Dockerfile runs `mkdir -p /app/cove`, which isn't used anywhere.
7. `restore` only sees history recorded under the key's current name; values from before a `rename` are under the old name.
