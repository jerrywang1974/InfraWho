# InfraWho

Self-hosted CMDB and credential vault for small infra teams (Phase 1).

**License:** MIT — see [LICENSE](LICENSE).

## Status

**Phase 1 surface is frozen.** Implemented stack: SQLite CMDB + vault, auth/RBAC/step-up, assets, accounts reveal, jobs/notes, export/import, CSP headers, optional `/metrics`, Vite React UI (login, assets, accounts/jobs/notes/reveal).

Canonical HTTP contract: [`api/openapi.yaml`](api/openapi.yaml). Search is `GET /api/v1/assets?q=` (no separate `/search` route). Phase 2 agent/remote paths are documented only under OpenAPI `x-future` — **not registered** in the Go router (no 501 stubs).

| Horizon | Item |
|---------|------|
| Phase 1 | Local `AuditEvent` only |
| **Phase 1.5 must-do (K23)** | Configurable **audit webhook** for at least `CREDENTIAL_REVEAL`, `EXPORT_WITH_SECRETS`, `ASSET_PURGE` (recommend `ASSET_SOFT_DELETE`) |
| Phase 1.5+ backlog | LDAP/OIDC **SSO** — not urgent; local accounts remain near-term auth |

## Requirements

- Go 1.23+
- Docker + Docker Compose (optional, for containerized lab runs)

## Configuration

| Variable | Default | Notes |
|----------|---------|--------|
| `INFRAWHO_DB_URL` | `sqlite:///data/infrawho.db` | Phase 1 **SQLite only**. Non-`sqlite:` schemes fail at startup. Parent dirs are created; embedded migrations run on open. |
| `INFRAWHO_MASTER_KEY_FILE` | _(empty)_ | Path to KEK file (32 raw bytes **or** base64 of 32 bytes). Preferred over env-embedded keys. |
| `INFRAWHO_KEY_VERSION` | `kek-v1` | Label stamped on new `secret_payloads.key_version` (must identify the loaded KEK). Update after rewrap when switching to a new KEK. |
| `INFRAWHO_LISTEN_ADDR` | `:8080` | HTTP listen address. |
| `INFRAWHO_COOKIE_SECURE` | `true` | Set `false` for plain-HTTP lab (Compose does this). Production behind TLS should keep `true`. |
| `INFRAWHO_TRUSTED_ORIGINS` | _(empty)_ | Optional comma-separated origins (`https://app.example`) or bare hosts. Scheme-bearing entries match that scheme only. |
| `INFRAWHO_TRUST_PROXY` | `false` | When `true`, honor `X-Forwarded-Host` / `X-Forwarded-For` from an upstream reverse proxy. Leave `false` when clients can reach the process directly (spoof risk). |
| `FEATURE_EXPORT_SECRETS` | `false` | When `true`, allows `POST /api/v1/export` with `include_secrets:true` (still requires admin + step-up + audit). |
| `INFRAWHO_FEATURE_METRICS` | `false` | When `true`, registers `GET /metrics` (Prometheus text). Not Ops Minimum. |

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

### KEK rotation (stop-the-world)

Secrets use envelope encryption: a per-secret DEK (AES-256-GCM) is wrapped by the KEK. Rotation is a **foreground** classic DEK rewrap — it unwraps/wraps DEKs and updates `key_version` only; `nonce` / `ciphertext` are unchanged.

**Runbook:**

1. **Stop** the HTTP service (`docker compose stop infrawho` or equivalent).
2. Generate the new KEK file, then run:

```bash
infrawho keys rewrap --from kek-v1 --to kek-v2 \
  --old-key-file /etc/infrawho/master-v1.key \
  --new-key-file /etc/infrawho/master-v2.key
```

Uses `INFRAWHO_DB_URL`. Re-run if interrupted; keep **both** key files until `remaining_from=0`. Do **not** start the app mid-rotation.

3. Point `INFRAWHO_MASTER_KEY_FILE` **only** at the new key, and set `INFRAWHO_KEY_VERSION` to the `--to` value (e.g. `kek-v2`) so new create/rotate stamps match the loaded KEK.
4. **Start** the service; confirm `GET /readyz` and sample a reveal on a non-critical account.
5. Destroy the old KEK only after checklist confirmation (`COUNT` of old `key_version` is 0).

```bash
infrawho keys --help   # prints the same runbook
```

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

The image runs as root by default so lab `0600` key bind-mounts stay readable. For production, prefer `docker run --user` matching the key file owner (or Docker/Podman secrets) — see comments in [`Dockerfile`](Dockerfile). That same UID must also be able to write the SQLite data path (e.g. Compose `/data`); chown the volume once if it was previously written as root. Do not bake a fixed non-root `USER` into the image if lab keys are root-owned.

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

## 運維最低要求（Ops Minimum）

Phase 1 **上線可接受**的最低維運集合（兼職管理員）。其餘（Prometheus／Alertmanager、稽核 webhook、Postgres、線上 KEK 輪替 UI、agent、OIDC/LDAP）明確延後。

> **警告：** KEK（主金鑰）遺失 = 資料庫密文與含密備份**永久不可解密**。安裝精靈會要求勾選確認後才進入主畫面。

### 1. Docker Compose + SQLite volume

```bash
openssl rand -out lab-master.key 32 && chmod 600 lab-master.key
docker compose up --build
curl -sS http://127.0.0.1:8080/readyz   # 須為 ok
```

- SQLite 落在 Compose volume `infrawho_data`（容器內 `/data/infrawho.db`）。
- 生產請改綁定組織路徑／secrets；勿把 `*.key` 提交進 git。
- 詳見上方 [Run with Docker Compose](#run-with-docker-compose)。

### 2. Master key 檔案 + 離線複本

| 項目 | 作法 |
|------|------|
| 線上 KEK | 宿主例如 `/etc/infrawho/master.key`，mode `0600`，唯讀掛載進容器 |
| 優先序 | **優先** `INFRAWHO_MASTER_KEY_FILE`；勿用 env 嵌金鑰於生產 |
| 離線複本 | 密封實體媒體或企業密碼庫；與 DB 備份**異地**存放 |
| RACI | 主管理員保管線上 key；備援同事或主管保管密封複本 |

```bash
sudo mkdir -p /etc/infrawho
sudo openssl rand -out /etc/infrawho/master.key 32
sudo chmod 600 /etc/infrawho/master.key
# 製作離線複本後，確認複本可讀、與線上檔一致（checksum），再封存
```

### 3. 每日備份到另一台主機

```bash
# 在執行 InfraWho 的主機（建議 cron 每日）
scripts/backup.sh --db /data/infrawho.db --out /var/backups/infrawho

# 將 tarball 同步到*另一台*主機（示例）
rsync -a /var/backups/infrawho/ backup-host:/backups/infrawho/
```

- 備份 tarball **永不**含 `master.key`（腳本以 `tar tz` 斷言拒絕 `*.key`）。
- 備份 = **僅密文** DB；還原時必須另備現行 KEK。
- 有 `sqlite3` 時用 `.backup`（線上一致快照）；否則設 `INFRAWHO_BACKUP_ALLOW_HOT_COPY=1` 或先停 HTTP。

### 4. 至少一次還原演練（restore drill）

文件打勾後才算 Ops Minimum 完成：

1. 在維護窗**停止** HTTP（`docker compose stop infrawho`）。
2. 自異地取回最新 tarball + 對應 KEK（離線複本）。
3. 還原到暫存路徑並驗證：

```bash
scripts/restore.sh --archive /path/to/infrawho-backup-….tar.gz --db /tmp/infrawho-restore.db
# 以還原 DB + 同一把 KEK 啟動實驗室實例，抽樣 GET /readyz 與一筆非關鍵 reveal
```

4. 記錄日期、執行人、結果；失敗則修正備份路徑／權限後重練。

### 5. TLS 終止於反向代理

- 應用可先在內網 HTTP；對外由組織既有 reverse proxy 終止 TLS。
- 設定示例：[`docs/reverse-proxy.example.conf`](docs/reverse-proxy.example.conf)。
- 生產建議：`INFRAWHO_LISTEN_ADDR=127.0.0.1:8080`、`INFRAWHO_TRUST_PROXY=true`、`INFRAWHO_TRUSTED_ORIGINS=https://…`、`INFRAWHO_COOKIE_SECURE=true`。
- HSTS 由 proxy 設定；應用本身不送 HSTS。

### Phase 1 明確不做（延後）

| 項目 | 去向 |
|------|------|
| 稽核 webhook | **Phase 1.5 必做**（K23） |
| LDAP / OIDC SSO | **1.5+ backlog**（非急；近期本地帳號） |
| Prometheus／Alertmanager | 非 Ops Minimum；可選 `INFRAWHO_FEATURE_METRICS` |
| Postgres | 1.5／有 HA 需求再排 |
| Agent／remote 路由 | Phase 2+；僅見 OpenAPI `x-future` |
| 線上 KEK 輪替 UI | 前景 CLI `infrawho keys rewrap` 即可（見上方 runbook） |

## Phase 2 `x-future`（僅文件）

下列路徑**保留名稱、不實作、不註冊**：

| Method | Path |
|--------|------|
| `POST` | `/api/v1/agents/register` |
| `GET` | `/api/v1/assets/{id}/connectivity` |
| `POST` | `/api/v1/assets/{id}/sessions` |

詳見 [`api/openapi.yaml`](api/openapi.yaml) 根層 `x-future` / `x-phase15`。

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

## Accounts & vault

Secrets are optional per account. List/detail responses never include ciphertext. Reveal requires **operator+**, valid **step-up**, and is rate-limited (30/session/min). Responses use `Cache-Control: no-store`. When a secret exists, `auth_type` cannot be changed via `PATCH` (use `rotate-secret`).

| Method | Path | Notes |
|--------|------|-------|
| `POST` | `/api/v1/assets/{id}/accounts` | Create account; optional `secret` (encrypted at rest) |
| `PATCH` | `/api/v1/accounts/{id}` | Metadata only; rejects `auth_type` change when `has_secret` |
| `DELETE` | `/api/v1/accounts/{id}` | Deletes account and secret payload |
| `POST` | `/api/v1/accounts/{id}/reveal` | Decrypt; step-up + audit `CREDENTIAL_REVEAL` |
| `POST` | `/api/v1/accounts/{id}/rotate-secret` | Replace secret; may change `auth_type` (re-encrypt with new AAD) |
| `GET` | `/api/v1/audit-events` | Admin list (`limit`/`offset`); filters: `action`, `actor_id`, `outcome` |

## Scheduled jobs & notes

Manual documentation only (not agent-discovered). Pagination uses `limit` (default 50, max 200) + `offset`. Soft-deleted assets reject **all** writes (create/patch/delete → `409 conflict`). List remains readable. Mutate requires **operator+** and a matching `Origin`/`Referer`.

| Method | Path | Notes |
|--------|------|-------|
| `GET` | `/api/v1/assets/{id}/jobs` | List jobs for asset |
| `POST` | `/api/v1/assets/{id}/jobs` | Create job (operator+) |
| `PATCH` | `/api/v1/jobs/{id}` | Update job (operator+); empty body → 422 |
| `DELETE` | `/api/v1/jobs/{id}` | Delete job (operator+) |
| `GET` | `/api/v1/assets/{id}/notes` | List notes for asset |
| `POST` | `/api/v1/assets/{id}/notes` | Create note (operator+); sets `author_id` |
| `PATCH` | `/api/v1/notes/{id}` | Update note (operator+); empty body → 422 |
| `DELETE` | `/api/v1/notes/{id}` | Delete note (operator+) |

## Export / import / DB backup

JSON export is **POST-only** (never GET). Default export is metadata (no secret field) for operator+. Plaintext secret export needs `FEATURE_EXPORT_SECRETS=true`, admin, step-up, and audit `EXPORT_WITH_SECRETS`. Import rejects active hostname conflicts (`409 hostname_conflict`); bodies with secrets require step-up and reseal under the current KEK. Ops DB backups use `scripts/backup.sh` (ciphertext DB only — **never** `master.key`).

| Method | Path | Notes |
|--------|------|-------|
| `POST` | `/api/v1/export` | Body `{ "format":"json", "include_secrets": false\|true }` |
| `POST` | `/api/v1/import` | Portable document; hostname conflict = reject |

```bash
scripts/backup.sh --db /data/infrawho.db --out /var/backups/infrawho
scripts/restore.sh --archive /var/backups/infrawho/infrawho-backup-….tar.gz --db /data/infrawho.db
```

Example first-run (after KEK is in place):

```bash
curl -sS -c /tmp/iw.ck -b /tmp/iw.ck -H 'Origin: http://127.0.0.1:8080' \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"change-me-now","acknowledge_kek_offline":true,"acknowledge_kek_irrecoverable":true,"acknowledge_backup_planned":true}' \
  http://127.0.0.1:8080/api/v1/setup/bootstrap
```
