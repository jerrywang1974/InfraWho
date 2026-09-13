#!/usr/bin/env bash
# Bring up InfraWho via Docker Compose for lab or single-box verify.
# Creates ./lab-master.key if missing (never commit it).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERIFY=0
BUILD=1
for arg in "$@"; do
  case "$arg" in
    --verify) VERIFY=1 ;;
    --no-build) BUILD=0 ;;
    -h|--help)
      cat <<EOF
Usage: $0 [--verify] [--no-build]

  (default)  docker compose -f docker-compose.yml up
  --verify   use docker-compose.verify.yml (host port 18080)
  --no-build skip --build

Requires: docker compose, openssl
EOF
      exit 0
      ;;
  esac
done

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker not found; install Docker or run scripts/verify-smoke.sh against a local binary" >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "error: docker compose plugin not available" >&2
  exit 1
fi

# Avoid the bind-mount directory footgun: missing key path → Docker creates a dir.
if [ -d lab-master.key ]; then
  echo "error: lab-master.key is a directory (compose footgun). Remove it and re-run:" >&2
  echo "  rm -rf lab-master.key && $0 $*" >&2
  exit 1
fi
if [ ! -f lab-master.key ]; then
  echo "generating lab-master.key (32 bytes, mode 0600)"
  openssl rand -out lab-master.key 32
  chmod 600 lab-master.key
fi

COMPOSE_FILES=(-f docker-compose.yml)
if [ "$VERIFY" -eq 1 ]; then
  COMPOSE_FILES=(-f docker-compose.verify.yml)
fi

ARGS=(up -d)
if [ "$BUILD" -eq 1 ]; then
  ARGS+=(--build)
fi

docker compose "${COMPOSE_FILES[@]}" "${ARGS[@]}"
echo
if [ "$VERIFY" -eq 1 ]; then
  echo "verify stack up — http://127.0.0.1:18080/"
  echo "smoke: ./scripts/verify-smoke.sh --base http://127.0.0.1:18080"
else
  echo "lab stack up — http://127.0.0.1:8080/"
  echo "smoke: ./scripts/verify-smoke.sh --base http://127.0.0.1:8080"
fi
