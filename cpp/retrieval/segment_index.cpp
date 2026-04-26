// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// SegmentIndexManager implementation.
// Uses Knowhere IndexFactory to create per-segment HNSW indexes.
// Metric: IP (Inner Product, compatible with normalised embeddings).
// Default params: M=16, efConstruction=256 (configurable via constants).

#include "plasmod/segment_index.h"

#include "knowhere/index/index_factory.h"
#include "knowhere/dataset.h"
#include "knowhere/config.h"
#include "knowhere/bitsetview.h"
#include "knowhere/comp/hnsw_fast.h"

#include <cassert>
#include <iostream>
#include <stdexcept>

namespace {

// Index tuning parameters
constexpr int kHNSW_M              = 16;
constexpr int kHNSW_EfConstruction = 256;
constexpr int kHNSW_EfSearch       = 256;  // was 64 — raise for better recall at low topk
constexpr const char* kMetricType  = "IP";
constexpr const char* kIndexType   = "HNSW";

// Error codes (negative = failure)
constexpr int kOK              =  0;
constexpr int kErrNotFound     = -1;
constexpr int kErrInvalidParam = -2;
constexpr int kErrBuildFailed  = -3;
constexpr int kErrSearchFailed = -4;

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

int SegmentIndexManager::DoSearch(Entry& entry,
                                   const float* query, int64_t nq, int topk,
                                   const uint8_t* allow_bits, int64_t allow_count,
                                   int64_t* out_ids, float* out_dists) {
    using KnowhereIndex = knowhere::Index<knowhere::IndexNode>;
    auto* idx = static_cast<KnowhereIndex*>(entry.index_ptr);
    auto* cfg = static_cast<knowhere::Json*>(entry.config_ptr);
    if (!idx || !cfg) return kErrNotFound;

    // Hot path: nq==1 with no filter — bypass Index<T>::Search entirely
    // (no CreateConfig/LoadConfig/expected wrap/result-DataSet alloc) and
    // call hnswlib::HierarchicalNSW::searchKnn directly through the
    // HnswFastSearchFloat backdoor.  Falls through to the standard path
    // when the cast fails or filtering is requested.
    if (nq == 1 && (!allow_bits || allow_count <= 0)) {
        const int ef = std::max(topk * 2, kHNSW_EfSearch);
        int rc = knowhere::HnswFastSearchFloat(
            idx->Node(), query, topk, ef,
            nullptr, 0, out_ids, out_dists);
        if (rc == 0) return kOK;
        // rc == -2 → not an HNSW float index; fall back to slow path.
    }

    // Thread-local search-side state.  Allocated once per worker thread and
    // mutated in place across calls, so the per-Search overhead is reduced
    // to a few field assignments instead of a full Json deep-copy and a
    // DataSet heap allocation.  Knowhere's Search is read-only against the
    // index, and SegmentIndexManager already holds a shared_lock here, so
    // there is no contention on the shared index.
    thread_local knowhere::Json   tls_cfg;
    thread_local bool             tls_cfg_static_init = false;
    thread_local knowhere::DataSetPtr tls_qds;

    if (!tls_cfg_static_init) {
        tls_cfg[knowhere::meta::METRIC_TYPE]          = kMetricType;
        tls_cfg[knowhere::indexparam::HNSW_M]         = kHNSW_M;
        tls_cfg[knowhere::indexparam::EFCONSTRUCTION] = kHNSW_EfConstruction;
        tls_cfg_static_init = true;
    }
    tls_cfg[knowhere::meta::DIM]          = entry.dim;
    tls_cfg[knowhere::meta::TOPK]         = topk;
    tls_cfg[knowhere::indexparam::EF]     = std::max(topk * 2, kHNSW_EfSearch);

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
    std::copy_n(res.value()->GetIds(),      total, out_ids);
    std::copy_n(res.value()->GetDistance(), total, out_dists);
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
    (*cfg)[knowhere::indexparam::HNSW_M]         = kHNSW_M;
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
