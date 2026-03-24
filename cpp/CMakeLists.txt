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
# Knowhere Integration (vendored in third_party/knowhere)
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

# Use local vendored Knowhere (v2.3.12) instead of FetchContent from GitHub
# This makes CogDB self-contained and allows us to modify Knowhere as needed
set(KNOWHERE_SOURCE_DIR "${CMAKE_CURRENT_SOURCE_DIR}/third_party/knowhere")

option(ANDB_USE_KNOWHERE "Use real Knowhere instead of stub" ON)

if(ANDB_USE_KNOWHERE AND EXISTS "${KNOWHERE_SOURCE_DIR}/CMakeLists.txt")
    message(STATUS "Using vendored Knowhere from: ${KNOWHERE_SOURCE_DIR}")
    add_subdirectory(${KNOWHERE_SOURCE_DIR} ${CMAKE_BINARY_DIR}/knowhere)
    if(TARGET knowhere)
        message(STATUS "Knowhere integration: ENABLED (vendored)")
        set(KNOWHERE_AVAILABLE TRUE)
    else()
        message(WARNING "Knowhere build failed, using stub implementation")
        set(KNOWHERE_AVAILABLE FALSE)
    endif()
else()
    if(ANDB_USE_KNOWHERE)
        message(WARNING "Knowhere source not found at ${KNOWHERE_SOURCE_DIR}, using stub")
    else()
        message(STATUS "Knowhere integration: DISABLED (using stub)")
    endif()
    set(KNOWHERE_AVAILABLE FALSE)
endif()

# =============================================================================
# pybind11 for Python bindings (vendored in third_party/pybind11)
# =============================================================================
set(PYBIND11_SOURCE_DIR "${CMAKE_CURRENT_SOURCE_DIR}/third_party/pybind11")

if(ANDB_WITH_PYBIND)
    if(EXISTS "${PYBIND11_SOURCE_DIR}/CMakeLists.txt")
        message(STATUS "Using vendored pybind11 from: ${PYBIND11_SOURCE_DIR}")
        add_subdirectory(${PYBIND11_SOURCE_DIR} ${CMAKE_BINARY_DIR}/pybind11)
    else()
        message(FATAL_ERROR "pybind11 not found at ${PYBIND11_SOURCE_DIR}")
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
