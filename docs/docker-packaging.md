# Docker 包裝說明（Lab / 單機驗證 vs 正式環境）

本倉庫用**同一份**多階段 [`Dockerfile`](../Dockerfile) 產出映像；差異在 Compose 與環境變數，而不是另做一套正式版 binary。

## 三種用途

| 檔案 | 用途 | Web UI | KEK | TLS |
|------|------|--------|-----|-----|
| [`docker-compose.yml`](../docker-compose.yml) | 日常 lab | `INFRAWHO_WEB_ROOT=/app/web`（映像內 SPA） | `./lab-master.key` | 無（`COOKIE_SECURE=false`） |
| [`docker-compose.verify.yml`](../docker-compose.verify.yml) | 單機驗證／smoke（port **18080**） | 同上 | `./lab-master.key` | 無 |
| [`docker-compose.prod.example.yml`](../docker-compose.prod.example.yml) | **正式環境（原始設計）** | **不設** `WEB_ROOT`（API-only） | 宿主 `/etc/infrawho/master.key` | 組織 reverse proxy |

正式環境請以 prod example + 設計文件 Ops Minimum 為準：Compose + SQLite volume、KEK 檔與離線複本、每日備份、還原演練、TLS 掛在既有 proxy。

## 快速開始（單機驗證）

```bash
./scripts/docker-lab-up.sh --verify
./scripts/verify-smoke.sh --base http://127.0.0.1:18080
```

瀏覽器開 `http://127.0.0.1:18080/`（映像內含 Vite build 的 SPA）。

若本機沒有 Docker，可用 binary：

```bash
openssl rand -out lab-master.key 32 && chmod 600 lab-master.key
export INFRAWHO_DB_URL='sqlite:///tmp/infrawho-lab.db'
export INFRAWHO_MASTER_KEY_FILE="$PWD/lab-master.key"
export INFRAWHO_COOKIE_SECURE=false
go run ./cmd/infrawho
# 另開終端
./scripts/verify-smoke.sh --base http://127.0.0.1:8080
```

## `INFRAWHO_WEB_ROOT`

- **有設**：Go 對非 `/api`、`/healthz`、`/readyz`、`/metrics` 的 GET 提供靜態檔，並對前端路由回 `index.html`（見 `internal/webui`）。
- **不設（正式）**：行為與原始設計相同——純 API，UI 由 proxy 或獨立靜態站提供。

## 測試與紀錄

- 單元：`go test ./internal/webui/ ./internal/config/ ./...`
- 煙霧：`scripts/verify-smoke.sh`（預設寫入 `docs/test-records/`）
- OpenAPI：`scripts/check-openapi.sh`
