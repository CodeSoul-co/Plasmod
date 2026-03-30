#!/bin/bash
# Build go-llama.cpp with CUDA support for GGUF embeddings
# Member B task: GGUF CUDA embedding provider
#
# Usage:
#   ./scripts/build_embeddings.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Configuration
LLAMA_DIR="${LLAMA_DIR:-/tmp/go-llama-cpp}"
CUDA_PATH="${CUDA_PATH:-/usr/local/cuda}"
NUM_JOBS="${NUM_JOBS:-$(nproc 2>/dev/null || echo 4)}"

echo "========================================="
echo "Building go-llama.cpp with CUDA"
echo "========================================="
echo "Target directory: $LLAMA_DIR"
echo "CUDA path:        $CUDA_PATH"
echo "Parallel jobs:    $NUM_JOBS"
echo "========================================="

# Check for CUDA toolkit
if ! command -v nvcc &> /dev/null; then
    echo "ERROR: nvcc not found. CUDA Toolkit is required."
    echo "Install CUDA Toolkit or set CUDA_PATH environment variable."
    exit 1
fi

NVCC_VERSION=$(nvcc --version | grep "release" | sed 's/.*release \([0-9.]*\).*/\1/')
echo "Found CUDA Toolkit: $NVCC_VERSION"
echo ""

# Clone repository if not exists
if [ ! -d "$LLAMA_DIR" ]; then
    echo "Cloning go-llama.cpp repository..."
    git clone --recurse-submodules https://github.com/go-skynet/go-llama.cpp "$LLAMA_DIR"
else
    echo "Using existing repository at $LLAMA_DIR"
    cd "$LLAMA_DIR"
    git pull origin master || true
    git submodule update --init --recursive || true
fi

cd "$LLAMA_DIR"

# Clean previous build
echo ""
echo "Cleaning previous build..."
make clean || true

# Build with CUDA support
echo ""
echo "Building with CUDA support (BUILD_TYPE=cublas)..."
BUILD_TYPE=cublas make libbinding.a -j"$NUM_JOBS"

echo ""
echo "========================================="
echo "Build completed successfully!"
echo "========================================="
echo "Output library: $LLAMA_DIR/libbinding.a"
echo ""
echo "To use in Go with CUDA:"
echo "  export LIBRARY_PATH=$LLAMA_DIR"
echo "  export C_INCLUDE_PATH=$LLAMA_DIR"
echo "  export CGO_LDFLAGS=\"-lcublas -lcudart -L$CUDA_PATH/lib64\""
echo "  export LD_LIBRARY_PATH=$CUDA_PATH/lib64:\$LD_LIBRARY_PATH"
echo "  go build -tags cuda ./..."
echo ""

# Verify output
if [ -f "$LLAMA_DIR/libbinding.a" ]; then
    echo "✓ libbinding.a created successfully"
    ls -lh "$LLAMA_DIR/libbinding.a"
else
    echo "✗ ERROR: libbinding.a not found"
    exit 1
fi
