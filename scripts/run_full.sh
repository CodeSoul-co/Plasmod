#!/usr/bin/env bash
# Plasmod core project: start MinIO + Plasmod dev server together.
# Usage: bash scripts/run_full.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLASMOD_DIR="${SCRIPT_DIR}/.."
MINIO_DATA_DIR="${PLASMOD_DIR}/../minio/data"
MINIO_PID="${PLASMOD_DIR}/../minio/.minio.pid"

echo "=== Plasmod Full Stack (MinIO + Plasmod) ==="

# ── 1. MinIO (local, port 9000/9001) ─────────────────────────────────────────
if curl -s --connect-timeout 2 http://127.0.0.1:9000/minio/health/live > /dev/null 2>&1; then
  echo "MinIO: already running (port 9000)"
else
  echo "Starting MinIO on port 9000..."
  mkdir -p "${MINIO_DATA_DIR}"
  nohup minio server "${MINIO_DATA_DIR}" \
    --address ":9000" --console-address ":9001" \
    > /tmp/minio.log 2>&1 &
  echo $! > "${MINIO_PID}"
  for i in $(seq 1 10); do
    sleep 1
    curl -s --connect-timeout 2 http://127.0.0.1:9000/minio/health/live > /dev/null 2>&1 && break
  done
  mc alias set myminio http://127.0.0.1:9000 minioadmin minioadmin 2>/dev/null || true
  mc mb myminio/plasmod-experiments 2>/dev/null || true
  echo "MinIO: started"
fi

# ── 2. Plasmod dev server ─────────────────────────────────────────────────────
cd "${PLASMOD_DIR}"
export PLASMOD_STORAGE=disk
export PLASMOD_HTTP_ADDR=127.0.0.1:8080
export S3_ENDPOINT=127.0.0.1:9000
export S3_ACCESS_KEY=minioadmin
export S3_SECRET_KEY=minioadmin
export S3_BUCKET=plasmod-experiments
export S3_SECURE=false
export S3_REGION=us-east-1

echo "Starting Plasmod..."
make dev &
echo "Plasmod: started (PID: $!)"

echo ""
echo "=== Port Status ==="
for entry in "9000:MinIO" "9001:MinIO-Console" "8080:Plasmod"; do
  port="${entry%%:*}"
  name="${entry##*:}"
  curl -s --connect-timeout 3 http://127.0.0.1:${port}/ > /dev/null 2>&1 && echo "$port ($name): UP" || echo "$port ($name): DOWN"
done
echo ""
echo "S3 endpoint: 127.0.0.1:9000 | Bucket: plasmod-experiments"
