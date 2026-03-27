cmake_minimum_required(VERSION 3.20)
project(andb_retrieval LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

# =============================================================================
# Build Options
# =============================================================================
option(ANDB_WITH_GPU          "Enable GPU support via Knowhere RAFT (requires CUDA)" OFF)

# GPU path requires CUDA toolkit and is only available on Linux x86_64/aarch64.
if(ANDB_WITH_GPU)
    enable_language(CUDA)
    find_package(CUDAToolkit REQUIRED)
    # Default to sm_70 (Volta) minimum; override with -DCMAKE_CUDA_ARCHITECTURES=...
    if(NOT DEFINED CMAKE_CUDA_ARCHITECTURES)
        set(CMAKE_CUDA_ARCHITECTURES 70 75 80 86)
    endif()
    message(STATUS "GPU support enabled: CUDA ${CUDAToolkit_VERSION}, architectures ${CMAKE_CUDA_ARCHITECTURES}")
else()
    message(STATUS "GPU support disabled (default). Set -DANDB_WITH_GPU=ON on Linux + CUDA to enable.")
endif()
option(ANDB_WITH_TESTS        "Build unit tests"                                     OFF)
option(ANDB_KNOWHERE_FAISS    "Build ann_core with faiss IVF indexes (needs OpenBLAS)" OFF)
option(ANDB_KNOWHERE_DISKANN  "Build ann_core with DiskANN"                           OFF)

# =============================================================================
# Platform Detection
# =============================================================================
if(APPLE)
    if(CMAKE_SYSTEM_PROCESSOR MATCHES "arm64")
        message(STATUS "Platform: macOS Apple Silicon (arm64)")
    else()
        message(STATUS "Platform: macOS x86_64")
    endif()
elseif(UNIX)
    if(CMAKE_SYSTEM_PROCESSOR MATCHES "aarch64")
        message(STATUS "Platform: Linux aarch64")
    else()
        message(STATUS "Platform: Linux x86_64")
    endif()
endif()

# =============================================================================
# vendor/ — third-party source-level dependencies (ANN index engine)
# Location: cpp/vendor/  (include/ src/ engines/ compat/)
# Contains the Knowhere vector index library (source-level, not a package).
# Internal headers use "knowhere/..." paths (upstream convention, unchanged).
# The outer directory is named vendor/ — neutral, conventional, no confusion
# with the cpp/knowhere/include/knowhere/ double-nesting we had before.
# No folly / glog / prometheus / opentelemetry required.
# HNSW always on; IVF/DiskANN/GPU opt-in.
# =============================================================================
set(KNOWHERE_WITH_FAISS   ${ANDB_KNOWHERE_FAISS}   CACHE BOOL "" FORCE)
set(KNOWHERE_WITH_DISKANN ${ANDB_KNOWHERE_DISKANN} CACHE BOOL "" FORCE)
set(KNOWHERE_WITH_GPU     ${ANDB_WITH_GPU}         CACHE BOOL "" FORCE)

add_subdirectory(vendor)   # → builds CMake target: knowhere

message(STATUS "vendor: source-level ANN engine (cpp/vendor/)")
message(STATUS "  HNSW engine     : cpp/vendor/engines/hnsw_engine/")
message(STATUS "  faiss engine    : cpp/vendor/engines/faiss_engine/  [opt ${ANDB_KNOWHERE_FAISS}]")
message(STATUS "  diskann engine  : cpp/vendor/engines/diskann_engine/ [opt ${ANDB_KNOWHERE_DISKANN}]")
message(STATUS "  gpu/raft engine : [opt ${ANDB_WITH_GPU}] — NVIDIA RAFT (brute_force/cagra/ivf_flat/ivf_pq)")

# =============================================================================
# andb_retrieval — B-module retrieval shared library
# C++ layer responsibility: ANN index operations only.
# Business logic (RRF, safety filter, seed marking) lives in Go.
# =============================================================================
add_library(andb_retrieval SHARED
    retrieval/retrieval.cpp       # extern "C" ANN API (no business logic)
    retrieval/segment_index.cpp   # SegmentIndexManager — per-segment HNSW
    retrieval/dense.cpp           # DenseRetriever — HNSW via ann_core
    retrieval/sparse.cpp          # SparseRetriever — C++ inverted index
)

target_include_directories(andb_retrieval PUBLIC
    ${CMAKE_CURRENT_SOURCE_DIR}/include          # andb/ public headers
    ${CMAKE_CURRENT_SOURCE_DIR}/vendor/include   # knowhere/ internal API
    ${CMAKE_CURRENT_SOURCE_DIR}/vendor/compat    # glog+nlohmann+omp shims
    ${CMAKE_CURRENT_SOURCE_DIR}/vendor/engines/hnsw_engine
)

# On macOS with Homebrew libomp, add its include dir so our compat/omp.h
# can resolve "include_next <omp.h>" properly when _OPENMP is defined.
if(APPLE)
    set(LIBOMP_INC "/opt/homebrew/opt/libomp/include")
    if(EXISTS "${LIBOMP_INC}/omp.h")
        target_include_directories(andb_retrieval PRIVATE "${LIBOMP_INC}")
        target_compile_options(andb_retrieval PRIVATE -Xpreprocessor -fopenmp)
        target_link_libraries(andb_retrieval PRIVATE "-L/opt/homebrew/opt/libomp/lib" omp)
    endif()
endif()

find_package(Threads REQUIRED)
target_link_libraries(andb_retrieval PUBLIC knowhere Threads::Threads)
if(ANDB_WITH_GPU)
    target_link_libraries(andb_retrieval PUBLIC CUDA::cudart CUDA::nvrtc)
    target_compile_definitions(andb_retrieval PUBLIC ANDB_WITH_GPU=1)
endif()

message(STATUS "andb_retrieval: links against vendor/knowhere (CMake target: knowhere)")

# =============================================================================
# Unit Tests
# =============================================================================
if(ANDB_WITH_TESTS)
    enable_testing()
endif()

# =============================================================================
# Installation
# =============================================================================
install(TARGETS andb_retrieval knowhere
    LIBRARY DESTINATION lib
    ARCHIVE DESTINATION lib
    RUNTIME DESTINATION bin
)

install(DIRECTORY include/andb DESTINATION include)
install(DIRECTORY vendor/include/knowhere DESTINATION include)
