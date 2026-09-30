# Cove: Review and Improvement Plan

A review of Cove v0.2.0: bugs, security concerns, and quality-of-life improvements, plus dedicated sections on **Lighthouse onboarding** and the **CLI**.

CoveClient has its own `IMPROVEMENTS.md`. Items that affect both repos are cross-referenced.

> **Progress (v1.0.0, `release/1.0.0`):** most items are done; they're marked **Done** in the tables below, and the details sections are kept as the record of why. Still open: SEC-11 (rate limiting), SEC-12 and QOL-9 (encryption key rotation), and hidden value entry from QOL-2. Unfamiliar terms are explained in the [Glossary](#10-glossary).

---

## Contents

1. [How to read this](#1-how-to-read-this)
2. [Backwards-compatibility rules](#2-backwards-compatibility-rules)
3. [Summary tables](#3-summary-tables)
4. [Focus: Lighthouse onboarding](#4-focus-lighthouse-onboarding)
5. [Focus: the CLI](#5-focus-the-cli)
6. [Details: security concerns](#6-details-security-concerns)
7. [Details: bugs](#7-details-bugs)
8. [Details: quality-of-life improvements](#8-details-quality-of-life-improvements)
9. [Suggested order of work](#9-suggested-order-of-work)
10. [Glossary](#10-glossary)

---

## 1. How to read this

Each item has an ID (`SEC-n`, `BUG-n`, `QOL-n`). Within each table, items are sorted by criticality, then by how much they improve things.

| Column | Scale |
|---|---|
| **Criticality** | **Critical**: data loss or compromise likely. **High**: real risk or breakage under normal use. **Medium**: breaks in specific situations, or a meaningful weakness. **Low**: minor, cosmetic, or theoretical. |
| **Effort** | **S**: under 2 hours. **M**: half a day to a day. **L**: 2–3 days. **XL**: a week or more. |
| **Improvement** | How much better things get once it's done: **High / Medium / Low**. |
| **Compat** | **Safe**: no effect on existing projects. **Opt-in**: new behavior only when enabled. **Care**: could affect existing setups; read the details first. |

---

## 2. Backwards-compatibility rules

Every project you have depends on Cove, so every proposal here follows these rules:

1. **The `/v0` contract doesn't change.** Existing routes keep their paths, success status codes, and response shapes. CoveClient v0.2.0 only checks *success* statuses (`200`/`201`), so correcting *error* status codes (for example `500` to `409`) doesn't break it.
2. **New features are additive.** They come as new routes, CLI commands, env vars, or client methods, never as changes to existing ones.
3. **The current master token (`COVE_CLIENT_SECRET`) keeps working** even after per-client tokens exist.
4. **New defaults that change behavior only apply to fresh installs, or are opt-in.** A running install keeps its current behavior.
5. **Stored data stays readable.** Any change to the ciphertext format must still decrypt existing rows.
6. **CoveClient changes are source-compatible.** Projects on v0.2.0 keep working with no edits, and upgrading is optional.

With these rules, no project should need an immediate update.

---

## 3. Summary tables

### Security concerns

| ID | Issue | Criticality | Effort | Improvement | Compat |
|---|---|---|---|---|---|
| [SEC-1](#sec-1-secret-values-end-up-in-docker-logs) | Secret values end up in Docker logs (CLI via TTY) — **Done** | **High** | M | High | Safe |
| [SEC-2](#sec-2-bootstrap-endpoint-hands-out-the-master-token-with-few-safeguards) | Bootstrap hands out the master token with few safeguards — **Done** | **High** | M | High | Safe/Opt-in |
| [SEC-3](#sec-3-api-published-on-every-host-interface-over-plain-http) | API published on every host interface over plain HTTP — **Done** | **High**\* | S | High | Care |
| [SEC-4](#sec-4-one-shared-master-token-for-every-app) | One shared master token for every app — **Done** | Medium | XL | High | Opt-in |
| [SEC-5](#sec-5-example-placeholder-becomes-a-real-vault-key) | Example placeholder `Kept Empty` becomes a real vault key — **Done** | Medium | S | Medium | Safe |
| [SEC-6](#sec-6-event-log-keeps-every-value-forever) | Event log keeps every value forever (including deleted ones and a copy per read) — **Done** | Medium | M | Medium | Safe |
| [SEC-7](#sec-7-no-http-server-timeouts) | No HTTP server timeouts — **Done** | Low | S | Medium | Safe |
| [SEC-8](#sec-8-audit-source-is-self-reported) | `X-Cove-Source` is self-reported and can be spoofed — **Done** | Low | (SEC-4) | Low | Safe |
| [SEC-9](#sec-9-container-hardening) | Container runs as root, base image unpinned — **Done** | Low | S | Low | Safe |
| [SEC-10](#sec-10-env-may-be-created-world-readable) | `.env` may be created world-readable — **Done** | Low | S | Low | Safe |
| [SEC-11](#sec-11-no-rate-limiting) | No rate limiting on auth or bootstrap | Low | S | Low | Safe |
| [SEC-12](#sec-12-ciphertext-isnt-bound-to-its-key-and-has-no-format-version) | Ciphertext isn't bound to its key and has no format version | Low | M | Low | Care |

\* Depends on your network. If the host firewall already blocks port 2100 from outside, this is Medium.

### Bugs

| ID | Issue | Criticality | Effort | Improvement | Compat |
|---|---|---|---|---|---|
| [BUG-1](#bug-1-generated-secrets-arent-loaded-on-first-run) | Generated secrets aren't loaded on first run, so values are encrypted with an empty key — **Done** | **High** | S | High | Safe |
| [BUG-2](#bug-2-ctrlc-and-shutdown-handling-are-broken) | Ctrl+C breaks the CLI silently, and `docker stop` hangs for 10s — **Done** | **High** | M | High | Safe |
| [BUG-3](#bug-3-health-check-ignores-the-database) | Health check ignores the database — **Done** | Medium | S | Medium | Safe |
| [BUG-4](#bug-4-crash-loop-when-a-secret-is-missing-from-the-read-only-env) | Crash loop when a secret is missing from the read-only `.env` — **Done** | Medium | S | Medium | Safe |
| [BUG-5](#bug-5-cli-skips-key-validation) | CLI skips key validation, so it can create keys the API can't reach — **Done** | Medium | S | Medium | Safe |
| [BUG-6](#bug-6-misleading-status-codes) | Misleading status codes (duplicate gives 500, missing gives 500, decrypt failure gives 404) — **Done** | Medium | S | Medium | Safe |
| [BUG-7](#bug-7-env-var-mismatches) | Env var mismatches (`APP_MARKER_DIR`/`PATH`, `APP_ENV_PATH` ignored on load) — **Done** | Low | S | Medium | Safe |
| [BUG-8](#bug-8-writes-and-audit-log-arent-atomic) | Writes and audit log aren't atomic, and log errors are ignored — **Done** | Low | M | Medium | Safe |
| [BUG-9](#bug-9-decrypt-can-panic-on-bad-data) | `Decrypt` can panic on bad data — **Done** | Low | S | Low | Safe |
| [BUG-10](#bug-10-bootstrap-errors-are-misreported) | Bootstrap errors are misreported (403 for any failure, raw error on `lock`) — **Done** | Low | S | Low | Safe |
| [BUG-11](#bug-11-read-counted-even-when-decrypt-fails) | Read counted and logged even when decrypt fails — **Done** | Low | S | Low | Safe |
| [BUG-12](#bug-12-delete-confirmation-uses-a-second-stdin-reader) | Delete confirmation uses a second stdin reader — **Done** | Low | S | Low | Safe |
| [BUG-13](#bug-13-hard-exits-skip-cleanup) | Hard exits (`os.Exit`, `log.Fatal`) skip cleanup — **Done** | Low | S | Low | Safe |
| [BUG-14](#bug-14-list-endpoint-returns-null-and-over-fetches) | List endpoint returns `null` when empty and fetches values it discards — **Done** | Low | S | Low | Safe |
| [BUG-15](#bug-15-unused-vaultjson-mount-can-create-a-directory) | Unused `vault.json` mount becomes a directory if the host file is missing — **Done** | Low | S | Low | Safe |

### Quality-of-life improvements

| ID | Improvement | Criticality | Effort | Improvement | Compat |
|---|---|---|---|---|---|
| [QOL-1](#qol-1-one-shot-cli-through-docker-exec) | One-shot CLI through `docker exec` (`cove get X`, `cove shell`) — **Done** | Medium | M | **High** | Safe |
| [QOL-2](#qol-2-safer-value-entry-hidden-input-spaces-generate) | Safer value entry: hidden input, spaces, `generate` — **Partly done** (`generate`; hidden input not added) | Medium | M | **High** | Safe |
| [QOL-3](#qol-3-bootstrap-v2-time-boxed-logged-restricted) | Bootstrap v2: time-boxed, logged, restricted (see §4) — **Done** | Medium | M | **High** | Safe/Opt-in |
| [QOL-4](#qol-4-tests-and-ci) | Tests and CI — **Done** | Medium | L | **High** | Safe |
| [QOL-5](#qol-5-automatic-schema-setup) | Automatic schema setup on startup — **Done** | Low | S | Medium | Safe |
| [QOL-6](#qol-6-cli-history-and-status-commands) | CLI `history` and `status` commands — **Done** | Low | M | Medium | Safe |
| [QOL-7](#qol-7-structured-request-logging) | Structured request/audit logging (no values) — **Done** | Low | S | Medium | Safe |
| [QOL-8](#qol-8-batch--prefix-fetch) | Batch or prefix fetch (load all of a project's secrets in one call) — **Done** (batch by key list; no prefix fetch) | Low | M | Medium | Safe |
| [QOL-9](#qol-9-vault-key-rotation) | Vault key rotation command | Low | L | Medium | Care |
| [QOL-10](#qol-10-event-log-retention) | Event log retention/pruning — **Done** | Low | M | Medium | Safe |
| [QOL-11](#qol-11-version-reporting) | Version reporting (`/v0/version`, banner, `version` command) — **Done** | Low | S | Low | Safe |
| [QOL-12](#qol-12-config-cleanup) | Config cleanup (`.env.example`, unused vars, one marker var) — **Done** | Low | S | Low | Safe |
| [QOL-13](#qol-13-line-editing-history-and-tab-completion) | Line editing, history, and tab completion in the interactive CLI — **Done** | Low | M | Low | Safe |
| [QOL-14](#qol-14-colour-handling) | Respect `NO_COLOR` / non-TTY output — **Done** | Low | S | Low | Safe |

---

## 4. Focus: Lighthouse onboarding

### What "bootstrap" is for

Lighthouse needs Cove's password (the token `COVE_CLIENT_SECRET`) before it can read any secrets. Bootstrap is how it gets that password **the very first time**, before it has any credentials at all.

### How it works today

1. On a brand-new install, a special endpoint `/v0/bootstrap/lighthouse` is **open**. Anyone can call it without logging in.
2. Lighthouse calls it through `coveclient.Bootstrap()`.
3. Cove creates a small file called `markers/bootstrap_completed` and replies with the token.
4. While that file exists, the endpoint refuses everyone with `403`. To reopen it, you have to `docker attach cove` and type `bootstrap clear`.

The only protection is "the first caller wins". Cove doesn't check *who* is calling.

### Why it's fragile

| # | What can go wrong | What happens |
|---|---|---|
| F1 | Lighthouse gets the token, then crashes before saving it | The endpoint is already locked, so Lighthouse can't ask again. You have to attach to Cove and reopen it by hand. |
| F2 | Something else calls the endpoint first (another container, or any device on your network, see SEC-3) | *That* caller gets the password to every secret. Lighthouse gets `403`. Nothing records who it was. |
| F3 | You reopen it with `bootstrap clear` and forget about it | It stays open forever. There's no timer. |
| F4 | Cove's first run with auto-generated secrets (BUG-1) | Cove doesn't actually have the token in memory yet, so bootstrap fails with `500`. |
| F5 | The marker folder can't be written (permissions, disk) | Cove answers `403 bootstrap_locked`, which looks like "already done" even though nothing was handed out (BUG-10). |
| F6 | Cove is running but its database isn't | Lighthouse's "is Cove up?" check passes (BUG-3), then every real request fails. |
| F7 | Reopening needs `docker attach` | Pressing Ctrl+C there breaks Cove's CLI (BUG-2), and typing `exit` shuts down the vault. |
| F8 | The token it hands out is the *master* token | Changing it later means updating every project at once (SEC-4). |

### Recommended path

**Step 0 (optional, no code): hand Lighthouse the token yourself**

This is a *one-time* manual step, just as bootstrap is a one-time step today. Only the way the token travels changes:

- **Today:** Lighthouse starts, calls the bootstrap endpoint over the network, and receives the token.
- **Step 0:** You copy the `COVE_CLIENT_SECRET=...` line from `/srv/server/storage/cove/.env` into **Lighthouse's own** `.env`, once. Lighthouse reads it on every start, like any other setting. Then run `bootstrap lock` in Cove so the endpoint is closed, because nothing needs it anymore.

After that it's just as hands-off as today, and nothing can grab the token first or leave Lighthouse locked out. **This is a stopgap**, useful only if you want to close the gap before writing any code. If you'd rather keep onboarding fully automatic, skip it and go straight to Steps 1–2.

> Don't take the shortcut of pointing Lighthouse at Cove's `.env` file (for example with `env_file:`). That file also holds `VAULT_ENCRYPTION_KEY`, which Lighthouse should never have.

**Step 1: make the bootstrap endpoint safe (QOL-3)**

Same URL and same response, so CoveClient v0.2.0 and Lighthouse keep working unchanged.

- **Auto-closing window.** A new CLI command, `bootstrap open 10m`, opens the endpoint for 10 minutes and then closes it by itself. The old `bootstrap clear` keeps working and opens it with a default time limit.
- **Closed by default on brand-new installs.** Your current install keeps whatever state it's in.
- **Grace period for crashes (optional).** After a successful handout, the *same machine* (same IP address) can ask again for about 2 minutes. If Lighthouse crashes right after receiving the token (F1), it just asks again on restart.
- **Allowed-network list (optional).** A setting such as `COVE_BOOTSTRAP_ALLOWED_CIDRS=172.18.0.0/16` only answers requests from that address range, e.g. your `spark` Docker network. Everyone else gets `403`.
- **Record every attempt** in the event log: when it happened, the caller's IP, and whether it succeeded.
- **Distinct error messages:** "locked", "window expired", "your IP isn't allowed", and "Cove couldn't write the marker file" are different errors instead of one.
- **`bootstrap status`** shows whether the endpoint is open, how long is left, and when and to whom the last token was handed out.

**Step 2: make Lighthouse's side foolproof (CoveClient CQ-4)**

Add one helper to CoveClient, `LoadOrBootstrap(path)`:

1. If the token file at `path` already exists, read it and use it. No network call.
2. Otherwise, call bootstrap, **save the token to the file right away** (in a way that can't leave a half-written file, and readable only by its owner), check that it works, and return it.
3. If Cove says "locked", return a clear error: *"run `cove bootstrap open` on the Cove host"*.

Lighthouse then calls this once at startup. It's safe to run on every start, and a crash at any point just means "try again next start".

**Step 3 (long term): per-project tokens (SEC-4)**

Instead of handing out the master token, bootstrap would hand out a token that belongs only to Lighthouse. You could cancel or replace it without touching any other project. The master token keeps working for everything already deployed.

---

## 5. Focus: the CLI

> **Decided 2026-09-28 (supersedes the proposals below where they differ):**
> - **Commands:** `get` (logged, no longer counts toward `read_count`), `create`, `update`, `delete [--yes]`, `list [prefix]`, `search <text>` (old `list x fuzzy` still works), `generate <key> [length]`, `rename <old> <new>`, `info <key>`, `history <key> [n]`, `restore <key> [version]`, `status`, `help [command]`, `bootstrap` (open/status come in Phase 3), `exit`.
> - **Principle:** everything a self-hoster needs is reachable from the CLI; nobody should need to open the database. Keep the command set lean.
> - **Output:** follow clig.dev / `gh` conventions: symbols + meaningful color only, no output prefix, color off when not a terminal or `NO_COLOR` is set, data to stdout and messages to stderr, exit codes, consistent messages.
> - **Libraries:** no cobra/urfave. Use `golang.org/x/term` for the interactive shell (history, arrow keys, tab completion of commands and keys), falling back to plain line reading when stdin isn't a terminal.
> - **Hidden value input:** not now.
> - **Prompt shows the environment** from `APP_ENV`: `cove (dev)>`, and `cove (prod)>` in red.
> - **Tab completion** of command names and secret keys, plus history and line editing, via `golang.org/x/term`.

### Background: attach vs. exec

There are two ways to "get into" a running container:

- **`docker attach cove`** connects your keyboard and screen to Cove's **main process**, the same one running the API. Anything you type goes straight into Cove, and Docker saves everything you see to the container's log. Keys like Ctrl+C go to the main process too.
- **`docker exec -it cove <command>`** starts a **separate, new program** inside the container, next to Cove. It has its own input and output, Docker doesn't save it to the log, and Ctrl+C or exiting only affects that new program.

Today the CLI only works with `attach`, which causes most of the problems below.

### What's wrong today

| Problem | Why it matters |
|---|---|
| The CLI is only available through `docker attach` | Ctrl+C breaks the CLI without telling you (BUG-2), and `exit` shuts the whole vault down, not just your session. |
| Everything you type and see is saved in `docker logs` | Secret values you create or `get` are stored in plain text in a log file on the server (SEC-1). |
| Values are typed on the command line | They're visible on screen and can't contain spaces. |
| No key checking | You can create keys the API can't reach (BUG-5). |
| No way to see what's going on | You can't see a secret's history, who read it, or whether the DB and bootstrap are healthy without opening `psql`. |
| Basic input | No up-arrow history, no tab completion, no `help <command>`. |
| `get` counts as an app reading the secret | Checking a value yourself adds 1 to `times_pulled`, which skews your usage numbers. |

### Proposed design (everything is added on; nothing existing is removed)

**1. One program, several ways to run it (QOL-1)**

| Command | What it does |
|---|---|
| `cove` (no arguments) | **Exactly what it does today:** API server plus the typed CLI. Current deploys keep working. |
| `cove serve` | API server only, no typed CLI. This becomes the recommended way to run it in Docker. |
| `cove shell` | Typed CLI only. Connects to the database but doesn't start an API server. |
| `cove <command> [args]` | Runs one command and exits, e.g. `cove list` or `cove get KEY`. |

In Docker you'd use `docker exec -it cove /cove shell` for the interactive CLI, or `docker exec cove /cove list MYAPP_` for one command. Once you're happy with that, change compose to run `cove serve` and remove `tty`/`stdin_open`. Then nothing is written to `docker logs` from the terminal anymore (SEC-1 fixed), and Ctrl+C can't affect the server (BUG-2 fixed).

**2. Command set**

| Command | Change |
|---|---|
| `create <key>` | If you leave out the value, Cove **asks for it with typing hidden** (like a password prompt). Typing the value on the line still works. |
| `update <key>` | Same hidden prompt. Shows the current version and asks you to confirm. |
| `generate <key> [length]` | **New.** Creates (or replaces) the secret with a random value and shows it once. Handy for passwords and API tokens you control. |
| `get <key>` | Stops counting as a "pull" by an app. |
| `delete <key> [--yes]` | Same confirmation as today (with BUG-12 fixed). `--yes` skips it for scripts. |
| `list [term] [fuzzy]` | Adds a count at the bottom, e.g. `12 of 40 secrets`. |
| `history <key> [n]` | **New.** The last *n* events for a secret: when, what (create/read/update/delete), version, and which app. Never shows values. |
| `status` | **New.** Database OK?, number of secrets, bootstrap open or locked, port, version, uptime. |
| `bootstrap open [time] \| lock \| status` | See §4. `clear` still works. |
| `help [command]` | Help for one command. |
| `version` | Shows which version of Cove is running. |

**3. Quotes for spaces:** `create KEY "a value with spaces"` works.

**4. Same key rules as the API:** the CLI uses the API's key check, so it can't create unreachable keys.

**5. Polish (QOL-13/14):** up-arrow history, tab completion for commands and key names, and no color codes when the output goes to a script or file.

---

## 6. Details: security concerns

Every item follows the same layout: **what's happening**, **why it matters**, **the fix**, and **will it break anything?**

### SEC-1: Secret values end up in Docker logs

**What's happening.** `docker-compose.yml` gives Cove a terminal (`tty: true`) so you can `docker attach` to it. Docker saves **everything that appears on that terminal** to a log file on the server (`docker logs cove` shows it). A terminal shows the letters you type, so the log includes your typing as well as Cove's replies. When you run `create API_KEY sk-live-abc123`, both the command and the value are saved. When you run `get API_KEY`, the value Cove prints is saved.

**Why it matters.** Your secrets now sit unencrypted in `/var/lib/docker/containers/<id>/<id>-json.log`, outside the vault. Anyone who can run `docker logs` on the server can read them, and they stay there until the container is removed.

**The fix.** Stop using the attached terminal for secrets. Use the `docker exec` CLI (QOL-1), which isn't saved to the log, with hidden input for values (QOL-2). Then remove `tty: true` from compose.

**Do now.** Run `docker logs cove` and look at what's there. If real secret values appear, consider changing those secrets. Recreating the container (`docker compose up -d --force-recreate cove`) starts a fresh log.

**Will it break anything?** No.

### SEC-2: Bootstrap endpoint hands out the master token with few safeguards

**What's happening.** An endpoint that needs no password hands out the one password that unlocks every secret. The only safeguard is "first caller wins".

**Why it matters.** Anything that can reach Cove before Lighthouse does can take the master token, and you'd never know. It can also be left open indefinitely.

**The fix.** Section 4, Steps 1–2 (auto-closing window, allowed-network list, logging, and the client helper).

**Will it break anything?** No. The URL and response stay the same.

### SEC-3: API published on every host interface over plain HTTP

**What's happening.** `ports: - "2100:2100"` in compose tells Docker: "make Cove reachable on port 2100 on **every network connection this server has**". That includes your LAN, and the internet if your router forwards that port. Two surprises make this worse:

1. **Docker bypasses ufw.** Docker adds its own firewall rules, and they run *before* ufw's. So `ufw deny 2100` often doesn't block it. This is a well-known Docker gotcha.
2. **Traffic isn't encrypted.** It's plain `http://`, so the token and every secret value cross the network as readable text.

**Why it matters.** Any device on your network can try to talk to Cove, and anyone who can watch network traffic between a project and Cove can read the token.

**Something to know.** Containers on the same Docker network (`spark`) **don't need `ports:` at all**. They reach Cove directly at `http://cove:2100`. `ports:` only matters for things *outside* Docker.

**The fix.** First, check how each project connects to Cove (its Cove URL setting):

- If every project uses `http://cove:2100`, delete the `ports:` section entirely.
- If something on the server itself (not in Docker) uses `localhost:2100`, change it to `"127.0.0.1:2100:2100"`, which is reachable only from the server itself.
- If other machines need Cove, put it behind HTTPS (a reverse proxy such as Caddy), or use a private VPN such as Tailscale.

**Will it break anything?** **Possibly.** Any project that connects through the server's LAN IP or hostname would lose access. That's why you check first.

### SEC-4: One shared master token for every app

**What's happening.** Every project uses the same token, and it can read, change, and delete every secret.

**Why it matters.** It's like giving every guest a master key to every room. If one project leaks it (in a log, a repo, or a hacked container), all your secrets are exposed. Changing the token means updating every project at the same moment.

**The fix (done in v1.0.0 as `cove.tokens`; see DOCUMENTATION.md §6 "Project tokens").** Give each project its own token, stored in a new `cove.clients` table. Each one can optionally be limited to certain keys (e.g. only `MYAPP_*`) or to read-only access. You'd create and cancel tokens with CLI commands like `token create myapp`, `token list`, and `token revoke myapp`. Cove would also know *which* project made each request, so the audit log becomes trustworthy (SEC-8).

**Will it break anything?** No. The master token keeps working. You move projects to their own tokens one at a time, whenever you like. This is the biggest job in this document, but also the biggest security gain.

### SEC-5: Example placeholder becomes a real vault key

**What's happening.** `.env.exmaple` contains `VAULT_ENCRYPTION_KEY=Kept Empty`. Cove only generates a key when the value is *blank*. `Kept Empty` isn't blank, so Cove uses it as the key.

**Why it matters.** If you ever copy the example without editing it, every secret is encrypted with the guessable password "Kept Empty".

**The fix.** Leave both values blank in the example file. Have Cove refuse to start if the key is suspiciously short or is a known placeholder.

**Will it break anything?** No (unless your real key really is "Kept Empty", in which case you want to know).

### SEC-6: Event log keeps every value forever

**What's happening.** The event log (`cove.event_log`) keeps an encrypted copy of a secret's value:
- on **every read** (a new copy each time an app fetches it),
- on **update** (old and new value),
- on **delete** (the value you deleted).

**Why it matters.** The copies are encrypted, so they're safe *as long as `VAULT_ENCRYPTION_KEY` stays secret*. If the key ever leaks, someone with a database backup can decrypt every value you've ever stored, including ones you deleted or replaced because you thought they were compromised. Deleting a secret doesn't really delete it. The table also grows with every single read.

**The fix.** Stop saving the value on *read* events. They only need "who read what, when". Add optional clean-up of old log rows (QOL-10), and a `purge <key>` command for when you really want a secret's old values gone.

**Will it break anything?** No.

### SEC-7: No HTTP server timeouts

**What's happening.** Cove's web server waits forever for a client to finish sending a request.

**Why it matters.** Each open connection uses a bit of memory. A misbehaving program, or an attacker, could open thousands of connections that send one byte a minute and tie the server up. This is called a "Slowloris" attack. It's unlikely on a private network, but it's cheap to prevent.

**The fix.** Tell Go's server how long to wait: about 5 seconds for request headers, 15 seconds for the whole request or response, and 60 seconds for idle connections. That's about five lines.

**Will it break anything?** No. Real requests to Cove take milliseconds.

### SEC-8: Audit source is self-reported

**What's happening.** `X-Cove-Source` is just a name the calling project writes into its request. Cove believes whatever it says.

**Why it matters.** Since every project has the same token, any project could claim to be another one, so the event log can't *prove* which project read a secret.

**The fix.** Per-project tokens (SEC-4). The token itself identifies the caller. Until then, treat the source column as a hint.

**Will it break anything?** No.

### SEC-9: Container hardening

This is two separate things.

**Running as root**

- **What's happening.** Inside the container, Cove runs as `root` (the all-powerful admin user).
- **Why it matters.** If someone found a bug that let them run their own code through Cove, they'd be root inside the container. Through your bind mounts, they could then change files in `/srv/server/storage/cove` on the host as root, and escaping the container is easier from root.
- **The fix.** Two lines in the Dockerfile make Cove run as an ordinary user. You **don't** need to create a user account on your server. Inside Docker, a user is just a number:
  ```dockerfile
  RUN adduser -D -u 10001 cove
  USER 10001
  ```
  The catch: that user (number 10001) must be allowed to read your `.env` and write to `markers/`. Run this once on the server:
  ```bash
  chown -R 10001:10001 /srv/server/storage/cove
  ```
  That's all. After that it's hands-off again.

**Unpinned base image**

- **What's happening.** The final build step uses `FROM alpine:latest`. `latest` is a label that moves: each rebuild may download a newer Alpine Linux than last time.
- **Why it matters.** Usually nothing happens, but a rebuild could change behavior or break without any change in your code, and you can't rebuild exactly what's running now.
- **The fix.** Name a version: `FROM alpine:3.22`. You still get security patches for 3.22 automatically, but never a surprise jump to a new major version. (You *could* pin the exact image with `alpine@sha256:...`, but then you'd get no updates unless a bot such as Dependabot bumps it for you. That's more hands-on than it's worth here.) Your Go build step (`golang:1.25.1-alpine`) is already pinned to a version.
- Also remove the unused `RUN mkdir -p /app/cove` line.

**Will it break anything?** The root change needs the one-time `chown` above, or Cove can't read its `.env`. Pinning Alpine is risk-free. This is rated Low, so it's fine to leave it for last.

### SEC-10: `.env` may be created world-readable

**What's happening.** When Cove creates the `.env` file to save generated secrets, the file gets default permissions, usually `644`, which means any user account on the server can read it.

**Why it matters.** That file holds the vault key. Anyone with any login on the server could read it.

**The fix.** Once, on the server: `chmod 600 /srv/server/storage/cove/.env` (only the owner can read or write). In the code, create the file with `600` permissions.

**Will it break anything?** No. If you also do SEC-9, make sure the file is owned by the Cove user (the `chown` there takes care of it).

### SEC-11: No rate limiting

**What's happening.** Something can guess tokens as fast as it likes, and Cove never slows it down.

**Why it matters.** Not much, honestly. The generated token is 32 random characters, far too many to guess. But a limit would make someone scanning your server show up as obvious errors in the logs.

**The fix.** A small per-IP limit (e.g. 10 failed logins a minute) on login failures and the bootstrap endpoint.

**Will it break anything?** No.

### SEC-12: Ciphertext isn't bound to its key and has no format version

**What's happening.** Each encrypted value is like a sealed envelope: Cove can tell if someone tampered with the envelope, but **the envelope doesn't say which secret it belongs to**. And nothing on it says which encryption scheme sealed it.

**Why it matters.**
1. Someone who can write to your database could copy the envelope from `DB_PASSWORD` into the `API_KEY` row, and Cove would hand out the database password as the API key without complaint. (Low risk: someone with database write access can already do plenty of damage.)
2. Without a version label, Cove can't safely change how it encrypts later. For example, it can't tell old-key and new-key values apart during key rotation (QOL-9).

**The fix.** New values get a `v1:` label at the front and are sealed together with their key name. Values without the label are read the old way, so everything already stored keeps working.

**Will it break anything?** Not if the fallback is done right. That's why it's marked Care and needs tests (QOL-4) first.

---

## 7. Details: bugs

### BUG-1: Generated secrets aren't loaded on first run

**What's happening.** When `COVE_CLIENT_SECRET` or `VAULT_ENCRYPTION_KEY` is blank, Cove generates a value and **writes it to the `.env` file**, but it never loads that value into the running program. The file has it, but Cove itself still sees blank values until it restarts.

**Why it matters.** During that first run:
- Every secret you create is encrypted with a **blank key**. After a restart, Cove loads the real key and **can never decrypt those secrets again. They're lost.**
- Every app gets "unauthorized", and bootstrap fails.

**The fix.** One line: after saving the new value to the file, also set it in the running program (`os.Setenv`). As a safety net, refuse to encrypt anything when the key is blank.

**Will it break anything?** No.

### BUG-2: Ctrl+C and shutdown handling are broken

**Background.** Programs receive **signals** from outside: Ctrl+C sends "interrupt" (SIGINT), and `docker stop` sends "please shut down" (SIGTERM). Cove listens for both. When one arrives, it flips a shared "we're shutting down" switch (Go calls this cancelling the **context**). Anything using that context stops immediately.

**What's happening.**
- **Ctrl+C while attached:** the switch flips, but Cove doesn't actually shut down. The CLI keeps running, and it uses that same switch for its database calls. So **every CLI command after that fails with `context canceled`**, even though the prompt looks normal. The API keeps working, because it doesn't use that switch.
- **`docker stop`:** the switch flips, but the main part of Cove is stuck waiting for you to type the next CLI line, so it never reaches its shutdown code. Docker waits 10 seconds, then force-kills it. Any request in progress is cut off, and database connections are never closed properly.

**The fix.** Let the CLI run in the background and make the main program wait for the shutdown switch. When it flips, shut the web server down gently (finish current requests) and close the database. Long term, moving the CLI into `docker exec` (QOL-1) separates the two completely.

**Will it break anything?** No.

### BUG-3: Health check ignores the database

**What's happening.** `/v0/health` always answers "healthy", even when Postgres is down.

**Why it matters.** Docker's healthcheck, and anything that waits for Cove to be healthy, think Cove is ready when it can't actually serve a single secret.

**The fix.** Keep `/v0/health` as it is (CoveClient's `Health()` uses it), and add a new `/v0/ready` that also checks the database and answers `503` when it's down. Point the Docker healthcheck at `/v0/ready`.

**Will it break anything?** No. `/v0/health` doesn't change.

### BUG-4: Crash loop when a secret is missing from the read-only `.env`

**What's happening.** In Docker, `.env` is mounted **read-only**. If either secret is blank, Cove tries to write a generated value into the file, can't, and crashes. Docker restarts it, it crashes again, and so on.

**Why it matters.** You get an endless restart loop with a confusing Go stack trace instead of a clear message.

**The fix.** If the file can't be written, stop with a plain message: *"COVE_CLIENT_SECRET is empty and /app/vault/.env is read-only. Set it in /srv/server/storage/cove/.env."*

**Will it break anything?** No.

### BUG-5: CLI skips key validation

**What's happening.** The API only accepts key names made of letters, numbers, `-`, `_`, and `.` (up to 256 characters). The CLI accepts anything.

**Why it matters.** A key such as `github/token` or `db:url` created in the CLI can **never** be read by your projects. The API rejects the name before looking it up.

**The fix.** Have the CLI use the same key-checking function as the API.

**Will it break anything?** No. (If you already have odd keys, `list` will show them, and you can recreate them with valid names.)

### BUG-6: Misleading status codes

**What's happening.** Some errors return the wrong HTTP code:

| Situation | Now | Should be | Why |
|---|---|---|---|
| Create a key that already exists | `500` (server broke) | `409` (conflict) | It's your mistake, not a server failure. |
| Update a key that doesn't exist | `500` | `404` (not found) | Same reason. |
| Read a key that exists but can't be decrypted | `404` | `500` | The key *does* exist. Something is wrong on the server (e.g. wrong vault key). |
| Read while the database is down | `404` | `500` or `503` | "Not found" sends you looking for a typo instead of a dead database. |

**Why it matters.** Wrong codes send you debugging in the wrong direction.

**The fix.** Return the right code for each case.

**Will it break anything?** No for CoveClient. It only checks for the *success* code, so these are still errors, just with a different number in the message. If any project checks for the text `"Unexpected Status 500"`, look at it first.

### BUG-7: Env var mismatches

**What's happening.** Three small mix-ups:
1. `docker-compose.yml` sets `APP_MARKER_DIR`, but the code reads `APP_MARKER_PATH`. It works only because Cove's built-in default happens to be the same folder.
2. Cove *reads* its settings from `./.env` or `/app/vault/.env`, but *writes* generated secrets to whatever `APP_ENV_PATH` says. If those differ, generated secrets go to a file Cove never reads.
3. If `APP_ENV_PATH` is blank, saving fails and Cove crashes.

**Why it matters.** Change one setting and things break in confusing ways.

**The fix.** Accept both marker names, read from `APP_ENV_PATH` when it's set, and use one shared default.

**Will it break anything?** No.

### BUG-8: Writes and audit log aren't atomic

**Background.** A **transaction** groups several database steps so they either *all* happen or *none* do, and nothing else can sneak in between them.

**What's happening.**
1. Updating a secret takes two separate steps: read the old value, then write the new one. If two updates happen at the same moment, both can read the same "old" value, and the log records the wrong history. This is called a **race condition**.
2. Saving a change and writing its log entry are separate steps too. If the log write fails, Cove ignores the error, so a change can happen with **no record of it**.

**Why it matters.** The audit log isn't fully trustworthy. It's rare with your traffic, but it's the kind of thing you'd only notice when you actually need the log.

**The fix.** Wrap each change and its log entry in one transaction, and at least print a warning if the log write fails.

**Will it break anything?** No.

### BUG-9: `Decrypt` can panic on bad data

**What's happening.** `Decrypt` assumes every stored value is at least 12 bytes long. It never checks, and it ignores errors when decoding the stored text. If a value is corrupted or too short, Go **panics** (crashes).

**Why it matters.** When the API triggers it, Go's web server catches the crash for that one request. When the **CLI** triggers it, nothing catches it and **the whole Cove process dies**, API included, until Docker restarts it.

**The fix.** Check the decode result and the length, and return a normal error.

**Will it break anything?** No.

### BUG-10: Bootstrap errors are misreported

**What's happening.** When Cove can't create the marker file, it assumes "already locked", even if the real reason is a permissions problem or a full disk. The API answers `403 bootstrap_locked`. In the CLI, `bootstrap lock` on an already-locked endpoint prints a raw, scary-looking `file exists` error.

**Why it matters.** You'd think bootstrap already happened when actually it never could.

**The fix.** Tell "already exists" apart from other errors. For `lock` when already locked, just say "already locked".

**Will it break anything?** No.

### BUG-11: Read counted even when decrypt fails

**What's happening.** Cove adds 1 to `times_pulled` and logs a "read" *before* checking that decryption worked.

**Why it matters.** Failed reads look like successful ones in your stats and log. (It also means every read writes to the database, which is fine at your scale.)

**The fix.** Count the read only after decryption succeeds.

**Will it break anything?** No.

### BUG-12: Delete confirmation uses a second stdin reader

**What's happening.** When you run `delete`, Cove creates a **second** keyboard reader to ask "are you sure?". Go's input reader grabs text in chunks, so the first reader may already have taken lines the second one needs.

**Why it matters.** Only if you paste several lines at once: the confirmation can read the wrong line, or lines can disappear.

**The fix.** Reuse the same reader.

**Will it break anything?** No.

### BUG-13: Hard exits skip cleanup

**What's happening.** In three places Cove stops immediately (`os.Exit` / `log.Fatal`) instead of shutting down properly: when the database URL is invalid, when the port is already in use, and when you type `exit`.

**Why it matters.** Database connections aren't closed and requests in progress are cut off. It's minor, but it adds up with BUG-2.

**The fix.** Pass errors back to `main`, and let `main` do one clean shutdown.

**Will it break anything?** No.

### BUG-14: List endpoint returns `null` and over-fetches

**What's happening.** With zero secrets, `GET /v0/secrets` returns `"secrets": null` instead of an empty list `[]`. Separately, the list query loads every *encrypted value* from the database, then throws them away.

**Why it matters.** `null` trips up some JSON consumers (CoveClient handles it fine). Loading values you don't need is wasted work, and it keeps secret data in memory for no reason.

**The fix.** Return `[]`, and don't select the value column.

**Will it break anything?** No.

### BUG-15: Unused `vault.json` mount can create a directory

**What's happening.** Compose still mounts `vault.json`, left over from before Cove used Postgres. If that file doesn't exist on the host, Docker creates a **folder** with that name instead.

**Why it matters.** It's confusing clutter, and a leftover folder there can make later mounts fail.

**The fix.** Remove the mount and the `APP_VAULT_PATH` setting.

**Will it break anything?** No. Cove doesn't use it.

---

## 8. Details: quality-of-life improvements

### QOL-1: One-shot CLI through `docker exec`

**What it is.** Run the CLI as its own program (`cove shell`, `cove list`, `cove get X`) with `docker exec`, instead of attaching to the server. See §5 for the full design.

**Why it's worth it.** It fixes the log leak (SEC-1) and the Ctrl+C problem (BUG-2), means `exit` can't take down the vault, and lets you script things, e.g. `docker exec cove /cove list MYAPP_`.

**Breaks anything?** No. Running `cove` with no arguments behaves exactly as it does today.

### QOL-2: Safer value entry (hidden input, spaces, `generate`)

**What it is.** When you leave the value off `create`/`update`, Cove asks for it with typing hidden. Quotes allow spaces. `generate` creates random values for you.

**Why it's worth it.** Values stop appearing on screen and in logs, and you stop inventing passwords by hand.

**Breaks anything?** No. Typing the value on the line still works.

### QOL-3: Bootstrap v2 (time-boxed, logged, restricted)

**What it is.** §4, Step 1: an auto-closing window, an optional allowed-network list, logging, and clear errors.

**Why it's worth it.** It turns Lighthouse onboarding from "fragile and risky" into "open it, start Lighthouse, done".

**Breaks anything?** No. The URL and response are the same.

### QOL-4: Tests and CI

**What it is.** Cove currently has **no automated tests**. Start with the cheapest, most valuable ones:
1. **Encryption:** encrypt then decrypt gives the original; the wrong key fails; corrupt data returns an error instead of crashing (BUG-9).
2. **Key rules:** a list of valid and invalid key names.
3. **API:** fake HTTP requests (Go's `httptest`) with a fake database, checking login, required headers, size limits, and status codes. These tests **lock in the `/v0` behavior your projects depend on**.
4. **Database code:** against a throwaway Postgres.

Then add CI: a GitHub Action that runs vet, the tests, and a Docker build on every push.

**Why it's worth it.** This is what lets you make all the other changes **without fearing you've broken your projects**.

**Breaks anything?** No.

### QOL-5: Automatic schema setup

**What it is.** Cove creates its schema and tables on startup if they don't exist.

**Why it's worth it.** A new install works without running SQL by hand, and future tables (like SEC-4's `clients`) have a natural home.

**Breaks anything?** No. "Create if not exists" does nothing to your existing tables.

### QOL-6: CLI `history` and `status` commands

**What it is.** `history <key>` shows a secret's recent events. `status` shows overall health (see §5).

**Why it's worth it.** It answers "is this working?" and "which app is using this secret?" without `psql`.

**Breaks anything?** No.

### QOL-7: Structured request logging

**What it is.** Log one line per API request: time, method, key, which app, result code, and how long it took. **Never values or tokens.** Go's built-in `log/slog` does this.

**Why it's worth it.** When a project can't get its secrets, you can see exactly what it asked for and what Cove answered. You'd also spot unexpected callers.

**Breaks anything?** No.

### QOL-8: Batch / prefix fetch

**What it is.** A way to fetch several secrets in one request, e.g. every key starting with `MYAPP_`.

**Why it's worth it.** Projects usually load several secrets at startup, one request each. One request is faster and makes the event log easier to read.

**Breaks anything?** No. It uses a new query option, and `GET /v0/secrets` without it behaves as it does today.

### QOL-9: Vault key rotation

**What it is.** A `cove rotate-key` command that re-encrypts every secret (and log entry) with a new vault key in one go, then tells you the new key to save.

**Why it's worth it.** Right now, **you can't ever change the vault key**. If you suspected it had leaked, your only option would be to recreate every secret by hand.

**Breaks anything?** It needs SEC-12's version label first, so Cove can tell old-key and new-key values apart if rotation stops halfway. Low urgency.

### QOL-10: Event log retention

**What it is.** An optional setting such as `COVE_EVENT_LOG_RETENTION_DAYS=90` that deletes old *read* entries daily, keeping creates, updates, and deletes forever.

**Why it's worth it.** It keeps the log table from growing forever. Pairs with SEC-6.

**Breaks anything?** No. It's off unless you set it.

### QOL-11: Version reporting

**What it is.** Stamp the version into the program when building it. Show it at startup, in `cove version`, and at a new `GET /v0/version` endpoint.

**Why it's worth it.** You can tell at a glance which Cove version is running.

**Breaks anything?** No.

### QOL-12: Config cleanup

**What it is.** Rename `.env.exmaple` to `.env.example`, add `COVE_DATABASE_URL` to it, blank the placeholders (SEC-5), and remove unused settings (`APP_VAULT_PATH`, and `APP_ENV` unless you start using it).

**Why it's worth it.** The example file becomes something you can actually copy and use.

**Breaks anything?** No. `APP_MARKER_DIR` keeps working as an alias (BUG-7).

### QOL-13: Line editing, history, and tab completion

**What it is.** Up-arrow to repeat commands, and Tab to complete command and key names, in `cove shell`.

**Why it's worth it.** It's nicer to use every day. Adds one small library.

**Breaks anything?** No.

### QOL-14: Colour handling

**What it is.** Don't print color codes when output goes to a file or script, or when `NO_COLOR` is set.

**Why it's worth it.** Without this, one-shot commands (QOL-1) show junk like `\033[32m` when used in scripts.

**Breaks anything?** No.

---

## 9. Suggested order of work

**Phase 0: today, no code**
- (Optional) Hand Lighthouse its token directly and run `bootstrap lock` (§4, Step 0).
- Look through `docker logs cove` for secret values (SEC-1). Change any that are exposed.
- Check each project's Cove URL, then decide what to do with `ports:` (SEC-3).
- `chmod 600 /srv/server/storage/cove/.env` (SEC-10). Back up `VAULT_ENCRYPTION_KEY` somewhere that isn't this server.
- If the token in `CoveClient/main/main.go` is real, change it (CoveClient CS-1).

**Phase 1: small, safe fixes (about 1 day)**
BUG-1, BUG-4, BUG-5, BUG-7, BUG-9, BUG-10, BUG-14, BUG-15, SEC-5, SEC-7, SEC-10, BUG-3 (`/v0/ready`), QOL-11, QOL-12. Start the tests (QOL-4) alongside, beginning with encryption and the API.

**Phase 2: CLI rework (2–3 days)**
QOL-1, QOL-2, QOL-6, BUG-2, BUG-12, BUG-13, QOL-14. Then switch compose to `cove serve` plus `docker exec`, and remove the TTY (closes SEC-1).

**Phase 3: Lighthouse onboarding (1–2 days)**
QOL-3 (bootstrap v2) and CoveClient CQ-4 (`LoadOrBootstrap`).

**Phase 4: data hygiene (1–2 days)**
BUG-6, BUG-8, BUG-11, SEC-6, QOL-5, QOL-7, QOL-10.

**Phase 5: bigger features (when needed)**
SEC-4 (per-project tokens), SEC-12 plus QOL-9 (version label and key rotation), QOL-8 (batch fetch), QOL-13, SEC-9, SEC-11.

---

## 10. Glossary

| Term | Plain meaning |
|---|---|
| **Token / bearer token** | A long password a program sends with each request (`Authorization: Bearer <token>`) to prove it's allowed in. |
| **Master token** | Cove's single token, `COVE_CLIENT_SECRET`. Whoever has it can do anything. |
| **Vault key** | `VAULT_ENCRYPTION_KEY`, used to encrypt and decrypt every stored secret. Lose it and everything is unreadable. Leak it and everything is readable. |
| **Ciphertext** | The encrypted, scrambled form of a value, which is what's stored in the database. |
| **Bootstrap** | The one-time handout of the token to a new client that has no credentials yet. |
| **Marker file** | An empty file whose *existence* means "bootstrap is locked". |
| **TTY** | A (virtual) terminal: the thing that shows your typing and a program's output. `tty: true` gives the container one. |
| **`docker attach` / `docker exec`** | Attach connects you to the container's main program. Exec starts a separate new program inside the container. See §5. |
| **Signal (SIGINT / SIGTERM)** | A message the OS sends a program. Ctrl+C sends SIGINT ("interrupt"), and `docker stop` sends SIGTERM ("please shut down"). |
| **Context (Go)** | A shared "stop working" switch that Go code checks. Cancelling it makes everything using it stop. |
| **Bind mount** | A file or folder on the server that's made to appear inside the container (e.g. your `.env`). |
| **Transaction** | A group of database steps that either all happen or none do. |
| **Race condition** | A bug that only happens when two things run at the exact same moment and step on each other. |
| **Panic** | Go's version of a crash: the program stops unless something catches it. |
| **Idempotent** | Safe to repeat: doing it twice has the same result as doing it once. |
| **CIDR** | A way to write a range of IP addresses, e.g. `172.18.0.0/16` = all addresses starting `172.18.`. |
| **Pinning (images)** | Naming an exact version of a Docker image instead of a moving label like `latest`. |
| **Additive / backwards-compatible** | Adds something new without changing what already exists, so current users aren't affected. |
