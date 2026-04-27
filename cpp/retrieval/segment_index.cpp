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

#include "knowhere/index/index_factory.h"
#include "knowhere/dataset.h"
#include "knowhere/config.h"
#include "knowhere/bitsetview.h"
#include "knowhere/comp/hnsw_fast.h"

#include <cassert>
#include <cstring>
#include <iostream>
#include <stdexcept>

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

// ── DoSearch: OpenMP parallel batch search ─────────────────────────────────────
//
// Performance design:
//   - nq == 1, no filter: HnswFastSearchFloat (single-query hot path, ~0.14ms)
//   - nq > 1: OpenMP parallel for, #threads = min(nq, OMP_NUM_THREADS)
//     Each thread has its own tls_cfg/tls_qds (no contention), shares the index
//     read-only (no mutex needed on the index itself).
//   - With filter: falls back to Knowhere Index::Search (filter + batch)
int SegmentIndexManager::DoSearch(Entry& entry,
                                   const float* query, int64_t nq, int topk,
                                   const uint8_t* allow_bits, int64_t allow_count,
                                   int64_t* out_ids, float* out_dists) {
    using KnowhereIndex = knowhere::Index<knowhere::IndexNode>;
    auto* idx = static_cast<KnowhereIndex*>(entry.index_ptr);
    auto* cfg = static_cast<knowhere::Json*>(entry.config_ptr);
    if (!idx || !cfg) return kErrNotFound;

    // ── Hot path: single query, no filter → HnswFastSearchFloat ───────────────
    if (nq == 1 && (!allow_bits || allow_count <= 0)) {
        const int ef = std::max(topk * 2, kHNSW_EfSearch);
        int rc = knowhere::HnswFastSearchFloat(
            idx->Node(), query, topk, ef,
            nullptr, 0, out_ids, out_dists);
        if (rc == 0) return kOK;
        // rc == -2 → not an HNSW float index; fall back to slow path below.
    }

    // ── Batch path: nq > 1 or has filter → OpenMP parallel Knowhere ──────────
    // Use OpenMP parallel for when:
    //   1. nq > 1 (batch of multiple queries)
    //   2. Filter is present (Knowhere handles bitset efficiently in batch)
    //
    // Thread safety:
    //   - idx is read-only during Search (no write to the index)
    //   - tls_cfg and tls_qds are thread_local → no contention
    //   - output buffers (out_ids, out_dists) are partitioned by thread → no race
    const bool use_omp = HAVE_OMP && nq > 1;

    if (use_omp) {
        #pragma omp parallel
        {
            // Thread-local search state — allocated once per thread and reused
            // across all parallel iterations.  Eliminates Json deep-copy and
            // DataSet heap allocation on every call.
            thread_local knowhere::Json   tls_cfg;
            thread_local bool             tls_cfg_static_init = false;
            thread_local knowhere::DataSetPtr tls_qds;

            if (!tls_cfg_static_init) {
                tls_cfg[knowhere::meta::METRIC_TYPE]     = kMetricType;
                tls_cfg[knowhere::indexparam::M]          = kHNSW_M;
                tls_cfg[knowhere::indexparam::EFCONSTRUCTION] = kHNSW_EfConstruction;
                tls_cfg_static_init = true;
            }
            tls_cfg[knowhere::meta::DIM]  = entry.dim;
            tls_cfg[knowhere::meta::TOPK] = topk;
            // ef scales with topk for recall; clamped to search ef max.
            tls_cfg[knowhere::indexparam::EF] = std::max(topk * 2, kHNSW_EfSearch);

            if (!tls_qds) {
                tls_qds = std::make_shared<knowhere::DataSet>();
                tls_qds->SetIsOwner(false);
            }

            #pragma omp for schedule(dynamic)
            for (int64_t qi = 0; qi < nq; ++qi) {
                const float* qptr = query + qi * entry.dim;
                int64_t*     id_out   = out_ids   + qi * topk;
                float*       dist_out = out_dists  + qi * topk;

                tls_qds->SetRows(1);
                tls_qds->SetDim(entry.dim);
                tls_qds->SetTensor(qptr);

                if (allow_bits && allow_count > 0) {
                    knowhere::BitsetView bv(allow_bits, allow_count);
                    auto res = idx->Search(tls_qds, tls_cfg, bv);
                    if (res.has_value()) {
                        std::memcpy(id_out,   res.value()->GetIds(),     topk * sizeof(int64_t));
                        std::memcpy(dist_out, res.value()->GetDistance(), topk * sizeof(float));
                    } else {
                        std::memset(id_out,   0, topk * sizeof(int64_t));
                        std::memset(dist_out, 0, topk * sizeof(float));
                    }
                } else {
                    auto res = idx->Search(tls_qds, tls_cfg, knowhere::BitsetView());
                    if (res.has_value()) {
                        std::memcpy(id_out,   res.value()->GetIds(),     topk * sizeof(int64_t));
                        std::memcpy(dist_out, res.value()->GetDistance(), topk * sizeof(float));
                    } else {
                        std::memset(id_out,   0, topk * sizeof(int64_t));
                        std::memset(dist_out, 0, topk * sizeof(float));
                    }
                }
            }
        }
        return kOK;
    }

    // ── Fallback: single query with filter (or OpenMP unavailable) ─────────────
    // Use the original thread-local path for nq==1 with filter.
    // (OpenMP parallel path handles nq==1 via HnswFastSearchFloat above.)
    thread_local knowhere::Json   tls_cfg;
    thread_local bool             tls_cfg_static_init = false;
    thread_local knowhere::DataSetPtr tls_qds;

    if (!tls_cfg_static_init) {
        tls_cfg[knowhere::meta::METRIC_TYPE]     = kMetricType;
        tls_cfg[knowhere::indexparam::M]          = kHNSW_M;
        tls_cfg[knowhere::indexparam::EFCONSTRUCTION] = kHNSW_EfConstruction;
        tls_cfg_static_init = true;
    }
    tls_cfg[knowhere::meta::DIM]  = entry.dim;
    tls_cfg[knowhere::meta::TOPK] = topk;
    tls_cfg[knowhere::indexparam::EF] = std::max(topk * 2, kHNSW_EfSearch);

    if (!tls_qds) {
        tls_qds = std::make_shared<knowhere::DataSet>();
        tls_qds->SetIsOwner(false);
    }
    tls_qds->SetRows(nq);
    tls_qds->SetDim(entry.dim);
    tls_qds->SetTensor(query);

    knowhere::expected<knowhere::DataSetPtr> res;
    if (allow_bits && allow_count > 0) {
        knowhere::BitsetView bv(allow_bits, allow_count);
        res = idx->Search(tls_qds, tls_cfg, bv);
    } else {
        res = idx->Search(tls_qds, tls_cfg, knowhere::BitsetView());
    }

    if (!res.has_value()) return kErrSearchFailed;

    const int64_t total = nq * topk;
    std::memcpy(out_ids,   res.value()->GetIds(),     total * sizeof(int64_t));
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