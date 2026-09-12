#!/usr/bin/env bash
# InfraWho DB backup — packs SQLite DB (+ WAL/SHM sidecars) only.
# Never includes master.key (or any *.key). Contract-tested via tar tz.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/backup.sh --db PATH --out DIR

Creates DIR/infrawho-backup-YYYYMMDDTHHMMSSZ.tar.gz containing only the
SQLite database file and optional -wal/-shm sidecars.

The master key must never be in the archive. This script packs explicit
DB paths only (not a recursive data directory), and refuses to add any
path whose basename is master.key or ends in .key.
EOF
}

DB_PATH=""
OUT_DIR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --db)
      DB_PATH="${2:-}"
      shift 2
      ;;
    --out)
      OUT_DIR="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$DB_PATH" || -z "$OUT_DIR" ]]; then
  usage >&2
  exit 2
fi

if [[ ! -f "$DB_PATH" ]]; then
  echo "database file not found: $DB_PATH" >&2
  exit 1
fi

base="$(basename -- "$DB_PATH")"
if [[ "$base" == "master.key" || "$base" == *.key ]]; then
  echo "refusing to backup a key file: $DB_PATH" >&2
  exit 1
fi

mkdir -p -- "$OUT_DIR"
OUT_DIR="$(cd -- "$OUT_DIR" && pwd)"
DB_ABS="$(cd -- "$(dirname -- "$DB_PATH")" && pwd)/$(basename -- "$DB_PATH")"
DB_NAME="$(basename -- "$DB_ABS")"

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
archive="$OUT_DIR/infrawho-backup-${stamp}.tar.gz"
staging="$(mktemp -d "${TMPDIR:-/tmp}/infrawho-backup.XXXXXX")"
cleanup() { rm -rf -- "$staging"; }
trap cleanup EXIT

# Copy only the DB and sidecars into a clean staging tree (never the key).
mkdir -p -- "$staging/data"
cp -p -- "$DB_ABS" "$staging/data/$DB_NAME"
for side in wal shm; do
  if [[ -f "${DB_ABS}-${side}" ]]; then
    cp -p -- "${DB_ABS}-${side}" "$staging/data/${DB_NAME}-${side}"
  fi
done

# Defense in depth: abort if staging somehow contains a key path.
if find "$staging" -type f \( -name 'master.key' -o -name '*.key' \) | grep -q .; then
  echo "refusing to create archive: key file present in staging" >&2
  exit 1
fi

tar -C "$staging" -czf "$archive" data

# Contract: listing must not mention master.key (or any .key).
listing="$(tar -tzf "$archive")"
if printf '%s\n' "$listing" | grep -E '(^|/)master\.key$|\.key$' >/dev/null; then
  rm -f -- "$archive"
  echo "backup contract failed: archive contains a key path" >&2
  exit 1
fi

echo "$archive"
