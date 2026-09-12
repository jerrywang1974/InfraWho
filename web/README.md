# InfraWho Web UI（Phase 1 shell）

React + TypeScript + Vite SPA. UI strings are Traditional Chinese (hardcoded).

## Features in this PR

- First-run setup wizard (`/setup`) against `/api/v1/setup/*`
- Login / logout / session via cookie (`credentials: 'include'`)
- Asset list + detail showing **purpose**, **owner**, **OS**
- Account / jobs / notes panels: placeholder「即將在下一 PR 接上」

Does **not** call accounts or jobs APIs. Asset detail drops nested account/job/note summaries from React state until PR 10.

## Lab development (Vite proxy)

1. Start the Go API on `:8080` with cookie Secure off:

```bash
export INFRAWHO_DB_URL='sqlite:///tmp/infrawho-lab.db'
export INFRAWHO_MASTER_KEY_FILE="$PWD/lab-master.key"
export INFRAWHO_LISTEN_ADDR=':8080'
export INFRAWHO_COOKIE_SECURE=false
go run ./cmd/infrawho
```

The Vite proxy keeps `changeOrigin: false`, so Go sees `Host: localhost:5173` matching the browser `Origin`. You usually do **not** need `INFRAWHO_TRUSTED_ORIGINS` for this lab path. Set it only if you browse via a different host/port.

2. In another terminal:

```bash
cd web
npm install
npm run dev
```

Open `http://localhost:5173`. Vite proxies `/api`, `/healthz`, and `/readyz` to `http://127.0.0.1:8080`.

## Production note

This PR ships the SPA under `web/` and documents the lab proxy. Embedding or static-serving `web/dist` from the Go process can be added in a follow-up; until then, build with `npm run build` and put `dist/` behind the same origin as the API (or keep using the Vite proxy in lab).

## Scripts

| Script | Purpose |
|--------|---------|
| `npm run dev` | Vite dev server + API proxy (`:5173`) |
| `npm run build` | Typecheck + production bundle to `dist/` |
| `npm run preview` | Preview `dist/` with the **same API proxy** (`:4173`) |

Prefer `npm run dev` for day-to-day lab work. `preview` also proxies `/api` so cookie auth works against `:8080`, but still requires the Go API running separately.
