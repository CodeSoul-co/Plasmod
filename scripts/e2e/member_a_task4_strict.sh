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

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"

ANDB_BASE_URL="${ANDB_BASE_URL:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/out/member_a_task4_strict}"
STRICT_FIXTURES="${STRICT_FIXTURES:-${REPO_ROOT}/scripts/e2e/fixtures/member_a_strict}"

echo "[task4-strict] start docker compose stack..."
docker compose up -d minio minio-init andb

echo "[task4-strict] wait /healthz..."
for _ in $(seq 1 60); do
  if curl -fsS "${ANDB_BASE_URL}/healthz" >/dev/null; then
    echo "[task4-strict] healthz OK"
    break
  fi
  sleep 2
done
if ! curl -fsS "${ANDB_BASE_URL}/healthz" >/dev/null; then
  echo "[task4-strict] ERROR: /healthz not ready"
  exit 1
fi

echo "[task4-strict] run fixture capture..."
ANDB_BASE_URL="${ANDB_BASE_URL}" python3 scripts/e2e/member_a_capture.py \
  --fixtures "${STRICT_FIXTURES}" \
  --http-timeout 180 \
  --out-dir "${OUT_DIR}"

echo "[task4-strict] build test builder image..."
docker build --target builder -t cogdb:test-builder .

echo "[task4-strict] run S3 cold-tier roundtrip tests..."
docker run --rm \
  --network cogdb_default \
  -v "${PWD}:/src" \
  -w /src \
  -e S3_ENDPOINT=minio:9000 \
  -e S3_ACCESS_KEY=minioadmin \
  -e S3_SECRET_KEY=minioadmin \
  -e S3_BUCKET=andb-integration \
  -e S3_SECURE=false \
  -e S3_REGION=us-east-1 \
  -e S3_PREFIX=andb/task4_strict \
  cogdb:test-builder /bin/sh -lc \
  '/usr/local/go/bin/go test ./src/internal/storage -run "TestTieredObjectStore_ArchiveMemory_WritesS3ColdEmbedding|TestTieredObjectStore_GetMemoryActivated_DeletesS3ColdEmbedding" -count=1 -v'

echo "[task4-strict] optional bucket listing:"
echo "  docker compose run --rm --entrypoint /bin/sh minio-init -lc 'mc alias set local http://minio:9000 minioadmin minioadmin >/dev/null && mc ls local/andb-integration/andb/task4_strict/'"
echo "[task4-strict] done."
