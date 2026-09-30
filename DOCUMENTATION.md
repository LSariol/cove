# Cove Documentation

Full reference for Cove, a self-hosted secret vault. Version v1.0.0, API version `v0`.

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
    tx.go                     The Store interface and WithinTx (transactions)
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
    logging.go                One log line per request (never values or tokens)
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
| `VAULT_NEW_ENCRYPTION_KEY` | No | `main` | Only for `cove rotate-key`: the key to re-encrypt the vault with ([§11](#rotating-the-vault-key)). Remove it afterwards. |
| `COVE_EVENT_LOG_RETENTION_DAYS` | No | `main` | Removes *read* events older than this many days, at startup and then daily. Creates, updates, deletes and renames are always kept. Empty keeps everything. |
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
| `00008_clear_read_event_values.sql` | Clears the encrypted value copies older read events stored (reads no longer store one) |
| `00009_prune_read_events.sql` | `cove.prune_read_events(interval)`: removes old read events for `COVE_EVENT_LOG_RETENTION_DAYS`. Runs as `cove_owner`; `cove_app` may only call it |
| `00010_tokens.sql` | `cove.tokens` (per-project tokens) and `cove.token_log` (every change to them; append-only for `cove_app`) |
| `00011_vault_key.sql` | `cove.vault_key`: a fingerprint of the key the vault is encrypted with (`cove_app` may only record it once). `set_updated_at` no longer counts a re-encryption as a modification |

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
| `read` | `NULL` | `NULL` | `Vault.Get` (API) / `Vault.Show` (CLI `get`) |
| `update` | previous ciphertext | new ciphertext | `Vault.Update`, `Vault.Restore` |
| `delete` | deleted ciphertext | `NULL` | `Vault.Delete` |
| `rename` | `NULL` | `NULL` | `Vault.Rename`, once under the old key and once under the new |

`kind` is the enum `cove.secret_event_kind`. `detail` holds optional context such as "renamed from X" or "restored version 3". `source` is the token's name for API calls made with a project token, the `X-Cove-Source` header value for calls made with the master token, or `cove_cli` for CLI calls. `secret_version` is the secret's version after the operation. `occurred_at` is set automatically.

The log holds ciphertext only, and only on the events that change a value (reads record who read which version, not the value). `restore` uses it to bring back earlier values.

**Every operation and its event are saved in one transaction.** If the event can't be recorded, the operation fails and nothing changes, so nothing happens without a record. Updates lock the secret's row first, so concurrent changes are applied and logged one after another.

The table is **append-only for `cove_app`**: it has `SELECT` and `INSERT` only, so the running app can't rewrite history. The one exception is `COVE_EVENT_LOG_RETENTION_DAYS`, which removes old *read* events through a function that can't touch anything else.

You don't need SQL to read it: the CLI's `history <key>` and `info <key>` show a secret's events and last read, and `restore` brings back earlier values.

### `cove.tokens` and `cove.token_log`

Per-project tokens ([§6](#project-tokens)). Manage them with the CLI's `token` command, not SQL.

| Column | Notes |
|---|---|
| `name` | The project, e.g. `lighthouse`. Unique; lowercase letters, digits, `.`, `_`, `-`; can't start with `cove`. Recorded as the source of the project's requests. |
| `token_hash` | SHA-256 of the token. **The token itself is never stored**; it's shown once, when created or rotated. |
| `read_patterns` / `write_patterns` | What the token can reach (see below). Write patterns also allow reading. |
| `created_at` / `rotated_at` / `last_used_at` | `last_used_at` is updated at most once a minute per token. |

`cove.token_log` records every create, rotate, revoke, allow, deny, and key rename that changed a token, with the source (`cove_cli`). It's append-only for `cove_app`, and it outlives revoked tokens. `token show <name>` shows the latest entries. Each change and its log entry are saved in one transaction.

---

## 5. Encryption

`internal/encryption/cipher.go`

- **Key:** `sha256.Sum256([]byte(VAULT_ENCRYPTION_KEY))`, which gives 32 bytes, so AES-256. It's computed once, when `encryption.NewCipher` is called at startup.
- **Mode:** AES-GCM with a random 12-byte nonce for each encryption.
- **Stored format:** `base64.URLEncoding( nonce || ciphertext || GCM tag )`.
- **Decrypt:** base64url-decodes the value, splits off the first 12 bytes as the nonce, then `gcm.Open`. GCM authenticates the data, so a wrong key or tampered value returns an error instead of garbage.

Consequences:

- **If you lose `VAULT_ENCRYPTION_KEY`, every secret is gone.** Back it up somewhere other than the Cove host.
- **Cove knows which key the vault is encrypted with.** `cove.vault_key` holds a fingerprint of it (a one-way hash; the key can't be worked out from it). On its first start, Cove checks that `VAULT_ENCRYPTION_KEY` decrypts a stored secret and records the fingerprint. After that it **refuses to start with a different key**, saying so, instead of failing every read. Every write checks it too, so values encrypted with two different keys can never be mixed.
- **Changing the key means re-encrypting everything:** use `cove rotate-key` ([§11](#rotating-the-vault-key)). Just editing `VAULT_ENCRYPTION_KEY` makes Cove refuse to start.
- The key is hashed with plain SHA-256, not a password KDF. That's fine for a long random key like the generated one. Don't use a short, human-chosen passphrase.

---

## 6. HTTP API

Base path: `/v0`. Every response is JSON with `Content-Type: application/json`.

### Response envelope

```json
{ "success": true,  "data":  { ... } }
{ "success": false, "error": { "type": "<code>", "message": "<text>" } }
```

An error can also carry `"keys": [...]`, the secret keys it's about (e.g. the missing keys of a batch read).

### Authentication

Every route except `/v0/health`, `/v0/ready` and `/v0/bootstrap/lighthouse` needs:

```
Authorization: Bearer <token>
```

The token is either the **master token** (`COVE_CLIENT_SECRET`), with full read/write access to every secret, or a **project token**, limited to the keys it was given (next section).

The middleware (`middleware.go`) checks these in order:

| Condition | Status | `error.type` |
|---|---|---|
| Header missing | 401 | `missing_token` |
| Not in the form `Bearer <token>` (case-sensitive `Bearer`) | 401 | `invalid_token_format` |
| The master token (constant-time compare) | — | accepted, full access |
| A project token (looked up by its SHA-256 hash) | — | accepted, limited access |
| The token store can't be reached | 503 | `auth_unavailable` (not 401, so a client doesn't conclude its token is wrong) |
| Neither | 401 | `invalid_token` |

**Rate limit on failures.** An address that fails 10 times within a minute (missing, malformed or wrong tokens, and refused bootstrap requests) is refused for 5 minutes: `429 too_many_requests` with a `Retry-After` header, and a `rate limit:` line in the server log. Requests with a valid token never count, so busy clients are unaffected, and a token check that fails because the database is down (`503`) doesn't count either. The counts live in memory and reset when Cove restarts. (Behind a reverse proxy, every client would share the proxy's address; Cove doesn't publish a port, so this doesn't apply today.)

### Project tokens

Each project can have its own token instead of sharing the master token. Create them in the CLI:

```
cove> token create marquee  --allow 'MARQUEE_*'  --allow SHARED_DISCORD_WEBHOOK_URL
cove> token create botsuite   --allow 'BOTSUITE_*'   --allow SHARED_TMDB_API_KEY --write BOTSUITE_TWITCH_ACCESS_TOKEN
```

- **Patterns:** an exact key (`SHARED_TMDB_API_KEY`), a prefix ending in `*` (`LIGHTHOUSE_*`), or `*` for every key. Keys are case-sensitive.
- **Read vs write:** `--allow` patterns can be read. `--write` patterns can also be created, updated and deleted.
- **Nothing is reachable unless a pattern covers it.** A key named `SHARED_*` isn't special; share it by adding it to each token that needs it (`token allow SHARED_OPENAI_API_KEY botsuite marquee`).
- **What a project token changes in the API:**
  - `GET /v0/secrets/{key}` needs a read (or write) pattern covering the key, and `POST`/`PATCH`/`DELETE` a write pattern. Otherwise the answer is `403 forbidden_key`, e.g. `marquee's token can't read BOTSUITE_DATABASE_URL`. The answer is the same whether or not the key exists, so a project can't probe for other projects' keys.
  - `GET /v0/secrets` lists only the keys the token can read.
  - The event log records the token's name as the source. `X-Cove-Source` isn't needed and is ignored, so the log can't be spoofed.
- **The master token is unchanged** and keeps full access, so projects can move to their own tokens one at a time.

See [Connecting a project](#connecting-a-project-the-standard) for how projects are expected to use Cove, and [§11](#moving-a-project-to-the-standard) for moving an existing project over.

### Connecting a project (the standard)

Every project gets its secrets the same way, so there's one thing to remember and one thing to check.

**Default: Lighthouse injects the values at deploy time.** The project's `docker-compose.yml` refers to each secret it needs as `${KEY}`, where `KEY` is the secret's name in Cove:

```yaml
environment:
  - DATABASE_URL=${MARQUEE_DATABASE_URL}
  - TMDB_API_KEY=${SHARED_TMDB_API_KEY}
```

When it deploys, Lighthouse finds every `${...}` name in the file (its parser: `\$\{([^}:]+)(?::[^}]*)?\}`), fetches those secrets from Cove, and runs `docker compose up` with `MARQUEE_DATABASE_URL=<value>` and so on in its environment; Compose then fills in the file. So a key used this way must also be a valid Compose variable name, which the [naming standard](#key-naming-standard) guarantees.

The project just reads environment variables (`os.Getenv("DATABASE_URL")`). It has **no Cove code, no Cove address and no Cove token**. Off-the-shelf images (ones you didn't write) work the same way.

**Exception: a project that changes secrets itself** (today: botsuite, refreshing its Twitch tokens) also gets its own token, allowed to write only the keys it updates:

```
cove> token create botsuite --allow 'BOTSUITE_*' --write BOTSUITE_TWITCH_ACCESS_TOKEN
cove> create BOTSUITE_COVE_TOKEN <the printed token>
```

```yaml
environment:
  - COVE_URL=http://cove:2100
  - COVE_TOKEN=${BOTSUITE_COVE_TOKEN}
```

The project uses CoveClient with those two values (`coveclient.New(os.Getenv("COVE_URL"), os.Getenv("COVE_TOKEN"), "botsuite")`). It can also read its own keys that way, or have them injected like any other project.

**Lighthouse** has its own read-only token over everything (`token create lighthouse --allow '*'`): it can read any secret to deploy it, but can't change or delete one. It gets that token with `bootstrap open lighthouse` and CoveClient's `LoadOrBootstrap`.

**The master token (`COVE_CLIENT_SECRET`) is given to no project.** Keep it for emergencies.

#### Key naming standard

Every key is **`PROJECT_PLATFORM_TYPE`**, in capitals, digits and underscores only, e.g. `BOTSUITE_TWITCH_CLIENT_ID`:

| Part | What it is | Examples |
|---|---|---|
| `PROJECT` | the project that owns it, or `SHARED` for one used by several | `BOTSUITE`, `MARQUEE`, `LIGHTHOUSE`, `SHARED` |
| `PLATFORM` | the service it's for | `TWITCH`, `NETFLIX`, `TMDB`, `GITHUB`, `DISCORD`, `DATABASE`, `COVE` |
| `TYPE` | what kind of value, from the fixed list below | `API_KEY`, `CLIENT_ID` |

| `TYPE` | For | Example |
|---|---|---|
| `API_KEY` | a single key a service issues | `BOTSUITE_NETFLIX_API_KEY` |
| `CLIENT_ID` / `CLIENT_SECRET` | OAuth app credentials | `BOTSUITE_TWITCH_CLIENT_SECRET` |
| `ACCESS_TOKEN` / `REFRESH_TOKEN` | OAuth tokens (the ones a project may write back) | `BOTSUITE_TWITCH_ACCESS_TOKEN` |
| `TOKEN` | any other bearer token | `LIGHTHOUSE_GITHUB_TOKEN`, `BOTSUITE_COVE_TOKEN` |
| `URL` | connection strings and webhooks | `MARQUEE_DATABASE_URL`, `SHARED_DISCORD_WEBHOOK_URL` |
| `PASSWORD` | passwords | `PLOP_SMTP_PASSWORD` |
| `SECRET` | a random value the project uses itself (signing, sessions) | `MARQUEE_SESSION_SECRET` |

Why: the names must work as `${...}` Compose variables (letters, digits, `_`; no `.` or `-`); the `PROJECT_` prefix gives each project token one pattern (`--allow 'BOTSUITE_*'`, and the `_` keeps `BOT_*` from matching `BOTSUITE_...`); and `list BOTSUITE_` or `search TWITCH` find things at a glance. The CLI warns when `create` or `rename` uses a name with other characters. `COVE_URL` isn't a secret: write `http://cove:2100` in the compose file directly.

Why injection is the default: on a single server, anyone who can see a container's environment can also get inside it, so passing a token instead of values protects nothing extra; injection needs no Cove code in the project, works for any language or image, and lets a project restart even while Cove is down. After changing a secret's value, redeploy the projects that use it (`info <key>` shows who reads it). A token is only worth its extra moving parts when the project must write.

### Routes

| Method | Path | Auth | `X-Cove-Source` | Purpose |
|---|---|---|---|---|
| GET | `/v0/health` | no | no | Liveness: the HTTP server is up |
| GET | `/v0/ready` | no | no | Readiness: the database is reachable |
| GET | `/v0/version` | yes | no | The running Cove version |
| GET | `/v0/bootstrap/lighthouse` | no | no | Token handout while opened with `bootstrap open` ([§7](#7-bootstrap-flow)) |
| GET | `/v0/auth` | yes | no | Checks that the token is valid |
| GET | `/v0/secrets` | yes | no | List metadata for all secrets (a project token sees only its keys) |
| GET | `/v0/secrets/{key}` | yes | master token only | Read (decrypted) value |
| POST | `/v0/secrets/{key}` | yes | master token only | Create |
| PATCH | `/v0/secrets/{key}` | yes | master token only | Update |
| DELETE | `/v0/secrets/{key}` | yes | master token only | Delete |
| POST | `/v0/batch` | yes | master token only | Read several secrets in one request (all or nothing) |

### Checks on `/v0/secrets/{key}`

In `handleSecretID`, these run in order after auth:

| Check | Status | `error.type` |
|---|---|---|
| Empty key (`/v0/secrets/`) | 400 | `missing_key` |
| Key longer than 256 bytes, or has a character outside `[A-Za-z0-9._-]` (so no `/`) | 400 | `invalid_key` |
| Project token doesn't cover the key (read for GET, write otherwise) | 403 | `forbidden_key` |
| Master token and `X-Cove-Source` missing | 400 | `missing_source` |
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
If the vault is empty (or a project token can read none of it), `secrets` is `[]`. A DB error returns `500 get_all`.

**`GET /v0/secrets/{key}`**: adds 1 to `times_pulled` (`read_count` in the database) and logs a `read` event.
```json
{ "success": true, "data": { "key": "my-api-key", "value": "abc123", "version": 3 } }
```
A missing key returns `404 not_found`. A value that can't be decrypted (usually a changed `VAULT_ENCRYPTION_KEY`) returns `500 decrypt_error`, and a database error `500 read_error`.

**`POST /v0/secrets/{key}`** with body `{ "value": "..." }` returns `201`:
```json
{ "success": true, "data": { "key": "my-api-key", "action": "created", "message": "my-api-key has been created." } }
```
A key that already exists returns `409 already_exists`; other failures `500 create_error`.

**`PATCH /v0/secrets/{key}`** with body `{ "value": "..." }` returns `200` with `"action": "updated"`. A key that doesn't exist returns `404 not_found`; other failures `500 update_error`.

**`DELETE /v0/secrets/{key}`** returns `200` with `"action": "deleted"`. A missing key returns `404 not_found`; other failures `500 delete_error`.

**`POST /v0/batch`** with body `{ "keys": ["MARQUEE_DATABASE_URL", "SHARED_TMDB_API_KEY"] }` (1–100 keys) returns `200`:
```json
{ "success": true, "data": { "secrets": [
    { "key": "MARQUEE_DATABASE_URL", "value": "postgres://...", "version": 2 },
    { "key": "SHARED_TMDB_API_KEY", "value": "...", "version": 1 } ] } }
```
Secrets come back in the order asked for, duplicates once. Each counts as a read and gets a `read` event, all in one transaction. It's **all or nothing**, checked in this order:

1. The body must list 1–100 valid keys: otherwise `400 invalid_body` / `invalid_key`.
2. A project token must be able to read **every** key: otherwise `403 forbidden_key`, **without saying which**. The server log names them (`docker logs cove`), so you can fix the access. This check comes before the existence check, so a batch can't reveal whether another project's key exists.
3. Every key must exist: otherwise `404 not_found`, naming **every** missing key in the message and in `error.keys`, e.g. `"keys": ["MARQUEE_TWITCH_CLIENT_ID", "MARQUEE_TWITCH_CLIENT_SECRET"]`.
4. If a value can't be decrypted: `500 decrypt_error`.

If any check fails, nothing is read or counted. With the master token, `X-Cove-Source` is required, as for a single read. CoveClient's `GetSecrets(keys...)` uses this endpoint (and falls back to one request per key on an older Cove).

Before v1.0.0, every read/delete failure was `404` and every create/update failure `500`. CoveClient only checks the success codes, so the more precise errors don't affect it.

### Error type reference

| `type` | Status | Where |
|---|---|---|
| `missing_token`, `invalid_token_format`, `invalid_token` | 401 | auth middleware |
| `missing_key`, `invalid_key`, `missing_source`, `invalid_body` | 400 | secret routes |
| `not_found` | 404 | unknown `/v0/secrets*` path; missing key on GET, PATCH or DELETE; missing keys in a batch (listed in `error.keys`) |
| `already_exists` | 409 | POST for a key that exists |
| `forbidden_key` | 403 | a project token that doesn't cover the key |
| `method_not_allowed` | 405 | wrong method |
| `bootstrap_locked`, `bootstrap_expired`, `bootstrap_forbidden` | 403 | bootstrap endpoint closed / window ran out / address not allowed ([§7](#7-bootstrap-flow)) |
| `decrypt_error` | 500 | the stored value can't be decrypted |
| `get_all`, `read_error`, `create_error`, `update_error`, `delete_error`, `server_error`, `marker_error` | 500 | database / config / bootstrap state failures |
| `too_many_requests` | 429 | too many failed attempts from this address; see `Retry-After` |
| `wrong_key` | 500 | Cove's `VAULT_ENCRYPTION_KEY` isn't the vault's key: it was rotated while Cove was running (restart it with the new key) |
| `not_ready` | 503 | `/v0/ready` when the database is unreachable |
| `auth_unavailable` | 503 | a project token couldn't be checked (database unreachable) |

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

The bootstrap endpoint gives a new client (e.g. Lighthouse) a token before it has any credentials: its own project token, or the master token (`COVE_CLIENT_SECRET`). Because it hands out a working token, it's **closed unless you open it**, and then only briefly.

### Onboarding a client

```
cove> token create lighthouse --allow '*'   # once, if it has no token yet (read-only over everything)
cove> bootstrap open lighthouse    # open for 10 minutes to hand out lighthouse's token (or e.g. `bootstrap open lighthouse 30m`)
        (start the client; it fetches the token once and saves it)
cove> bootstrap status             # check it worked: shows the last handout and recent attempts
```

Only a hash of each project token is stored, so `bootstrap open <project>` gives the project a **new** token to hand out; its previous token stops working. Plain `bootstrap open` hands out the master token, as before.

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
- **Refusals count toward the rate limit:** 10 within a minute block the address for 5 minutes (`429`), even if the endpoint is opened meanwhile.
- **CLI:** `bootstrap open [project] [duration]` (1m–24h; `bootstrap clear` is the older name), `bootstrap lock` (close now, including any grace period), `bootstrap status` (or just `bootstrap`; also shows which token it hands out).

### State

The state lives in `<APP_MARKER_PATH>/bootstrap.json` (default `/app/vault/markers/bootstrap.json`, bind-mounted in Docker so it survives restarts): when the open window ends, the last handout (time and address), and, for `bootstrap open <project>`, the project's new token. That token is kept only while someone can still receive it: it's removed once the window and grace period are over, on `bootstrap lock`, or when the endpoint is opened again. The file is written atomically, readable only by its owner. A missing file means closed; a corrupt one fails closed. The v0.2.0 marker file, `bootstrap_completed`, is ignored and removed on the next `open` or `lock`.

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
| `delete` | `d` | `delete <key> [--yes]` | Asks for confirmation unless `--yes`. With no way to answer (e.g. `docker exec` without `-it`) it fails and suggests `--yes`. Warns which project tokens list the key (they keep it, so a restore makes it reachable again). |
| `rename` | | `rename <key> <new-key> [--yes]` | Keeps the value, version and read count. Logged under both keys. If project tokens list the key by name, it asks to update them too (`--yes` does it without asking). |
| `restore` | | `restore <key> [version] [--yes]` | Brings back the previous value, a specific version, or a deleted secret's last value. Saved as a new version, so nothing is lost. |
| `list` | `l` | `list [prefix]` | Table of keys (optionally starting with `prefix`): version, reads, created, updated, and a count. Never shows values. |
| `search` | `s` | `search <text>` | Keys containing `text` (not case-sensitive). The older `list <text> fuzzy` still works. |
| `info` | `i` | `info <key>` | Version, app reads, when and by whom it was last read, created and updated times, and which project tokens can read it. For a deleted key, says when and by whom it was deleted. |
| `history` | | `history <key> [count]` | The last `count` events (default 20): when, what, version, source, detail. Works for deleted keys. |
| `status` | | `status` | Version, environment, database, schema version, number of secrets, vault key (OK with fingerprint and last rotation, or WRONG), bootstrap state. Non-zero exit when something needs attention. |
| `bootstrap` | `b` | `bootstrap open [project] [duration]` / `lock` / `status` | Opens the bootstrap endpoint for 10 minutes (or the given duration) to hand out a project's new token or the master token, closes it, or shows its state and recent attempts ([§7](#7-bootstrap-flow)). |
| `token` | `t` | `token [list]` / `create <name> [--allow <p>]... [--write <p>]...` / `show <name>` / `allow <p> <name>... [--write]` / `deny <p> <name>...` / `rotate <name> [--yes]` / `revoke <name> [--yes]` | Per-project tokens ([§6](#project-tokens)). `create` and `rotate` print the token once, on stdout. Patterns that match no secret get a warning (usually a typo); `deny` warns if a wildcard still covers the key. |
| `help` | `h` | `help [command]` / `help setup` / `help patterns` | A short grouped overview; one command in detail with examples; or a step-by-step guide to setting up a project, or to token patterns. Works without a database (`cove help`). |
| `exit` | `quit` | `exit` | Leaves the shell (or stops the server in plain `cove`). |

After `create` or `generate`, the CLI says which project tokens can read the new secret, or gives the `token allow` command if none can (only once project tokens exist).

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

Two-stage build: `golang:1.25.1-alpine` builds a static binary, then it's copied to `/cove` in `alpine:3.24`. The working directory is `/app`. The image exposes `2100` and runs `/cove serve`. The `VERSION` build argument is stamped into the binary (compose passes `COVE_VERSION`, default `dev`).

- **Cove runs as user `10001`, not root.** That user exists only inside the image; the host needs no account for it. But the bind-mounted files must be owned by it: run `chown -R 10001:10001 /srv/server/storage/cove` on the host once (see [First deploy](#first-deploy)). If you forget, Cove stops at startup with a "permission denied" message that includes the command. The binary itself stays owned by root, so Cove can't overwrite it.
- **The base image is pinned** (`alpine:3.24`, which follows 3.24.x patch releases), so a rebuild gets the same base as before. Bump it on purpose.
- **`.dockerignore` keeps `.env`, `markers/`, `.git` and the docs out of the build**, so no secrets end up in the build cache.

### Compose (`docker-compose.yml`)

| Setting | Value |
|---|---|
| Port | **Not published.** Only containers on `spark` can reach Cove, at `http://cove:2100`; nothing on the LAN can connect. The CLI uses `docker exec`, which needs no port. |
| Network | `spark` (external, must already exist) |
| `.env` | `/srv/server/storage/cove/.env` → `/app/vault/.env` (**read-only**) |
| Markers | `/srv/server/storage/cove/markers` → `/app/vault/markers` |
| Command | `cove serve` (the image's default): API only, no TTY, so nothing typed into the CLI reaches `docker logs` |
| Restart | `unless-stopped` |
| Healthcheck | `wget -qO- http://localhost:2100/v0/ready` every 10s (unhealthy when the database is unreachable) |
| Restrictions | `read_only: true` (the container's filesystem is read-only; Cove writes only to the markers mount, and `/tmp` is a tmpfs), `cap_drop: [ALL]` (no special Linux permissions; port 2100 doesn't need any), `no-new-privileges` (nothing in the container can gain more rights) |

Because `.env` is mounted read-only, **the host `.env` must already contain `COVE_CLIENT_SECRET` and `VAULT_ENCRYPTION_KEY`**. If either is empty, Cove can't save a generated value, and stops with a message naming the setting and the file. Generate the values first (for example by running Cove locally once, or with `openssl rand -base64 36 | tr -dc 'A-Za-z0-9'`).

### First deploy

```bash
# on the server
mkdir -p /srv/server/storage/cove/markers
cp .env.example /srv/server/storage/cove/.env
# edit it: COVE_DATABASE_URL, COVE_CLIENT_SECRET, VAULT_ENCRYPTION_KEY
chmod 600 /srv/server/storage/cove/.env
chown -R 10001:10001 /srv/server/storage/cove    # Cove's user in the container

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

If you replace or recreate the host `.env` or `markers/` (e.g. edit `.env` with a tool that writes a new file as root), run the `chown` again.

### Using the CLI in the container

```bash
docker exec -it cove /cove shell       # interactive, with Tab completion; exit leaves the shell
docker exec cove /cove list MYAPP_     # one command
docker exec cove /cove status          # health overview; exits non-zero if something's wrong
```

`docker exec` sessions run as the same user as Cove (`10001`), aren't recorded in `docker logs`, and `exit` or Ctrl+C only end the session. A handy alias on the server: `alias cove='docker exec -it cove /cove shell'`.

There's no `docker attach` CLI any more: the container runs `cove serve` without a TTY. (Plain `cove`, with the prompt on stdin, still exists for running Cove directly in a terminal.)

---

## 11. Operations

### Backups

Back up all of these. **Without the key, the DB dump is useless.**

1. PostgreSQL: `pg_dump -n cove ...`
2. `/srv/server/storage/cove/.env` (contains `VAULT_ENCRYPTION_KEY` and `COVE_CLIENT_SECRET`)

### Moving a project to the standard

One project at a time ([Connecting a project](#connecting-a-project-the-standard) describes the target).

**A project that only reads secrets:**
1. List what it gets from Cove today: its `GetSecret`/`GetSecrets` calls, and any `${...}` placeholders already in its compose file.
2. For each secret it fetches itself, add a line to its compose file (`NAME=${KEY}`) and read `os.Getenv("NAME")` in the code instead.
3. Remove `COVE_CLIENT_SECRET` (and any Cove URL) from its compose/`.env`, and CoveClient from its code.
4. Redeploy with Lighthouse. Check: it starts and works, and `history <one of its keys>` shows `lighthouse` as the reader (not the project).

**A project that writes back (e.g. botsuite):**
1. `token create <project> --allow '<PROJECT>_*' --write <each key it updates>`, then `create <PROJECT>_COVE_TOKEN <the printed token>`.
2. `token show <project>`: check what it can reach; warnings mean a pattern matches nothing.
3. In its compose: `COVE_URL=http://cove:2100` and `COVE_TOKEN=${<PROJECT>_COVE_TOKEN}`, replacing `COVE_CLIENT_SECRET`. In the code, CoveClient v1.0.0 with those two values.
4. Redeploy. Check: `token list` shows a "last used" time; `history <a key it writes>` shows the project's name. If it hits `403 forbidden_key`, the message names the key: `token allow <key> <project>` (add `--write` to change it) fixes it without a restart.

When no project holds the master token any more, change it (see [Rotating the master token](#rotating-the-master-token)).

### Rotating a project token

`token rotate <project>` prints a new token with the same access; the old one stops working at once. Save it with `update <PROJECT>_COVE_TOKEN <new token>` and redeploy the project. For Lighthouse itself (which fetches its token with `LoadOrBootstrap`), delete its token file and use `bootstrap open lighthouse` instead. If a token may have leaked, `token revoke <project>` stops it immediately, and every shared secret it could read should be changed too (`info <key>` lists who can read a key).

### Rotating the vault key

`cove rotate-key` re-encrypts every secret, and every value copy in the event log, with a new `VAULT_ENCRYPTION_KEY`, in **one transaction**: it either finishes completely or changes nothing. Do it if the key may have leaked, or to replace a short key. It takes a second or two; projects with injected values keep running throughout.

1. **Back up** the database (`pg_dump -d cove_db`) and the `.env` file, **with the old key**. Until you're sure everything works, that pair is your way back.
2. Get a new key: `docker compose run --rm cove /cove rotate-key` prints a line to add (`VAULT_NEW_ENCRYPTION_KEY=...`) and changes nothing. Add that line to `/srv/server/storage/cove/.env`, keeping `VAULT_ENCRYPTION_KEY` as it is. The new key is now saved before anything uses it.
3. Stop Cove: `docker compose stop cove`. (Recommended, not required: a Cove left running can't read or write until it's restarted with the new key, but it can never store anything with the old one.)
4. Rotate: `docker compose run --rm cove /cove rotate-key`. It needs `COVE_MIGRATE_DATABASE_URL`, because only the schema owner may rewrite the event log. If any stored value can't be decrypted with the old key, it stops, names them, and changes nothing.
5. In the `.env`: set `VAULT_ENCRYPTION_KEY` to the new value and delete the `VAULT_NEW_ENCRYPTION_KEY` line. If your editor wrote a new file, run the `chown` from [§10](#first-deploy) again.
6. Start Cove: `docker compose up -d --force-recreate cove` (`--force-recreate` makes Docker pick up the edited file).
7. **Check:** `docker exec cove /cove status` shows `Vault key: OK (fingerprint ..., rotated <now>)`, and `docker exec cove /cove get <a key>` works.

If you start Cove after step 4 but forget step 5, it refuses to start and says exactly which lines to change. **Way back** after step 4: the old key no longer opens the vault, so restore the backup from step 1 (database and `.env` together).

### Rotating the master token

1. Put a new `COVE_CLIENT_SECRET` (at least 24 characters) in the host `.env`.
2. `docker compose restart cove`
3. Update every client still using the master token, either by hand or by running `bootstrap open` and letting a client fetch it again (with `LoadOrBootstrap`, delete its token file first). Projects with their own tokens are unaffected.

### Recovering a deleted or overwritten secret

`restore <key>` brings back the previous value (or a deleted secret's last value); `restore <key> <version>` brings back a specific one. `history <key>` shows the versions. The restored value is saved as a new version.

### Network exposure

Cove publishes no port: it's reachable only from containers on the `spark` Docker network, at `http://cove:2100`. That traffic never leaves the server, so plain HTTP is fine there, and nothing on your LAN (or beyond) can even try a token.

- **Every client uses `http://cove:2100`.** A project on another Docker network must join `spark`.
- **Something on the server itself, outside Docker,** can be given access with `ports: ["127.0.0.1:2100:2100"]`, which only that machine can reach; from elsewhere, use an SSH tunnel (`ssh -L 2100:127.0.0.1:2100 server`).
- **Never publish Cove on all interfaces** (`"2100:2100"`) or to the internet. If it ever has to cross a network, put it behind a TLS reverse proxy (Caddy, Nginx, Traefik) with an allowlist.

### Entering secret values

Nothing typed at the `cove>` prompt reaches `docker logs`, and its up-arrow history is kept in memory only. A **one-shot** command is different: `docker exec cove /cove create KEY value` puts the value in your server's shell history (`~/.bash_history`). So enter values inside `docker exec -it cove /cove shell`, or use `generate` so you never type the value at all.

---

## 12. Known issues and gotchas

These remain in the current code.

> See [IMPROVEMENTS.md](IMPROVEMENTS.md) for ratings (criticality, effort, improvement), proposed fixes, and a suggested order of work.

1. The master token still has full access to everything, and anyone who has it can do anything. Once every project has its own token, keep the master token for emergencies only.
2. Token management happens only in the CLI (which connects to the database directly), not through the API.
3. `restore` only sees history recorded under the key's current name; values from before a `rename` are under the old name.
4. Because the audit log is mandatory, a broken `event_log` table (e.g. a lost `INSERT` grant) makes reads and writes fail until it's fixed. The error message and the server log name the cause.
