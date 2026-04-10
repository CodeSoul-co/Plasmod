#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo "[check] run prod visibility tests"
if command -v go >/dev/null 2>&1; then
  APP_MODE=prod go test ./src/internal/access -run 'TestWrapVisibility_Prod_StripsDebugFields|TestGateway_DebugEndpoint_ModeGated' -count=1
else
  docker run --rm -v "$PWD:/src" -w /src cogdb:test-builder /bin/sh -lc \
    "APP_MODE=prod /usr/local/go/bin/go test ./src/internal/access -run 'TestWrapVisibility_Prod_StripsDebugFields|TestGateway_DebugEndpoint_ModeGated' -count=1"
fi

echo "[check] static scan: debug route must stay mode-gated"
if ! python3 - <<'PY'
from pathlib import Path
text = Path("src/internal/access/gateway.go").read_text(encoding="utf-8")
if "if isTestMode() {" not in text:
    raise SystemExit(1)
PY
then
  echo "ERROR: debug routes are not gated by test mode" >&2
  exit 1
fi

echo "[check] static scan: forbidden debug symbols in prod-facing sdk"
if python3 - <<'PY'
from pathlib import Path
text = Path("sdk/nodejs/src/index.js").read_text(encoding="utf-8").lower()
bad = ["_debug", "raw_response", "raw_request", "chain_traces"]
raise SystemExit(0 if any(x in text for x in bad) else 1)
PY
then
  echo "ERROR: potential debug leakage symbols in sdk/nodejs/src/index.js" >&2
  exit 1
fi

echo "[check] prod visibility guard passed"
