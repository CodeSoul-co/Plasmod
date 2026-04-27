#!/bin/bash
# =============================================================================
# Quick environment verification
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/.env"

echo "========================================="
echo "Plasmod Environment Verification"
echo "========================================="
echo "Root:     ${PLASMOD_TEST_ENV}"
echo "Project:  ${PLASMOD_ROOT}"

# Check model file
if [ -f "${PLASMOD_EMBEDDER_MODEL_PATH}" ]; then
    echo "[OK] ONNX model: ${PLASMOD_EMBEDDER_MODEL_PATH}"
    echo "     Size: $(du -h ${PLASMOD_EMBEDDER_MODEL_PATH} | cut -f1)"
else
    echo "[FAIL] ONNX model not found: ${PLASMOD_EMBEDDER_MODEL_PATH}"
    exit 1
fi

# Check ONNX Runtime library
if [ -f "${ONNXRUNTIME_LIB_PATH}" ]; then
    echo "[OK] ONNX Runtime: ${ONNXRUNTIME_LIB_PATH}"
else
    echo "[WARN] ONNX Runtime library not found: ${ONNXRUNTIME_LIB_PATH}"
fi

# Check Python SDK
SDK_PATH="${PLASMOD_ROOT}/sdk/python"
if python3 -c "import sys; sys.path.insert(0, '${SDK_PATH}'); from plasmod_sdk import PlasmodClient; print('OK')" 2>/dev/null; then
    echo "[OK] Python SDK: plasmod_sdk.PlasmodClient"
else
    echo "[FAIL] Python SDK not properly installed"
fi

# Check Go server source
if [ -f "${PLASMOD_ROOT}/src/cmd/server/main.go" ]; then
    echo "[OK] Go server source: found"
else
    echo "[FAIL] Go server source not found"
fi

# Check directories
for dir in models data logs; do
    if [ -d "${PLASMOD_TEST_ENV}/${dir}" ]; then
        echo "[OK] ${dir}/ directory exists"
    fi
done

echo ""
echo "Environment ready. To start server:"
echo "  bash ${SCRIPT_DIR}/start_server.sh"
echo ""
echo "To load env in Python:"
echo "  import os"
echo "  os.environ['PLASMOD_EMBEDDER_MODEL_PATH'] = '${PLASMOD_EMBEDDER_MODEL_PATH}'"
