#!/usr/bin/env bash
# InfraWho DB backup — packs SQLite DB (+ WAL/SHM sidecars) only.
# Never includes master.key (or any *.key). Contract-tested via tar tz.
#
# Prefer sqlite3 ".backup" for a consistent snapshot under WAL. If sqlite3 is
# unavailable, falls back to cp and requires INFRAWHO_BACKUP_ALLOW_HOT_COPY=1
# (or stop the HTTP service first for a consistent copy).
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/backup.sh --db PATH --out DIR

Creates DIR/infrawho-backup-YYYYMMDDTHHMMSSZ.tar.gz containing only the
SQLite database file and optional -wal/-shm sidecars.

The master key must never be in the archive. This script packs explicit
DB paths only (not a recursive data directory), and refuses to add any
path whose basename is master.key or ends in .key.

Consistency:
  - Uses `sqlite3 DB ".backup '…'"` when sqlite3 is on PATH (safe while online).
  - Otherwise copies files; set INFRAWHO_BACKUP_ALLOW_HOT_COPY=1 to acknowledge
    hot-copy risk, or stop the HTTP service before backup.
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

mkdir -p -- "$staging/data"
staged_db="$staging/data/$DB_NAME"

copy_consistent() {
  if command -v sqlite3 >/dev/null 2>&1; then
    # Online-safe consistent snapshot (no need to copy -wal/-shm separately).
    sqlite3 "$DB_ABS" ".backup '$staged_db'"
    return 0
  fi
  if [[ "${INFRAWHO_BACKUP_ALLOW_HOT_COPY:-}" != "1" ]]; then
    echo "sqlite3 not found; refusing hot cp of a WAL database." >&2
    echo "Install sqlite3, or stop HTTP and re-run with INFRAWHO_BACKUP_ALLOW_HOT_COPY=1." >&2
    exit 1
  fi
  echo "warning: hot-copying DB without sqlite3 .backup; stop HTTP for consistency" >&2
  cp -p -- "$DB_ABS" "$staged_db"
  for side in wal shm; do
    if [[ -f "${DB_ABS}-${side}" ]]; then
      cp -p -- "${DB_ABS}-${side}" "$staging/data/${DB_NAME}-${side}"
    fi
  done
}

copy_consistent

# Defense in depth: abort if staging somehow contains a key path.
if find "$staging" -type f \( -name 'master.key' -o -name '*.key' \) | grep -q .; then
  echo "refusing to create archive: key file present in staging" >&2
  exit 1
fi

tar -C "$staging" -czf "$archive" data

# Contract: listing must not mention master.key (or any .key basename).
listing="$(tar -tzf "$archive")"
if printf '%s\n' "$listing" | grep -E '(^|/)master\.key$|\.key$' >/dev/null; then
  rm -f -- "$archive"
  echo "backup contract failed: archive contains a key path" >&2
  exit 1
fi

echo "$archive"
