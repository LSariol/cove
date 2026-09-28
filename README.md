# Cove

**Cove** is a lightweight, self-hosted secret management server written in Go. It exposes a RESTful API for storing, retrieving, updating, and deleting secrets, backed by a PostgreSQL database with AES-256-GCM encryption at rest. It is designed for personal and home-lab projects that need a simple, private alternative to cloud-based secret managers.

Secrets are encrypted before being written to the database. Keys are never exposed through the list endpoint — only metadata (version, timestamps, pull count) is returned. Every read, write, update, and delete is written to an append-only event log.

> Full documentation (internals, deployment, error reference, known issues): [DOCUMENTATION.md](DOCUMENTATION.md)

---

## Features

- AES-256-GCM encryption at rest (keys derived via SHA-256)
- PostgreSQL-backed storage with a full event log
- Bearer token authentication on all secret endpoints
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
internal/bootstrap/       Marker file that locks the bootstrap endpoint
internal/server/          HTTP API: routing, middleware, and handlers
internal/cli/             Interactive CLI for managing secrets directly
```

Cove runs two things concurrently: the HTTP server (default port `2110` for dev, `2100` for prod) and an interactive CLI on stdin. The CLI connects to the same database as the API, so changes made via CLI are immediately visible through the API and vice versa.

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

   The server starts on the port defined by `APP_PORT`. The interactive CLI prompt (`Cove CLI>`) appears immediately in the same terminal.

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
| `APP_ENV_PATH`       | Yes      | Absolute or relative path to the `.env` file (used for auto-generated secret persistence) |
| `APP_MARKER_PATH`    | No       | Directory for bootstrap marker files. Defaults to `/app/vault/markers`. |

> **Important:** If you rotate `VAULT_ENCRYPTION_KEY`, existing secrets in the database cannot be decrypted. Back up your key and treat it like a master password.

---

## Docker Deployment

The included `docker-compose.yml` mounts external volumes for the `.env` file and a markers directory, so secrets and state survive container restarts.

1. Create the host directories and your `.env` file:
   ```bash
   mkdir -p /srv/server/storage/cove/markers
   cp .env.example /srv/server/storage/cove/.env
   # Edit /srv/server/storage/cove/.env with your values
   ```

2. Ensure the `spark` Docker network exists (or update `docker-compose.yml` to match your network):
   ```bash
   docker network create spark
   ```

3. Start the service:
   ```bash
   docker compose up -d
   ```

   The container exposes port `2100` and restarts automatically unless stopped. A health check polls `/v0/health` every 10 seconds.

4. To access the interactive CLI inside the running container:
   ```bash
   docker attach cove
   ```

   Use `Ctrl+P`, `Ctrl+Q` to detach without stopping the container.

---

## API Reference

All endpoints are prefixed with `/v0/`.

All responses use a uniform JSON envelope:
```json
{ "success": true, "data": { ... } }
{ "success": false, "error": { "type": "error_code", "message": "human readable message" } }
```

All endpoints except `/v0/health` and `/v0/bootstrap/lighthouse` require a `Bearer` token:

```
Authorization: Bearer <COVE_CLIENT_SECRET>
```

All secret-by-ID endpoints (`/v0/secrets/{key}`) also require `X-Cove-Source`, which identifies the calling application and is written to the event log:

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

### Bootstrap

**`GET /v0/bootstrap/lighthouse`** — No authentication required.

Returns the `COVE_CLIENT_SECRET` in plaintext. This endpoint is one-use only: it creates a marker file on first call and returns `403 Forbidden` on subsequent calls until the marker is cleared via the CLI.

This is intended for automated clients (e.g., [CoveClient](https://github.com/LSariol/CoveClient)) that need to retrieve their bearer token on first boot without any pre-shared credentials.

```json
{
  "success": true,
  "data": { "secret": "<COVE_CLIENT_SECRET>" }
}
```

---

## CLI Reference

When Cove starts, a `Cove CLI>` prompt is available in the terminal (or via `docker attach`). All commands operate directly on the database.

| Command | Alias | Usage | Description |
|---------|-------|-------|-------------|
| `get` | `g` | `get <key>` | Display the decrypted value of a secret |
| `create` | `c` | `create <key> <value>` | Create a new secret |
| `update` | `u` | `update <key> <value>` | Update an existing secret |
| `delete` | `d` | `delete <key>` | Delete a secret (prompts for confirmation) |
| `list` | `l` | `list` | List all secrets (metadata only) |
| `list` | `l` | `list <term>` | List secrets whose keys start with `<term>` |
| `list` | `l` | `list <term> fuzzy` | List secrets whose keys contain `<term>` |
| `bootstrap` | `b` | `bootstrap clear` | Remove the bootstrap marker (re-enables the bootstrap endpoint) |
| `bootstrap` | `b` | `bootstrap lock` | Create the bootstrap marker (disables the bootstrap endpoint) |
| `help` | `h` | `help` | Show available commands |
| `exit` | `quit` | `exit` | Shut down Cove |

---

## CoveClient

[CoveClient](https://github.com/LSariol/CoveClient) is an official Go module that wraps the Cove HTTP API. It handles authentication, the `X-Cove-Source` header, and response envelope decoding, so consuming applications do not need to implement raw HTTP logic.

```bash
go get github.com/lsariol/coveclient
```

---

## Security Notes

- `COVE_CLIENT_SECRET` and `VAULT_ENCRYPTION_KEY` are auto-generated on first start if not provided, and written back to the `.env` file. Store these values securely — losing `VAULT_ENCRYPTION_KEY` means losing access to all stored secrets.
- The bootstrap endpoint is designed for one-time automated use. After a client has retrieved its bearer token, use `bootstrap lock` (CLI) to disable it.
- Cove is intended for internal/private networks. It does not implement TLS termination — place it behind a reverse proxy (e.g., Nginx, Caddy) if exposed beyond localhost.
- All create, read, update, and delete operations are written to `cove.event_log`, which retains the encrypted old and new values for audit purposes.