# Cove Documentation

Full reference for Cove, a self-hosted secret vault. Version v0.2.0, API version `v0`.

The [README](readme.md) is a quick overview. This document goes further: how each piece works internally, how the server is deployed, how secrets are stored, and the known issues in the current code.

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

Cove stores key/value secrets for other self-hosted projects. It is a single Go binary that does two things at once:

- It runs an **HTTP API** on `APP_PORT`. Other apps call it (usually through CoveClient) to read and manage secrets.
- It runs an **interactive CLI** on stdin (`Cove CLI>`), for managing secrets directly on the host or inside the container.

Both use the same PostgreSQL connection pool, so a change made in one shows up in the other immediately.

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
cmd/cove/main.go              Entry point
internal/config/env.go        .env loading, generating missing secrets
internal/database/
    database.go               pgxpool connection (NewDB, Connect, Close)
    models.go                 Secret, EventLogInput, event types
    store.go                  CRUD queries + LogEvent
internal/encryption/
    encryption.go             Encrypt / Decrypt (AES-256-GCM)
    generator.go              GenerateSecret (random alphanumeric)
internal/server/
    server.go                 Server struct, ListenAndServe
    handlers.go               Routes, health/auth/bootstrap handlers, key validation, marker files
    secrets.go                Secret CRUD handlers, writeResponse / writeError
    auth.go                   Bearer-token middleware
    models.go                 JSON response types
internal/cli/cli.go           Interactive CLI
```

### Startup sequence (`cmd/cove/main.go`)

1. `config.Load()` loads `.env` from the working directory. If that file doesn't exist, it tries `/app/vault/.env`. If neither exists, it panics.
2. `config.Ensure()` checks for `COVE_CLIENT_SECRET` and `VAULT_ENCRYPTION_KEY`. If either is empty, it generates a value and writes it back to the file at `APP_ENV_PATH` (see [§12](#12-known-issues-and-gotchas): the value is written to the file but not loaded into the running process).
3. A context is created that is cancelled on `SIGINT` / `SIGTERM`.
4. If `COVE_MIGRATE_DATABASE_URL` is set, pending database migrations are applied (see [§4](#4-database)). A failed migration stops startup.
5. `database.NewDB()` reads `COVE_DATABASE_URL`, and `Connect()` opens a `pgxpool` and pings it with a 3-second timeout. If this fails, the process exits.
6. `CheckSchemaVersion()` confirms every migration built into this binary has been applied. If not, Cove stops with a message saying to set `COVE_MIGRATE_DATABASE_URL`.
7. The HTTP server starts in a goroutine (`srv.Start()`).
8. The CLI loop runs on the main goroutine (`cli.StartCLI(ctx)`).
9. When stdin closes, `StartCLI` returns and `main` waits for a signal (`<-ctx.Done()`). So Cove can run headless: with no stdin attached, the API keeps serving.

### Request flow

```
HTTP request
  → http.ServeMux (handlers.go: defineRoutes)
  → authenticateClientSecret middleware (auth.go)       [not for /health, /bootstrap]
  → handleSecretsCollection / handleSecretID            [path + key + X-Cove-Source checks]
  → getSecret / postSecret / patchSecret / deleteSecret (secrets.go)
  → database.* (store.go) → encryption.Encrypt/Decrypt
  → database.LogEvent
  → writeResponse / writeError (JSON envelope)
```

---

## 3. Configuration

All configuration comes from environment variables. `config.Load()` reads them from a `.env` file. godotenv **never overrides** a variable that is already set, so values from docker-compose's `environment:` section take priority over the `.env` file.

| Variable | Required | Used by | Description |
|---|---|---|---|
| `COVE_DATABASE_URL` | **Yes** | `database.NewDB` | Connection string for the runtime role, e.g. `postgres://cove_app:pass@sparkdb:5432/cove_db`. Not in `.env.exmaple`; you must add it. |
| `COVE_MIGRATE_DATABASE_URL` | No | `database.Migrate` | Connection string for the migrator role, e.g. `postgres://cove_migrator:pass@sparkdb:5432/cove_db`. When set, Cove applies pending migrations on startup and `cove migrate` works. When unset, the database must already be migrated. |
| `COVE_CLIENT_SECRET` | Yes* | `server/auth.go`, bootstrap | Bearer token that clients must send. *Generated (32 chars) if empty. |
| `VAULT_ENCRYPTION_KEY` | Yes* | `encryption` | Master key material. SHA-256 of this value is the AES key. *Generated (45 chars) if empty. |
| `APP_PORT` | **Yes** | `server.Start` | Listen port. The server binds `0.0.0.0:$APP_PORT`. |
| `APP_ENV_PATH` | Yes* | `config.Ensure` | File that generated secrets are written to. *Only needed when a secret has to be generated. |
| `APP_MARKER_PATH` | No | bootstrap marker | Directory for the `bootstrap_completed` marker. Default: `/app/vault/markers`. |
| `APP_ENV` | No | — | `DEV` / `PROD` label. Not read by the code. |
| `APP_VAULT_PATH` | No | — | Left over from the old `vault.json` storage. Not read by the code. |
| `APP_MARKER_DIR` | No | — | Set in `docker-compose.yml`, but **the code reads `APP_MARKER_PATH`**. In Docker it works anyway because the default path is the same. |

Generated secrets use `encryption.GenerateSecret(n)`: `n` characters from `[a-zA-Z0-9]`, picked with `crypto/rand`.

> **Warning:** `config.Store` rewrites the whole `.env` file with `godotenv.Write`. Comments and formatting are lost, and values are re-quoted.

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
| `read_count` | Goes up by 1 on every `GetSecret` call (API **and** CLI `get`). Exposed in the API as `times_pulled`. |
| `created_at` / `updated_at` | `updated_at` is set by the `set_updated_at` trigger on any change except a `read_count`-only update. |

### `cove.event_log`

Every operation in `store.go` writes one row through `LogEvent`:

| `kind` | `old_encrypted_value` | `new_encrypted_value` | Written by |
|---|---|---|---|
| `create` | `NULL` | new ciphertext | `CreateSecret` |
| `read` | current ciphertext | `NULL` | `GetSecret` |
| `update` | previous ciphertext | new ciphertext | `UpdateSecret` |
| `delete` | deleted ciphertext | `NULL` | `DeleteSecret` |

`kind` is the enum `cove.secret_event_kind`. `source` is the `X-Cove-Source` header value for API calls, or `cove_cli` for CLI calls. `secret_version` is the secret's version after the operation. `occurred_at` is set automatically.

The log holds ciphertext only. With `VAULT_ENCRYPTION_KEY` you can decrypt old values to recover a previous version or a deleted secret. Errors from `LogEvent` are ignored (`_ = d.LogEvent(...)`), so a failed log write never fails the operation. The table is **append-only for `cove_app`**: it has `SELECT` and `INSERT` only, so the running app can't rewrite history.

Useful queries:

```sql
-- Who has been reading a secret?
SELECT occurred_at, source FROM cove.event_log
WHERE secret_key = 'my-key' AND kind = 'read'
ORDER BY occurred_at DESC LIMIT 20;

-- History of a secret
SELECT occurred_at, secret_version, kind, source FROM cove.event_log
WHERE secret_key = 'my-key' ORDER BY occurred_at;
```

---

## 5. Encryption

`internal/encryption/encryption.go`

- **Key:** `sha256.Sum256([]byte(os.Getenv("VAULT_ENCRYPTION_KEY")))`, which gives 32 bytes, so AES-256. The env var is read on every call.
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

The middleware (`auth.go`) checks these in order:

| Condition | Status | `error.type` |
|---|---|---|
| Header missing | 401 | `missing_token` |
| Not in the form `Bearer <token>` (case-sensitive `Bearer`) | 401 | `invalid_token_format` |
| Token doesn't match (constant-time compare) | 401 | `invalid_token` |

There is only one token. Every client gets the same access: full read/write to every secret.

### Routes

| Method | Path | Auth | `X-Cove-Source` | Purpose |
|---|---|---|---|---|
| any | `/v0/health` | no | no | Liveness check |
| GET | `/v0/bootstrap/lighthouse` | no | no | One-time token handout ([§7](#7-bootstrap-flow)) |
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
This only shows that the HTTP server is up. It doesn't check the database.

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
If the vault is empty, `secrets` is `null`, not `[]`. A DB error returns `500 get_all`.

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
| `bootstrap_locked` | 403 | bootstrap already used |
| `get_all`, `create_error`, `update_error`, `server_error` | 500 | DB / config failures |

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

The bootstrap endpoint lets a new client (named "Lighthouse" in the code) get `COVE_CLIENT_SECRET` without having any credentials yet. A marker file controls whether it's open:

```
<APP_MARKER_PATH>/bootstrap_completed      (default /app/vault/markers/bootstrap_completed)
```

`GET /v0/bootstrap/lighthouse`:

1. Tries to create the marker with `O_CREATE|O_EXCL`, so only one caller can succeed.
2. If the marker already exists (or can't be created), returns `403 bootstrap_locked`.
3. If `COVE_CLIENT_SECRET` is empty, deletes the marker again and returns `500 server_error`.
4. Otherwise returns `{ "secret": "<COVE_CLIENT_SECRET>" }` and prints `Bootstrap complete`.

Typical use:

```
Cove CLI> bootstrap clear        # open the window
          (start the new client; it calls Bootstrap() once and stores the token)
          (the first call automatically closes the window again)
Cove CLI> bootstrap lock         # or close it by hand without it being used
```

The marker directory is bind-mounted in Docker, so the lock state survives container restarts. On a fresh install there's no marker, so **the endpoint starts open**. Anyone who can reach the port before your client does gets the token. Run `bootstrap lock` right after the first deploy if you don't plan to use it.

---

## 8. CLI

The prompt is `Cove CLI>`. Input is split on whitespace (`strings.Fields`), so **keys and values can't contain spaces** from the CLI. Use the API for those. CLI actions go directly to the database (skipping HTTP and key validation) and are logged with source `cove_cli`.

| Command | Alias | Usage | Notes |
|---|---|---|---|
| `get` | `g` | `get <key>` | Prints the decrypted value. **Adds 1 to `read_count` and logs a `read` event.** |
| `create` | `c` | `create <key> <value>` | |
| `update` | `u` | `update <key> <value>` | Adds 1 to the version. |
| `delete` | `d` | `delete <key>` | Asks for `y`/`yes` to confirm. |
| `list` | `l` | `list` | All secrets, as a table (key, date added, last modified, version, times pulled). |
| | | `list <term>` | Keys starting with `<term>` (case-insensitive). |
| | | `list <term> fuzzy` / `list <term> f` | Keys containing `<term>` (case-insensitive). |
| `bootstrap` | `b` | `bootstrap clear` | Deletes the marker, which **opens** the bootstrap endpoint. |
| | | `bootstrap lock` | Creates the marker, which **closes** it. Errors if it's already locked. |
| `help` | `h` | `help` | |
| `exit` | `quit` | `exit` | Calls `os.Exit(0)` right away. |

Output colors: green = success, yellow = warning, red = error, cyan = info, plain = tables.

---

## 9. Running locally

Prerequisites: Go 1.25.1+ and a PostgreSQL instance with the schema from [§4](#4-database).

```bash
cp .env.exmaple .env
```

Edit `.env`:

```env
COVE_DATABASE_URL=postgres://user:pass@localhost:5432/yourdb
COVE_CLIENT_SECRET=           # empty = generate
VAULT_ENCRYPTION_KEY=         # empty = generate
APP_ENV=DEV
APP_PORT=2110
APP_ENV_PATH=.env
APP_MARKER_PATH=./markers
```

> **Important:** `.env.exmaple` uses the literal text `Kept Empty` as the value of both secrets. That's a non-empty string, so Cove will use it as-is and won't generate anything. Delete it or replace it with real values.

```bash
go run ./cmd/cove
# or
go build -o cove ./cmd/cove && ./cove
```

**On first run with empty secrets, restart once** after the values have been generated (see [§12](#12-known-issues-and-gotchas)).

VS Code: `.vscode/launch.json` has a debug configuration for `cmd/cove`.

---

## 10. Deploying with Docker

### Image (`Dockerfile`)

Two-stage build: `golang:1.25.1-alpine` builds the binary, then it's copied to `/cove` in `alpine:latest`. The working directory is `/app`. The image exposes `2100`.

### Compose (`docker-compose.yml`)

| Setting | Value |
|---|---|
| Port | `2100:2100` |
| Network | `spark` (external, must already exist) |
| `.env` | `/srv/server/storage/cove/.env` → `/app/vault/.env` (**read-only**) |
| Markers | `/srv/server/storage/cove/markers` → `/app/vault/markers` |
| `vault.json` | `/srv/server/storage/cove/vault.json` → `/app/vault/vault.json`. Legacy and unused, but **the host file must exist** or Docker will create a directory in its place. |
| `stdin_open` + `tty` | Keeps the CLI usable with `docker attach` |
| Restart | `unless-stopped` |
| Healthcheck | `wget -qO- http://localhost:2100/v0/health` every 10s |

Because `.env` is mounted read-only, **the host `.env` must already contain `COVE_CLIENT_SECRET` and `VAULT_ENCRYPTION_KEY`**. If either is empty, `config.Ensure` can't write the file and the container panics on start. Generate the values first (for example by running Cove locally once, or with `openssl rand -base64 36 | tr -dc 'A-Za-z0-9'`).

### First deploy

```bash
# on the server
mkdir -p /srv/server/storage/cove/markers
touch /srv/server/storage/cove/vault.json
cp .env.exmaple /srv/server/storage/cove/.env
# edit it: COVE_DATABASE_URL, COVE_CLIENT_SECRET, VAULT_ENCRYPTION_KEY
chmod 600 /srv/server/storage/cove/.env

docker network create spark        # if it doesn't exist
docker compose up -d --build
docker compose logs -f cove
```

The Postgres host in `COVE_DATABASE_URL` must be reachable from inside the container. If Postgres is on the `spark` network, use its container name as the host.

### Updating

```bash
git pull
docker compose up -d --build
```

State lives in Postgres and the bind mounts, so rebuilding the container is safe.

### Using the CLI in the container

```bash
docker attach cove        # detach with Ctrl+P, Ctrl+Q (Ctrl+C sends SIGINT to Cove)
```

---

## 11. Operations

### Backups

Back up all of these. **Without the key, the DB dump is useless.**

1. PostgreSQL: `pg_dump -n cove ...`
2. `/srv/server/storage/cove/.env` (contains `VAULT_ENCRYPTION_KEY` and `COVE_CLIENT_SECRET`)

### Rotating the client token

1. Put a new `COVE_CLIENT_SECRET` in the host `.env`.
2. `docker compose restart cove`
3. Update every client, either by hand or by running `bootstrap clear` and letting a client call `Bootstrap()` again.

### Recovering a deleted or overwritten secret

Find the ciphertext in `cove.event_log.old_encrypted_value`, decrypt it with the vault key (same algorithm as [§5](#5-encryption)), and `create` / `update` it back.

### Network exposure

Cove speaks plain HTTP and has no rate limiting. Keep it on the internal Docker network or LAN. If it has to be reachable from outside, put it behind a TLS reverse proxy (Caddy, Nginx, Traefik).

---

## 12. Known issues and gotchas

These are found in the v0.2.0 code and haven't been fixed. They're grouped by how much they matter.

> See [IMPROVEMENTS.md](IMPROVEMENTS.md) for ratings (criticality, effort, improvement), proposed fixes, and a suggested order of work.

### Can cause data loss or failed starts

1. **Generated secrets aren't used until restart.** `config.Ensure` writes new values to the `.env` file but never calls `os.Setenv`. For the rest of that first run, `VAULT_ENCRYPTION_KEY` and `COVE_CLIENT_SECRET` are empty. As a result:
   - Secrets created in that session are encrypted with SHA-256 of the empty string. **After a restart they can't be decrypted.**
   - Every authenticated API call returns `401`, and bootstrap returns `500`.
   - **Workaround:** after the first start with empty secrets, stop Cove and start it again before storing anything.
2. **Read-only `.env` in Docker.** If either secret is missing from the host `.env`, `Ensure` fails to write and `main` panics. Pre-fill both values (see [§10](#10-deploying-with-docker)).
3. **`.env.exmaple` placeholders.** `Kept Empty` is a real value, so no secrets get generated, and the vault key becomes the literal string `Kept Empty`.

### Configuration mismatches

4. `docker-compose.yml` sets `APP_MARKER_DIR`, but the code reads `APP_MARKER_PATH`. It works only because the default (`/app/vault/markers`) matches.
5. `APP_VAULT_PATH`, the `vault.json` mount, and `APP_ENV` are unused leftovers.
6. `COVE_DATABASE_URL` is missing from `.env.exmaple`. The filename itself is misspelled (`exmaple`).
7. `config.Load` ignores `APP_ENV_PATH`. It always tries `./.env` and then `/app/vault/.env`, while `Ensure` writes to `APP_ENV_PATH`. If these point to different files, generated secrets go to a file that never gets loaded.
8. The Dockerfile runs `mkdir -p /app/cove`, which isn't used anywhere.

### API behavior

9. Status codes are coarse: a duplicate create returns `500` (not `409`), updating a missing key returns `500` (not `404`), and a decrypt failure on GET returns `404`.
10. `GET /v0/secrets` returns `"secrets": null` when the vault is empty.
11. `/v0/health` and `/v0/auth` accept any HTTP method, and health doesn't check the database.
12. The bootstrap endpoint starts **open** on a fresh install (no marker file).
13. `read_count` goes up even when decryption then fails.
14. There's one global token. Clients can't be given read-only or per-secret access.

### Process and runtime

15. **Shutdown with a TTY attached:** on `SIGTERM` the context is cancelled, but `StartCLI` stays blocked reading stdin, so the process doesn't exit. Docker kills it after its stop timeout (10s by default).
16. `exit` / `quit` call `os.Exit(0)`, so the DB pool isn't closed and the HTTP server isn't shut down gracefully.
17. The HTTP server uses `http.ListenAndServe` with no read/write timeouts. `log.Fatal` ends the whole process if the listener fails (for example, if the port is in use).
18. `database.Connect` calls `os.Exit(1)` if `pgxpool.New` fails (for example, a malformed URL) instead of returning an error.
19. `encryption.Decrypt` ignores base64 decode errors and slices the nonce without checking the length. A stored value shorter than 12 bytes causes a panic. `net/http` recovers from it for API calls, but it crashes the process from the CLI.
20. `LogEvent` errors are thrown away, so a broken `event_log` table fails silently.
21. `config.Store` rewrites the whole `.env` file and drops comments.
