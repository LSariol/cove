# Cove Documentation

The complete reference for **Cove v1.0.0**, the self-hosted secret vault for the `spark` server's projects. API version `v0`.

- [README](README.md): a short overview and quick start.
- [CHANGELOG](CHANGELOG.md): what changed in each release.
- [CoveClient](https://github.com/LSariol/CoveClient): the Go client library, with its own `DOCUMENTATION.md`.

---

## Contents

1. [Overview](#1-overview)
2. [Quick reference](#2-quick-reference)
3. [Architecture](#3-architecture)
4. [Configuration](#4-configuration)
5. [Database](#5-database)
6. [Encryption and the vault key](#6-encryption-and-the-vault-key)
7. [Security model](#7-security-model)
8. [HTTP API](#8-http-api)
9. [Connecting a project (the standard)](#9-connecting-a-project-the-standard)
10. [Bootstrap](#10-bootstrap)
11. [CLI](#11-cli)
12. [Running locally](#12-running-locally)
13. [Deploying with Docker](#13-deploying-with-docker)
14. [Operations](#14-operations)
15. [Troubleshooting](#15-troubleshooting)
16. [Development](#16-development)
17. [Known limitations](#17-known-limitations)

---

## 1. Overview

Cove stores the secrets (API keys, passwords, connection strings, tokens) that the other projects on the server need. It's one Go program with two faces:

- An **HTTP API** that projects and Lighthouse call, usually through CoveClient.
- A **CLI** for managing secrets by hand: `docker exec -it cove /cove shell` for a prompt, or one command at a time.

Both work on the same Postgres database, so a change in one shows up in the other at once.

**Where it fits:** Lighthouse, the orchestrator, reads secrets from Cove and injects them into each project when it deploys it ([§9](#9-connecting-a-project-the-standard)). Projects that need to change a secret themselves call Cove directly with their own token. Cove runs in Docker on the `spark` network, reachable only at `http://cove:2100`, with its data in the `cove_db` database on `sparkdb`.

| Property | How |
|---|---|
| Encryption at rest | AES-256-GCM; the database only ever holds ciphertext. The key comes from `VAULT_ENCRYPTION_KEY`, which Cove fingerprints so it can't run with the wrong one. |
| Access | **Per-project tokens**, each limited to the keys it needs, read-only or read/write. There is no token with access to everything unless you create one. |
| Audit | Every create, read, update, delete and rename is recorded, with who did it, in the same transaction as the change. Token changes and bootstrap requests are recorded too. |
| History | Every value a secret has had is kept (encrypted), so any version, or a deleted secret, can be restored. |
| Onboarding | A bootstrap endpoint, closed by default, hands a new client (Lighthouse) its token once. |
| Network | No published port; reachable only from containers on `spark`. |
| Hardening | Runs as a non-root user in a read-only container with no Linux capabilities; failed logins are rate-limited. |

---

## 2. Quick reference

Everyday tasks, all in the Cove CLI (`docker exec -it cove /cove shell`, or `alias cove='docker exec -it cove /cove shell'`):

| Task | Command |
|---|---|
| Add a secret | `create MARQUEE_TMDB_API_KEY abc123` (or `generate MARQUEE_SESSION_SECRET 64` for a random one) |
| Change a secret | `update MARQUEE_TMDB_API_KEY newvalue`, then redeploy the projects that use it |
| See a secret | `get MARQUEE_TMDB_API_KEY` |
| Find secrets | `list MARQUEE_` (starts with), `search TWITCH` (contains) |
| Who can read a secret / who read it | `info KEY`, `history KEY` |
| Undo a change or a delete | `restore KEY` (previous value) or `restore KEY 3` (version 3) |
| Rename to the naming standard | `rename old.name MARQUEE_PLATFORM_TYPE` |
| Give a project its own token | `token create marquee --allow 'MARQUEE_*'` |
| Share a key with projects | `token allow SHARED_OPENAI_API_KEY botsuite marquee` |
| See what a project can reach | `token show marquee` |
| Onboard Lighthouse | `bootstrap open lighthouse`, then start Lighthouse |
| Health check | `status` |
| Help | `help`, `help <command>`, `help setup`, `help patterns` |

Naming: keys are `PROJECT_PLATFORM_TYPE`, e.g. `BOTSUITE_TWITCH_CLIENT_ID` ([§9](#key-naming-standard)).

---

## 3. Architecture

```
cmd/cove/
  main.go                   Entry point: picks the mode (serve, shell, one command, migrate, rotate-key, version) and wires the packages together
  rotate.go                 cove rotate-key, and the startup message for a wrong vault key
internal/
  config/config.go          Every setting, read from the environment once; generating missing secrets
  encryption/
    cipher.go               AES-256-GCM Encrypt / Decrypt, and the key fingerprint
    random.go               GenerateSecret (random letters and digits)
  database/
    database.go             Connection pool
    migrate.go, migrations/ goose migrations (built into the binary) and the startup schema check
    store.go                SQL for secrets and the event log
    tokens.go               SQL for project tokens and their log
    vaultkey.go             The vault key fingerprint, and RotateKey
    tx.go                   The Store interface and WithinTx (transactions)
    types.go                Row types
  vault/
    vault.go                The rules for secrets: encrypt, store, log every operation, key checks
    keys.go                 ValidateKey
    vaulttest/              In-memory Store for tests
  tokens/
    tokens.go               Token type, patterns, generation and hashing
    manager.go              Create / rotate / revoke / allow / deny / authenticate, each logged
    tokenstest/             In-memory token Store for tests
  bootstrap/gate.go         The bootstrap gate: open window, one handout, grace period, allowed networks
  server/
    server.go               Server; Run serves until stopped, then shuts down gracefully
    routes.go               URL → handler table
    middleware.go           Token check
    ratelimit.go            Per-address limit on failed attempts
    secrets.go, batch.go    /v0/secrets and /v0/batch
    system.go               /v0/health, /v0/ready, /v0/auth, /v0/version
    bootstrap.go            /v0/bootstrap/lighthouse
    respond.go, api_types.go, logging.go   JSON envelope, response types, one log line per request
  cli/
    shell.go, terminal.go   The prompt (line editing, Tab completion), Exec, confirmations
    commands.go             Command table, help and guides
    cmd_*.go                One file per group of commands
    naming.go               The naming-standard warning
    output.go               stdout/stderr, symbols, color, the prompt
```

Dependencies point one way: `main` → `server` / `cli` → `vault` / `tokens` / `bootstrap` → `database` + `encryption`. Only `config` reads environment variables.

**Where things go:**
- A rule about secrets (validation, what's logged, transactions) → `vault`, so the API and the CLI both get it.
- A rule about tokens → `tokens`.
- SQL → `database`; a schema change → a new migration ([§5](#migrations)).
- An API route → a handler in `server/`, registered in `routes.go`.
- A CLI command → a `cli/cmd_*.go` function plus one entry in `commandTable()`; `help` picks it up.
- A setting → a field in `config.Config`, read in `fromEnv`.

### Startup (`cove serve`)

1. `config.Load` reads the `.env` file: `APP_ENV_PATH` if set (as in Docker), else the first of `./.env` and `/app/vault/.env`.
2. `config.Ensure` generates a missing `VAULT_ENCRYPTION_KEY` and saves it to that file. `Validate` stops startup if `COVE_DATABASE_URL` or `APP_PORT` is missing or a setting is malformed (a short vault key only warns).
3. If `COVE_MIGRATE_DATABASE_URL` is set, pending migrations are applied.
4. The connection pool opens as `cove_app`; `CheckSchemaVersion` refuses a database missing any migration this build needs.
5. `EnsureKey`: the first time, Cove checks that `VAULT_ENCRYPTION_KEY` decrypts a stored secret and records its fingerprint; afterwards it refuses to start with any other key.
6. The API serves on `0.0.0.0:$APP_PORT` until `SIGINT`/`SIGTERM` (`docker stop`), then gives requests in progress 5 seconds to finish. With `COVE_EVENT_LOG_RETENTION_DAYS`, old read events are pruned daily.

Every startup error is one line, `cove: <message>`, and exit status 1. `cove shell` and one-shot commands use the same `.env` and steps 1 and 4 only; they never generate secrets or migrate.

### A request

```
HTTP request → logRequests (one log line) → ServeMux (routes.go)
  → requireToken: a project token; rate limit on failures            [not /health, /ready, /bootstrap]
  → handler: key checks, the token's access check
  → vault: encrypt/decrypt, store, event log, key check — one transaction
  → JSON envelope
```

---

## 4. Configuration

Settings are environment variables, read once from the `.env` file. Values already in the environment (e.g. Compose's `environment:`) take priority over the file.

| Variable | Required | Description |
|---|---|---|
| `COVE_DATABASE_URL` | **Yes** | Connection string for the runtime role, e.g. `postgres://cove_app:pass@sparkdb:5432/cove_db`. |
| `COVE_MIGRATE_DATABASE_URL` | Recommended | Connection string for the migrator role (`cove_migrator`). When set, Cove applies pending migrations on startup. `cove migrate` and `cove rotate-key` need it. |
| `VAULT_ENCRYPTION_KEY` | Yes* | The vault key ([§6](#6-encryption-and-the-vault-key)). Under 24 characters only warns. *Generated (45 characters) if empty and the file is writable. **Never just edit it:** use `cove rotate-key`. |
| `VAULT_NEW_ENCRYPTION_KEY` | No | Only while rotating the vault key ([§14](#rotating-the-vault-key)); delete it afterwards. |
| `COVE_VERSION` | No | The version Cove reports in `status`, `cove version` and its first log line. Set in `docker-compose.yml`. Without it: `dev`, plus the git commit for a local build. |
| `APP_PORT` | **Yes** | Port the API listens on (`2100` in Docker, `2110` for local dev). |
| `APP_ENV` | No | `DEV` or `PROD`, shown in the CLI prompt (`cove (prod)>` in red). |
| `APP_ENV_PATH` | No | The `.env` file to load first, and where generated secrets are saved. Default: the file that was loaded. |
| `APP_MARKER_PATH` | No | Directory for the bootstrap state file. Default: `/app/vault/markers`. |
| `COVE_BOOTSTRAP_ALLOWED_CIDRS` | No | Networks or addresses allowed to use the bootstrap endpoint, comma-separated, e.g. `172.18.0.0/16`. Empty allows any. |
| `COVE_EVENT_LOG_RETENTION_DAYS` | No | Removes *read* events older than this many days, daily. Changes are always kept. Empty keeps everything. |

A malformed value (a bad network, a non-number) stops startup with a message naming the setting. Generated secrets are random letters and digits from `crypto/rand`. When Cove saves one, it rewrites the `.env` file (comments are lost) with owner-only permissions; in Docker the file is mounted read-only, so set both values yourself.

---

## 5. Database

Postgres, through `pgx`. The schema is the `cove` schema in `cove_db`, managed by goose migrations built into the binary.

### Roles

| Role | Login | Used for |
|---|---|---|
| `cove_owner` | No | Owns the schema and everything in it |
| `cove_migrator` | Yes | Runs migrations and key rotation. Member of `cove_owner` with `SET role = 'cove_owner'`, so what it creates is owned by `cove_owner` |
| `cove_app` | Yes | Cove at runtime (`COVE_DATABASE_URL`) |
| `cove_reader` | Yes | Read-only access for inspection (pgAdmin) |

Roles, the database and connect access are created once per environment by an admin (they're server-wide, so migrations can't create them):

```sql
CREATE ROLE cove_owner    NOLOGIN;
CREATE ROLE cove_migrator LOGIN PASSWORD '...';
CREATE ROLE cove_app      LOGIN PASSWORD '...';
CREATE ROLE cove_reader   LOGIN PASSWORD '...';
GRANT cove_owner TO cove_migrator;
ALTER ROLE cove_migrator SET role = 'cove_owner';

CREATE DATABASE cove_db OWNER cove_owner;
REVOKE ALL ON DATABASE cove_db FROM PUBLIC;
GRANT CONNECT ON DATABASE cove_db TO cove_migrator, cove_app, cove_reader;
```

What `cove_app` may do, by table (granted by the migrations):

| Table | `cove_app` | Why |
|---|---|---|
| `secrets` | read, insert, update, delete | the vault itself |
| `event_log` | read, insert | **append-only**: the running app can't rewrite history. Old *read* events are pruned only through `prune_read_events`, which can't touch anything else |
| `tokens` | read, insert, update, delete | token management |
| `token_log`, `bootstrap_log` | read, insert | append-only |
| `vault_key` | read, insert | may record the key once; only the owner (`cove rotate-key`) can change it |
| `goose_db_version` | read | the startup schema check |

`cove_reader` can read every table. Token hashes and ciphertext are useless without the tokens and the vault key.

### Migrations

| Migration | What it does |
|---|---|
| `00001_baseline` | The v0.2.0 schema as it was in prod; creates only what's missing |
| `00002_role_grants` | Grants for `cove_app` / `cove_reader`, default privileges, append-only `event_log` |
| `00003_rename_columns` | v1.0.0 column names; replaces the broken `set_last_modified` trigger with `set_updated_at` |
| `00004_event_log_history_index` | Index for per-secret history |
| `00005_secret_key_format_check` | Key format rule (`[A-Za-z0-9._-]`, 1–256), `NOT VALID` so old rows aren't checked |
| `00006_event_log_rename_and_detail` | `rename` event kind and the `detail` column |
| `00007_bootstrap_log` | `bootstrap_log` |
| `00008_clear_read_event_values` | Clears value copies old read events stored |
| `00009_prune_read_events` | `prune_read_events(interval)` for retention |
| `00010_tokens` | `tokens` and `token_log` |
| `00011_vault_key` | `vault_key`; a re-encryption no longer bumps `updated_at` |

- **On startup** with `COVE_MIGRATE_DATABASE_URL`; a Postgres lock stops two instances migrating at once. **By hand:** `cove migrate status`, `cove migrate up`.
- Cove refuses to start if the database is missing a migration it needs. A database *newer* than the binary is allowed, so rolling back the code after an additive migration still works.
- **Rules for new ones:** every schema change is a migration; never edit one that has run in prod; name it `000NN_description.sql` starting with `-- +goose Up`; wrap `$$` blocks in `-- +goose StatementBegin` / `StatementEnd`; forward-only (a mistake is fixed by the next migration).

### Tables

**`secrets`**: one row per secret.

| Column | Notes |
|---|---|
| `id` | UUID; stays the same through renames |
| `key` | The name, unique, `[A-Za-z0-9._-]`, 1–256 characters |
| `encrypted_value` | AES-GCM ciphertext, base64url |
| `version` | 1 on create, +1 on every update or restore |
| `read_count` | Reads by apps through the API (`times_pulled` in the API); the CLI's `get` doesn't count |
| `created_at`, `updated_at` | `updated_at` changes only on real modifications (not reads or re-encryption) |

**`event_log`**: the audit trail and the value history. Every vault operation writes one row in the same transaction; if it can't, the operation fails.

| `kind` | Value copy stored | Written by |
|---|---|---|
| `create` | the new value | create, restore of a deleted secret |
| `read` | none (who read which version) | API reads, the CLI's `get` |
| `update` | old and new value | update, restore |
| `delete` | the deleted value | delete |
| `rename` | none | rename, once under each name |

`source` is who did it: the token's name, or `cove_cli`. `detail` holds context such as "renamed from X" or "restored version 3".

**`tokens`**: `name` (lowercase, e.g. `botsuite`), `token_hash` (SHA-256; **the token itself is never stored**), `read_patterns`, `write_patterns`, `created_at`, `rotated_at`, `last_used_at` (updated at most once a minute).

**`token_log`**: every create, rotate, revoke, allow, deny and key rename that changed a token. Kept after a token is revoked.

**`bootstrap_log`**: every bootstrap request: time, address, outcome (`granted`, `redelivered`, `locked`, `expired`, `forbidden`, `error`).

**`vault_key`**: one row, the fingerprint of the vault key, when it was recorded, and when it was last rotated.

---

## 6. Encryption and the vault key

- **Algorithm:** AES-256-GCM with a random 12-byte nonce per value. The AES key is SHA-256 of `VAULT_ENCRYPTION_KEY`. Stored as base64url(nonce ‖ ciphertext ‖ tag). GCM detects tampering, so a wrong key or altered value is an error, never garbage.
- **The key is everything.** Without `VAULT_ENCRYPTION_KEY`, the database is unreadable. Back it up somewhere other than the server ([§14](#backups)).
- **Cove knows its key.** `vault_key` holds a one-way fingerprint of it. On the first start Cove checks that the key decrypts a stored secret, then records it; afterwards it **refuses to start with any other key**. Every write checks the fingerprint at the end of its transaction, so values encrypted with two different keys can never be mixed, even while a rotation runs.
- **Changing the key** means re-encrypting everything: `cove rotate-key` ([§14](#rotating-the-vault-key)) does it in one transaction.
- Plain SHA-256 is fine for a long random key like the generated one; don't use a short human passphrase.

---

## 7. Security model

What protects what, and what's left to you.

| Threat | Protection |
|---|---|
| Someone on the LAN or internet | No published port; only containers on `spark` can connect. |
| Guessing tokens | Tokens are 32+ random characters; 10 failures a minute from an address block it for 5 minutes. |
| A leaked project token | It reaches only its own keys (and writes only its `--write` keys); `token revoke` stops it at once without touching other projects. |
| A compromised project reading others' secrets | Project tokens are scoped; with injection, a project holds only its own values. |
| A project changing a shared secret | Tokens are read-only unless given `--write` for specific keys. |
| Probing which keys exist | Forbidden answers don't depend on whether the key exists, for single reads and batches. |
| A spoofed audit trail | The source is the token's name, which the caller can't choose. The event log is append-only for the app. |
| A compromised Cove process rewriting history | `cove_app` can't update or delete the event, token or bootstrap logs, or change the recorded vault key. |
| A container breakout | Runs as user 10001, read-only filesystem, no capabilities, no new privileges; the binary can't be modified. |
| Database dump stolen | Values are encrypted; useless without the vault key. |
| Secrets in logs | Request logs never include bodies, values or tokens; the CLI isn't attached to `docker logs`. |
| Bootstrap abuse | Closed by default, one handout per opening, 10-minute window, optional network allowlist, every attempt logged. |

**Not protected against:** someone with root or Docker access on the server (they can read the `.env` with the vault key, and the containers' environments, and can use the CLI). Cove is only as safe as the server. Back up the vault key off the server.

---

## 8. HTTP API

Base path `/v0`, JSON everywhere. Clients use `http://cove:2100` on the `spark` network.

```json
{ "success": true,  "data":  { ... } }
{ "success": false, "error": { "type": "<code>", "message": "<text>", "keys": ["..."] } }
```

`error.keys` appears only when an error is about specific keys (a batch's missing keys).

### Authentication

`Authorization: Bearer <token>` on every route except `/v0/health`, `/v0/ready` and `/v0/bootstrap/lighthouse`. The token is a **project token**, created with `token create` in the CLI, with access to its patterns only. There is no master token: full access over the API exists only if you create a token with `--write '*'`.

| Situation | Answer |
|---|---|
| No header / not `Bearer <token>` | `401 missing_token` / `401 invalid_token_format` |
| A valid token (looked up by hash) | access per its patterns; recorded under its name. An `X-Cove-Source` header, sent by older clients, is ignored |
| Token store unreachable | `503 auth_unavailable` (not 401, so a client doesn't discard a good token) |
| Anything else | `401 invalid_token` |
| 10 failed attempts in a minute from an address | `429 too_many_requests` with `Retry-After`, for 5 minutes |

### Project tokens

Created in the CLI (`token create`, [§11](#11-cli)). A **pattern** is an exact key, a prefix ending in `*` (`MARQUEE_*`), or `*`; keys are case-sensitive.
- `--allow` patterns can be read; `--write` patterns can also be created, updated and deleted.
- Nothing is reachable unless a pattern covers it.
- `GET /v0/secrets` lists only readable keys; a key outside the token's access is `403 forbidden_key`, whether or not it exists.

Tokens are `cove_` + 43 random characters; only their SHA-256 hash is stored, so a token is shown once (on `create` or `rotate`) and can't be recovered.

### Routes

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/v0/health` | – | The HTTP server is up |
| GET | `/v0/ready` | – | The database is reachable (the Docker healthcheck) |
| GET | `/v0/bootstrap/lighthouse` | – | One-time token handout ([§10](#10-bootstrap)) |
| GET | `/v0/auth` | ✓ | Is this token valid? |
| GET | `/v0/version` | ✓ | The running version |
| GET | `/v0/secrets` | ✓ | List secrets (metadata only) |
| GET | `/v0/secrets/{key}` | ✓ | Read a value (counts as a read) |
| POST | `/v0/secrets/{key}` | ✓ | Create, body `{"value": "..."}` → `201` |
| PATCH | `/v0/secrets/{key}` | ✓ | Update, body `{"value": "..."}` |
| DELETE | `/v0/secrets/{key}` | ✓ | Delete (restorable) |
| POST | `/v0/batch` | ✓ | Read several secrets at once, body `{"keys": [...]}` |

On `/v0/secrets/{key}`, checks run in this order: key present and valid (`400 missing_key` / `invalid_key`), the token's access (`403 forbidden_key`), method (`405`). Bodies are at most 64 KB (`400 invalid_body`).

### Responses

```jsonc
// GET /v0/secrets
{ "secrets": [ { "key": "MARQUEE_DATABASE_URL", "version": 3, "times_pulled": 12,
                 "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-04-10T08:30:00Z" } ] }
// GET /v0/secrets/{key}
{ "key": "MARQUEE_DATABASE_URL", "value": "postgres://...", "version": 3 }
// POST / PATCH / DELETE /v0/secrets/{key}
{ "key": "MARQUEE_DATABASE_URL", "action": "created", "message": "MARQUEE_DATABASE_URL has been created." }
// POST /v0/batch
{ "secrets": [ { "key": "...", "value": "...", "version": 1 }, ... ] }
// /v0/health, /v0/ready, /v0/auth, /v0/version
{ "healthy": true, "time": "..." }   { "ready": true, "time": "..." }   { "authenticated": true, "time": "..." }   { "version": "v1.0.0" }
```

**Batch reads** (`POST /v0/batch`, 1–100 keys) are **all or nothing**: secrets come back in the order asked for, each counted as a read, in one transaction. If the token can't read one key, the whole request is `403 forbidden_key` without saying which (the server log names them). That check comes before the existence check, so a batch can't reveal whether another project's key exists. If keys are missing, it's `404 not_found` naming every missing key in `error.keys`. If anything fails, nothing is read. CoveClient's `GetSecrets` uses it.

### Error types

| `type` | Status | Meaning |
|---|---|---|
| `missing_token`, `invalid_token_format`, `invalid_token` | 401 | see Authentication |
| `missing_key`, `invalid_key`, `invalid_body` | 400 | bad request |
| `forbidden_key` | 403 | the project token doesn't cover the key |
| `bootstrap_locked`, `bootstrap_expired`, `bootstrap_forbidden` | 403 | bootstrap refused ([§10](#10-bootstrap)) |
| `not_found` | 404 | no such secret (or route); batch: `error.keys` lists them |
| `method_not_allowed` | 405 | wrong method |
| `already_exists` | 409 | creating a key that exists |
| `too_many_requests` | 429 | rate-limited; see `Retry-After` |
| `decrypt_error` | 500 | a stored value can't be decrypted |
| `wrong_key` | 500 | the vault key was rotated while this Cove was running: restart it |
| `get_all`, `read_error`, `create_error`, `update_error`, `delete_error`, `server_error`, `marker_error` | 500 | database or bootstrap-state failure (details in the server log) |
| `not_ready`, `auth_unavailable` | 503 | the database is unreachable |

### Example

```bash
# from a container on the spark network
TOKEN=cove_...    # a project token
curl -H "Authorization: Bearer $TOKEN" http://cove:2100/v0/secrets/MARQUEE_DATABASE_URL
curl -H "Authorization: Bearer $TOKEN" -d '{"keys":["MARQUEE_DATABASE_URL","SHARED_TMDB_API_KEY"]}' http://cove:2100/v0/batch
```

---

## 9. Connecting a project (the standard)

Every project gets its secrets the same way.

**Default: Lighthouse injects them at deploy time.** The project's `docker-compose.yml` refers to each secret by its Cove name as `${KEY}`:

```yaml
environment:
  - DATABASE_URL=${MARQUEE_DATABASE_URL}
  - TMDB_API_KEY=${SHARED_TMDB_API_KEY}
```

Lighthouse finds every `${...}` (its regex: `\$\{([^}:]+)(?::[^}]*)?\}`), fetches those secrets from Cove (best as one `GetSecrets` batch: one request, all or nothing), and runs `docker compose up` with `MARQUEE_DATABASE_URL=<value>` etc. in its environment; Compose fills in the file. The project just reads environment variables: **no Cove code, address or token**. Off-the-shelf images work the same way.

**Exception: a project that changes secrets itself** (today botsuite, refreshing Twitch tokens) also gets its own token, with `--write` only on the keys it updates:

```
cove> token create botsuite --allow 'BOTSUITE_*' --write BOTSUITE_TWITCH_ACCESS_TOKEN --write BOTSUITE_TWITCH_REFRESH_TOKEN
cove> create BOTSUITE_COVE_TOKEN <the printed token>
```
```yaml
environment:
  - COVE_URL=http://cove:2100
  - COVE_TOKEN=${BOTSUITE_COVE_TOKEN}
```

The project uses CoveClient: `coveclient.New(os.Getenv("COVE_URL"), os.Getenv("COVE_TOKEN"), "botsuite")`.

**Lighthouse** has a read-only token over everything (`token create lighthouse --allow '*'`), fetched with `bootstrap open lighthouse` and CoveClient's `LoadOrBootstrap`. **There is no shared master token.**

**Why injection is the default:** on a single server, anyone who can see a container's environment can get inside it, so handing a project a token instead of values protects nothing extra. Injection needs no Cove code, works for any language or image, and lets a project restart while Cove is down. After changing a value, redeploy the projects that use it (`info KEY` shows who can read it).

### Key naming standard

Every key is **`PROJECT_PLATFORM_TYPE`**, with an optional `ROLE` before the type (`PROJECT_PLATFORM_ROLE_TYPE`): capitals, digits and `_` only.

| Part | Meaning | Examples |
|---|---|---|
| `PROJECT` | the owner; `SHARED` for keys several projects use | `BOTSUITE`, `MARQUEE`, `LIGHTHOUSE`, `PLOP`, `SHARED`, and `SPARK` for the server's own infrastructure (sparkdb's superuser, the Cloudflare tunnel) |
| `PLATFORM` | the service | `TWITCH`, `NETFLIX`, `TMDB`, `GITHUB`, `DISCORD`, `OPENAI`, `CLOUDFLARE`, `DATABASE`, `COVE` |
| `ROLE` (optional) | which of several credentials for the same platform | `APP`, `MIGRATOR`, `READER`, `OWNER`, `ADMIN` for database roles; `BOT`, `OWNER` for accounts |
| `TYPE` | the kind of value, from this list | see below |

| `TYPE` | For | Example |
|---|---|---|
| `API_KEY` | a key a service issues | `BOTSUITE_NETFLIX_API_KEY` |
| `CLIENT_ID` / `CLIENT_SECRET` | OAuth app credentials | `BOTSUITE_TWITCH_CLIENT_SECRET` |
| `ACCESS_TOKEN` / `REFRESH_TOKEN` | OAuth tokens | `BOTSUITE_TWITCH_ACCESS_TOKEN` |
| `TOKEN` | other bearer tokens | `LIGHTHOUSE_GITHUB_TOKEN`, `BOTSUITE_COVE_TOKEN` |
| `URL` | connection strings, webhooks | `MARQUEE_DATABASE_URL`, `SHARED_DISCORD_WEBHOOK_URL` |
| `PASSWORD` | passwords | `PLOP_SMTP_PASSWORD` |
| `SECRET` | random values a project uses itself | `MARQUEE_SESSION_SECRET` |
| `ID` | an identifier that isn't secret but belongs with the rest (e.g. a user ID) | `BOTSUITE_TWITCH_BOT_ID`, `MARQUEE_TWITCH_OWNER_ID` |

Database examples: `BOTSUITE_DATABASE_URL` (the app's connection string), `BOTSUITE_DATABASE_APP_PASSWORD`, `BOTSUITE_DATABASE_READER_PASSWORD`, `MARQUEE_DATABASE_MIGRATOR_URL`, `SPARK_DATABASE_ADMIN_PASSWORD`.

Why: `${...}` names must be letters, digits and `_` (Compose rejects `.` and `-`); the `PROJECT_` prefix gives each token one pattern (`BOTSUITE_*`; the `_` stops `BOT_*` matching `BOTSUITE_...`); and `list BOTSUITE_` / `search TWITCH` find things at a glance. The CLI warns on `create`, `generate` or `rename` of a non-standard name. `COVE_URL` isn't a secret: write it in the compose file.

---

## 10. Bootstrap

The bootstrap endpoint hands a project its own token before it has any credentials. It's **closed unless you open it**.

```
cove> token create lighthouse --allow '*'   # once
cove> bootstrap open lighthouse             # open for 10 minutes (or: bootstrap open lighthouse 30m)
        start Lighthouse: LoadOrBootstrap(<token file>) fetches the token once and saves it
cove> bootstrap status                      # shows the handout and recent attempts
```

Only a hash of each project token is stored, so `bootstrap open <project>` gives the project a **new** token (its previous one stops working).

| Situation | `GET /v0/bootstrap/lighthouse` |
|---|---|
| Open | `200 {"secret": "..."}`, and it **closes** |
| Same address again within 2 minutes (it crashed before saving) | `200` again |
| Closed (never opened, used, or `bootstrap lock`) | `403 bootstrap_locked` |
| The window ran out unused | `403 bootstrap_expired` |
| Address outside `COVE_BOOTSTRAP_ALLOWED_CIDRS` | `403 bootstrap_forbidden` (doesn't use up the window) |
| State file unreadable | `500 marker_error` |

- The address comes from the connection, never from headers.
- Every attempt is recorded in `bootstrap_log`; refusals count toward the rate limit.
- **State** is `<APP_MARKER_PATH>/bootstrap.json` (owner-only, written atomically): the window, the last handout, and while it can still be handed out, a project's new token, which is removed once the window and grace period are over, on `lock`, or when reopened. A missing file means closed; a corrupt one fails closed.

---

## 11. CLI

The CLI works on the vault directly (not through the API); everything it does is recorded as `cove_cli`.

| Run as | What it is |
|---|---|
| `docker exec -it cove /cove shell` | The prompt, `cove (prod)>`, with line editing, history and Tab completion. `exit` / Ctrl+D leave it. |
| `docker exec cove /cove <command>` | One command; exit status 0 or 1. |
| `cove help` | Help, even without a database. |
| `cove` (no arguments) | Server plus prompt on stdin, for running locally in a terminal (`exit` stops the server). |
| `cove migrate [status\|up]`, `cove rotate-key`, `cove version` | Admin modes. |

### Commands

`help` lists them grouped; `help <command>` shows every form with examples; `help setup` and `help patterns` are step-by-step guides.

| Command | Usage | Notes |
|---|---|---|
| `get`, `g` | `get <key>` | Prints only the value. Logged, but not counted as an app read. |
| `create`, `c` | `create <key> <value>` | Says which tokens can read the new secret, or how to grant access. |
| `update`, `u` | `update <key> <value>` | New version. |
| `generate` | `generate <key> [length] [--yes]` | Random letters and digits (32, or 16–256), printed once. Asks before replacing. |
| `delete`, `d` | `delete <key> [--yes]` | Asks first; warns which tokens list the key. Restorable. |
| `rename` | `rename <key> <new-key> [--yes]` | Keeps value, version and history (restore still finds pre-rename versions). Offers to update tokens that list the key. |
| `restore` | `restore <key> [version] [--yes]` | Previous value, a version, or a deleted secret; saved as a new version. |
| `list`, `l` | `list [prefix]` | Keys, versions, reads, dates; never values. |
| `search`, `s` | `search <text>` | Keys containing the text. |
| `info`, `i` | `info <key>` | Details, last read, and which tokens can read it. |
| `history` | `history <key> [count]` | Recent events (20): what, version, who. |
| `status` | `status` | Version, database, schema, secrets, vault key, bootstrap. Non-zero exit on problems. |
| `token`, `t` | `list` / `create <name> [--allow p]... [--write p]...` / `show` / `allow <p> <name>... [--write]` / `deny <p> <name>...` / `rotate <name>` / `revoke <name>` | Project tokens. `create`/`rotate` print the token once, on stdout. Warns about patterns matching nothing and wildcards still covering a denied key. |
| `bootstrap`, `b` | `open <project> [duration]` / `lock` / `status` | [§10](#10-bootstrap). |
| `help`, `h` / `exit` | | |

Output follows [clig.dev](https://clig.dev): data on stdout (so `$(cove get KEY)` works); messages on stderr marked `✓` `!` `✗` `?`; color only on a terminal and never with `NO_COLOR`. Arguments are split on spaces, so values with spaces need the API.

---

## 12. Running locally

Needs **Go 1.27.1** or newer and a Postgres with the roles from [§5](#roles) (the dev `sparkdb-dev` container works).

```bash
cp .env.example .env      # set COVE_DATABASE_URL and COVE_MIGRATE_DATABASE_URL; leave the secrets empty
go run ./cmd/cove         # server + prompt; or: go run ./cmd/cove serve, then go run ./cmd/cove shell
```

The first start generates the vault key into `.env` and records it. Nothing can use the API until you create a token: `token create <project> --allow '<PROJECT>_*'`.

---

## 13. Deploying with Docker

**Image** (`Dockerfile`): Go 1.27.1 builds a static binary into `alpine:3.24`; it runs `/cove serve` as user `10001`. The version Cove reports comes from `COVE_VERSION` in the compose file's `environment:`, written out as a plain value (a `${...}` placeholder there would make Lighthouse try to fetch it from Cove as a secret). `.dockerignore` keeps `.env`, `markers/` and `.git` out of the build.

**Compose** (`docker-compose.yml`):

| Setting | Value |
|---|---|
| Network | `spark` (external); **no published port** |
| `.env` | `/srv/server/storage/cove/.env` → `/app/vault/.env`, read-only |
| State | `/srv/server/storage/cove/markers` → `/app/vault/markers` |
| Restrictions | read-only filesystem (plus a `/tmp` tmpfs), all capabilities dropped, no-new-privileges |
| Restart / health | `unless-stopped`; `/v0/ready` every 10 s |

### First deploy

```bash
mkdir -p /srv/server/storage/cove/markers
cp .env.example /srv/server/storage/cove/.env    # set the database URLs and VAULT_ENCRYPTION_KEY (the file is read-only in the container)
chmod 600 /srv/server/storage/cove/.env
sudo chown -R 10001:10001 /srv/server/storage/cove   # Cove's user in the container; no host account needed
docker compose up -d --build
docker exec cove /cove status
```

### Updating

```bash
git pull
docker compose up -d --build
```

State lives in Postgres and the bind mounts, so rebuilding is safe. If an editor replaces the `.env` with a new root-owned file, run the `chown` again.

---

## 14. Operations

### Backups

Back up **both**, and keep them together: a database dump without the key is useless.
1. `docker exec sparkdb pg_dump -U <admin> -d cove_db > cove_db-$(date +%F).sql`
2. `/srv/server/storage/cove/.env` (the vault key), stored off the server.

### Moving a project to the standard

Rename its keys to the standard in the same change as its compose file (anything asking for an old name gets "not found"; `rename` keeps history and offers to update tokens).

- **A project that only reads:** compose `NAME=${KEY}` per secret; code reads `os.Getenv`; no Cove token or CoveClient; redeploy with Lighthouse. Check: `history KEY` shows `lighthouse`.
- **A project that writes:** `token create <project> --allow '<PROJECT>_*' --write <keys it updates>`, `create <PROJECT>_COVE_TOKEN <token>`; compose `COVE_URL` + `COVE_TOKEN=${<PROJECT>_COVE_TOKEN}`; redeploy. Check: `token list` shows it used; a `403 forbidden_key` names the key to `token allow`.

### Rotating a project token

`token rotate <project>` prints a new token; the old one stops at once. Store it (`update <PROJECT>_COVE_TOKEN <token>`) and redeploy. For Lighthouse: delete its token file and `bootstrap open lighthouse`. If a token leaked: `token revoke <project>` now, then change every secret it could read (`token show <project>`).

### Rotating the vault key

`cove rotate-key` re-encrypts every secret and every history value in **one transaction**: it finishes or changes nothing.

1. **Back up** the database and the `.env` (with the old key): that pair is the way back.
2. `docker compose run --rm cove /cove rotate-key` prints a `VAULT_NEW_ENCRYPTION_KEY=...` line and changes nothing. Add it to the host `.env`.
3. `docker compose stop cove` (recommended).
4. `docker compose run --rm cove /cove rotate-key`. It needs `COVE_MIGRATE_DATABASE_URL`; if any value can't be decrypted it names them and changes nothing.
5. In the `.env`: `VAULT_ENCRYPTION_KEY=<new value>`; delete the `VAULT_NEW_ENCRYPTION_KEY` line.
6. `docker compose up -d --force-recreate cove`.
7. Check: `status` shows `Vault key: OK (..., rotated <now>)`; `get` a key.

Starting after step 4 but before step 5 is refused with a message saying which lines to change. After step 4 the old key no longer opens the vault; the way back is restoring step 1's backup.

### Emergency access

There is no master token. If a project's token is lost or leaked, or you need to read or change anything, use the CLI on the server (`docker exec -it cove /cove shell`): it works on the database directly and needs no token. `token rotate <project>` gives a project a new token.

### Recovering a secret

`history KEY` shows versions; `restore KEY` brings back the previous value or a deleted secret; `restore KEY 3` a specific version. Restoring saves a new version, so nothing is lost.

### Network

Every client uses `http://cove:2100` on `spark`. For something on the host outside Docker, publish to `127.0.0.1:2100` only; from elsewhere, an SSH tunnel. Never publish on all interfaces.

### Entering values

The prompt isn't logged anywhere and its history is in memory only. A one-shot `docker exec cove /cove create KEY value` lands in the server's bash history, so enter values in `cove shell`, or use `generate`.

---

## 15. Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `cove: can't read /app/vault/.env: permission denied` | The host folder isn't owned by Cove's user: `sudo chown -R 10001:10001 /srv/server/storage/cove`. |
| `cove: no .env file found` | `APP_ENV_PATH` points nowhere, or no `.env` in the working directory. |
| `the database schema is at version N, but this version of Cove needs M` | Set `COVE_MIGRATE_DATABASE_URL`, or run `cove migrate up`. |
| `VAULT_ENCRYPTION_KEY isn't the key this vault is encrypted with` | The `.env` has the wrong key. Restore it from the backup. |
| `the vault was re-encrypted with VAULT_NEW_ENCRYPTION_KEY` | Finish step 5 of the rotation. |
| A project gets `401 invalid_token` | Wrong or revoked token; `token list` shows tokens and last use. |
| A project gets `403 forbidden_key` | Its token doesn't cover the key: `token allow KEY <project>` (no restart needed). For a batch, `docker logs cove` names the key. |
| A project gets `404 not_found` | The key doesn't exist, often a rename; `search` for it. |
| `429 too_many_requests` | Something sent 10 wrong tokens in a minute. Fix it; the block lifts within 5 minutes. |
| `500 wrong_key` | The vault key was rotated while Cove ran: finish the rotation and recreate the container. |
| `503 not_ready` / unhealthy container | Cove can't reach `sparkdb`. |
| Lighthouse gets `bootstrap_locked` / `bootstrap_expired` | Run `bootstrap open lighthouse` and start it within 10 minutes. |
| `status` says `Vault key: WRONG` | See the vault-key rows above. |

---

## 16. Development

```bash
go vet ./... && go test ./...
```

- Unit tests use in-memory stores (`vaulttest`, `tokenstest`) and need no database.
- **Integration tests** run against a real, disposable Postgres (never a real vault) when both are set:
  ```bash
  COVE_TEST_MIGRATE_URL=postgres://cove_migrator:...@localhost:5499/cove_db
  COVE_TEST_DATABASE_URL=postgres://cove_app:...@localhost:5499/cove_db
  ```
  Create the roles and database as in [§5](#roles) first.
- **CI** (`.github/workflows/ci.yml`) on every push and pull request: gofmt, `go vet`, `go test -race` with a Postgres 16 service (integration tests included), and a build.
- The `/v0` API contract (paths, status codes, JSON fields) is pinned by `internal/server` tests; CoveClient depends on it.
- **The Go version is pinned in two places, kept equal:** `go 1.27.1` in `go.mod` (the minimum for any build; CI uses it too) and `golang:1.27.1-alpine` in the `Dockerfile` (what prod runs). When Go releases a patch (e.g. 1.27.2, with security fixes), bump both in one commit.
- Keep dependencies patched: `govulncheck ./...` (via `go run golang.org/x/vuln/cmd/govulncheck@latest`) should report nothing.

### Releasing

1. Work lands on a branch, then `release/<version>`, and prod is updated only from `main`.
2. Update `CHANGELOG.md` and set `COVE_VERSION` in `docker-compose.yml` to the new version; merge to `main`; tag `vX.Y.Z` in Cove (and CoveClient if it changed); push the tags.
3. Pushing `main` deploys it (Lighthouse), or on the server: `git pull && docker compose up -d --build`.

Versions follow [semantic versioning](https://semver.org). The HTTP API stays `/v0` as long as it's backwards compatible.

---

## 17. Known limitations

1. **Token and key management** happens in the CLI (directly on the database), not through the API.
2. **Values with spaces** can't be entered in the CLI; use the API.
3. **`history <key>`** shows events under that name only; after a rename, `history <old name>` shows the earlier ones (restore follows renames automatically).
4. **The audit log is mandatory:** if an event can't be written (e.g. a lost grant), reads and writes fail until it's fixed; the error names the cause.
5. **Rate-limit counts** live in memory and reset when Cove restarts.
6. **Anyone with root or Docker access on the server** can read the vault key from the `.env` and use the CLI ([§7](#7-security-model)).
