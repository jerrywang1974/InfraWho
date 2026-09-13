#!/usr/bin/env bash
# Lightweight parse check for the frozen Phase 1 OpenAPI contract.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SPEC="$ROOT/api/openapi.yaml"

if [[ ! -f "$SPEC" ]]; then
  echo "missing $SPEC" >&2
  exit 1
fi

python3 - "$SPEC" <<'PY'
import sys
path = sys.argv[1]
try:
    import yaml  # type: ignore
except ImportError:
    import subprocess
    subprocess.check_call(
        [sys.executable, "-m", "pip", "install", "--quiet", "PyYAML"],
        stdout=subprocess.DEVNULL,
    )
    import yaml  # type: ignore

with open(path, "r", encoding="utf-8") as f:
    doc = yaml.safe_load(f)

if not isinstance(doc, dict) or "openapi" not in doc or "paths" not in doc:
    raise SystemExit("openapi.yaml: missing openapi/paths keys")
if "x-future" not in doc:
    raise SystemExit("openapi.yaml: missing x-future extension")
print(f"ok: {path} (openapi {doc.get('openapi')}, {len(doc['paths'])} paths)")
PY
