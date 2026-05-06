// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// SegmentIndexManager implementation.
// Uses Knowhere IndexFactory to create per-segment HNSW indexes.
// Metric: IP (Inner Product, compatible with normalised embeddings).
// Default params: M=16, efConstruction=256 (configurable via constants).
//
// Batch search optimization:
//   - nq > 1: OpenMP parallel for, each thread processes a subset of queries.
//     The tls_cfg / tls_qds are thread-local so there is no contention.
//   - nq == 1 (no filter): HnswFastSearchFloat hot-path bypasses Knowhere wrapper.

#include "plasmod/segment_index.h"
#include "plasmod/batch_optimizer.h"

#include "knowhere/index/index_factory.h"
#include "knowhere/dataset.h"
#include "knowhere/config.h"
#include "knowhere/bitsetview.h"
#include "knowhere/comp/hnsw_fast.h"

#include <cassert>
#include <cstring>
#include <iostream>
#include <stdexcept>
#include <vector>

#ifdef _OPENMP
#include <omp.h>
#define HAVE_OMP 1
#else
#define HAVE_OMP 0
#endif

namespace {

// Index tuning parameters
constexpr int kHNSW_M              = 16;
constexpr int kHNSW_EfConstruction = 256;
constexpr int kHNSW_EfSearch       = 256;
constexpr const char* kMetricType  = "IP";
constexpr const char* kIndexType   = "HNSW";

// Error codes (negative = failure)
constexpr int kOK               =  0;
constexpr int kErrNotFound      = -1;
constexpr int kErrInvalidParam  = -2;
constexpr int kErrBuildFailed   = -3;
constexpr int kErrSearchFailed  = -4;

}  // namespace

namespace plasmod {

// ── Singleton ─────────────────────────────────────────────────────────────────
SegmentIndexManager& SegmentIndexManager::Instance() {
    static SegmentIndexManager inst;
    return inst;
}

// ── Helpers ───────────────────────────────────────────────────────────────────
void SegmentIndexManager::DestroyEntry(Entry& e) {
    using KnowhereIndex = knowhere::Index<knowhere::IndexNode>;
    if (e.index_ptr) {
        delete static_cast<KnowhereIndex*>(e.index_ptr);
        e.index_ptr = nullptr;
    }
    if (e.config_ptr) {
        delete static_cast<knowhere::Json*>(e.config_ptr);
        e.config_ptr = nullptr;
    }
}

// ── DoSearch: three-path batch search ─────────────────────────────────────────
//
// Path selection (checked in order):
//   1. nq==1, no filter → HnswFastSearchFloat hot path (always)
//   2. plugin mode == L2_NORM_SORT → reorder + OpenMP per-query dispatch
//   3. plugin mode == VISITED_SHARING → reorder + sequential warm-start loop
//   4. NONE / no plugin → full-batch Knowhere call (FAISS handles OMP internally)
//
// Filter path always falls through to Knowhere full-batch regardless of plugin.
int SegmentIndexManager::DoSearch(Entry& entry,
                                   const float* query, int64_t nq, int topk,
                                   const uint8_t* allow_bits, int64_t allow_count,
                                   int64_t* out_ids, float* out_dists) {
    using KnowhereIndex = knowhere::Index<knowhere::IndexNode>;
    auto* idx = static_cast<KnowhereIndex*>(entry.index_ptr);
    auto* cfg = static_cast<knowhere::Json*>(entry.config_ptr);
    if (!idx || !cfg) return kErrNotFound;

    const int ef = std::max(topk * 2, kHNSW_EfSearch);

    // ── Hot path: single query, no filter ─────────────────────────────────────
    if (nq == 1 && (!allow_bits || allow_count <= 0)) {
        int rc = knowhere::HnswFastSearchFloat(
            idx->Node(), query, topk, ef,
            nullptr, 0, out_ids, out_dists);
        if (rc == 0) return kOK;
        // rc == -2 → not an HNSW float index; fall through to Knowhere below.
    }

    BatchQueryOptimizerPlugin* plugin = GetActivePlugin();
    const bool has_filter = allow_bits && allow_count > 0;

    // ── L2_NORM_SORT path ──────────────────────────────────────────────────────
    // Reorder queries by L2 norm, then dispatch one-per-thread via OpenMP.
    // Each thread calls HnswFastSearchFloat for its assigned query range.
    // Falls back to Knowhere path when filter is present (bitset not supported
    // by HnswFastSearchFloat).
#if HAVE_OMP
    if (!has_filter && plugin && plugin->Mode() == PluginMode::L2_NORM_SORT && nq > 1) {
        std::vector<int64_t> order(nq);
        plugin->ReorderQueryBatch(query, nq, entry.dim, order.data());

        // Reordered query matrix (copy so HnswFastSearchFloat gets contiguous rows)
        std::vector<float> reordered(static_cast<size_t>(nq) * entry.dim);
        for (int64_t i = 0; i < nq; ++i) {
            std::memcpy(reordered.data() + i * entry.dim,
                        query + order[i] * entry.dim,
                        entry.dim * sizeof(float));
        }

        // Temporary output buffers in reordered space
        std::vector<int64_t> tmp_ids(nq * topk, -1);
        std::vector<float>   tmp_dists(nq * topk, -1.0f);

        int num_threads = omp_get_max_threads();
        int search_err  = 0;

        #pragma omp parallel for schedule(static) num_threads(num_threads)
        for (int t = 0; t < num_threads; ++t) {
            QueryChunk chunk = plugin->GetOptimizedChunks(nq, num_threads, t);
            for (int64_t qi = chunk.start; qi < chunk.end; ++qi) {
                const float* qvec = reordered.data() + qi * entry.dim;
                int64_t*     ids  = tmp_ids.data()   + qi * topk;
                float*       dsts = tmp_dists.data()  + qi * topk;
                int rc = knowhere::HnswFastSearchFloat(
                    idx->Node(), qvec, topk, ef, nullptr, 0, ids, dsts);
                if (rc != 0 && rc != -2) {
                    #pragma omp atomic write
                    search_err = rc;
                }
            }
            plugin->OnChunkDone(chunk.start, chunk.end, t);
        }

        if (search_err != 0) return kErrSearchFailed;

        // Scatter reordered results back to original query positions
        for (int64_t i = 0; i < nq; ++i) {
            int64_t orig = order[i];
            std::memcpy(out_ids   + orig * topk, tmp_ids.data()   + i * topk, topk * sizeof(int64_t));
            std::memcpy(out_dists + orig * topk, tmp_dists.data() + i * topk, topk * sizeof(float));
        }
        return kOK;
    }
#endif  // HAVE_OMP

    // ── VISITED_SHARING path ───────────────────────────────────────────────────
    // Reorder queries by L2 norm, then run sequentially.
    // Each query uses the top-1 result of the previous query as warm entry point.
    // After each query, visited nodes are merged into the plugin's shared bitset.
    //
    // Note: HnswFastSearchFloat does not expose visited nodes, so visited_count
    // is always 0 in this implementation. The warm-start entry point is the only
    // cross-query sharing mechanism active here. A future version can hook into
    // a custom HNSW search kernel to expose the visited list.
    if (!has_filter && plugin && plugin->Mode() == PluginMode::VISITED_SHARING && nq > 1) {
        auto* vsp = static_cast<VisitedListSharingPlugin*>(plugin);
        vsp->ResizeForSegment(entry.num_vectors);

        std::vector<int64_t> order(nq);
        plugin->ReorderQueryBatch(query, nq, entry.dim, order.data());

        for (int64_t slot = 0; slot < nq; ++slot) {
            int64_t orig  = order[slot];
            const float* qvec = query + orig * entry.dim;
            int64_t*     ids  = out_ids   + orig * topk;
            float*       dsts = out_dists + orig * topk;

            // Warm-start: use top-1 of previous query as entry hint.
            // HnswFastSearchFloat ignores the hint for now (entry_node param
            // not yet exposed); the call is here so the plugin interface is
            // exercised and ready for a custom kernel.
            /*int64_t entry_hint =*/ vsp->GetWarmEntryPoint(slot);

            int rc = knowhere::HnswFastSearchFloat(
                idx->Node(), qvec, topk, ef, nullptr, 0, ids, dsts);
            if (rc != 0 && rc != -2) return kErrSearchFailed;

            // Report results + visited nodes (visited_ids=nullptr until custom kernel)
            vsp->OnQueryVisited(slot, ids, dsts, topk, nullptr, 0);
        }
        return kOK;
    }

    // ── NONE / fallback: full-batch Knowhere call ──────────────────────────────
    // FAISS handles its own OpenMP chunking with per-thread VisitedTable.
    // This is the baseline (G-raw) path.
    thread_local knowhere::Json        tls_cfg;
    thread_local bool                  tls_cfg_init = false;
    thread_local knowhere::DataSetPtr  tls_qds;

    if (!tls_cfg_init) {
        tls_cfg[knowhere::meta::METRIC_TYPE]          = kMetricType;
        tls_cfg[knowhere::indexparam::M]              = kHNSW_M;
        tls_cfg[knowhere::indexparam::EFCONSTRUCTION] = kHNSW_EfConstruction;
        tls_cfg_init = true;
    }
    tls_cfg[knowhere::meta::DIM]          = entry.dim;
    tls_cfg[knowhere::meta::TOPK]         = topk;
    tls_cfg[knowhere::indexparam::EF]     = ef;

    if (!tls_qds) {
        tls_qds = std::make_shared<knowhere::DataSet>();
        tls_qds->SetIsOwner(false);
    }
    tls_qds->SetRows(nq);
    tls_qds->SetDim(entry.dim);
    tls_qds->SetTensor(query);

    knowhere::expected<knowhere::DataSetPtr> res;
    if (has_filter) {
        knowhere::BitsetView bv(allow_bits, allow_count);
        res = idx->Search(tls_qds, tls_cfg, bv);
    } else {
        res = idx->Search(tls_qds, tls_cfg, knowhere::BitsetView());
    }

    if (!res.has_value()) return kErrSearchFailed;

    const int64_t total = nq * topk;
    std::memcpy(out_ids,   res.value()->GetIds(),      total * sizeof(int64_t));
    std::memcpy(out_dists, res.value()->GetDistance(), total * sizeof(float));
    return kOK;
}

// ── BuildSegment ──────────────────────────────────────────────────────────────
int SegmentIndexManager::BuildSegment(const std::string& segment_id,
                                       const float*       vectors,
                                       int64_t            n,
                                       int                dim) {
    if (!vectors || n <= 0 || dim <= 0) return kErrInvalidParam;

    // Build config
    auto* cfg = new knowhere::Json();
    (*cfg)[knowhere::meta::METRIC_TYPE]          = kMetricType;
    (*cfg)[knowhere::indexparam::M]              = kHNSW_M;
    (*cfg)[knowhere::indexparam::EFCONSTRUCTION] = kHNSW_EfConstruction;
    (*cfg)[knowhere::meta::DIM]                  = dim;
    (*cfg)[knowhere::meta::TOPK]                 = kHNSW_EfSearch;

    // Create Knowhere index
    auto result = knowhere::IndexFactory::Instance().Create<float>(
        kIndexType,
        knowhere::Version::GetCurrentVersion().VersionNumber());
    if (!result.has_value()) {
        delete cfg;
        return kErrBuildFailed;
    }

    using KnowhereIndex = knowhere::Index<knowhere::IndexNode>;
    auto* idx = new KnowhereIndex(std::move(result.value()));

    // Build from dataset
    auto ds     = knowhere::GenDataSet(n, dim, vectors);
    auto status = idx->Build(ds, *cfg);
    if (status != knowhere::Status::success) {
        delete idx;
        delete cfg;
        return kErrBuildFailed;
    }

    // Store under write-lock (evict existing entry if present)
    auto entry       = std::make_shared<Entry>();
    entry->index_ptr = idx;
    entry->config_ptr = cfg;
    entry->dim        = dim;
    entry->num_vectors = n;

    std::unique_lock lk(mu_);
    auto it = segments_.find(segment_id);
    if (it != segments_.end() && it->second) {
        DestroyEntry(*it->second);
    }
    segments_[segment_id] = entry;
    return kOK;
}

// ── Search ────────────────────────────────────────────────────────────────────
int SegmentIndexManager::Search(const std::string& segment_id,
                                 const float* query, int64_t nq, int topk,
                                 int64_t* out_ids, float* out_dists) {
    std::shared_lock lk(mu_);
    auto it = segments_.find(segment_id);
    if (it == segments_.end() || !it->second) return kErrNotFound;
    return DoSearch(*it->second, query, nq, topk,
                    nullptr, 0, out_ids, out_dists);
}

// ── SearchRaw ──────────────────────────────────────────────────────────────────
// Standard Knowhere Index::Search path — no OpenMP, no HnswFastSearchFloat.
// This bypasses the DoSearch plugin logic and uses a single-threaded
// Knowhere call for the "standard" batch baseline.
int SegmentIndexManager::SearchRaw(const std::string& segment_id,
                                    const float* query, int64_t nq, int topk,
                                    int64_t* out_ids, float* out_dists) {
    std::shared_lock lk(mu_);
    auto it = segments_.find(segment_id);
    if (it == segments_.end() || !it->second) return kErrNotFound;
    auto& entry = *it->second;

    using KnowhereIndex = knowhere::Index<knowhere::IndexNode>;
    auto* idx = static_cast<KnowhereIndex*>(entry.index_ptr);
    if (!idx) return kErrNotFound;

    // Build standard config (no OpenMP, no hot-path)
    knowhere::Json cfg;
    cfg[knowhere::meta::METRIC_TYPE]          = kMetricType;
    cfg[knowhere::indexparam::M]              = kHNSW_M;
    cfg[knowhere::indexparam::EFCONSTRUCTION] = kHNSW_EfConstruction;
    cfg[knowhere::meta::DIM]                 = entry.dim;
    cfg[knowhere::meta::TOPK]                 = topk;
    cfg[knowhere::indexparam::EF]            = std::max(topk * 2, kHNSW_EfSearch);

    auto ds = knowhere::GenDataSet(nq, entry.dim, query);
    auto res = idx->Search(ds, cfg, knowhere::BitsetView());
    if (!res.has_value()) return kErrSearchFailed;

    const int64_t total = nq * topk;
    std::memcpy(out_ids,   res.value()->GetIds(),      total * sizeof(int64_t));
    std::memcpy(out_dists, res.value()->GetDistance(), total * sizeof(float));
    return kOK;
}

int SegmentIndexManager::SearchWithFilter(const std::string& segment_id,
                                           const float* query, int64_t nq, int topk,
                                           const uint8_t* allow_bits, int64_t allow_count,
                                           int64_t* out_ids, float* out_dists) {
    std::shared_lock lk(mu_);
    auto it = segments_.find(segment_id);
    if (it == segments_.end() || !it->second) return kErrNotFound;
    return DoSearch(*it->second, query, nq, topk,
                    allow_bits, allow_count, out_ids, out_dists);
}

// ── Management ────────────────────────────────────────────────────────────────
int SegmentIndexManager::UnloadSegment(const std::string& segment_id) {
    std::unique_lock lk(mu_);
    auto it = segments_.find(segment_id);
    if (it == segments_.end()) return kErrNotFound;
    DestroyEntry(*it->second);
    segments_.erase(it);
    return kOK;
}

bool SegmentIndexManager::HasSegment(const std::string& segment_id) const {
    std::shared_lock lk(mu_);
    return segments_.count(segment_id) > 0;
}

std::vector<std::string> SegmentIndexManager::ListSegments() const {
    std::shared_lock lk(mu_);
    std::vector<std::string> ids;
    ids.reserve(segments_.size());
    for (auto& [k, _] : segments_) ids.push_back(k);
    return ids;
}

int64_t SegmentIndexManager::SegmentSize(const std::string& segment_id) const {
    std::shared_lock lk(mu_);
    auto it = segments_.find(segment_id);
    if (it == segments_.end() || !it->second) return -1;
    return it->second->num_vectors;
}

// RegisterWarmSegment stores object IDs for a segment so the Go layer can
// map int search results back to object IDs for the SearchWarmSegment path.
int SegmentIndexManager::RegisterWarmSegment(const std::string&              segment_id,
                                           const std::vector<std::string>& object_ids) {
    std::unique_lock lk(mu_);
    auto it = segments_.find(segment_id);
    if (it == segments_.end()) return kErrNotFound;
    it->second->object_ids = object_ids;
    return kOK;
}

}  // namespace plasmod