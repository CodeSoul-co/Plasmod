#!/usr/bin/env bash
# pre_flight.sh — CogDB integration test environment pre-check.
#
# Verifies all runtime prerequisites before running integration_fixture.py:
#   1. Docker daemon + compose plugin
#   2. llamacpp libbinding.a compiled inside the image
#   3. onnxruntime .so present in the image
#   4. NVIDIA GPU availability (non-fatal if absent — marks mode as CPU-only)
#   5. CUDA inside container (only when GPU detected)
#   6. Server healthz
#   7. APP_MODE=test confirmed via /v1/system/mode
#   8. /v1/debug/echo round-trip (test-only endpoint)
#
# Usage:
#   bash scripts/e2e/pre_flight.sh
# Env:
#   ANDB_BASE_URL  (default http://127.0.0.1:8080)
#   CONTAINER      (default cogdb-andb-1)
#   HEALTHZ_RETRIES / HEALTHZ_INTERVAL_SEC

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ANDB_BASE_URL="${ANDB_BASE_URL:-http://127.0.0.1:8080}"
CONTAINER="${CONTAINER:-cogdb-andb-1}"
HEALTHZ_RETRIES="${HEALTHZ_RETRIES:-60}"
HEALTHZ_INTERVAL_SEC="${HEALTHZ_INTERVAL_SEC:-2}"
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/out/integration_test}"
mkdir -p "${OUT_DIR}"

PASS=0
FAIL=0
WARN=0
GPU_AVAILABLE=false

_pass() { echo "[PASS] $1"; PASS=$((PASS+1)); }
_fail() { echo "[FAIL] $1"; FAIL=$((FAIL+1)); }
_warn() { echo "[WARN] $1"; WARN=$((WARN+1)); }
_info() { echo "[INFO] $1"; }

echo "========================================"
echo " CogDB Pre-Flight Check"
echo " $(date '+%Y-%m-%d %H:%M:%S')"
echo "========================================"

# ── 1. Docker daemon ─────────────────────────────────────────────────────────
echo
echo "── [1/8] Docker daemon ──"
if docker info >/dev/null 2>&1; then
    DOCKER_VER=$(docker --version | awk '{print $3}' | tr -d ',')
    COMPOSE_VER=$(docker compose version --short 2>/dev/null || echo "?")
    _pass "Docker ${DOCKER_VER}, Compose ${COMPOSE_VER}"
else
    _fail "Docker daemon not running"
fi

# ── 2. llamacpp libbinding.a ─────────────────────────────────────────────────
echo
echo "── [2/8] llamacpp libbinding.a inside image ──"
if docker image inspect cogdb:latest >/dev/null 2>&1; then
    if docker run --rm --entrypoint /bin/sh cogdb:latest -c \
        "ls /src/libs/go-llama.cpp/libbinding.a" >/dev/null 2>&1; then
        _pass "libbinding.a present in cogdb:latest"
    else
        # libbinding.a might have been cleaned; check the binary compiles
        if docker run --rm --entrypoint /bin/sh cogdb:latest -c \
            "ls /usr/local/bin/andb-server" >/dev/null 2>&1; then
            _warn "libbinding.a not found (static-linked into binary — OK)"
        else
            _fail "andb-server binary not found in image"
        fi
    fi
else
    _warn "cogdb:latest not built yet — run docker compose build first"
fi

# ── 3. onnxruntime .so ───────────────────────────────────────────────────────
echo
echo "── [3/8] libonnxruntime.so inside image ──"
if docker image inspect cogdb:latest >/dev/null 2>&1; then
    if docker run --rm --entrypoint /bin/sh cogdb:latest -c \
        "ls /usr/local/lib/libonnxruntime.so" >/dev/null 2>&1; then
        ORT_SIZE=$(docker run --rm --entrypoint /bin/sh cogdb:latest -c \
            "du -sh /usr/local/lib/libonnxruntime.so" 2>/dev/null | awk '{print $1}')
        _pass "libonnxruntime.so present (${ORT_SIZE})"
    else
        _fail "libonnxruntime.so NOT found — Dockerfile Phase 0 patch needed"
    fi
else
    _warn "cogdb:latest not built yet — skipping onnxruntime check"
fi

# ── 4. NVIDIA GPU availability (host) ────────────────────────────────────────
echo
echo "── [4/8] NVIDIA GPU (host) ──"
if command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi >/dev/null 2>&1; then
    GPU_AVAILABLE=true
    GPU_INFO=$(nvidia-smi --query-gpu=name,driver_version,memory.total \
        --format=csv,noheader 2>/dev/null | head -3)
    while IFS= read -r line; do
        _pass "GPU: ${line}"
    done <<< "${GPU_INFO}"
    GPU_COUNT=$(echo "${GPU_INFO}" | wc -l)
    _info "Total GPUs: ${GPU_COUNT}"
else
    _warn "nvidia-smi not available — running in CPU-only mode"
    GPU_AVAILABLE=false
fi

# ── 5. CUDA inside container (GPU mode only) ──────────────────────────────────
echo
echo "── [5/8] CUDA inside container ──"
if ${GPU_AVAILABLE} && docker image inspect cogdb:latest >/dev/null 2>&1; then
    if docker run --rm --gpus all --entrypoint /bin/sh cogdb:latest -c \
        "nvidia-smi" >/dev/null 2>&1; then
        CUDA_VER=$(docker run --rm --gpus all --entrypoint /bin/sh cogdb:latest -c \
            "nvidia-smi --query-gpu=driver_version --format=csv,noheader 2>/dev/null | head -1")
        _pass "CUDA accessible inside container (driver ${CUDA_VER})"
    else
        _warn "GPU available on host but not inside container — check --gpus flag / docker runtime"
    fi
elif ! ${GPU_AVAILABLE}; then
    _info "Skipped (CPU-only mode)"
else
    _warn "cogdb:latest not built — skipping CUDA container check"
fi

# ── 6. Server healthz ────────────────────────────────────────────────────────
echo
echo "── [6/8] Server /healthz ──"
_healthz_ok=false
for i in $(seq 1 "${HEALTHZ_RETRIES}"); do
    if curl -fsS "${ANDB_BASE_URL}/healthz" >/dev/null 2>&1; then
        _healthz_ok=true
        break
    fi
    sleep "${HEALTHZ_INTERVAL_SEC}"
done
if ${_healthz_ok}; then
    _pass "/healthz → 200 OK"
else
    _fail "/healthz did not respond after $((HEALTHZ_RETRIES * HEALTHZ_INTERVAL_SEC))s"
fi

# ── 7. APP_MODE=test via /v1/system/mode ─────────────────────────────────────
echo
echo "── [7/8] APP_MODE=test (/v1/system/mode) ──"
MODE_RESP=$(curl -fsS "${ANDB_BASE_URL}/v1/system/mode" 2>/dev/null || echo '{}')
MODE_VAL=$(echo "${MODE_RESP}" | python3 -c \
    "import sys,json; d=json.load(sys.stdin); print(d.get('app_mode', d.get('mode','unknown')))" 2>/dev/null || echo "unknown")
if [ "${MODE_VAL}" = "test" ]; then
    _pass "APP_MODE=test confirmed"
else
    _fail "Expected mode=test, got: ${MODE_VAL} (start with APP_MODE=test)"
fi

# ── 8. /v1/debug/echo (test-only endpoint) ───────────────────────────────────
echo
echo "── [8/8] /v1/debug/echo (test-only) ──"
ECHO_RESP=$(curl -fsS -X POST "${ANDB_BASE_URL}/v1/debug/echo" \
    -H "Content-Type: application/json" \
    -d '{"ping":"pre_flight"}' 2>/dev/null || echo '{}')
ECHO_VAL=$(echo "${ECHO_RESP}" | python3 -c \
    "import sys,json; d=json.load(sys.stdin); e=d.get('echo',d); print(e.get('ping','') if isinstance(e,dict) else '')" 2>/dev/null || echo "")
if [ "${ECHO_VAL}" = "pre_flight" ]; then
    _pass "/v1/debug/echo round-trip OK"
else
    _fail "/v1/debug/echo did not echo back (got: ${ECHO_RESP})"
fi

# ── Summary ──────────────────────────────────────────────────────────────────
echo
echo "========================================"
echo " Summary"
echo "   PASS : ${PASS}"
echo "   WARN : ${WARN}"
echo "   FAIL : ${FAIL}"
echo "   Mode : $(${GPU_AVAILABLE} && echo 'GPU (NVIDIA)' || echo 'CPU-only')"
echo "========================================"

# Write machine-readable summary
python3 -c "
import json, datetime
print(json.dumps({
    'timestamp': datetime.datetime.utcnow().isoformat() + 'Z',
    'pass': ${PASS}, 'warn': ${WARN}, 'fail': ${FAIL},
    'gpu_available': $(${GPU_AVAILABLE} && echo 'True' || echo 'False'),
    'mode': '$(${GPU_AVAILABLE} && echo gpu || echo cpu)'
}, indent=2))
" > "${OUT_DIR}/pre_flight.json"
_info "Report: ${OUT_DIR}/pre_flight.json"

if [ "${FAIL}" -gt 0 ]; then
    echo
    echo "ERROR: ${FAIL} check(s) failed — fix before running integration_fixture.py" >&2
    exit 1
fi
echo
echo "Pre-flight OK — proceed with integration test."
