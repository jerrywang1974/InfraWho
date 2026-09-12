# InfraWho

Self-hosted CMDB and credential vault for small infra teams (Phase 1).

**License:** MIT — see [LICENSE](LICENSE).

## Status

Auth + assets API: config loading, SQLite open + auto-migrate, session cookies (RBAC + step-up), install wizard, asset CRUD (soft-delete / purge), CSP/security headers, optional `/metrics`, `/healthz`, `/readyz` (DB ping + loadable KEK), Docker Compose. Web UI Phase 1 shell (login, setup wizard, asset list/detail) lives under [`web/`](web/). Vault accounts/jobs UI panels come in a later PR.

## Requirements

- Go 1.23+
- Docker + Docker Compose (optional, for containerized lab runs)

## Configuration

| Variable | Default | Notes |
|----------|---------|--------|
| `INFRAWHO_DB_URL` | `sqlite:///data/infrawho.db` | Phase 1 **SQLite only**. Non-`sqlite:` schemes fail at startup. Parent dirs are created; embedded migrations run on open. |
| `INFRAWHO_MASTER_KEY_FILE` | _(empty)_ | Path to KEK file (32 raw bytes **or** base64 of 32 bytes). Preferred over env-embedded keys. |
| `INFRAWHO_LISTEN_ADDR` | `:8080` | HTTP listen address. |
| `INFRAWHO_COOKIE_SECURE` | `true` | Set `false` for plain-HTTP lab (Compose does this). Production behind TLS should keep `true`. |
| `INFRAWHO_TRUSTED_ORIGINS` | _(empty)_ | Optional comma-separated origins (`https://app.example`) or bare hosts. Scheme-bearing entries match that scheme only. Used for CSRF Origin checks behind a different public host. |
| `INFRAWHO_TRUST_PROXY` | `false` | When `true`, honor `X-Forwarded-Host` / `X-Forwarded-For` from an upstream reverse proxy. Leave `false` when clients can reach the process directly (spoof risk). |
| `INFRAWHO_FEATURE_METRICS` | `false` | Design `FEATURE_METRICS`. When `true`, registers `GET /metrics` (Prometheus text: `login_failures_total`, `reveal_total`, `rate_limited_total`). Not Ops Minimum. |

### Master key (KEK)

The master key **must never be committed**. `/readyz` returns `503` if the DB is unreachable or the key file is missing/unloadable; `/healthz` stays `200` while the process is up.

Generate a lab key (outside git, or a gitignored `*.key` name):

```bash
openssl rand -out lab-master.key 32
chmod 600 lab-master.key
```

Or base64:

```bash
openssl rand -base64 32 > lab-master.key
chmod 600 lab-master.key
```

Production-oriented placement: host path such as `/etc/infrawho/master.key` with mode `0600`, bind-mounted read-only into the container. Keep an offline copy of the KEK separate from DB backups — losing the KEK makes ciphertext permanently unrecoverable.

## Run locally

```bash
export INFRAWHO_DB_URL='sqlite:///tmp/infrawho-lab.db'
export INFRAWHO_MASTER_KEY_FILE="$PWD/lab-master.key"
export INFRAWHO_LISTEN_ADDR=':8080'

go run ./cmd/infrawho
```

Checks:

```bash
curl -sS http://127.0.0.1:8080/healthz   # ok
curl -sS http://127.0.0.1:8080/readyz    # ok when DB pings and KEK loads
```

## Run with Docker Compose

**Create the key file before the first `compose up`.** If `./lab-master.key` is missing, Docker creates a **directory** at that path for the bind mount; `/readyz` will stay `503` until you remove the directory and replace it with a real key file.

```bash
# Required first — do not start Compose without this file
openssl rand -out lab-master.key 32
chmod 600 lab-master.key

# If you already hit the footgun:
#   rm -rf lab-master.key && openssl rand -out lab-master.key 32 && chmod 600 lab-master.key

docker compose up --build
curl -sS http://127.0.0.1:8080/readyz   # expect ok
```

Compose bind-mounts `./lab-master.key` to `/etc/infrawho/master.key` and stores SQLite under the `infrawho_data` volume.

The image runs as root by default so lab `0600` key bind-mounts stay readable. For production, prefer `docker run --user` matching the key file owner (or Docker/Podman secrets) — see comments in [`Dockerfile`](Dockerfile). Do not bake a fixed non-root `USER` into the image if lab keys are root-owned.

## Health endpoints

| Path | Behavior |
|------|----------|
| `GET /healthz` | Liveness — always `200` if the process is up. |
| `GET /readyz` | Readiness — `200` when the DB pings and `INFRAWHO_MASTER_KEY_FILE` loads; otherwise `503`. |
| `GET /metrics` | Optional Prometheus text counters; only registered when `INFRAWHO_FEATURE_METRICS=true`. No app auth — scrape from loopback/trusted network only (or protect at the proxy). |

All responses include CSP and related browser hardening headers (`Content-Security-Policy`, `X-Content-Type-Options`, `X-Frame-Options`, etc.). HSTS is **not** set by the app — configure it on the TLS-terminating reverse proxy.

## Production / reverse proxy

Bind the app to loopback and terminate TLS at your org proxy. Example env:

```bash
export INFRAWHO_LISTEN_ADDR='127.0.0.1:8080'
export INFRAWHO_TRUST_PROXY=true
export INFRAWHO_TRUSTED_ORIGINS='https://infrawho.example'
export INFRAWHO_COOKIE_SECURE=true
export INFRAWHO_MASTER_KEY_FILE=/etc/infrawho/master.key
# optional:
# export INFRAWHO_FEATURE_METRICS=true
```

Nginx sketch: [`docs/reverse-proxy.example.conf`](docs/reverse-proxy.example.conf) (forwards `X-Forwarded-Host` / `X-Forwarded-For` / `X-Forwarded-Proto`, sets HSTS). Only enable `INFRAWHO_TRUST_PROXY` when the proxy is the sole path to the process.

## Web UI (lab)

React + Vite SPA in [`web/`](web/). UI strings are Traditional Chinese.

```bash
# API (cookie Secure off for plain HTTP lab)
export INFRAWHO_COOKIE_SECURE=false
# …plus DB URL / master key / listen addr as above…
go run ./cmd/infrawho

# SPA (proxies /api to :8080; Host stays localhost:5173 so Origin checks match)
cd web && npm install && npm run dev
```

`INFRAWHO_TRUSTED_ORIGINS` is optional for the default Vite lab proxy (`changeOrigin: false`). See [`web/README.md`](web/README.md). Production embedding of `web/dist` from Go is deferred.

## Auth & first-run setup

Session cookie `infrawho_session`: `HttpOnly; SameSite=Strict; Path=/` (and `Secure` when `INFRAWHO_COOKIE_SECURE=true`). Idle 30m / absolute 12h. State-changing requests require a matching `Origin` or `Referer`.

| Method | Path | Notes |
|--------|------|-------|
| `GET` | `/api/v1/setup/status` | Bootstrap / KEK / checklist banner flags |
| `POST` | `/api/v1/setup/bootstrap` | Create first admin + checklist ack (requires KEK) |
| `POST` | `/api/v1/setup/acknowledge` | Persist checklist (admin session) |
| `POST` | `/api/v1/auth/login` | Sets session cookie |
| `POST` | `/api/v1/auth/logout` | Clears session |
| `GET` | `/api/v1/auth/me` | Current user + `step_up_active` |
| `POST` | `/api/v1/auth/step-up` | Re-check password; step-up valid 5m |

## Assets

Pagination uses `limit` (default 50, max 200) + `offset` (default 0). Soft-deleted assets are omitted from `GET /api/v1/assets` unless `include_deleted=true`. Operator may create/update active metadata; soft-delete and purge require **admin + step-up**.

| Method | Path | Notes |
|--------|------|-------|
| `GET` | `/api/v1/assets` | List; filters: `q`, `tag`, `environment`, `status`, `include_deleted` |
| `POST` | `/api/v1/assets` | Create (operator+); body may include `tags` |
| `GET` | `/api/v1/assets/{id}` | Detail + accounts/jobs/notes summaries (no secrets) |
| `PATCH` | `/api/v1/assets/{id}` | Update metadata; rejects `deleted_at` / `status=retired` (422) |
| `DELETE` | `/api/v1/assets/{id}` | Soft-delete (`status=retired`, `deleted_at=now`); admin + step-up |
| `POST` | `/api/v1/assets/{id}/purge` | Hard-delete soft-deleted asset and cascaded rows; admin + step-up (409 if still active) |

Example first-run (after KEK is in place):

```bash
curl -sS -c /tmp/iw.ck -b /tmp/iw.ck -H 'Origin: http://127.0.0.1:8080' \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"change-me-now","acknowledge_kek_offline":true,"acknowledge_kek_irrecoverable":true,"acknowledge_backup_planned":true}' \
  http://127.0.0.1:8080/api/v1/setup/bootstrap
```
