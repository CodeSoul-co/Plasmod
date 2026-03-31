#!/bin/bash
# Build go-llama.cpp with CUDA (CUBLAS) support for GGUF embeddings.
# Member B task: GGUF CUDA embedding provider.
#
# Usage:
#   ./scripts/build_embeddings.sh                         # CUBLAS build (default)
#   BUILD_TYPE=openblas ./scripts/build_embeddings.sh     # CPU-only build
#
# Environment variables:
#   LLAMA_DIR    - Path to go-llama.cpp checkout (default: /tmp/go-llama-cpp)
#   BUILD_TYPE   - cublas | openblas (default: cublas)
#   NUM_JOBS     - Parallel make jobs (default: nproc)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

LLAMA_DIR="${LLAMA_DIR:-/tmp/go-llama-cpp}"
BUILD_TYPE="${BUILD_TYPE:-cublas}"
NUM_JOBS="${NUM_JOBS:-$(nproc 2>/dev/null || echo 4)}"
CUDA_LIB="${CUDA_LIB:-/usr/lib/x86_64-linux-gnu}"

echo "========================================="
echo "Building go-llama.cpp ($BUILD_TYPE)"
echo "========================================="
echo "Target directory: $LLAMA_DIR"
echo "Build type:       $BUILD_TYPE"
echo "Parallel jobs:    $NUM_JOBS"
echo "========================================="

# ── 1. Clone if not present ────────────────────────────────────────────────
if [ ! -d "$LLAMA_DIR" ]; then
    echo ""
    echo "Cloning go-llama.cpp ..."
    git clone --depth=1 --recurse-submodules \
        https://github.com/go-skynet/go-llama.cpp "$LLAMA_DIR"
    # Pin to the commit referenced in go.mod
    cd "$LLAMA_DIR"
    git fetch --depth=1 origin 6a8041ef6b46
    git checkout 6a8041ef6b46
    git submodule update --init --recursive
else
    echo "Using existing repository at $LLAMA_DIR"
    cd "$LLAMA_DIR"
fi

cd "$LLAMA_DIR"

# ── 2. Apply CUDA patch exactly once ──────────────────────────────────────
# The Makefile's `prepare` target runs `patch -p1 < patches/1902-cuda.patch`.
# If we already applied it manually (or from a previous build run), skip it.
if [ ! -f prepare ]; then
    echo ""
    echo "Applying 1902-cuda.patch ..."
    cd llama.cpp
    patch -p1 < ../patches/1902-cuda.patch
    cd ..
    touch prepare          # sentinel: tells make not to re-apply
    echo "Patch applied."
else
    echo "Patch already applied (prepare sentinel exists), skipping."
fi

# ── 3. Clean object files from previous runs ──────────────────────────────
echo ""
echo "Cleaning stale objects ..."
rm -f *.o *.a
rm -rf build

# ── 4. Build ───────────────────────────────────────────────────────────────
echo ""
echo "Building libbinding.a (BUILD_TYPE=$BUILD_TYPE) ..."

if [ "$BUILD_TYPE" = "cublas" ]; then
    # Check CUDA libraries are accessible
    if [ ! -f "$CUDA_LIB/libcudart.so" ] && [ ! -f "$CUDA_LIB/libcudart.so.11.0" ]; then
        echo "WARNING: libcudart not found in $CUDA_LIB"
        echo "         Set CUDA_LIB to the directory containing libcudart.so"
    fi
    CGO_LDFLAGS="-lcublas -lcudart -L$CUDA_LIB" \
        BUILD_TYPE=cublas LLAMA_CUBLAS=ON \
        make libbinding.a -j"$NUM_JOBS"
else
    make libbinding.a -j"$NUM_JOBS"
fi

# ── 5. Verify ──────────────────────────────────────────────────────────────
echo ""
echo "========================================="
if [ -f "$LLAMA_DIR/libbinding.a" ]; then
    echo "Build completed successfully!"
    echo "========================================="
    echo "Output: $LLAMA_DIR/libbinding.a"
    ls -lh "$LLAMA_DIR/libbinding.a"
    if [ "$BUILD_TYPE" = "cublas" ]; then
        echo ""
        echo "CUDA symbols check:"
        nm "$LLAMA_DIR/libbinding.a" 2>/dev/null | grep -c "ggml_cuda\|cublas" || true
        echo "  (non-zero = CUBLAS symbols present)"
    fi
    echo ""
    echo "To build Go code with GGUF CUDA:"
    echo "  export LIBRARY_PATH=$LLAMA_DIR"
    echo "  export CGO_LDFLAGS=\"-lcublas -lcudart -L$CUDA_LIB -L$LLAMA_DIR\""
    echo "  go build -tags cuda ./src/internal/..."
else
    echo "ERROR: libbinding.a not found after build!"
    echo "========================================="
    exit 1
fi
