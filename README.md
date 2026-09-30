# Cove

**Cove** is a lightweight, self-hosted secret management server written in Go. It exposes a RESTful API for storing, retrieving, updating, and deleting secrets, backed by a PostgreSQL database with AES-256-GCM encryption at rest. It is designed for personal and home-lab projects that need a simple, private alternative to cloud-based secret managers.

Secrets are encrypted before being written to the database. Keys are never exposed through the list endpoint — only metadata (version, timestamps, pull count) is returned. Every read, write, update, and delete is written to an append-only event log.

> Full documentation (internals, deployment, error reference, known issues): [DOCUMENTATION.md](DOCUMENTATION.md)

---

## Features

- AES-256-GCM encryption at rest (keys derived via SHA-256)
- PostgreSQL-backed storage with a full event log
- Bearer token authentication on all secret endpoints
- Per-project tokens, each limited to the secrets it needs (read-only or read/write)
- One-time bootstrap endpoint for automated client setup
- Interactive CLI for direct vault management
- Docker-ready with a minimal Alpine image

---

## Architecture

```
cmd/cove/main.go          Entry point — reads config, wires packages together, starts server and CLI
internal/config/          Every setting, read from the environment once; secret auto-generation
internal/vault/           The rules for secrets: validation, encryption, and the event log
internal/database/        PostgreSQL connection pool, SQL queries, and migrations
internal/encryption/      AES-256-GCM cipher and random secret generation
internal/bootstrap/       The bootstrap gate: time-limited, one-time token handout for new clients
internal/server/          HTTP API: routing, middleware, and handlers
internal/cli/             Interactive CLI for managing secrets directly
```

Cove runs the HTTP server (default port `2110` for dev, `2100` for prod), and has a CLI you can open alongside it (`cove shell`) or use for single commands (`cove list`). The CLI connects to the same database as the API, so changes made via CLI are immediately visible through the API and vice versa.

---

## Prerequisites

- Go 1.25+
- PostgreSQL instance (local or remote)
- Docker & Docker Compose (for containerized deployment)

---

## Database Setup

Cove manages its own schema with [goose](https://github.com/pressly/goose) migrations (`internal/database/migrations/`). You only need to create the roles and the database once, as a Postgres superuser:

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

Then set `COVE_MIGRATE_DATABASE_URL` (as `cove_migrator`) and start Cove. It creates the schema and applies every pending migration before serving. `cove migrate status` shows what's applied. See [DOCUMENTATION.md §4](DOCUMENTATION.md#4-database) for details.

---

## Local Setup (Development)

1. Clone the repository:
   ```bash
   git clone https://github.com/LSariol/Cove.git
   cd Cove
   ```

2. Copy the example environment file:
   ```bash
   cp .env.example .env
   ```

3. Edit `.env` and fill in your database URL and optionally your secrets. If `COVE_CLIENT_SECRET` or `VAULT_ENCRYPTION_KEY` are left empty, Cove will generate and persist them automatically on first start.

   ```env
   COVE_DATABASE_URL=postgres://cove_app:password@localhost:5432/cove_db
   COVE_MIGRATE_DATABASE_URL=postgres://cove_migrator:password@localhost:5432/cove_db

   # Leave empty to auto-generate on first start, or provide your own values:
   COVE_CLIENT_SECRET=
   VAULT_ENCRYPTION_KEY=

   APP_ENV=DEV
   APP_PORT=2110
   APP_ENV_PATH=.env
   APP_MARKER_PATH=./markers
   ```

4. Build and run:
   ```bash
   go build -o cove ./cmd/cove
   ./cove
   ```

   The server starts on the port defined by `APP_PORT`, with the CLI prompt (`cove (dev)>`) in the same terminal. Use `./cove serve` for the server alone, and `./cove shell` in another terminal for the CLI.

---

## Environment Variables

| Variable             | Required | Description |
|----------------------|----------|-------------|
| `COVE_DATABASE_URL`  | Yes      | PostgreSQL connection string for the runtime role (`postgres://cove_app:pass@host:port/cove_db`) |
| `COVE_MIGRATE_DATABASE_URL` | No | Connection string for the migrator role. When set, pending migrations are applied on startup. When unset, the database must already be migrated. |
| `COVE_CLIENT_SECRET` | No       | Bearer token clients must send. Auto-generated and persisted if empty. |
| `VAULT_ENCRYPTION_KEY` | No     | Key used to derive the AES-256 encryption key. Auto-generated and persisted if empty. |
| `APP_ENV`            | No       | Runtime environment label (`DEV` or `PROD`) |
| `APP_PORT`           | Yes      | Port the HTTP server listens on |
| `APP_ENV_PATH`       | No       | The `.env` file to load first, and where auto-generated secrets are saved. Defaults to the `.env` file that was loaded. |
| `APP_MARKER_PATH`    | No       | Directory for the bootstrap state file. Defaults to `/app/vault/markers`. |
| `COVE_BOOTSTRAP_ALLOWED_CIDRS` | No | Networks/addresses allowed to bootstrap, e.g. `172.18.0.0/16`. Empty allows any. |

> **Important:** Back up `VAULT_ENCRYPTION_KEY` and treat it like a master password: without it, the stored secrets can't be decrypted. Don't just edit it (Cove will refuse to start with a key the vault wasn't encrypted with): change it with `cove rotate-key`, which re-encrypts everything. See [DOCUMENTATION.md §11](DOCUMENTATION.md#rotating-the-vault-key).

---

## Docker Deployment

The included `docker-compose.yml` mounts external volumes for the `.env` file and a markers directory, so secrets and state survive container restarts.

1. Create the host directories and your `.env` file:
   ```bash
   mkdir -p /srv/server/storage/cove/markers
   cp .env.example /srv/server/storage/cove/.env
   # Edit /srv/server/storage/cove/.env with your values
   chmod 600 /srv/server/storage/cove/.env
   chown -R 10001:10001 /srv/server/storage/cove
   ```

   Cove runs as user `10001` inside the container, not root. The `chown` lets it read `.env` and write `markers/`; no account is needed on the host.

2. Ensure the `spark` Docker network exists (or update `docker-compose.yml` to match your network):
   ```bash
   docker network create spark
   ```

3. Start the service:
   ```bash
   COVE_VERSION=$(git describe --tags --always) docker compose up -d
   ```

   Cove isn't published on any host port: other containers on the `spark` network reach it at `http://cove:2100`, and nothing on your LAN can connect. It restarts automatically unless stopped. A health check polls `/v0/ready` (which checks the database) every 10 seconds.

4. To use the CLI inside the running container:
   ```bash
   docker exec -it cove /cove shell     # interactive, with Tab completion
   docker exec cove /cove status        # or one command at a time
   ```

---

## API Reference

All endpoints are prefixed with `/v0/`.

All responses use a uniform JSON envelope:
```json
{ "success": true, "data": { ... } }
{ "success": false, "error": { "type": "error_code", "message": "human readable message" } }
```

All endpoints except `/v0/health`, `/v0/ready` and `/v0/bootstrap/lighthouse` require a `Bearer` token:

```
Authorization: Bearer <token>
```

The token is either the master token (`COVE_CLIENT_SECRET`), which can reach every secret, or a **project token** created with `token create` in the CLI, which can only reach the keys it was given:

```
cove> token create marquee --allow 'MARQUEE_*' --allow SHARED_DISCORD_WEBHOOK_URL
```

A project token asking for a key outside its access gets `403 forbidden_key`, and `GET /v0/secrets` lists only its keys. See [DOCUMENTATION.md §6](DOCUMENTATION.md#project-tokens).

With the master token, secret-by-ID endpoints (`/v0/secrets/{key}`) also require `X-Cove-Source`, which identifies the calling application and is written to the event log. With a project token, the token's name is recorded instead.

```
X-Cove-Source: my-app
```

---

### Health Check

**`GET /v0/health`** — No authentication required.

```json
{
  "success": true,
  "data": { "healthy": true, "time": "2026-05-15T12:00:00Z" }
}
```

---

### Verify Authentication

**`GET /v0/auth`** — Requires authentication.

```json
{
  "success": true,
  "data": { "authenticated": true, "time": "2026-05-15T12:00:00Z" }
}
```

---

### List All Secrets (metadata only)

**`GET /v0/secrets`** — Requires authentication.

Returns public metadata for all secrets. Secret values are never included.

```json
{
  "success": true,
  "data": {
    "secrets": [
      {
        "key": "my-api-key",
        "version": 3,
        "times_pulled": 12,
        "created_at": "2026-01-01T00:00:00Z",
        "updated_at": "2026-04-10T08:30:00Z"
      }
    ]
  }
}
```

---

### Get a Secret

**`GET /v0/secrets/{key}`** — Requires authentication and `X-Cove-Source`.

Returns the decrypted secret value. Increments `times_pulled` and writes a `read` event to the log.

```json
{
  "success": true,
  "data": { "key": "my-api-key", "value": "abc123", "version": 3 }
}
```

---

### Create a Secret

**`POST /v0/secrets/{key}`** — Requires authentication and `X-Cove-Source`.

Request body:
```json
{ "value": "my-secret-value" }
```

Response (`201 Created`):
```json
{
  "success": true,
  "data": { "key": "my-api-key", "action": "created", "message": "my-api-key has been created." }
}
```

Key validation: max 256 characters, only `[A-Za-z0-9\-_.]` allowed.

---

### Update a Secret

**`PATCH /v0/secrets/{key}`** — Requires authentication and `X-Cove-Source`.

Increments the version number and updates `last_modified`. The old encrypted value is preserved in the event log.

Request body:
```json
{ "value": "new-secret-value" }
```

Response (`200 OK`):
```json
{
  "success": true,
  "data": { "key": "my-api-key", "action": "updated", "message": "my-api-key has been updated." }
}
```

---

### Delete a Secret

**`DELETE /v0/secrets/{key}`** — Requires authentication and `X-Cove-Source`.

Response (`200 OK`):
```json
{
  "success": true,
  "data": { "key": "my-api-key", "action": "deleted", "message": "my-api-key has been deleted." }
}
```

---

### Read Several Secrets

**`POST /v0/batch`** with `{ "keys": ["a", "b", "c"] }` (1–100 keys) returns all of them in one request: `{ "secrets": [ {"key", "value", "version"}, ... ] }`. All or nothing: a key the token can't read fails the whole request with `403 forbidden_key` (without naming it; the server log does), and missing keys fail it with `404 not_found`, listing every missing key in `error.keys`.

---

### Bootstrap

**`GET /v0/bootstrap/lighthouse`** — No authentication required.

Returns a token in plaintext, but only while opened in the CLI (10 minutes by default): `bootstrap open <project>` hands out a new token for that project, plain `bootstrap open` the master token (`COVE_CLIENT_SECRET`). It closes after one handout; the same address can fetch it again within 2 minutes, in case it crashed before saving it. Otherwise it answers `403` with `bootstrap_locked`, `bootstrap_expired`, or `bootstrap_forbidden` (address not in `COVE_BOOTSTRAP_ALLOWED_CIDRS`). Every attempt is recorded; `bootstrap status` shows them.

This is intended for automated clients that need their bearer token on first boot without any pre-shared credentials. With [CoveClient](https://github.com/LSariol/CoveClient), use `LoadOrBootstrap(path)`: it saves the token and reuses it on later starts.

```json
{
  "success": true,
  "data": { "secret": "<token>" }
}
```

---

## CLI Reference

Open the prompt with `cove shell` (in Docker: `docker exec -it cove /cove shell`), or run one command with `cove <command>`. See [DOCUMENTATION.md §8](DOCUMENTATION.md#8-cli) for details.

| Command | Usage | Description |
|---------|-------|-------------|
| `get` | `get <key>` | Print a secret's value |
| `create` | `create <key> <value>` | Create a secret |
| `update` | `update <key> <value>` | Change a secret's value (new version) |
| `generate` | `generate <key> [length] [--yes]` | Create or replace a secret with a random value |
| `delete` | `delete <key> [--yes]` | Delete a secret (asks unless `--yes`) |
| `rename` | `rename <key> <new-key> [--yes]` | Rename a secret, keeping its value and history (and updating tokens that list it) |
| `restore` | `restore <key> [version] [--yes]` | Bring back an earlier value or a deleted secret |
| `list` | `list [prefix]` | List secrets (never values) |
| `search` | `search <text>` | List secrets whose keys contain `<text>` |
| `info` | `info <key>` | A secret's details and last read |
| `history` | `history <key> [count]` | A secret's recent events |
| `status` | `status` | Health overview |
| `bootstrap` | `bootstrap [open [project] [duration]\|lock\|status]` | Open the bootstrap endpoint for 10 minutes, close it, or show its state |
| `token` | `token [list\|create\|show\|allow\|deny\|rotate\|revoke] ...` | Per-project tokens: create one, see what it reaches, change its access, rotate or revoke it |
| `help` | `help [command\|setup\|patterns]` | Overview, one command with examples, or a guide |
| `exit` | `exit` | Leave the shell |

---

## Connecting a project

The standard: **Lighthouse injects each project's secrets when it deploys it.** The project's compose file refers to each by its Cove name (`DATABASE_URL=${MARQUEE_DATABASE_URL}`), Lighthouse fetches every `${...}` from Cove and passes it to `docker compose`, and the project reads plain environment variables; it has no Cove code or token. Only a project that changes secrets itself (e.g. botsuite) gets its own token (`COVE_URL=http://cove:2100`, `COVE_TOKEN=${BOTSUITE_COVE_TOKEN}`) and uses CoveClient. Lighthouse has a read-only token over everything; no project gets the master token. Keys are named **`PROJECT_PLATFORM_TYPE`**, e.g. `BOTSUITE_TWITCH_CLIENT_ID`, `SHARED_TMDB_API_KEY`, `MARQUEE_DATABASE_URL` (capitals, digits and `_` only, so they work as Compose variables). Details: [DOCUMENTATION.md §6](DOCUMENTATION.md#connecting-a-project-the-standard). In the CLI: `help setup`.

## CoveClient

[CoveClient](https://github.com/LSariol/CoveClient) is an official Go module that wraps the Cove HTTP API. It handles authentication, the `X-Cove-Source` header, and response envelope decoding, so consuming applications do not need to implement raw HTTP logic.

```bash
go get github.com/lsariol/coveclient
```

---

## Security Notes

- `COVE_CLIENT_SECRET` and `VAULT_ENCRYPTION_KEY` are auto-generated on first start if not provided, and written back to the `.env` file. Store these values securely — losing `VAULT_ENCRYPTION_KEY` means losing access to all stored secrets.
- Give each project its own token (`token create`) instead of sharing `COVE_CLIENT_SECRET`: a leak then exposes only that project's secrets, and revoking it doesn't affect anyone else. Only a hash of each token is stored.
- The bootstrap endpoint is closed unless you open it with `bootstrap open`, and closes again after one handout or 10 minutes. Set `COVE_BOOTSTRAP_ALLOWED_CIDRS` to limit it to your Docker network.
- Cove is intended for internal/private networks. It does not implement TLS termination — place it behind a reverse proxy (e.g., Nginx, Caddy) if exposed beyond localhost.
- All create, read, update, and delete operations are written to `cove.event_log`, which retains the encrypted old and new values for audit purposes.