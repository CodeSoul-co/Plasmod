#!/usr/bin/env bash
set -euo pipefail

# Strict Task 4 verification:
# 1) Start MinIO + ANDB stack
# 2) Run fixture-driven capture (API-level E2E)
# 3) Run S3 cold-tier roundtrip unit tests in builder container:
#    - ArchiveMemory writes S3 cold memory + embedding
#    - GetMemoryActivated deletes S3 embedding on reactivation
#
# Usage:
#   bash scripts/e2e/member_a_task4_strict.sh
# Env:
#   COMPOSE_FILES (optional compose file list)
#   COMPOSE_SERVICES (default "minio minio-init andb")
#   HEALTHZ_RETRIES / HEALTHZ_INTERVAL_SEC
#   CAPTURE_HTTP_TIMEOUT (default 180)
#   TEST_DOCKER_NETWORK (default: auto from running andb container; override if needed)
#   TEST_BUILDER_IMAGE (default cogdb:test-builder)
#   TEST_S3_* for container test env

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"
source "${REPO_ROOT}/scripts/e2e/member_a_common.sh"
ma_enable_failure_diagnostics "task4-strict"

ANDB_BASE_URL="${ANDB_BASE_URL:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/out/member_a_task4_strict}"
# Same defaults as member_a_verify / member_a_capture (integration_tests fixtures).
STRICT_FIXTURES="${STRICT_FIXTURES:-${REPO_ROOT}/integration_tests/fixtures/member_a}"
COMPOSE_SERVICES="${COMPOSE_SERVICES:-minio minio-init andb}"
HEALTHZ_RETRIES="${HEALTHZ_RETRIES:-60}"
HEALTHZ_INTERVAL_SEC="${HEALTHZ_INTERVAL_SEC:-2}"
CAPTURE_HTTP_TIMEOUT="${CAPTURE_HTTP_TIMEOUT:-180}"
TEST_DOCKER_NETWORK="${TEST_DOCKER_NETWORK:-}"
TEST_BUILDER_IMAGE="${TEST_BUILDER_IMAGE:-cogdb:test-builder}"
TEST_S3_ENDPOINT="${TEST_S3_ENDPOINT:-minio:9000}"
TEST_S3_ACCESS_KEY="${TEST_S3_ACCESS_KEY:-minioadmin}"
TEST_S3_SECRET_KEY="${TEST_S3_SECRET_KEY:-minioadmin}"
TEST_S3_BUCKET="${TEST_S3_BUCKET:-andb-integration}"
TEST_S3_SECURE="${TEST_S3_SECURE:-false}"
TEST_S3_REGION="${TEST_S3_REGION:-us-east-1}"
TEST_S3_PREFIX="${TEST_S3_PREFIX:-andb/task4_strict}"

echo "[task4-strict] start docker compose stack..."
ma_compose up -d ${COMPOSE_SERVICES}

echo "[task4-strict] wait /healthz..."
ma_wait_healthz "${ANDB_BASE_URL}" "${HEALTHZ_RETRIES}" "${HEALTHZ_INTERVAL_SEC}" "task4-strict"

if [[ -z "${TEST_DOCKER_NETWORK}" ]]; then
  TEST_DOCKER_NETWORK="$(ma_compose_default_network)"
fi
echo "[task4-strict] docker network for S3 roundtrip tests: ${TEST_DOCKER_NETWORK}"

echo "[task4-strict] run fixture capture..."
ANDB_BASE_URL="${ANDB_BASE_URL}" python3 scripts/e2e/member_a_capture.py \
  --fixtures "${STRICT_FIXTURES}" \
  --http-timeout "${CAPTURE_HTTP_TIMEOUT}" \
  --out-dir "${OUT_DIR}"

echo "[task4-strict] build test builder image..."
docker build \
  --build-arg GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  --build-arg GOSUMDB="${GOSUMDB:-sum.golang.google.cn}" \
  --target builder \
  -t "${TEST_BUILDER_IMAGE}" .

echo "[task4-strict] run S3 cold-tier roundtrip tests..."
docker run --rm \
  --network "${TEST_DOCKER_NETWORK}" \
  -v "${PWD}:/src" \
  -w /src \
  -e S3_ENDPOINT="${TEST_S3_ENDPOINT}" \
  -e S3_ACCESS_KEY="${TEST_S3_ACCESS_KEY}" \
  -e S3_SECRET_KEY="${TEST_S3_SECRET_KEY}" \
  -e S3_BUCKET="${TEST_S3_BUCKET}" \
  -e S3_SECURE="${TEST_S3_SECURE}" \
  -e S3_REGION="${TEST_S3_REGION}" \
  -e S3_PREFIX="${TEST_S3_PREFIX}" \
  "${TEST_BUILDER_IMAGE}" /bin/sh -lc \
  '/usr/local/go/bin/go test ./src/internal/storage -run "TestTieredObjectStore_ArchiveMemory_WritesS3ColdEmbedding|TestTieredObjectStore_GetMemoryActivated_DeletesS3ColdEmbedding" -count=1 -v'

echo "[task4-strict] optional bucket listing:"
echo "  docker compose run --rm --entrypoint /bin/sh minio-init -lc 'mc alias set local http://minio:9000 minioadmin minioadmin >/dev/null && mc ls local/${TEST_S3_BUCKET}/${TEST_S3_PREFIX}/'"
echo "[task4-strict] done."
