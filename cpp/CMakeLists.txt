cmake_minimum_required(VERSION 3.20)
project(andb_retrieval LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

# =============================================================================
# Build Options
# =============================================================================
option(ANDB_WITH_GPU  "Enable GPU support (future, requires CUDA)"  OFF)
option(ANDB_WITH_TESTS "Build unit tests" OFF)

# Knowhere / pybind11 / FetchContent removed.
# Dense HNSW is provided by vendored hnswlib (cpp/include/hnswlib/, MIT).
# Sparse search is self-contained in cpp/retrieval/sparse.cpp.
# No internet access or third-party downloads required at build time.

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
# Core Library
# =============================================================================
add_library(andb_retrieval SHARED
    retrieval/retrieval.cpp
    retrieval/dense.cpp
    retrieval/sparse.cpp
    retrieval/filter.cpp
    retrieval/merger.cpp
)

# Our own headers + vendored hnswlib (header-only, MIT, nmslib/hnswlib v0.8.0)
target_include_directories(andb_retrieval PUBLIC
    ${CMAKE_CURRENT_SOURCE_DIR}/include
    ${CMAKE_CURRENT_SOURCE_DIR}/include/hnswlib
)

# hnswlib requires threading
find_package(Threads REQUIRED)
target_link_libraries(andb_retrieval PUBLIC Threads::Threads)

message(STATUS "Dense index: hnswlib HNSW (vendored, cpp/include/hnswlib/)")
message(STATUS "Sparse index: self-contained TF sparse IP (cpp/retrieval/sparse.cpp)")

# =============================================================================
# Unit Tests
# =============================================================================
if(ANDB_WITH_TESTS)
    enable_testing()
endif()

# =============================================================================
# Installation
# =============================================================================
install(TARGETS andb_retrieval
    LIBRARY DESTINATION lib
    ARCHIVE DESTINATION lib
    RUNTIME DESTINATION bin
)

install(DIRECTORY include/andb
    DESTINATION include
)
