#!/bin/bash
# Test build script for Member B CUDA implementations
# Tests compilation of all CUDA embedding providers

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "========================================="
echo "Member B CUDA Build Test"
echo "========================================="
echo "Project root: $PROJECT_ROOT"
echo ""

# Check CUDA availability
echo "Checking CUDA..."
if command -v nvcc &> /dev/null; then
    NVCC_VERSION=$(nvcc --version | grep "release" | sed 's/.*release \([0-9.]*\).*/\1/')
    echo "✓ CUDA Toolkit found: $NVCC_VERSION"
else
    echo "✗ CUDA Toolkit not found"
    echo "  Install CUDA or build without -tags cuda"
fi
echo ""

# Test 1: Build embedding package with CUDA tags
echo "========================================="
echo "Test 1: Build embedding package (CUDA)"
echo "========================================="
cd "$PROJECT_ROOT"

echo "Building: go build -tags cuda ./src/internal/dataplane/embedding/"
if go build -tags cuda ./src/internal/dataplane/embedding/; then
    echo "✓ Embedding package builds successfully with CUDA tags"
else
    echo "✗ Embedding package build failed"
    exit 1
fi
echo ""

# Test 2: Check for CUDA provider implementations
echo "========================================="
echo "Test 2: Verify CUDA implementations"
echo "========================================="

check_impl() {
    local file=$1
    local provider=$2
    if grep -q "ErrProviderUnavailable" "$file" | head -1; then
        echo "⚠ $provider: May contain stubs (check manually)"
    else
        echo "✓ $provider: Implementation present"
    fi
}

check_impl "src/internal/dataplane/embedding/onnx_cuda.go" "ONNX CUDA"
check_impl "src/internal/dataplane/embedding/gguf_cuda.go" "GGUF CUDA"
check_impl "src/internal/dataplane/embedding/tensorrt_cuda.go" "TensorRT CUDA"
echo ""

# Test 3: Line count verification
echo "========================================="
echo "Test 3: Code metrics"
echo "========================================="
wc -l src/internal/dataplane/embedding/*cuda.go | tail -1
echo ""

# Test 4: Build scripts verification
echo "========================================="
echo "Test 4: Build scripts"
echo "========================================="
if [ -x "scripts/build_cpp.sh" ]; then
    echo "✓ build_cpp.sh is executable"
else
    echo "✗ build_cpp.sh not executable"
fi

if [ -x "scripts/build_embeddings.sh" ]; then
    echo "✓ build_embeddings.sh is executable"
else
    echo "✗ build_embeddings.sh not executable"
fi
echo ""

echo "========================================="
echo "Build test completed!"
echo "========================================="
echo ""
echo "Next steps:"
echo "  1. Run: ./scripts/build_cpp.sh"
echo "  2. Run: ./scripts/build_embeddings.sh"
echo "  3. Test: go test -tags cuda ./src/internal/dataplane/embedding/ -v"
echo ""
