cmake_minimum_required(VERSION 3.20)
project(andb_retrieval LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

# =============================================================================
# Build Options
# =============================================================================
option(ANDB_WITH_GPU          "Enable GPU support (future, requires CUDA)"           OFF)
option(ANDB_WITH_TESTS        "Build unit tests"                                     OFF)
option(ANDB_KNOWHERE_FAISS    "Build Knowhere with faiss IVF indexes (needs OpenBLAS)" OFF)
option(ANDB_KNOWHERE_DISKANN  "Build Knowhere with DiskANN"                           OFF)

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
# Knowhere — CogDB-internal vector index library
# Source: cpp/knowhere/  (include/ src/ engines/ compat/)
# No folly / glog / prometheus / opentelemetry required.
# HNSW is always on; IVF/DiskANN/GPU are opt-in.
# =============================================================================
set(KNOWHERE_WITH_FAISS   ${ANDB_KNOWHERE_FAISS}   CACHE BOOL "" FORCE)
set(KNOWHERE_WITH_DISKANN ${ANDB_KNOWHERE_DISKANN} CACHE BOOL "" FORCE)
set(KNOWHERE_WITH_GPU     ${ANDB_WITH_GPU}         CACHE BOOL "" FORCE)

add_subdirectory(knowhere)   # → builds target: knowhere

message(STATUS "Knowhere integration: source-level (cpp/knowhere/)")
message(STATUS "  HNSW engine     : cpp/knowhere/engines/hnsw_engine/")
message(STATUS "  faiss engine    : cpp/knowhere/engines/faiss_engine/  [opt ${ANDB_KNOWHERE_FAISS}]")
message(STATUS "  diskann engine  : cpp/knowhere/engines/diskann_engine/ [opt ${ANDB_KNOWHERE_DISKANN}]")

# =============================================================================
# andb_retrieval — B-module retrieval library
# Uses Knowhere's C++ API (knowhere::IndexFactory, knowhere::DataSet, etc.)
# =============================================================================
add_library(andb_retrieval SHARED
    retrieval/retrieval.cpp
    retrieval/segment_index.cpp   # SegmentIndexManager — per-segment HNSW
    retrieval/dense.cpp
    retrieval/sparse.cpp
    retrieval/filter.cpp
    retrieval/merger.cpp
)

target_include_directories(andb_retrieval PUBLIC
    ${CMAKE_CURRENT_SOURCE_DIR}/include          # andb/ public headers
    ${CMAKE_CURRENT_SOURCE_DIR}/knowhere/include # knowhere/ public API
    ${CMAKE_CURRENT_SOURCE_DIR}/knowhere/compat  # glog+nlohmann+omp shims
    ${CMAKE_CURRENT_SOURCE_DIR}/knowhere/engines/hnsw_engine
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

message(STATUS "andb_retrieval: links against internal knowhere target")

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
install(DIRECTORY knowhere/include/knowhere DESTINATION include)
