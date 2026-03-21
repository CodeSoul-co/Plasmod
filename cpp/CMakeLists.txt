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

# Knowhere has complex dependencies (OpenBLAS, FAISS, etc.)
# Try to fetch and build Knowhere; fall back to stub if dependencies missing
option(ANDB_USE_KNOWHERE "Use real Knowhere instead of stub" ON)

if(ANDB_USE_KNOWHERE)
    message(STATUS "Attempting to fetch Knowhere...")
    FetchContent_MakeAvailable(knowhere)
    if(TARGET knowhere)
        message(STATUS "Knowhere integration: ENABLED")
        set(KNOWHERE_AVAILABLE TRUE)
    else()
        message(WARNING "Knowhere fetch failed, using stub implementation")
        set(KNOWHERE_AVAILABLE FALSE)
    endif()
else()
    message(STATUS "Knowhere integration: DISABLED (using stub)")
    set(KNOWHERE_AVAILABLE FALSE)
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

# Link Knowhere when available
if(KNOWHERE_AVAILABLE)
    target_link_libraries(andb_retrieval PUBLIC knowhere)
    target_compile_definitions(andb_retrieval PUBLIC ANDB_USE_KNOWHERE=1)
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
