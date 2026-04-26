#!/usr/bin/env bash
# setup_env.sh — Create a Python virtual environment and install all dependencies
# Usage: bash scripts/setup_env.sh
#
# Requires: Python 3.11+ (install via asdf or apt — see README Environment Setup)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VENV_DIR="$REPO_ROOT/.venv"

# ── Locate a suitable Python interpreter (3.11+) ─────────────────────────────
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
    echo "ERROR: Python 3.11+ not found. Install it first:" >&2
    echo "  Ubuntu:  sudo apt install python3.11 python3.11-venv python3.11-dev" >&2
    echo "  asdf:    asdf install python 3.11.9" >&2
    exit 1
fi

echo "==> Using $($PYTHON_BIN --version)"

# ── Create virtual environment ────────────────────────────────────────────────
if [ -d "$VENV_DIR" ]; then
    echo "==> .venv already exists, skipping creation (remove it with: rm -rf .venv)"
else
    echo "==> Creating virtual environment: $VENV_DIR"
    "$PYTHON_BIN" -m venv "$VENV_DIR"
fi

# ── Activate and install dependencies ────────────────────────────────────────
# shellcheck source=/dev/null
source "$VENV_DIR/bin/activate"

echo "==> Upgrading pip"
pip install --upgrade pip -q

echo "==> Installing runtime dependencies (requirements.txt)"
pip install -r "$REPO_ROOT/requirements.txt"

echo "==> Installing Python SDK (editable)"
pip install -e "$REPO_ROOT/sdk/python"

echo ""
echo "Done. Activate the environment with:"
echo "    source .venv/bin/activate"
echo ""
echo "  Verify:"
echo "    python -c \"import plasmod_sdk; print('SDK ready')\""
