#!/usr/bin/env bash
# 完整的 15 个实验批量运行脚本（G1-G4，包括 old/new/plugin 模式）
# 使用真实的 deep1B 数据集（10M base + 10K query）
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_ENV="$(cd "${SCRIPT_DIR}/.." && pwd)"
PROJECT_ROOT="$(cd "${TEST_ENV}/.." && pwd)"

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULT_DIR="${TEST_ENV}/results/four_group"
mkdir -p "${RESULT_DIR}"

echo "========================================="
echo "Plasmod Layer 1 完整基准测试（15 个实验）"
echo "========================================="
echo "数据集: deep1B (10M base + 10K query)"
echo "实验组: G1-G4 (15 个实验)"
echo "  - G1: old, new (2 个)"
echo "  - G2: old, L2NormSort, VisitedSharing, raw (4 个)"
echo "  - G3: old, L2NormSort, VisitedSharing, raw (4 个)"
echo "  - G4: old, L2NormSort, VisitedSharing, raw (4 个)"
echo "结果目录: ${RESULT_DIR}"
echo "时间戳: ${TIMESTAMP}"
echo "========================================="
echo ""

# 完整的实验参数
INDEXED_DATASET="${TEST_ENV}/data/deep/base.10M.fbin"
QUERY_DATASET="${TEST_ENV}/data/deep/query.public.10K.fbin"
GROUNDTRUTH="${TEST_ENV}/data/deep/groundtruth.public.10K.ibin"
INDEXED_COUNT=10000000
NUM_QUERIES=10000
OLD_QUERIES=200
OLD_TIMEOUT=300
TOPK=10

echo "实验参数:"
echo "  --indexed-dataset=${INDEXED_DATASET}"
echo "  --query-dataset=${QUERY_DATASET}"
echo "  --groundtruth=${GROUNDTRUTH}"
echo "  --indexed-count=${INDEXED_COUNT}"
echo "  --num-queries=${NUM_QUERIES} (new/raw/plugin 模式)"
echo "  --old-queries=${OLD_QUERIES} (old 模式，避免超时)"
echo "  --old-timeout=${OLD_TIMEOUT}s"
echo "  --topk=${TOPK}"
echo ""

# 检查 Plasmod 服务器是否运行（G4 需要）
echo "检查 Plasmod 服务器状态..."
if pgrep -f "bin/plasmod" > /dev/null; then
    echo "✓ Plasmod 服务器已运行"
    SERVER_RUNNING=true
else
    echo "✗ Plasmod 服务器未运行，启动中..."
    cd "${PROJECT_ROOT}"
    nohup ./bin/plasmod > "${TEST_ENV}/results/plasmod_server_${TIMESTAMP}.log" 2>&1 &
    SERVER_PID=$!
    echo "  服务器 PID: ${SERVER_PID}"
    echo "  等待服务器启动..."
    sleep 5
    if pgrep -f "bin/plasmod" > /dev/null; then
        echo "✓ 服务器启动成功"
        SERVER_RUNNING=true
    else
        echo "✗ 服务器启动失败，G4 实验将被跳过"
        SERVER_RUNNING=false
    fi
fi
echo ""

# 运行完整的 15 个实验
echo "开始运行实验..."
cd "${TEST_ENV}"

if [ "$SERVER_RUNNING" = true ]; then
    # 包含 G4 的完整实验
    python3 scripts/benchmark_standalone.py \
      --indexed-dataset="${INDEXED_DATASET}" \
      --query-dataset="${QUERY_DATASET}" \
      --groundtruth="${GROUNDTRUTH}" \
      --indexed-count=${INDEXED_COUNT} \
      --num-queries=${NUM_QUERIES} \
      --old-queries=${OLD_QUERIES} \
      --old-timeout=${OLD_TIMEOUT} \
      --topk=${TOPK}
else
    # 跳过 G4
    python3 scripts/benchmark_standalone.py \
      --indexed-dataset="${INDEXED_DATASET}" \
      --query-dataset="${QUERY_DATASET}" \
      --groundtruth="${GROUNDTRUTH}" \
      --indexed-count=${INDEXED_COUNT} \
      --num-queries=${NUM_QUERIES} \
      --old-queries=${OLD_QUERIES} \
      --old-timeout=${OLD_TIMEOUT} \
      --topk=${TOPK} \
      --skip-http
fi

echo ""
echo "========================================="
echo "实验完成！"
echo "========================================="
echo "结果文件: ${RESULT_DIR}/deep_bench_*.json"
echo ""
echo "查看最新结果:"
echo "  ls -lt ${RESULT_DIR}/ | head -5"
echo ""
