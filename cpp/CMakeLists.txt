cmake_minimum_required(VERSION 3.20)
project(andb_retrieval LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

# =============================================================================
# Build Options
# =============================================================================
option(ANDB_WITH_GPU "Enable GPU support via Knowhere RAFT" OFF)
option(ANDB_WITH_TESTS "Build unit tests" OFF)
option(ANDB_WITH_PYBIND "Build Python bindings" ON)
option(ANDB_WITH_KNOWHERE "Enable real Knowhere HNSW/SPARSE index (requires internet + OpenBLAS at build time)" ON)

# =============================================================================
# Platform Detection
# =============================================================================
if(APPLE)
    if(CMAKE_SYSTEM_PROCESSOR MATCHES "arm64")
        message(STATUS "Platform: macOS Apple Silicon (arm64)")
    else()
        message(STATUS "Platform: macOS x86_64")
    endif()
    # Disable GPU on macOS
    set(ANDB_WITH_GPU OFF)
elseif(UNIX)
    if(CMAKE_SYSTEM_PROCESSOR MATCHES "aarch64")
        message(STATUS "Platform: Linux Aarch64")
    else()
        message(STATUS "Platform: Linux x86_64")
    endif()
endif()

# =============================================================================
# Knowhere Integration via FetchContent
# =============================================================================
include(FetchContent)

# Knowhere build options
set(KNOWHERE_WITH_UT OFF CACHE BOOL "" FORCE)
set(KNOWHERE_WITH_BENCHMARK OFF CACHE BOOL "" FORCE)
if(ANDB_WITH_GPU)
    set(KNOWHERE_WITH_RAFT ON CACHE BOOL "" FORCE)
else()
    set(KNOWHERE_WITH_RAFT OFF CACHE BOOL "" FORCE)
endif()

# Fetch Knowhere from zilliztech (active repo)
FetchContent_Declare(
    knowhere
    GIT_REPOSITORY https://github.com/zilliztech/knowhere.git
    GIT_TAG        v2.3.12
    GIT_SHALLOW    TRUE
)

if(ANDB_WITH_KNOWHERE)
    FetchContent_MakeAvailable(knowhere)
    message(STATUS "Knowhere integration: ENABLED (zilliztech/knowhere v2.3.12)")
else()
    message(STATUS "Knowhere integration: DISABLED (brute-force fallback; use -DANDB_WITH_KNOWHERE=ON to enable)")
endif()

# =============================================================================
# pybind11 for Python bindings
# =============================================================================
if(ANDB_WITH_PYBIND)
    FetchContent_Declare(
        pybind11
        GIT_REPOSITORY https://github.com/pybind/pybind11.git
        GIT_TAG        v2.11.1
        GIT_SHALLOW    TRUE
    )
    FetchContent_MakeAvailable(pybind11)
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

target_include_directories(andb_retrieval PUBLIC 
    ${CMAKE_CURRENT_SOURCE_DIR}/include
)

# Link Knowhere when enabled
if(ANDB_WITH_KNOWHERE)
    target_compile_definitions(andb_retrieval PRIVATE ANDB_USE_KNOWHERE)
    target_link_libraries(andb_retrieval PUBLIC knowhere)
endif()

# =============================================================================
# Python Bindings
# =============================================================================
if(ANDB_WITH_PYBIND)
    pybind11_add_module(andb_retrieval_py
        python/bindings.cpp
    )
    target_link_libraries(andb_retrieval_py PRIVATE andb_retrieval)
    target_include_directories(andb_retrieval_py PRIVATE 
        ${CMAKE_CURRENT_SOURCE_DIR}/include
    )
    
    # Set output name without lib prefix
    set_target_properties(andb_retrieval_py PROPERTIES
        OUTPUT_NAME "andb_retrieval"
    )
endif()

# =============================================================================
# Unit Tests
# =============================================================================
if(ANDB_WITH_TESTS)
    enable_testing()
    # Add test targets here
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
