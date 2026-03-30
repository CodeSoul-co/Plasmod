#!/usr/bin/env bash
# setup_env.sh — 创建 Python 虚拟环境并安装全部依赖
# 用法: bash scripts/setup_env.sh
#
# 前提: Python 3.11+（可通过 asdf 或 apt 安装，见 README 开发环境准备章节）

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VENV_DIR="$REPO_ROOT/.venv"

# ── 选择 Python 解释器 ────────────────────────────────────────────────────────
find_python() {
    for bin in python3.11 python3.12 python3; do
        if command -v "$bin" &>/dev/null; then
            local ver
            ver=$("$bin" -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")
            local major minor
            major=$(echo "$ver" | cut -d. -f1)
            minor=$(echo "$ver" | cut -d. -f2)
            if [ "$major" -ge 3 ] && [ "$minor" -ge 11 ]; then
                echo "$bin"
                return
            fi
        fi
    done
    echo ""
}

PYTHON_BIN=$(find_python)

if [ -z "$PYTHON_BIN" ]; then
    echo "ERROR: Python 3.11+ 未找到。请先安装：" >&2
    echo "  Ubuntu:  sudo apt install python3.11 python3.11-venv python3.11-dev" >&2
    echo "  asdf:    asdf install python 3.11.9" >&2
    exit 1
fi

echo "==> 使用 $($PYTHON_BIN --version)"

# ── 创建 venv ─────────────────────────────────────────────────────────────────
if [ -d "$VENV_DIR" ]; then
    echo "==> 已存在 .venv，跳过创建（如需重建请先 rm -rf .venv）"
else
    echo "==> 创建虚拟环境: $VENV_DIR"
    "$PYTHON_BIN" -m venv "$VENV_DIR"
fi

# ── 激活并安装依赖 ─────────────────────────────────────────────────────────────
# shellcheck source=/dev/null
source "$VENV_DIR/bin/activate"

echo "==> 升级 pip"
pip install --upgrade pip -q

echo "==> 安装运行时依赖 (requirements.txt)"
pip install -r "$REPO_ROOT/requirements.txt"

echo "==> 安装 Python SDK（可编辑模式）"
pip install -e "$REPO_ROOT/sdk/python"

echo ""
echo "✓ 环境就绪！启动虚拟环境："
echo "    source .venv/bin/activate"
echo ""
echo "  验证："
echo "    python -c \"import andb_sdk; print('SDK ready')\""
