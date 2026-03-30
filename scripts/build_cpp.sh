#!/bin/bash
# Build libandb_retrieval.so (Knowhere HNSW retrieval library)
# Member B task: Linux build script for C++ retrieval plane
#
# Usage:
#   ./scripts/build_cpp.sh              # CPU-only build
#   ANDB_WITH_GPU=ON ./scripts/build_cpp.sh  # GPU-enabled build

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CPP_DIR="$PROJECT_ROOT/cpp"
BUILD_DIR="$CPP_DIR/build"

# Build options
ANDB_WITH_GPU="${ANDB_WITH_GPU:-OFF}"
CMAKE_CUDA_ARCHITECTURES="${CMAKE_CUDA_ARCHITECTURES:-70;75;80;86}"
BUILD_TYPE="${BUILD_TYPE:-Release}"
NUM_JOBS="${NUM_JOBS:-$(nproc 2>/dev/null || echo 4)}"

echo "========================================="
echo "Building libandb_retrieval.so"
echo "========================================="
echo "Project root: $PROJECT_ROOT"
echo "C++ source:   $CPP_DIR"
echo "Build dir:    $BUILD_DIR"
echo "GPU support:  $ANDB_WITH_GPU"
if [ "$ANDB_WITH_GPU" = "ON" ]; then
    echo "CUDA archs:   $CMAKE_CUDA_ARCHITECTURES"
fi
echo "Build type:   $BUILD_TYPE"
echo "Parallel jobs: $NUM_JOBS"
echo "========================================="

# Create build directory
mkdir -p "$BUILD_DIR"
cd "$BUILD_DIR"

# Configure with CMake
CMAKE_ARGS=(
    -DCMAKE_BUILD_TYPE="$BUILD_TYPE"
    -DANDB_WITH_GPU="$ANDB_WITH_GPU"
    -DANDB_WITH_TESTS=OFF
    -DANDB_KNOWHERE_FAISS=OFF
    -DANDB_KNOWHERE_DISKANN=OFF
)

if [ "$ANDB_WITH_GPU" = "ON" ]; then
    CMAKE_ARGS+=(-DCMAKE_CUDA_ARCHITECTURES="$CMAKE_CUDA_ARCHITECTURES")
    
    # Check for CUDA toolkit
    if ! command -v nvcc &> /dev/null; then
        echo "ERROR: nvcc not found. CUDA Toolkit is required for GPU build."
        echo "Install CUDA Toolkit or set ANDB_WITH_GPU=OFF for CPU-only build."
        exit 1
    fi
    
    NVCC_VERSION=$(nvcc --version | grep "release" | sed 's/.*release \([0-9.]*\).*/\1/')
    echo "Found CUDA Toolkit: $NVCC_VERSION"
fi

echo ""
echo "Running CMake configuration..."
cmake .. "${CMAKE_ARGS[@]}"

echo ""
echo "Building with $NUM_JOBS parallel jobs..."
make -j"$NUM_JOBS"

echo ""
echo "========================================="
echo "Build completed successfully!"
echo "========================================="
echo "Output library: $BUILD_DIR/libandb_retrieval.so"
echo ""
echo "To use in Go:"
echo "  export LD_LIBRARY_PATH=$BUILD_DIR:\$LD_LIBRARY_PATH"
echo "  go build -tags retrieval ./..."
echo ""

# Verify output
if [ -f "$BUILD_DIR/libandb_retrieval.so" ]; then
    echo "✓ libandb_retrieval.so created successfully"
    ls -lh "$BUILD_DIR/libandb_retrieval.so"
else
    echo "✗ ERROR: libandb_retrieval.so not found"
    exit 1
fi
