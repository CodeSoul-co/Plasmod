#!/usr/bin/env bash
set -euo pipefail

# Unified Member A verification entrypoint.
# Steps:
# 1) member_a_verify.sh
# 2) optional member_a_gpu_check.sh (MEMBER_A_RUN_GPU_CHECK=true)
# 3) member_a_task4_strict.sh
#
# Usage:
#   bash scripts/e2e/member_a_all.sh

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"
source "${REPO_ROOT}/scripts/e2e/member_a_common.sh"
ma_enable_failure_diagnostics "member-a-all"

MEMBER_A_RUN_GPU_CHECK="${MEMBER_A_RUN_GPU_CHECK:-false}"

echo "[member-a-all] step1: baseline verify"
bash scripts/e2e/member_a_verify.sh

if [[ "${MEMBER_A_RUN_GPU_CHECK}" == "true" ]]; then
  echo "[member-a-all] step2: gpu check"
  bash scripts/e2e/member_a_gpu_check.sh
else
  echo "[member-a-all] step2: gpu check skipped (set MEMBER_A_RUN_GPU_CHECK=true to enable)"
fi

echo "[member-a-all] step3: strict task4"
bash scripts/e2e/member_a_task4_strict.sh

echo "[member-a-all] done."
