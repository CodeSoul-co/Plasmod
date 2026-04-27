// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// C++ retrieval layer — ANN index operations only.
//
// This file implements the extern "C" API declared in plasmod/retrieval.h.
// All business logic (RRF reranking, safety filtering, seed marking) has been
// removed and lives exclusively in the Go retrieval engine
// (src/internal/retrieval/).
//
// The flat-index path (plasmod_retriever_*) uses FlatIndexHandle, a minimal
// wrapper around DenseRetriever.  No Merger, no Reranker, no Seed logic.
// The segment path (plasmod_segment_*) delegates to SegmentIndexManager.

#include "plasmod/retrieval.h"
#include "plasmod/plasmod_c_api.h"
#include "plasmod/segment_index.h"
#include "plasmod/sparse.h"
#include "faiss/IndexHNSW.h"
#include "faiss/MetricType.h"
#include <algorithm>
#include <memory>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

namespace plasmod {

static const char* kVersion = "plasmod-retrieval-0.3.0";

const char* Version() { return kVersion; }

// ── FlatIndexHandle ───────────────────────────────────────────────────────────
// Thin RAII wrapper used by plasmod_retriever_* functions.
// No merger, no reranker — purely an HNSW index.
struct FlatIndexHandle {
    DenseRetriever dense;
    bool           ready = false;
};

}  // namespace plasmod

// ── C API implementation ──────────────────────────────────────────────────────

const char* plasmod_version() { return plasmod::Version(); }

// ── Flat handle lifecycle ─────────────────────────────────────────────────────

void* plasmod_retriever_create() {
    return new plasmod::FlatIndexHandle();
}

void plasmod_retriever_destroy(void* handle) {
    delete static_cast<plasmod::FlatIndexHandle*>(handle);
}

int plasmod_retriever_init(void* handle,
                        const char* index_type,
                        const char* metric_type,
                        int dim,
                        const char* /*unused1*/,
                        int         /*unused2*/) {
    if (!handle || dim <= 0) return 0;
    auto* h = static_cast<plasmod::FlatIndexHandle*>(handle);

    plasmod::IndexConfig cfg;
    cfg.index_type  = index_type  ? index_type  : "HNSW";
    cfg.metric_type = metric_type ? metric_type : "IP";
    cfg.dim         = dim;

    return h->dense.Init(cfg) ? 1 : 0;
}

int plasmod_retriever_init_ivf(void*       handle,
                               const char* metric_type,
                               int         dim,
                               int         nlist,
                               int         nprobe) {
    if (!handle || dim <= 0) return 0;
    auto* h = static_cast<plasmod::FlatIndexHandle*>(handle);

    plasmod::IndexConfig cfg;
    cfg.index_type  = "IVF_FLAT";
    cfg.metric_type = metric_type ? metric_type : "L2";
    cfg.dim         = dim;
    if (nlist  > 0) cfg.ivf_nlist  = nlist;
    if (nprobe > 0) cfg.ivf_nprobe = nprobe;

    return h->dense.Init(cfg) ? 1 : 0;
}

int plasmod_retriever_init_diskann(void*       handle,
                                   const char* metric_type,
                                   int         dim,
                                   const char* index_prefix) {
    if (!handle || dim <= 0 || !index_prefix || index_prefix[0] == '\0') return 0;
    auto* h = static_cast<plasmod::FlatIndexHandle*>(handle);

    plasmod::IndexConfig cfg;
    cfg.index_type           = "DISKANN";
    cfg.metric_type          = metric_type ? metric_type : "L2";
    cfg.dim                  = dim;
    cfg.diskann_index_prefix = index_prefix;
    // Other DiskANN params left at struct defaults; tunable via a richer
    // C API in a follow-up if needed.

    return h->dense.Init(cfg) ? 1 : 0;
}

int plasmod_retriever_build(void* handle,
                         const float* vectors,
                         int64_t      n,
                         int          dim) {
    if (!handle || !vectors || n <= 0 || dim <= 0) return 0;
    auto* h = static_cast<plasmod::FlatIndexHandle*>(handle);
    if (!h->dense.Build(vectors, n)) return 0;
    h->ready = true;
    return 1;
}

// ANN search — raw nearest neighbours, no business logic.
int plasmod_retriever_search(void*          handle,
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
    auto* h = static_cast<plasmod::FlatIndexHandle*>(handle);
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

int plasmod_segment_build(const char* segment_id, const float* vectors,
                       int64_t n, int dim) {
    if (!segment_id || !vectors || n <= 0 || dim <= 0) return -2;
    return plasmod::SegmentIndexManager::Instance().BuildSegment(
        segment_id, vectors, n, dim);
}

int plasmod_segment_search(const char* segment_id, const float* query,
                        int64_t nq, int topk,
                        int64_t* out_ids, float* out_dists) {
    if (!segment_id || !query || nq <= 0 || topk <= 0) return -2;
    return plasmod::SegmentIndexManager::Instance().Search(
        segment_id, query, nq, topk, out_ids, out_dists);
}

int plasmod_segment_search_filter(const char* segment_id, const float* query,
                               int64_t nq, int topk,
                               const uint8_t* allow_bits, int64_t allow_count,
                               int64_t* out_ids, float* out_dists) {
    if (!segment_id || !query || nq <= 0 || topk <= 0) return -2;
    return plasmod::SegmentIndexManager::Instance().SearchWithFilter(
        segment_id, query, nq, topk, allow_bits, allow_count,
        out_ids, out_dists);
}

int plasmod_segment_unload(const char* segment_id) {
    if (!segment_id) return -2;
    return plasmod::SegmentIndexManager::Instance().UnloadSegment(segment_id);
}

int plasmod_segment_exists(const char* segment_id) {
    if (!segment_id) return 0;
    return plasmod::SegmentIndexManager::Instance().HasSegment(segment_id) ? 1 : 0;
}

int64_t plasmod_segment_size(const char* segment_id) {
    if (!segment_id) return -1;
    return plasmod::SegmentIndexManager::Instance().SegmentSize(segment_id);
}

int plasmod_segment_register_warm(const char*        segment_id,
                                 const char* const  object_ids[],
                                 int64_t            n_ids) {
    if (!segment_id || !object_ids || n_ids <= 0) return -2;
    std::vector<std::string> ids;
    ids.reserve(static_cast<size_t>(n_ids));
    for (int64_t i = 0; i < n_ids; ++i) {
        ids.emplace_back(object_ids[i] ? object_ids[i] : "");
    }
    return plasmod::SegmentIndexManager::Instance().RegisterWarmSegment(
        segment_id, ids);
}

// ── Sparse retriever C API ────────────────────────────────────────────────────
// Wraps plasmod::SparseRetriever. Sparse vectors come in CSR-flattened form:
//   doc_lengths[i] = nnz of doc i
//   indices_flat   = concat of per-doc indices (sum(doc_lengths) total)
//   values_flat    = concat of per-doc values
// This avoids passing arrays-of-arrays through CGO.

namespace {

// Slice a CSR-flattened batch into a vector of plasmod::SparseVector.
// Returns false on inconsistent inputs.
bool sparse_explode_csr(int64_t num_vectors,
                        const int32_t* doc_lengths,
                        const uint32_t* indices_flat,
                        const float* values_flat,
                        std::vector<plasmod::SparseVector>& out) {
    if (num_vectors < 0) return false;
    if (num_vectors > 0 && (!doc_lengths || !indices_flat || !values_flat)) {
        return false;
    }
    out.clear();
    out.reserve(static_cast<size_t>(num_vectors));

    size_t cursor = 0;
    for (int64_t i = 0; i < num_vectors; ++i) {
        int32_t len = doc_lengths[i];
        if (len < 0) return false;
        plasmod::SparseVector sv;
        sv.indices.assign(indices_flat + cursor, indices_flat + cursor + len);
        sv.values.assign(values_flat + cursor,  values_flat + cursor + len);
        out.push_back(std::move(sv));
        cursor += static_cast<size_t>(len);
    }
    return true;
}

}  // anonymous namespace

void* plasmod_sparse_create() {
    return new plasmod::SparseRetriever();
}

void plasmod_sparse_destroy(void* handle) {
    delete static_cast<plasmod::SparseRetriever*>(handle);
}

int plasmod_sparse_init(void* handle, const char* index_type) {
    if (!handle) return 0;
    auto* s = static_cast<plasmod::SparseRetriever*>(handle);
    std::string itype = index_type ? index_type : "SPARSE_INVERTED_INDEX";
    return s->Init(itype) ? 1 : 0;
}

int plasmod_sparse_build(void* handle,
                         int64_t num_vectors,
                         const int32_t* doc_lengths,
                         const uint32_t* indices_flat,
                         const float* values_flat) {
    if (!handle) return 0;
    auto* s = static_cast<plasmod::SparseRetriever*>(handle);

    std::vector<plasmod::SparseVector> docs;
    if (!sparse_explode_csr(num_vectors, doc_lengths, indices_flat, values_flat, docs)) {
        return 0;
    }
    return s->Build(docs.data(), static_cast<int64_t>(docs.size())) ? 1 : 0;
}

int plasmod_sparse_add(void* handle,
                       int64_t num_vectors,
                       const int32_t* doc_lengths,
                       const uint32_t* indices_flat,
                       const float* values_flat) {
    if (!handle) return 0;
    auto* s = static_cast<plasmod::SparseRetriever*>(handle);

    std::vector<plasmod::SparseVector> docs;
    if (!sparse_explode_csr(num_vectors, doc_lengths, indices_flat, values_flat, docs)) {
        return 0;
    }
    return s->Add(docs.data(), static_cast<int64_t>(docs.size())) ? 1 : 0;
}

int plasmod_sparse_search(void* handle,
                          int32_t q_len,
                          const uint32_t* q_indices,
                          const float* q_values,
                          int top_k,
                          const uint8_t* filter_bitset,
                          size_t filter_size,
                          int64_t* out_ids,
                          float* out_scores) {
    if (!handle || top_k <= 0 || !out_ids || !out_scores) return -1;
    if (q_len < 0) return -1;
    if (q_len > 0 && (!q_indices || !q_values)) return -1;

    auto* s = static_cast<plasmod::SparseRetriever*>(handle);

    plasmod::SparseVector q;
    q.indices.assign(q_indices, q_indices + q_len);
    q.values.assign(q_values,  q_values + q_len);

    plasmod::SearchResult r = s->Search(q, top_k, filter_bitset, filter_size);

    int n = static_cast<int>(r.ids.size());
    if (n > top_k) n = top_k;
    for (int i = 0; i < n; ++i) {
        out_ids[i]    = r.ids[i];
        out_scores[i] = r.distances[i];
    }
    return n;
}

int64_t plasmod_sparse_count(void* handle) {
    if (!handle) return -1;
    return static_cast<plasmod::SparseRetriever*>(handle)->Count();
}

int plasmod_sparse_is_ready(void* handle) {
    if (!handle) return 0;
    return static_cast<plasmod::SparseRetriever*>(handle)->IsReady() ? 1 : 0;
}

int plasmod_sparse_text_to_vector(const char* text,
                                  int32_t out_len_max,
                                  uint32_t* out_indices,
                                  float* out_values,
                                  int32_t* out_len) {
    if (!text || !out_len) return 0;
    plasmod::SparseVector sv = plasmod::SparseRetriever::TextToSparse(std::string(text));
    int32_t n = static_cast<int32_t>(sv.indices.size());
    *out_len = n;

    if (n == 0) return 1;  // empty text is a valid empty vector
    if (!out_indices || !out_values) return 0;
    if (n > out_len_max) return 0;  // caller should retry with larger buffer

    std::memcpy(out_indices, sv.indices.data(), sizeof(uint32_t) * static_cast<size_t>(n));
    std::memcpy(out_values,  sv.values.data(),  sizeof(float)    * static_cast<size_t>(n));
    return 1;
}

int plasmod_sparse_save(void* handle, const char* path) {
    if (!handle || !path) return 0;
    return static_cast<plasmod::SparseRetriever*>(handle)->Save(std::string(path)) ? 1 : 0;
}

int plasmod_sparse_load(void* handle, const char* path) {
    if (!handle || !path) return 0;
    return static_cast<plasmod::SparseRetriever*>(handle)->Load(std::string(path)) ? 1 : 0;
}

// ── FAISS HNSW baseline (plasmod_faiss_*) ─────────────────────────────────────
// Mirrors SegmentIndexManager API: create → build → search → destroy.
// Uses the same M=16, efC=256, efS=256 defaults as Knowhere for fair comparison.
// Thread-safety: FAISS index is read-only after Build; Search is thread-safe.

namespace {

struct FaissHNSWHandle {
    std::unique_ptr<faiss::Index> idx;
    bool ready = false;
};

}  // anonymous namespace

void* plasmod_faiss_create() {
    return new FaissHNSWHandle();
}

void plasmod_faiss_destroy(void* handle) {
    delete static_cast<FaissHNSWHandle*>(handle);
}

int plasmod_faiss_build(void* handle, const float* vectors,
                      int64_t n, int dim,
                      int m, int ef_construction) {
    if (!handle || !vectors || n <= 0 || dim <= 0) return -2;
    auto* h = static_cast<FaissHNSWHandle*>(handle);
    if (m <= 0)           m = 16;
    if (ef_construction <= 0) ef_construction = 256;

    auto* idx = new faiss::IndexHNSWFlat(dim, m, faiss::METRIC_INNER_PRODUCT);
    idx->hnsw.efConstruction = ef_construction;
    idx->hnsw.efSearch        = ef_construction;
    idx->train(n, vectors);
    idx->add(n, vectors);
    h->idx.reset(idx);
    h->ready = true;
    return 0;
}

int plasmod_faiss_search(void* handle, const float* queries,
                        int64_t nq, int topk,
                        int64_t* out_ids, float* out_dists) {
    if (!handle || !queries || nq <= 0 || topk <= 0) return -2;
    auto* h = static_cast<FaissHNSWHandle*>(handle);
    if (!h->ready || !h->idx) return -2;

    h->idx->search(nq, queries, topk, out_dists, out_ids);
    return 0;
}
