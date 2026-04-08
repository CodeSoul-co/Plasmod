#!/usr/bin/env bash
set -euo pipefail

# Member A GPU check:
# - Bring up ANDB with docker-compose.gpu.yml overlay
# - Verify container can see NVIDIA runtime/device
#
# Usage:
#   bash scripts/e2e/member_a_gpu_check.sh

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"

echo "[member-a-gpu] start stack with GPU overlay..."
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d andb

echo "[member-a-gpu] probing GPU visibility in container..."
docker compose -f docker-compose.yml -f docker-compose.gpu.yml exec -T andb /bin/sh -lc '
if command -v nvidia-smi >/dev/null 2>&1; then
  nvidia-smi
  exit 0
fi

if ls /dev/nvidia* >/dev/null 2>&1; then
  echo "nvidia device nodes are present:"
  ls -l /dev/nvidia*
  exit 0
fi

echo "ERROR: no nvidia-smi and no /dev/nvidia* found in container"
exit 1
'

echo "[member-a-gpu] OK"
