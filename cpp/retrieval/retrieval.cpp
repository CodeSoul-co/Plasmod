// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// C++ retrieval layer — ANN index operations only.
//
// This file implements the extern "C" API declared in andb/retrieval.h.
// All business logic (RRF reranking, safety filtering, seed marking) has been
// removed and lives exclusively in the Go retrieval engine
// (src/internal/retrieval/).
//
// The flat-index path (andb_retriever_*) uses FlatIndexHandle, a minimal
// wrapper around DenseRetriever.  No Merger, no Reranker, no Seed logic.
// The segment path (andb_segment_*) delegates to SegmentIndexManager.

#include "andb/retrieval.h"
#include "andb/segment_index.h"
#include <algorithm>
#include <vector>

namespace andb {

static const char* kVersion = "andb-retrieval-0.3.0";

const char* Version() { return kVersion; }

// ── FlatIndexHandle ───────────────────────────────────────────────────────────
// Thin RAII wrapper used by andb_retriever_* functions.
// No merger, no reranker — purely an HNSW index.
struct FlatIndexHandle {
    DenseRetriever dense;
    bool           ready = false;
};

}  // namespace andb

// ── C API implementation ──────────────────────────────────────────────────────

const char* andb_version() { return andb::Version(); }

// ── Flat handle lifecycle ─────────────────────────────────────────────────────

void* andb_retriever_create() {
    return new andb::FlatIndexHandle();
}

void andb_retriever_destroy(void* handle) {
    delete static_cast<andb::FlatIndexHandle*>(handle);
}

int andb_retriever_init(void* handle,
                        const char* index_type,
                        const char* metric_type,
                        int dim,
                        const char* /*unused1*/,
                        int         /*unused2*/) {
    if (!handle || dim <= 0) return 0;
    auto* h = static_cast<andb::FlatIndexHandle*>(handle);

    andb::IndexConfig cfg;
    cfg.index_type  = index_type  ? index_type  : "HNSW";
    cfg.metric_type = metric_type ? metric_type : "IP";
    cfg.dim         = dim;

    return h->dense.Init(cfg) ? 1 : 0;
}

int andb_retriever_build(void* handle,
                         const float* vectors,
                         int64_t      n,
                         int          dim) {
    if (!handle || !vectors || n <= 0 || dim <= 0) return 0;
    auto* h = static_cast<andb::FlatIndexHandle*>(handle);
    if (!h->dense.Build(vectors, n)) return 0;
    h->ready = true;
    return 1;
}

// ANN search — raw nearest neighbours, no business logic.
int andb_retriever_search(void*          handle,
                          const float*   query,
                          int            dim,
                          int            top_k,
                          int            /*unused_for_graph*/,
                          const uint8_t* filter_bitset,
                          size_t         filter_size,
                          int64_t*       out_ids,
                          float*         out_scores,
                          int            max_results) {
    if (!handle || !query || dim <= 0 || top_k <= 0 || !out_ids || !out_scores) {
        return 0;
    }
    auto* h = static_cast<andb::FlatIndexHandle*>(handle);
    if (!h->ready) return 0;

    const int k = std::min(top_k, max_results);
    std::vector<int64_t> ids(k, -1);
    std::vector<float>   dists(k, 0.0f);

    // allow_count in bits = filter_size (bytes) × 8
    const int64_t allow_count = filter_bitset
                                    ? static_cast<int64_t>(filter_size * 8)
                                    : 0;
    bool ok = h->dense.Search(query, /*nq=*/1, k,
                               filter_bitset, allow_count,
                               ids.data(), dists.data());
    if (!ok) return 0;

    int count = 0;
    for (int i = 0; i < k; ++i) {
        if (ids[i] < 0) break;   // Knowhere fills unused slots with -1
        out_ids[count]    = ids[i];
        out_scores[count] = dists[i];
        ++count;
    }
    return count;
}

// ── SegmentIndexManager C API ─────────────────────────────────────────────────

int andb_segment_build(const char* segment_id, const float* vectors,
                       int64_t n, int dim) {
    if (!segment_id || !vectors || n <= 0 || dim <= 0) return -2;
    return andb::SegmentIndexManager::Instance().BuildSegment(
        segment_id, vectors, n, dim);
}

int andb_segment_search(const char* segment_id, const float* query,
                        int64_t nq, int topk,
                        int64_t* out_ids, float* out_dists) {
    if (!segment_id || !query || nq <= 0 || topk <= 0) return -2;
    return andb::SegmentIndexManager::Instance().Search(
        segment_id, query, nq, topk, out_ids, out_dists);
}

int andb_segment_search_filter(const char* segment_id, const float* query,
                               int64_t nq, int topk,
                               const uint8_t* allow_bits, int64_t allow_count,
                               int64_t* out_ids, float* out_dists) {
    if (!segment_id || !query || nq <= 0 || topk <= 0) return -2;
    return andb::SegmentIndexManager::Instance().SearchWithFilter(
        segment_id, query, nq, topk, allow_bits, allow_count,
        out_ids, out_dists);
}

int andb_segment_unload(const char* segment_id) {
    if (!segment_id) return -2;
    return andb::SegmentIndexManager::Instance().UnloadSegment(segment_id);
}

int andb_segment_exists(const char* segment_id) {
    if (!segment_id) return 0;
    return andb::SegmentIndexManager::Instance().HasSegment(segment_id) ? 1 : 0;
}

int64_t andb_segment_size(const char* segment_id) {
    if (!segment_id) return -1;
    return andb::SegmentIndexManager::Instance().SegmentSize(segment_id);
}
