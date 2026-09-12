#!/usr/bin/env bash
# End-to-end API smoke against a running InfraWho (binary or Docker).
# Writes a markdown test record under docs/test-records/ when --record is set
# (default on).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BASE="${INFRAWHO_SMOKE_BASE:-http://127.0.0.1:8080}"
RECORD=1
ORIGIN=""
PASS=0
FAIL=0
SKIP=0
RESULTS=()

usage() {
  cat <<EOF
Usage: $0 [--base URL] [--no-record]

  --base URL     default http://127.0.0.1:8080 (verify compose: :18080)
  --no-record    do not write docs/test-records/*.md
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --base) BASE="$2"; shift 2 ;;
    --no-record) RECORD=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage; exit 2 ;;
  esac
done

ORIGIN="$BASE"
CK="$(mktemp)"
WORKDIR="$(mktemp -d)"
trap 'rm -f "$CK"; rm -rf "$WORKDIR"' EXIT

note() { RESULTS+=("$1"); echo "$1"; }
ok() { PASS=$((PASS + 1)); note "PASS: $1"; }
fail() { FAIL=$((FAIL + 1)); note "FAIL: $1 — $2"; }
skip() { SKIP=$((SKIP + 1)); note "SKIP: $1 — $2"; }

json_field() {
  python3 -c 'import json,sys; d=json.load(sys.stdin); print(d'"$1"')'
}

wait_ready() {
  local i
  for i in $(seq 1 60); do
    if curl -fsS "$BASE/readyz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

echo "smoke against $BASE"
if ! curl -fsS "$BASE/healthz" | grep -qx 'ok'; then
  fail "healthz" "not ok"
else
  ok "healthz"
fi

if ! wait_ready; then
  fail "readyz" "timed out (need DB + KEK)"
  # Still write record and exit non-zero
else
  ok "readyz"
fi

# Bootstrap or login
STATUS="$(curl -fsS -c "$CK" -b "$CK" -H "Origin: $ORIGIN" "$BASE/api/v1/setup/status" || true)"
if echo "$STATUS" | grep -q '"needs_bootstrap":true'; then
  curl -fsS -c "$CK" -b "$CK" -X POST -H "Origin: $ORIGIN" -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"ChangeMe-Now-123!","acknowledge_kek_offline":true,"acknowledge_kek_irrecoverable":true,"acknowledge_backup_planned":true}' \
    "$BASE/api/v1/setup/bootstrap" >/dev/null
  ok "bootstrap admin"
else
  curl -fsS -c "$CK" -b "$CK" -X POST -H "Origin: $ORIGIN" -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"ChangeMe-Now-123!"}' \
    "$BASE/api/v1/auth/login" >/dev/null
  ok "login admin"
fi

HOST="smoke-$(date +%s).local"
python3 - <<PY
import json
from pathlib import Path
Path("$WORKDIR/asset.json").write_text(json.dumps({
  "name": "Smoke VM",
  "hostname": "$HOST",
  "asset_type": "vm",
  "os_family": "linux",
  "environment": "lab",
  "purpose": "docker packaging smoke",
  "tags": ["smoke"]
}))
Path("$WORKDIR/account.json").write_text(json.dumps({
  "username": "root",
  "auth_type": "password",
  "description": "smoke",
  "secret": "smoke-secret-value"
}))
Path("$WORKDIR/job.json").write_text(json.dumps({
  "name": "smoke-job",
  "scheduler_type": "cron",
  "schedule_expr": "0 3 * * *",
  "command_or_path": "/bin/true",
  "enabled_doc": True
}))
PY

ASSET="$(curl -fsS -c "$CK" -b "$CK" -X POST -H "Origin: $ORIGIN" -H "Content-Type: application/json" \
  --data-binary @"$WORKDIR/asset.json" "$BASE/api/v1/assets")"
ASSET_ID="$(printf '%s' "$ASSET" | json_field "['id']")"
ok "create asset $ASSET_ID"

ACCT="$(curl -fsS -c "$CK" -b "$CK" -X POST -H "Origin: $ORIGIN" -H "Content-Type: application/json" \
  --data-binary @"$WORKDIR/account.json" "$BASE/api/v1/assets/$ASSET_ID/accounts")"
ACCT_ID="$(printf '%s' "$ACCT" | json_field "['id']")"
ok "create account $ACCT_ID"

CODE="$(curl -sS -o "$WORKDIR/r1.json" -w '%{http_code}' -c "$CK" -b "$CK" -X POST \
  -H "Origin: $ORIGIN" -H "Content-Type: application/json" -d '{}' \
  "$BASE/api/v1/accounts/$ACCT_ID/reveal")"
if [ "$CODE" = "403" ] && grep -q step_up_required "$WORKDIR/r1.json"; then
  ok "reveal blocked without step-up"
else
  fail "reveal without step-up" "http=$CODE body=$(cat "$WORKDIR/r1.json")"
fi

curl -fsS -c "$CK" -b "$CK" -X POST -H "Origin: $ORIGIN" -H "Content-Type: application/json" \
  -d '{"password":"ChangeMe-Now-123!"}' "$BASE/api/v1/auth/step-up" >/dev/null
ok "step-up"

REVEAL="$(curl -fsS -c "$CK" -b "$CK" -X POST -H "Origin: $ORIGIN" -H "Content-Type: application/json" -d '{}' \
  "$BASE/api/v1/accounts/$ACCT_ID/reveal")"
SECRET="$(printf '%s' "$REVEAL" | json_field "['secret']")"
if [ "$SECRET" = "smoke-secret-value" ]; then
  ok "reveal secret"
else
  fail "reveal secret" "got=$SECRET"
fi

curl -fsS -c "$CK" -b "$CK" -X POST -H "Origin: $ORIGIN" -H "Content-Type: application/json" \
  --data-binary @"$WORKDIR/job.json" "$BASE/api/v1/assets/$ASSET_ID/jobs" >/dev/null
ok "create job"

curl -fsS -c "$CK" -b "$CK" -X POST -H "Origin: $ORIGIN" -H "Content-Type: application/json" \
  -d '{"body":"smoke note"}' "$BASE/api/v1/assets/$ASSET_ID/notes" >/dev/null
ok "create note"

LIST="$(curl -fsS -c "$CK" -b "$CK" "$BASE/api/v1/assets?q=Smoke")"
if echo "$LIST" | grep -q "$HOST"; then
  ok "search assets"
else
  fail "search assets" "$LIST"
fi

# Optional SPA check (Docker verify image sets WEB_ROOT)
SPA_CODE="$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/" || true)"
if [ "$SPA_CODE" = "200" ]; then
  ok "SPA index (/) http 200"
else
  skip "SPA index" "http=$SPA_CODE (API-only is OK for production design)"
fi

EXPORT="$(curl -fsS -c "$CK" -b "$CK" -X POST -H "Origin: $ORIGIN" -H "Content-Type: application/json" \
  -d '{"format":"json","include_secrets":false}' "$BASE/api/v1/export")"
if echo "$EXPORT" | grep -q '"include_secrets"'; then
  ok "export metadata"
else
  fail "export metadata" "unexpected body"
fi

echo
echo "summary: PASS=$PASS FAIL=$FAIL SKIP=$SKIP"

if [ "$RECORD" -eq 1 ]; then
  mkdir -p "$ROOT/docs/test-records"
  STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
  OUT="$ROOT/docs/test-records/${STAMP}-verify-smoke.md"
  {
    echo "# Verify smoke record — $STAMP"
    echo
    echo "- **Base URL:** \`$BASE\`"
    echo "- **Host:** \`$(hostname 2>/dev/null || echo unknown)\`"
    echo "- **Git:** \`$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)\` (\`$(git -C "$ROOT" branch --show-current 2>/dev/null || echo unknown)\`)"
    echo "- **Result:** PASS=$PASS FAIL=$FAIL SKIP=$SKIP"
    echo
    echo "## Cases"
    echo
    for line in "${RESULTS[@]}"; do
      echo "- $line"
    done
    echo
    echo "## Notes"
    echo
    echo "- Lab/verify packaging: see \`docs/docker-packaging.md\`."
    echo "- Production remains API-only behind reverse proxy (\`docker-compose.prod.example.yml\`)."
  } >"$OUT"
  echo "wrote $OUT"
fi

[ "$FAIL" -eq 0 ]
