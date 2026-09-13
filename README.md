# InfraWho

Self-hosted CMDB and credential vault for small infra teams (Phase 1).

**License:** MIT — see [LICENSE](LICENSE).

## Status

Runnable skeleton: config loading, `/healthz`, `/readyz` (fails if the master key / KEK cannot be loaded), Docker Compose. Auth, DB schema, vault crypto, and UI come in later PRs.

## Requirements

- Go 1.23+
- Docker + Docker Compose (optional, for containerized lab runs)

## Configuration

| Variable | Default | Notes |
|----------|---------|--------|
| `INFRAWHO_DB_URL` | `sqlite:///data/infrawho.db` | Phase 1 **SQLite only**. Non-`sqlite:` schemes fail at startup. |
| `INFRAWHO_MASTER_KEY_FILE` | _(empty)_ | Path to KEK file (32 raw bytes **or** base64 of 32 bytes). Preferred over env-embedded keys. |
| `INFRAWHO_LISTEN_ADDR` | `:8080` | HTTP listen address. |

### Master key (KEK)

The master key **must never be committed**. `/readyz` returns `503` if the file is missing or unloadable; `/healthz` stays `200` while the process is up.

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
curl -sS http://127.0.0.1:8080/readyz    # ok when KEK loads
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

## Health endpoints

| Path | Behavior |
|------|----------|
| `GET /healthz` | Liveness — always `200` if the process is up. |
| `GET /readyz` | Readiness — `200` only when `INFRAWHO_MASTER_KEY_FILE` is set and the KEK loads; otherwise `503`. |
