#!/usr/bin/env bash
# InfraWho DB restore from a backup.sh archive.
# Does not touch or restore master.key — supply the KEK separately.
# Stop the HTTP service before restoring.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/restore.sh --archive PATH --db PATH

Extracts the SQLite DB (and -wal/-shm if present) from an Infrawho backup
tarball to PATH. Stop the HTTP service before restoring.

Never extracts master.key; archives from backup.sh do not contain it.
Only members under data/ without ".." or absolute paths are accepted.
EOF
}

ARCHIVE=""
DB_PATH=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --archive)
      ARCHIVE="${2:-}"
      shift 2
      ;;
    --db)
      DB_PATH="${2:-}"
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

if [[ -z "$ARCHIVE" || -z "$DB_PATH" ]]; then
  usage >&2
  exit 2
fi

if [[ ! -f "$ARCHIVE" ]]; then
  echo "archive not found: $ARCHIVE" >&2
  exit 1
fi

listing="$(tar -tzf "$ARCHIVE")"

# Refuse archives that list a key path.
if printf '%s\n' "$listing" | grep -E '(^|/)master\.key$|\.key$' >/dev/null; then
  echo "refusing restore: archive contains a key path" >&2
  exit 1
fi

# Allowlist: only relative members under data/, no ".." components.
while IFS= read -r member; do
  [[ -z "$member" ]] && continue
  case "$member" in
    data|data/)
      continue
      ;;
    data/*)
      ;;
    *)
      echo "refusing restore: member outside data/: $member" >&2
      exit 1
      ;;
  esac
  case "$member" in
    /*|*/..|*/../*|../*|..)
      echo "refusing restore: unsafe member path: $member" >&2
      exit 1
      ;;
  esac
  if [[ "$member" == *'..'* ]]; then
    echo "refusing restore: unsafe member path: $member" >&2
    exit 1
  fi
done <<< "$listing"

db_name="$(basename -- "$DB_PATH")"
if [[ "$db_name" == "master.key" || "$db_name" == *.key ]]; then
  echo "refusing to restore onto a key path: $DB_PATH" >&2
  exit 1
fi

mkdir -p -- "$(dirname -- "$DB_PATH")"
staging="$(mktemp -d "${TMPDIR:-/tmp}/infrawho-restore.XXXXXX")"
cleanup() { rm -rf -- "$staging"; }
trap cleanup EXIT

# Extract only allowlisted file members under data/ (skip directory entries).
mapfile -t members < <(printf '%s\n' "$listing" | grep -E '^data/' | grep -v '/$' || true)
if [[ ${#members[@]} -eq 0 ]]; then
  echo "no data/ file members in archive" >&2
  exit 1
fi
tar -xzf "$ARCHIVE" -C "$staging" -- "${members[@]}"

src=""
if [[ -f "$staging/data/$db_name" ]]; then
  src="$staging/data/$db_name"
else
  src="$(find "$staging/data" -maxdepth 1 -type f -name '*.db' | head -n 1 || true)"
fi

if [[ -z "$src" || ! -f "$src" ]]; then
  echo "no database file found in archive" >&2
  exit 1
fi

cp -p -- "$src" "$DB_PATH"
src_base="$(basename -- "$src")"
for side in wal shm; do
  if [[ -f "$staging/data/${src_base}-${side}" ]]; then
    cp -p -- "$staging/data/${src_base}-${side}" "${DB_PATH}-${side}"
  else
    rm -f -- "${DB_PATH}-${side}"
  fi
done

echo "restored $DB_PATH"
