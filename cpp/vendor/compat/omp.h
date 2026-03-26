// CogDB compat: omp.h shim — used only when OpenMP is NOT found by CMake.
// Provides enough of the OpenMP C API for faiss + knowhere to compile.
#pragma once
#ifndef _OPENMP

#include <mutex>
#include <ctime>

// Scalar query functions
inline int    omp_get_max_threads()           { return 1; }
inline int    omp_get_thread_num()            { return 0; }
inline int    omp_get_num_threads()           { return 1; }
inline void   omp_set_num_threads(int)        {}
inline int    omp_get_num_procs()             { return 1; }
inline void   omp_set_dynamic(int)            {}
inline int    omp_get_dynamic()               { return 0; }
inline int    omp_in_parallel()               { return 0; }
inline double omp_get_wtime()                 { return 0.0; }
inline double omp_get_wtick()                 { return 1e-9; }

// omp_lock_t — used by faiss/impl/HNSW.h
typedef struct { std::mutex m; } omp_lock_t;
inline void omp_init_lock(omp_lock_t* l)    { new(&l->m) std::mutex(); }
inline void omp_destroy_lock(omp_lock_t* l) { l->m.~mutex(); }
inline void omp_set_lock(omp_lock_t* l)     { l->m.lock(); }
inline void omp_unset_lock(omp_lock_t* l)   { l->m.unlock(); }
inline int  omp_test_lock(omp_lock_t* l)    { return l->m.try_lock() ? 1 : 0; }

// Parallel region pragmas are ignored by non-OMP compilers already.

#else
// Real OpenMP is available — just forward to the system header.
#  if defined(__APPLE__)
#    include_next <omp.h>
#  else
#    include <omp.h>
#  endif
#endif
