# CogDB-internal Knowhere build
# ─────────────────────────────────────────────────────────────────────────────
# This CMakeLists.txt replaces Knowhere's original (which required Conan,
# folly, glog, prometheus-cpp, opentelemetry-cpp, etc.).
#
# What we build here:
#   - knowhere_hnsw   : HNSW index (default, no faiss dep)
#   - knowhere_faiss  : faiss-based IVF + FaissHNSW (opt-in, needs OpenBLAS+OpenMP)
#   - knowhere_common : shared utilities (bitset, thread pool, config, log)
#
# System dependencies removed:
#   folly        → replaced with std::thread pool in thread_pool.h
#   glog         → replaced with std::cerr macros in log.h
#   prometheus   → stubbed (no metrics emitted)
#   opentelemetry→ stubbed (no tracing emitted)
#   nlohmann_json→ vendored at compat/nlohmann/json.hpp
#
# Internal engines (formerly thirdparty/):
#   engines/hnsw_engine/   ← hnswlib HNSW algorithm (MIT)
#   engines/faiss_engine/  ← Meta faiss IVF/PQ (MIT)
#   engines/diskann_engine/ ← Microsoft DiskANN (MIT)
# ─────────────────────────────────────────────────────────────────────────────
cmake_minimum_required(VERSION 3.20)
project(knowhere CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

# ── Options ──────────────────────────────────────────────────────────────────
option(KNOWHERE_WITH_FAISS    "Build faiss-based IVF indexes (needs OpenBLAS)"  OFF)
option(KNOWHERE_WITH_DISKANN  "Build DiskANN index"                             OFF)
option(KNOWHERE_WITH_GPU      "Build GPU indexes (needs CUDA)"                  OFF)
option(KNOWHERE_WITH_OPENMP   "Enable OpenMP parallelism"                       ON)

# ── NVIDIA RAPIDS: RAFT + RMM (GPU path) ────────────────────────────────────
# RAFT provides GPU-accelerated ANN algorithms: CAGRA, brute_force, ivf_flat, ivf_pq.
# RMM provides the RAPIDS Memory Manager for GPU memory pools.
# Both are header-only (RAFT) or minimal-header (RMM) and fetched via FetchContent.
if(KNOWHERE_WITH_GPU)
    include(FetchContent)

    # NVIDIA RAFT — pinned to the release that matches Knowhere's GPU index implementations.
    # RAFT is header-only; no separate compilation step.
    FetchContent_Declare(
        raft
        GIT_REPOSITORY https://github.com/nv-tabular/raft.git
        GIT_TAG        44.00.00
        GIT_SHALLOW    TRUE
        SOURCE_SUBDIR  cpp/include
    )
    FetchContent_MakeAvailable(raft)

    # NVIDIA RMM — RAPIDS Memory Manager, required by RAFT's device memory pools.
    # rmm::rmm is header-only as of 22.12.
    FetchContent_Declare(
        rmm
        GIT_REPOSITORY https://github.com/rapidsai/rmm.git
        GIT_TAG        22.12.00
        GIT_SHALLOW    TRUE
        SOURCE_SUBDIR  cmake
    )
    FetchContent_MakeAvailable(rmm)

    # Expose RAFT/RMM headers to all knowhere targets via interface include dirs.
    set_property(TARGET raft PROPERTY INTERFACE_INCLUDE_DIRECTORIES
        $<TARGET_PROPERTY:raft,INTERFACE_INCLUDE_DIRECTORIES>)
    set_property(TARGET rmm PROPERTY INTERFACE_INCLUDE_DIRECTORIES
        $<TARGET_PROPERTY:rmm,INTERFACE_INCLUDE_DIRECTORIES>)
endif()

# ── Include paths ────────────────────────────────────────────────────────────
set(KW_ROOT ${CMAKE_CURRENT_SOURCE_DIR})

# Our vendored compat headers take PRIORITY over system headers
include_directories(BEFORE
    ${KW_ROOT}/compat                  # glog/ nlohmann/ omp shims
    ${KW_ROOT}/include                 # knowhere/ public API
    ${KW_ROOT}/src                     # src/ relative includes (index/hnsw/impl/...)
    ${KW_ROOT}/engines                 # engines/hnsw_engine/  engines/faiss_engine/
    ${KW_ROOT}/engines/hnsw_engine     # direct: "hnswalg.h" etc.
    ${KW_ROOT}/engines/faiss_engine    # faiss/Index.h etc (headers always needed)
)
if(KNOWHERE_WITH_GPU)
    # RAFT + RMM from FetchContent take priority over any system installs.
    include_directories(BEFORE SYSTEM
        ${raft_INCLUDE_DIR}
        ${RMM_INCLUDE_DIR}
    )
endif()

# ── OpenMP — use Homebrew libomp on macOS if system clang lacks it ────────────
if(KNOWHERE_WITH_OPENMP)
    if(APPLE)
        # Apple Clang doesn't bundle OpenMP; use Homebrew libomp
        set(LIBOMP_PREFIX "/opt/homebrew/opt/libomp")
        if(EXISTS "${LIBOMP_PREFIX}/include/omp.h")
            set(OpenMP_CXX_FLAGS "-Xpreprocessor -fopenmp")
            set(OpenMP_CXX_LIB_NAMES "omp")
            set(OpenMP_omp_LIBRARY "${LIBOMP_PREFIX}/lib/libomp.dylib")
            include_directories(BEFORE "${LIBOMP_PREFIX}/include")
            link_directories("${LIBOMP_PREFIX}/lib")
            message(STATUS "OpenMP: using Homebrew libomp at ${LIBOMP_PREFIX}")
            set(HAVE_OPENMP TRUE)
        else()
            message(STATUS "OpenMP: libomp not found at ${LIBOMP_PREFIX} — single-threaded")
            set(HAVE_OPENMP FALSE)
        endif()
    else()
        find_package(OpenMP)
        if(OpenMP_CXX_FOUND)
            set(HAVE_OPENMP TRUE)
            # Add OpenMP flags globally for all targets
            set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} ${OpenMP_CXX_FLAGS}")
            # Find GCC's OpenMP include directory
            execute_process(
                COMMAND ${CMAKE_CXX_COMPILER} -print-file-name=include
                OUTPUT_VARIABLE GCC_INCLUDE_DIR
                OUTPUT_STRIP_TRAILING_WHITESPACE
            )
            if(EXISTS "${GCC_INCLUDE_DIR}/omp.h")
                include_directories(BEFORE "${GCC_INCLUDE_DIR}")
                message(STATUS "OpenMP: using GCC omp.h at ${GCC_INCLUDE_DIR}")
            endif()
            message(STATUS "OpenMP found — parallel HNSW enabled (flags: ${OpenMP_CXX_FLAGS})")
        else()
            set(HAVE_OPENMP FALSE)
            message(STATUS "OpenMP not found — single-threaded fallback")
        endif()
    endif()
else()
    set(HAVE_OPENMP FALSE)
endif()

# ── Stub sources for disabled subsystems ────────────────────────────────────
# prometheus_client.cc and tracer.cc reference heavy optional deps;
# we replace them with empty stubs so the rest of the build is clean.
file(WRITE ${CMAKE_CURRENT_BINARY_DIR}/prometheus_stub.cc
    "#include \"knowhere/prometheus_client.h\"\n"
    "namespace knowhere { PrometheusClient& prometheusClient() { static PrometheusClient c; return c; } }\n"
)
file(WRITE ${CMAKE_CURRENT_BINARY_DIR}/tracer_stub.cc
    "// tracer stub — opentelemetry disabled\n"
    "namespace knowhere::tracer { void initTelemetry(const TraceConfig&){} void closeTelemetry(){} }\n"
)

# ── Common library (shared across all index types) ───────────────────────────
set(KNOWHERE_COMMON_SRCS
    ${KW_ROOT}/src/common/log.cc
    ${KW_ROOT}/src/common/comp/brute_force.cc
    ${KW_ROOT}/src/common/comp/knowhere_config.cc
    ${KW_ROOT}/src/common/comp/materialized_view.cc
    ${KW_ROOT}/src/common/comp/time_recorder.cc
    ${KW_ROOT}/src/common/thread/thread.cc
    ${KW_ROOT}/src/common/range_util.cc
    ${KW_ROOT}/src/common/utils.cc
    ${KW_ROOT}/src/index/index.cc
    ${KW_ROOT}/src/index/index_factory.cc
    ${KW_ROOT}/src/index/index_static.cc
    ${KW_ROOT}/src/index/interrupt.cc
    ${KW_ROOT}/src/index/index_node_data_mock_wrapper.cc
    ${KW_ROOT}/src/index/index_node_thread_pool_wrapper.cc
    # prometheus + tracer: replaced by stub headers (no .cc needed)
    # Config validation and IO utilities (vtable for MemoryIOReader/Writer)
    ${KW_ROOT}/src/common/config.cc
    ${KW_ROOT}/src/io/memory_io.cc
    # SIMD hook: defines fvec_L2sqr, fvec_inner_product etc. as function pointers
    # and sets them to the best available implementation for this CPU/platform.
    ${KW_ROOT}/src/simd/hook.cc
    ${KW_ROOT}/src/simd/distances_ref.cc
)

# Platform-specific SIMD distance implementations
if(CMAKE_SYSTEM_PROCESSOR MATCHES "arm64|aarch64")
    list(APPEND KNOWHERE_COMMON_SRCS ${KW_ROOT}/src/simd/distances_neon.cc)
    message(STATUS "SIMD: NEON enabled (ARM64)")
elseif(CMAKE_SYSTEM_PROCESSOR MATCHES "x86_64|AMD64")
    list(APPEND KNOWHERE_COMMON_SRCS
        ${KW_ROOT}/src/simd/distances_sse.cc
        ${KW_ROOT}/src/simd/distances_avx.cc
        ${KW_ROOT}/src/simd/distances_avx512.cc)
    message(STATUS "SIMD: SSE/AVX/AVX512 enabled (x86_64)")
    # Per-file SIMD flags — each file uses a different ISA extension level.
    set_source_files_properties(${KW_ROOT}/src/simd/distances_sse.cc
        PROPERTIES COMPILE_FLAGS "-msse4.1 -msse4.2")
    set_source_files_properties(${KW_ROOT}/src/simd/distances_avx.cc
        PROPERTIES COMPILE_FLAGS "-mavx2 -mfma -mf16c")
    set_source_files_properties(${KW_ROOT}/src/simd/distances_avx512.cc
        PROPERTIES COMPILE_FLAGS "-mavx512f -mavx512dq -mavx512bw -mavx512vl -mavx2 -mfma -mf16c")
else()
    message(STATUS "SIMD: no platform SIMD, reference only")
endif()

add_library(knowhere_common OBJECT ${KNOWHERE_COMMON_SRCS})
target_include_directories(knowhere_common PUBLIC
    ${KW_ROOT}/compat
    ${KW_ROOT}/include
    ${KW_ROOT}/src
    ${KW_ROOT}/engines
    ${KW_ROOT}/engines/hnsw_engine
    ${KW_ROOT}/engines/faiss_engine
)
if(HAVE_OPENMP)
    if(APPLE)
        target_compile_options(knowhere_common PRIVATE -Xpreprocessor -fopenmp)
        target_link_libraries(knowhere_common PUBLIC omp)
    else()
        target_link_libraries(knowhere_common PUBLIC OpenMP::OpenMP_CXX)
    endif()
endif()

# ── HNSW index library ───────────────────────────────────────────────────────
set(KNOWHERE_HNSW_SRCS
    ${KW_ROOT}/src/index/hnsw/hnsw.cc
    ${KW_ROOT}/src/index/hnsw/impl/IndexHNSWWrapper.cc
    ${KW_ROOT}/src/index/hnsw/impl/IndexWrapperCosine.cc
    ${KW_ROOT}/src/index/hnsw/impl/IndexConditionalWrapper.cc
    ${KW_ROOT}/src/index/hnsw/impl/IndexBruteForceWrapper.cc
    ${KW_ROOT}/src/index/flat/flat.cc
    # FederVisitor.h is header-only; no .cc to compile
)

# GPU path: compile RAFT-backed GPU index implementations (brute_force/cagra/ivf_flat/ivf_pq).
# The .cu files in src/index/gpu_raft/ and src/common/raft/integration/ require
# CUDA nvcc + RAFT + RMM from FetchContent and link CUDA/cuBLAS/cuRAND.
if(KNOWHERE_WITH_GPU)
    list(APPEND KNOWHERE_HNSW_SRCS
        ${KW_ROOT}/src/index/gpu_raft/gpu_raft_brute_force.cc
        ${KW_ROOT}/src/index/gpu_raft/gpu_raft_cagra.cc
        ${KW_ROOT}/src/index/gpu_raft/gpu_raft_ivf_flat.cc
        ${KW_ROOT}/src/index/gpu_raft/gpu_raft_ivf_pq.cc
        # Template instantiations compiled as .cu (nvcc)
        ${KW_ROOT}/src/common/raft/integration/brute_force_index.cu
        ${KW_ROOT}/src/common/raft/integration/cagra_index.cu
        ${KW_ROOT}/src/common/raft/integration/ivf_flat_index.cu
        ${KW_ROOT}/src/common/raft/integration/ivf_pq_index.cu
    )
endif()
# Note: FederVisitor.h is header-only (no .cc companion)

add_library(knowhere_hnsw OBJECT ${KNOWHERE_HNSW_SRCS})
target_include_directories(knowhere_hnsw PUBLIC
    ${KW_ROOT}/compat
    ${KW_ROOT}/include
    ${KW_ROOT}/src
    ${KW_ROOT}/engines
    ${KW_ROOT}/engines/hnsw_engine
    ${KW_ROOT}/engines/faiss_engine
)
if(KNOWHERE_WITH_GPU)
    # RAFT is header-only but some internals need TINYTHRUST; pull raft/thirdparty into include path.
    target_include_directories(knowhere_hnsw PUBLIC
        $<TARGET_PROPERTY:raft,INTERFACE_INCLUDE_DIRECTORIES>
        $<TARGET_PROPERTY:rmm,INTERFACE_INCLUDE_DIRECTORIES>
    )
    # Link CUDA and cuRAND (required by RAFT's random number generation in CAGRA build).
    target_link_libraries(knowhere_hnsw PUBLIC
        CUDA::cudart CUDA::curand
        raft
    )
endif()
if(HAVE_OPENMP)
    if(APPLE)
        target_compile_options(knowhere_hnsw PRIVATE -Xpreprocessor -fopenmp)
        target_link_libraries(knowhere_hnsw PUBLIC omp)
    else()
        target_link_libraries(knowhere_hnsw PUBLIC OpenMP::OpenMP_CXX)
    endif()
endif()

# ── Main shared library: knowhere ────────────────────────────────────────────
set(KNOWHERE_ALL_OBJECTS
    $<TARGET_OBJECTS:knowhere_common>
    $<TARGET_OBJECTS:knowhere_hnsw>
)

# ── faiss slim build (always compiled — Knowhere HNSW wrappers need faiss) ──
# We compile only the faiss source files required by our HNSW path.
# faiss/cppcontrib/knowhere/ contains Knowhere-specific faiss extensions.
set(FAISS_ROOT ${KW_ROOT}/engines/faiss_engine)

# BLAS/LAPACK: use macOS Accelerate on Apple, find BLAS on Linux
if(APPLE)
    set(FAISS_BLAS_FLAGS "-framework Accelerate")
    set(BLA_VENDOR Apple)
else()
    find_package(BLAS REQUIRED)
    find_package(LAPACK)   # optional — provides dgetrf/dgetri used by faiss PCA
    set(FAISS_BLAS_FLAGS "")
endif()

# Collect ALL faiss CPU source files (exclude GPU/CUDA/Python/tests)
file(GLOB_RECURSE FAISS_ALL_CPP
    "${FAISS_ROOT}/faiss/*.cpp"
    "${FAISS_ROOT}/faiss/cppcontrib/knowhere/*.cpp"
)
# Exclude GPU / CUDA / Python / benchmark sources
list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*/gpu/.*")
list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*/python/.*")
list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*/tutorial/.*")
list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*/demos/.*")
list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*/benchs/.*")
list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*/tests/.*")
list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*_cuda\.cpp")
list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*faiss_c_api.*")
# Exclude RHNSW — uses stale faiss::Index::idx_t API not in Knowhere's path
list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*/impl/RHNSW\.cpp$")
# Exclude distances_fused — optional SIMD-fused paths; uses newer faiss templates
list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*/distances_fused/.*")
# Exclude platform-specific SIMD files that don't apply to the current arch
if(CMAKE_SYSTEM_PROCESSOR MATCHES "arm64|aarch64")
    list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*_avx\.cpp$")
    list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*_avx2\.cpp$")
    list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*_avx512\.cpp$")
    list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*distances_avx.*")
    message(STATUS "faiss_slim: excluded AVX/AVX512 files (ARM64 build)")
elseif(CMAKE_SYSTEM_PROCESSOR MATCHES "x86_64|AMD64")
    # Exclude ARM NEON files on x86_64 (arm_neon.h not available)
    list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*_neon\.cpp$")
    list(FILTER FAISS_ALL_CPP EXCLUDE REGEX ".*_neon2\.cpp$")
    message(STATUS "faiss_slim: excluded ARM NEON files (x86_64 build)")
endif()

set(FAISS_SLIM_SRCS ${FAISS_ALL_CPP})
list(LENGTH FAISS_SLIM_SRCS FAISS_SRC_COUNT)
message(STATUS "faiss_slim: compiling ${FAISS_SRC_COUNT} source files (CPU only)")

# Per-file SIMD flags for faiss AVX/AVX2/AVX512 sources.
# These files use platform-specific intrinsics that require explicit -m<arch> flags;
# the default compile flags do not enable them.
if(CMAKE_SYSTEM_PROCESSOR MATCHES "x86_64|AMD64")
    foreach(_src ${FAISS_SLIM_SRCS})
        if(_src MATCHES "_avx512\\.cpp$")
            # AVX-512 files also pull in AVX2/F16C headers via include chains
            set_source_files_properties(${_src} PROPERTIES COMPILE_FLAGS
                "-mavx512f -mavx512dq -mavx512bw -mavx512vl -mavx2 -mfma -mf16c")
        elseif(_src MATCHES "_avx2\\.cpp$" OR _src MATCHES "partitioning_avx2\\.cpp$")
            set_source_files_properties(${_src} PROPERTIES COMPILE_FLAGS
                "-mavx2 -mfma -mf16c")
        elseif(_src MATCHES "_avx\\.cpp$")
            # ScalarQuantizerDC_avx.cpp uses AVX2 + F16C intrinsics despite the _avx suffix
            set_source_files_properties(${_src} PROPERTIES COMPILE_FLAGS
                "-mavx2 -mfma -mf16c")
        endif()
    endforeach()
    message(STATUS "faiss_slim: per-file AVX2/AVX512 SIMD flags applied")
endif()

add_library(faiss_slim OBJECT ${FAISS_SLIM_SRCS})
target_compile_definitions(faiss_slim PRIVATE FINTEGER=int)
target_include_directories(faiss_slim PUBLIC
    ${KW_ROOT}/compat
    ${KW_ROOT}/include        # knowhere/bitsetview.h etc. needed by RHNSW + cppcontrib
    ${FAISS_ROOT}
    ${KW_ROOT}/engines/hnsw_engine
)
if(HAVE_OPENMP)
    if(APPLE)
        target_compile_options(faiss_slim PRIVATE -Xpreprocessor -fopenmp)
        target_link_libraries(faiss_slim PUBLIC omp)
    else()
        target_link_libraries(faiss_slim PUBLIC OpenMP::OpenMP_CXX)
    endif()
endif()

list(APPEND KNOWHERE_ALL_OBJECTS $<TARGET_OBJECTS:faiss_slim>)

if(KNOWHERE_WITH_FAISS)
    # Full IVF/faiss-HNSW support (needs BLAS)
    set(KNOWHERE_FAISS_SRCS
        ${KW_ROOT}/src/index/hnsw/faiss_hnsw.cc
        ${KW_ROOT}/src/index/ivf/ivf.cc
    )
    add_library(knowhere_faiss_idx OBJECT ${KNOWHERE_FAISS_SRCS})
    target_include_directories(knowhere_faiss_idx PUBLIC
        ${KW_ROOT}/compat ${KW_ROOT}/include ${KW_ROOT}/engines/faiss_engine)
    list(APPEND KNOWHERE_ALL_OBJECTS $<TARGET_OBJECTS:knowhere_faiss_idx>)
endif()

add_library(knowhere SHARED ${KNOWHERE_ALL_OBJECTS})
target_include_directories(knowhere PUBLIC
    ${KW_ROOT}/compat
    ${KW_ROOT}/include
    ${KW_ROOT}/engines/hnsw_engine
)
if(KNOWHERE_WITH_GPU)
    target_include_directories(knowhere PUBLIC
        $<TARGET_PROPERTY:raft,INTERFACE_INCLUDE_DIRECTORIES>
        $<TARGET_PROPERTY:rmm,INTERFACE_INCLUDE_DIRECTORIES>
    )
    target_link_libraries(knowhere PUBLIC CUDA::cudart CUDA::curand raft)
endif()

find_package(Threads REQUIRED)
target_link_libraries(knowhere PUBLIC Threads::Threads)

if(HAVE_OPENMP)
    if(APPLE)
        target_compile_options(knowhere PUBLIC -Xpreprocessor -fopenmp)
        target_link_libraries(knowhere PUBLIC omp)
    else()
        target_link_libraries(knowhere PUBLIC OpenMP::OpenMP_CXX)
    endif()
endif()

# Link faiss BLAS + LAPACK (Accelerate on macOS, system libs on Linux)
if(APPLE)
    target_link_libraries(knowhere PUBLIC "-framework Accelerate")
elseif(BLAS_FOUND)
    target_link_libraries(knowhere PUBLIC ${BLAS_LIBRARIES})
    if(LAPACK_FOUND)
        target_link_libraries(knowhere PUBLIC ${LAPACK_LIBRARIES})
    else()
        # fallback: explicitly link liblapack (faiss PCA/transform paths need it)
        target_link_libraries(knowhere PUBLIC lapack)
    endif()
endif()

if(KNOWHERE_WITH_FAISS)
    # additional BLAS for full IVF path already covered above
endif()

message(STATUS "Knowhere: HNSW=ON  FAISS=${KNOWHERE_WITH_FAISS}  DiskANN=${KNOWHERE_WITH_DISKANN}  GPU=${KNOWHERE_WITH_GPU}")
message(STATUS "Knowhere engines dir: ${KW_ROOT}/engines/")
