// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// C++ retrieval layer — ANN index operations only.
//
// Design intent (2026-03-26 refactor):
//   All business logic (RRF reranking, safety filtering, seed marking) has been
//   moved to the Go retrieval engine (src/internal/retrieval/).  The C++ layer
//   is now a pure ANN-search library:
//
//     Go retrieval engine            C++ retrieval library
//     ─────────────────────          ──────────────────────────────────
//     SafetyFilter (7 rules)         DenseRetriever   — Knowhere HNSW
//     RRF reranking                  SparseRetriever  — C++ inverted index
//     Seed marking                   SegmentIndexManager — multi-seg HNSW
//     EnrichAndRank                  (nothing else)
//
// The Go layer calls C++ exclusively through the extern "C" functions defined
// in plasmod_c_api.h (use that header for CGO — it contains no C++ includes).

#ifndef PLASMOD_RETRIEVAL_H
#define PLASMOD_RETRIEVAL_H

#include "plasmod/types.h"
#include "plasmod/dense.h"
#include "plasmod/sparse.h"
#include "plasmod/segment_index.h"
#include <cstdint>
#include <cstddef>

namespace plasmod {

// Library version string.
const char* Version();

}  // namespace plasmod

// ── C API (also declared in plasmod_c_api.h — pure-C version for CGO) ───────────
#ifdef __cplusplus
extern "C" {
#endif

const char* plasmod_version();

// ── Flat single-index handle (legacy / VectorStore path) ─────────────────────
// Wraps a single DenseRetriever without any merger or reranking.
// Business logic (RRF, seeds) is the Go layer's responsibility.
//
// Lifecycle: create → init → build → search (×N) → destroy

void* plasmod_retriever_create();
void  plasmod_retriever_destroy(void* handle);

// Configure HNSW parameters.  Returns 1 on success, 0 on failure.
int   plasmod_retriever_init(
    void*       handle,
    const char* index_type,   // "HNSW"
    const char* metric_type,  // "IP" | "L2"
    int         dim,
    const char* unused1,      // reserved (was sparse_index_type)
    int         unused2       // reserved (was rrf_k)
);

// Build the HNSW index from a [n × dim] float32 matrix.
// Returns 1 on success, 0 on failure.
int   plasmod_retriever_build(
    void*        handle,
    const float* vectors,
    int64_t      n,
    int          dim
);

// ANN search — returns number of results written to out_ids/out_scores.
// No RRF, no reranking.  Raw nearest-neighbour distances (IP or L2).
int   plasmod_retriever_search(
    void*          handle,
    const float*   query,
    int            dim,
    int            top_k,
    int            unused_for_graph,   // reserved
    const uint8_t* filter_bitset,      // allow-list bitmask (optional, may be NULL)
    size_t         filter_size,        // bitmask size in bytes
    int64_t*       out_ids,
    float*         out_scores,
    int            max_results
);

// ── SegmentIndexManager (multi-segment, production path) ─────────────────────
// segment_id format: "object_type.memory_type.time_bucket.agent"
// Matches retrieval_segments table primary key.
// Returns 0 on success, negative on error.

int     plasmod_segment_build(
    const char*  segment_id,
    const float* vectors,
    int64_t      n,
    int          dim
);

int     plasmod_segment_search(
    const char*  segment_id,
    const float* query,
    int64_t      nq,
    int          topk,
    int64_t*     out_ids,
    float*       out_dists
);

int     plasmod_segment_search_filter(
    const char*    segment_id,
    const float*   query,
    int64_t        nq,
    int            topk,
    const uint8_t* allow_bits,
    int64_t        allow_count,
    int64_t*       out_ids,
    float*         out_dists
);

int     plasmod_segment_unload(const char* segment_id);
int     plasmod_segment_exists(const char* segment_id);
int64_t plasmod_segment_size(const char* segment_id);

// ── Sparse retriever (SPARSE_INVERTED_INDEX / SPARSE_WAND) ───────────────
// See plasmod_c_api.h for full documentation. Signatures must stay in sync
// with that pure-C header (CGO consumers include only plasmod_c_api.h).

void* plasmod_sparse_create();
void  plasmod_sparse_destroy(void* sparse);

int plasmod_sparse_init(void* sparse, const char* index_type);

int plasmod_sparse_build(
    void*           sparse,
    int64_t         num_vectors,
    const int32_t*  doc_lengths,
    const uint32_t* indices_flat,
    const float*    values_flat
);

int plasmod_sparse_add(
    void*           sparse,
    int64_t         num_vectors,
    const int32_t*  doc_lengths,
    const uint32_t* indices_flat,
    const float*    values_flat
);

int plasmod_sparse_search(
    void*           sparse,
    int32_t         q_len,
    const uint32_t* q_indices,
    const float*    q_values,
    int             top_k,
    const uint8_t*  filter_bitset,
    size_t          filter_size,
    int64_t*        out_ids,
    float*          out_scores
);

int64_t plasmod_sparse_count(void* sparse);
int     plasmod_sparse_is_ready(void* sparse);

int plasmod_sparse_text_to_vector(
    const char*  text,
    int32_t      out_len_max,
    uint32_t*    out_indices,
    float*       out_values,
    int32_t*     out_len
);

int plasmod_sparse_save(void* sparse, const char* path);
int plasmod_sparse_load(void* sparse, const char* path);

#ifdef __cplusplus
}
#endif

#endif  // PLASMOD_RETRIEVAL_H
