#!/usr/bin/env bash
# Build the exact artifacts used by plasmod_test_env experiments.
#
# Run this before every experiment after changing Go or C++ code.
# It rebuilds:
#   1. cpp/build/libplasmod_retrieval.dylib  (Knowhere/HNSW retrieval)
#   2. bin/plasmod                          (server binary with retrieval tag)
#   3. plasmod_test_env/bin/plasmod          (copy for bookkeeping)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_ENV="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${TEST_ENV}/.env"

cd "${PLASMOD_ROOT}"

JOBS="${JOBS:-}"
if [[ -z "${JOBS}" ]]; then
  if command -v nproc >/dev/null 2>&1; then
    JOBS="$(nproc)"
  else
    JOBS="$(sysctl -n hw.ncpu 2>/dev/null || echo 4)"
  fi
fi

LIBOMP_PREFIX="${LIBOMP_PREFIX:-}"
if [[ -z "${LIBOMP_PREFIX}" ]] && command -v brew >/dev/null 2>&1; then
  LIBOMP_PREFIX="$(brew --prefix libomp 2>/dev/null || true)"
fi
LIBOMP_PREFIX="${LIBOMP_PREFIX:-/opt/homebrew/opt/libomp}"
BUILD_TYPE="${BUILD_TYPE:-Release}"

echo "========================================="
echo "Build Plasmod Experiment Artifacts"
echo "========================================="
echo "Root:       ${PLASMOD_ROOT}"
echo "Test env:   ${PLASMOD_TEST_ENV}"
echo "Jobs:       ${JOBS}"
echo "libomp:     ${LIBOMP_PREFIX}"
echo "Build type: ${BUILD_TYPE}"
echo "========================================="

if [[ -f "${TEST_ENV}/.server.pid" ]]; then
  PID="$(cat "${TEST_ENV}/.server.pid" || true)"
  if [[ -n "${PID}" ]] && kill -0 "${PID}" 2>/dev/null; then
    echo "[build] stopping running server PID=${PID}"
    kill "${PID}" || true
    sleep 1
  fi
  rm -f "${TEST_ENV}/.server.pid"
fi

echo
echo "[build] C++ retrieval library"
cmake -S cpp -B cpp/build \
  -DCMAKE_BUILD_TYPE="${BUILD_TYPE}" \
  -DANDB_WITH_KNOWHERE=ON \
  -DOpenMP_C_FLAGS="-Xclang -fopenmp -I${LIBOMP_PREFIX}/include" \
  -DOpenMP_omp_LIBRARY="${LIBOMP_PREFIX}/lib/libomp.dylib" \
  -DCMAKE_PREFIX_PATH="/opt/homebrew;${LIBOMP_PREFIX}"
cmake --build cpp/build --parallel "${JOBS}"

if [[ ! -f cpp/build/libplasmod_retrieval.dylib ]]; then
  echo "ERROR: cpp/build/libplasmod_retrieval.dylib was not produced" >&2
  exit 1
fi
if [[ ! -f cpp/build/vendor/libknowhere.dylib ]]; then
  echo "ERROR: cpp/build/vendor/libknowhere.dylib was not produced" >&2
  exit 1
fi

echo
echo "[build] Go server binary with retrieval tag"
CGO_LDFLAGS="-L${PLASMOD_ROOT}/cpp/build -lplasmod_retrieval -Wl,-rpath,${PLASMOD_ROOT}/cpp/build" \
  go build -tags retrieval -o bin/plasmod ./src/cmd/server

echo
echo "[build] Go benchmark binary with retrieval tag"
CGO_LDFLAGS="-L${PLASMOD_ROOT}/cpp/build -lplasmod_retrieval -Wl,-rpath,${PLASMOD_ROOT}/cpp/build" \
  go build -tags retrieval -o bin/plasmod_bench ./src/cmd/benchmark

mkdir -p "${TEST_ENV}/bin"
cp bin/plasmod "${TEST_ENV}/bin/plasmod"
cp bin/plasmod_bench "${TEST_ENV}/bin/plasmod_bench"

echo
echo "[build] artifacts"
ls -lh \
  bin/plasmod \
  "${TEST_ENV}/bin/plasmod" \
  cpp/build/libplasmod_retrieval.dylib \
  cpp/build/vendor/libknowhere.dylib

echo
echo "Done. Start experiments with:"
echo "  bash ${TEST_ENV}/start_server.sh"
