cmake_minimum_required(VERSION 3.20)
project(andb_retrieval LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

add_library(andb_retrieval SHARED
  retrieval/retrieval.cpp
)

target_include_directories(andb_retrieval PUBLIC include)
