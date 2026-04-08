#!/usr/bin/env bash
set -euo pipefail

# Member A GPU check:
# - Bring up ANDB with docker-compose.gpu.yml overlay
# - Verify container can see NVIDIA runtime/device
#
# Usage:
#   bash scripts/e2e/member_a_gpu_check.sh
# Env:
#   COMPOSE_FILES (default "docker-compose.yml docker-compose.gpu.yml")
#   GPU_SERVICE (default andb)

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"
source "${REPO_ROOT}/scripts/e2e/member_a_common.sh"
ma_enable_failure_diagnostics "member-a-gpu"
COMPOSE_FILES="${COMPOSE_FILES:-docker-compose.yml docker-compose.gpu.yml}"
GPU_SERVICE="${GPU_SERVICE:-andb}"

echo "[member-a-gpu] start stack with GPU overlay..."
ma_compose up -d "${GPU_SERVICE}"

echo "[member-a-gpu] probing GPU visibility in container..."
ma_compose exec -T "${GPU_SERVICE}" /bin/sh -lc '
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
