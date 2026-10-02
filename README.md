# Cove

**Cove** is a small self-hosted secret vault written in Go. It keeps the API keys, passwords and connection strings that the other projects on the server need, encrypted in Postgres, and hands them out over an internal HTTP API.

- **Encrypted at rest** with AES-256-GCM; Cove refuses to run with the wrong key, and `cove rotate-key` replaces it safely.
- **Per-project tokens**, each limited to the secrets it needs, read-only or read/write, plus a master token for emergencies.
- **Full audit and history:** every read and change is recorded with who did it, and any earlier value (or a deleted secret) can be restored.
- **A CLI** for everyday management, with Tab completion and built-in guides (`help setup`).
- **Hardened by default:** no published port, rate-limited logins, a non-root read-only container, and a bootstrap endpoint that's closed unless you open it.

> **Documentation:** [DOCUMENTATION.md](DOCUMENTATION.md) is the complete reference (configuration, database, security model, API, CLI, deployment, operations, troubleshooting). Changes per release: [CHANGELOG.md](CHANGELOG.md). Client library: [CoveClient](https://github.com/LSariol/CoveClient).

---

## How projects use it

Lighthouse injects each project's secrets when it deploys it. A project's `docker-compose.yml` names what it needs by its Cove key:

```yaml
environment:
  - DATABASE_URL=${MARQUEE_DATABASE_URL}
  - TMDB_API_KEY=${SHARED_TMDB_API_KEY}
```

and the project reads plain environment variables. Only a project that changes secrets itself gets its own token and uses CoveClient. Keys are named **`PROJECT_PLATFORM_TYPE`**, e.g. `BOTSUITE_TWITCH_CLIENT_ID`. Details: [DOCUMENTATION.md §9](DOCUMENTATION.md#9-connecting-a-project-the-standard).

---

## Quick start

### Docker (the server)

```bash
mkdir -p /srv/server/storage/cove/markers
cp .env.example /srv/server/storage/cove/.env     # set the database URLs, COVE_CLIENT_SECRET, VAULT_ENCRYPTION_KEY
chmod 600 /srv/server/storage/cove/.env
sudo chown -R 10001:10001 /srv/server/storage/cove
docker compose up -d --build
docker exec cove /cove status
```

Cove joins the external `spark` network and is reachable only there, at `http://cove:2100`. The database roles it needs are in [DOCUMENTATION.md §5](DOCUMENTATION.md#roles).

### Locally (Go 1.27.1)

```bash
cp .env.example .env      # set COVE_DATABASE_URL and COVE_MIGRATE_DATABASE_URL
go run ./cmd/cove         # server + CLI prompt; secrets are generated on first start
```

---

## CLI at a glance

```bash
docker exec -it cove /cove shell      # the prompt: cove (prod)>
```

```
create MARQUEE_TMDB_API_KEY abc123         add a secret (generate KEY 64 for a random one)
update KEY value / get KEY / delete KEY    change, show, delete (restorable)
list MARQUEE_ / search TWITCH              find secrets (never shows values)
info KEY / history KEY / restore KEY [v]   who can read it, what happened, undo
token create marquee --allow 'MARQUEE_*'   a project token
bootstrap open lighthouse                  let Lighthouse fetch its token once
status / help / help setup                 health, commands, step-by-step guide
```

---

## Development

```bash
go vet ./... && go test ./...
```

Integration tests run against a disposable Postgres when `COVE_TEST_DATABASE_URL` and `COVE_TEST_MIGRATE_URL` are set; CI runs everything on each push. See [DOCUMENTATION.md §16](DOCUMENTATION.md#16-development).
