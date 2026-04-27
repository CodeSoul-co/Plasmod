#!/bin/bash
# =============================================================================
# Plasmod server startup script
# Uses ONNX embedder, CPU-only mode
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/.env"

LOG_FILE="${PLASMOD_LOG_DIR}/server_$(date +%Y%m%d_%H%M%S).log"

echo "========================================="
echo "Plasmod ONNX Server (CPU mode)"
echo "========================================="
echo "Model:     ${PLASMOD_EMBEDDER_MODEL_PATH}"
echo "Storage:   ${PLASMOD_STORAGE:-disk (default)}"
echo "Data dir:  ${PLASMOD_DATA_DIR}"
echo "Log:       ${LOG_FILE}"
echo "========================================="

cd "${PLASMOD_ROOT}"

# Set library path for CGO HNSW
export DYLD_LIBRARY_PATH="${PLASMOD_ROOT}/cpp/build/vendor:${DYLD_LIBRARY_PATH}"

PLASMOD_EMBEDDER=${PLASMOD_EMBEDDER} \
PLASMOD_EMBEDDER_MODEL_PATH=${PLASMOD_EMBEDDER_MODEL_PATH} \
PLASMOD_EMBEDDER_DIM=${PLASMOD_EMBEDDER_DIM} \
ONNXRUNTIME_LIB_PATH=${ONNXRUNTIME_LIB_PATH} \
PLASMOD_STORAGE=disk \
PLASMOD_DATA_DIR=${PLASMOD_DATA_DIR} \
APP_MODE=${APP_MODE} \
"${PLASMOD_ROOT}/bin/plasmod" \
  > "${LOG_FILE}" 2>&1 &

SERVER_PID=$!
echo $SERVER_PID > "${SCRIPT_DIR}/.server.pid"

echo "Server started (PID: $SERVER_PID)"
echo "Log: ${LOG_FILE}"

sleep 5

if kill -0 $SERVER_PID 2>/dev/null; then
    echo "Server is running on http://127.0.0.1:8080"
    curl -s http://127.0.0.1:8080/healthz
    echo ""
else
    echo "ERROR: Server failed to start"
    echo "Log output:"
    cat "${LOG_FILE}"
    exit 1
fi
