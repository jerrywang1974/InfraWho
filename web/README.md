# InfraWho Web UI（Phase 1 shell）

React + TypeScript + Vite SPA. UI strings are Traditional Chinese (hardcoded).

## Features in this PR

- First-run setup wizard (`/setup`) against `/api/v1/setup/*`
- Login / logout / session via cookie (`credentials: 'include'`)
- Asset list + detail showing **purpose**, **owner**, **OS**
- Account / jobs / notes panels: placeholder「即將在下一 PR 接上」

Does **not** call accounts or jobs APIs.

## Lab development (Vite proxy)

1. Start the Go API on `:8080` with cookie Secure off and Vite origin trusted:

```bash
export INFRAWHO_DB_URL='sqlite:///tmp/infrawho-lab.db'
export INFRAWHO_MASTER_KEY_FILE="$PWD/lab-master.key"
export INFRAWHO_LISTEN_ADDR=':8080'
export INFRAWHO_COOKIE_SECURE=false
export INFRAWHO_TRUSTED_ORIGINS='http://localhost:5173'
go run ./cmd/infrawho
```

2. In another terminal:

```bash
cd web
npm install
npm run dev
```

Open `http://localhost:5173`. Vite proxies `/api`, `/healthz`, and `/readyz` to `http://127.0.0.1:8080`.

`INFRAWHO_TRUSTED_ORIGINS` is required because browser `Origin` is the Vite host while the API Host is `:8080`.

## Production note

This PR ships the SPA under `web/` and documents the lab proxy. Embedding or static-serving `web/dist` from the Go process can be added in a follow-up; until then, build with `npm run build` and put `dist/` behind the same origin as the API (or keep using the Vite proxy in lab).

## Scripts

| Script | Purpose |
|--------|---------|
| `npm run dev` | Vite dev server + API proxy |
| `npm run build` | Typecheck + production bundle to `dist/` |
| `npm run preview` | Preview production build locally |
