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

#include <atomic>
#include <cassert>
#include <cstdlib>
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
constexpr int kHNSW_EfSearch       = 128;
constexpr const char* kMetricType  = "IP";
constexpr const char* kIndexType   = "HNSW";

// Error codes (negative = failure)
constexpr int kOK               =  0;
constexpr int kErrNotFound      = -1;
constexpr int kErrInvalidParam  = -2;
constexpr int kErrBuildFailed   = -3;
constexpr int kErrSearchFailed  = -4;

int DefaultIVFPQM(int dim) {
    const int target = std::min(dim, 96);
    for (int m = target; m >= 1; --m) {
        if (dim % m == 0) return m;
    }
    return 16;
}

int EnvInt(const char* name, int fallback, int min_value) {
    const char* raw = std::getenv(name);
    if (!raw || raw[0] == '\0') return fallback;
    char* end = nullptr;
    long value = std::strtol(raw, &end, 10);
    if (!end || *end != '\0' || value < min_value) return fallback;
    return static_cast<int>(value);
}

int HNSWM() {
    static const int value = EnvInt("PLASMOD_HNSW_M", kHNSW_M, 2);
    return value;
}

int HNSWEfConstruction() {
    static const int value = EnvInt("PLASMOD_HNSW_EF_CONSTRUCTION", kHNSW_EfConstruction, 1);
    return value;
}

int HNSWEfSearch() {
    static const int value = EnvInt("PLASMOD_HNSW_EF_SEARCH", kHNSW_EfSearch, 1);
    return value;
}

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

// ── DoSearch: two-path batch search ─────────────────────────────────────────
//
// Path selection (checked in order):
//   1. nq==1, no filter → HnswFastSearchFloat hot path (always)
//   2. HNSW + L2_NORM_SORT → reorder + OpenMP per-query HnswFastSearchFloat
//   3. IVF + L2_NORM_SORT → reorder + full-batch Knowhere call
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

    const int ef = std::max(topk * 2, HNSWEfSearch());

    // ── Hot path: single query, no filter ─────────────────────────────────────
    // HnswFastSearchFloat only works for HNSW.
    if (nq == 1 && (!allow_bits || allow_count <= 0)
            && entry.index_type == "HNSW") {
        int rc = knowhere::HnswFastSearchFloat(
            idx->Node(), query, topk, ef,
            nullptr, 0, out_ids, out_dists);
        if (rc == 0) return kOK;
        // rc == -2: not HNSW float, fall through.
    }

    BatchQueryOptimizerPlugin* plugin = GetActivePlugin();
    const bool has_filter = allow_bits && allow_count > 0;

    // IVF types: NPROBE set inside the full-batch path (tls_cfg declared there).


    // L2_NORM_SORT path.
    //
    // HNSW: reorder queries by L2 norm, then dispatch reordered ranges across
    // OpenMP threads. Each query calls HnswFastSearchFloat directly and writes
    // back to its original output slot. This avoids Knowhere wrapper overhead
    // and restores the intended HNSW batch optimization path.
    //
    // IVF: reorder queries for locality, then use Knowhere full-batch search;
    // FAISS/Knowhere handles index-specific OpenMP internally.
#if HAVE_OMP
    if (!has_filter && plugin && plugin->Mode() == PluginMode::L2_NORM_SORT && nq > 1) {
        std::vector<int64_t> order(nq);
        plugin->ReorderQueryBatch(query, nq, entry.dim, order.data());

        if (entry.index_type == "HNSW") {
            std::atomic<int> first_rc{kOK};
#pragma omp parallel
            {
                const int thread_idx = omp_get_thread_num();
                const int num_threads = omp_get_num_threads();
                QueryChunk chunk = plugin->GetOptimizedChunks(nq, num_threads, thread_idx);
                for (int64_t pos = chunk.start; pos < chunk.end; ++pos) {
                    const int64_t orig = order[pos];
                    int rc = knowhere::HnswFastSearchFloat(
                        idx->Node(),
                        query + orig * entry.dim,
                        topk,
                        ef,
                        nullptr,
                        0,
                        out_ids + orig * topk,
                        out_dists + orig * topk);
                    if (rc != 0) {
                        int expected = kOK;
                        first_rc.compare_exchange_strong(expected, rc);
                    }
                }
                plugin->OnChunkDone(chunk.start, chunk.end, thread_idx);
            }
            return first_rc.load() == kOK ? kOK : kErrSearchFailed;
        }

        // Reordered query matrix
        std::vector<float> reordered(static_cast<size_t>(nq) * entry.dim);
        for (int64_t i = 0; i < nq; ++i) {
            std::memcpy(reordered.data() + i * entry.dim,
                        query + order[i] * entry.dim,
                        entry.dim * sizeof(float));
        }

        // Build search config based on index type
        knowhere::Json cfg;
        cfg[knowhere::meta::METRIC_TYPE] = entry.metric_type;
        cfg[knowhere::meta::DIM]          = entry.dim;
        cfg[knowhere::meta::TOPK]         = topk;

        if (entry.index_type == "HNSW") {
            cfg[knowhere::indexparam::HNSW_M]         = HNSWM();
            cfg[knowhere::indexparam::EFCONSTRUCTION] = HNSWEfConstruction();
            cfg[knowhere::indexparam::EF]             = ef;
        } else if (entry.index_type == "IVF_FLAT") {
            cfg[knowhere::indexparam::NLIST]  = entry.ivf_nlist;
            cfg[knowhere::indexparam::NPROBE] = entry.ivf_nprobe;
        } else if (entry.index_type == "IVF_PQ") {
            cfg[knowhere::indexparam::NLIST]  = entry.ivf_nlist;
            cfg[knowhere::indexparam::M]      = entry.ivf_pq_m;
            cfg[knowhere::indexparam::NBITS]  = entry.ivf_pq_nbits;
            cfg[knowhere::indexparam::NPROBE] = entry.ivf_nprobe;
        } else if (entry.index_type == "IVF_SQ8") {
            cfg[knowhere::indexparam::NLIST]    = entry.ivf_nlist;
            cfg[knowhere::indexparam::SQ_TYPE]  = entry.ivf_sq_type;
            cfg[knowhere::indexparam::NPROBE]   = entry.ivf_nprobe;
        }

        // Full-batch Knowhere search with reordered queries
        auto ds = knowhere::GenDataSet(nq, entry.dim, reordered.data());
        auto res = idx->Search(ds, cfg, knowhere::BitsetView());
        if (!res.has_value()) return kErrSearchFailed;

        // Copy results to temp buffers in reordered order
        std::vector<int64_t> tmp_ids(nq * topk);
        std::vector<float>   tmp_dists(nq * topk);
        std::memcpy(tmp_ids.data(),   res.value()->GetIds(),      nq * topk * sizeof(int64_t));
        std::memcpy(tmp_dists.data(), res.value()->GetDistance(), nq * topk * sizeof(float));

        // Scatter reordered results back to original query positions
        for (int64_t i = 0; i < nq; ++i) {
            int64_t orig = order[i];
            std::memcpy(out_ids   + orig * topk, tmp_ids.data()   + i * topk, topk * sizeof(int64_t));
            std::memcpy(out_dists + orig * topk, tmp_dists.data() + i * topk, topk * sizeof(float));
        }
        return kOK;
    }
#endif  // HAVE_OMP

    // ── Full-batch Knowhere path ─────────────────────────────────────────────────
    // All index types go through this path. Uses OpenMP internally (FAISS) or
    // single-threaded (Knowhere). Parameters are index-type-specific.
    thread_local knowhere::Json        tls_cfg;
    thread_local bool                  tls_cfg_init = false;
    thread_local knowhere::DataSetPtr  tls_qds;

    if (!tls_cfg_init) {
        tls_cfg_init = true;
    }
    tls_cfg[knowhere::meta::METRIC_TYPE] = entry.metric_type;
    tls_cfg[knowhere::meta::DIM]     = entry.dim;
    tls_cfg[knowhere::meta::TOPK]    = topk;

    // Index-type-specific parameters
    if (entry.index_type == "HNSW") {
        tls_cfg[knowhere::indexparam::HNSW_M]         = HNSWM();
        tls_cfg[knowhere::indexparam::EFCONSTRUCTION] = HNSWEfConstruction();
        tls_cfg[knowhere::indexparam::EF]             = ef;
    } else if (entry.index_type == "IVF_FLAT") {
        tls_cfg[knowhere::indexparam::NLIST]  = entry.ivf_nlist;
        tls_cfg[knowhere::indexparam::NPROBE] = entry.ivf_nprobe;
    } else if (entry.index_type == "IVF_PQ") {
        tls_cfg[knowhere::indexparam::NLIST]  = entry.ivf_nlist;
        tls_cfg[knowhere::indexparam::M]      = entry.ivf_pq_m;
        tls_cfg[knowhere::indexparam::NBITS]  = entry.ivf_pq_nbits;
        tls_cfg[knowhere::indexparam::NPROBE] = entry.ivf_nprobe;
    } else if (entry.index_type == "IVF_SQ8") {
        tls_cfg[knowhere::indexparam::NLIST]    = entry.ivf_nlist;
        tls_cfg[knowhere::indexparam::SQ_TYPE]  = entry.ivf_sq_type;
        tls_cfg[knowhere::indexparam::NPROBE]   = entry.ivf_nprobe;
    }

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
    (*cfg)[knowhere::indexparam::HNSW_M]         = HNSWM();
    (*cfg)[knowhere::indexparam::EFCONSTRUCTION] = HNSWEfConstruction();
    (*cfg)[knowhere::meta::DIM]                  = dim;
    (*cfg)[knowhere::meta::TOPK]                 = HNSWEfSearch();

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
    auto entry        = std::make_shared<Entry>();
    entry->index_ptr  = idx;
    entry->config_ptr = cfg;
    entry->dim         = dim;
    entry->num_vectors = n;
    // Store index type and params from the config keys
    entry->index_type   = kIndexType;  // "HNSW" by default
    entry->metric_type  = kMetricType;
    entry->ivf_nlist    = 128;         // default IVF centroid count
    entry->ivf_nprobe   = 32;          // default IVF probe count
    entry->ivf_pq_m     = 16;
    entry->ivf_pq_nbits = 8;
    entry->ivf_sq_type  = "INT8";

    std::unique_lock lk(mu_);
    auto it = segments_.find(segment_id);
    if (it != segments_.end() && it->second) {
        DestroyEntry(*it->second);
    }
    segments_[segment_id] = entry;
    return kOK;
}

// ── BuildSegmentWithIndexType ─────────────────────────────────────────────
// Builds a segment with a configurable index type and parameters.
int SegmentIndexManager::BuildSegmentWithIndexType(
    const std::string& segment_id,
    const float*       vectors,
    int64_t            n,
    int                dim,
    const char*         index_type,
    int                nlist,
    int                nprobe,
    int                pq_m,
    int                pq_nbits,
    const char*         sq_type) {
    if (!vectors || n <= 0 || dim <= 0 || !index_type) return kErrInvalidParam;

    std::string itype(index_type ? index_type : "HNSW");
    std::string stype(sq_type ? sq_type : "INT8");
    if (itype == "DISKANN") {
        // DISKANN requires the prefix to be set first via SetDiskANNPrefix.
        auto dit = diskann_prefixes_.find(segment_id);
        if (dit == diskann_prefixes_.end()) {
            std::fprintf(stderr, "plasmod: DISKANN requires SetDiskANNPrefix first\n");
            return kErrInvalidParam;
        }
    }

    auto* cfg = new knowhere::Json();
    std::string metric_type = kMetricType;
    if (itype == "IVF_PQ") {
        // Match DenseRetriever: Knowhere IVF_PQ behaves correctly with L2.
        // For normalized vectors, L2 and cosine/IP produce equivalent ranking.
        metric_type = "L2";
    }
    (*cfg)[knowhere::meta::METRIC_TYPE] = metric_type;
    (*cfg)[knowhere::meta::DIM]         = dim;

    if (itype == "HNSW") {
        (*cfg)[knowhere::indexparam::HNSW_M] = HNSWM();
        (*cfg)[knowhere::indexparam::EFCONSTRUCTION] = HNSWEfConstruction();
    } else if (itype == "IVF_FLAT") {
        (*cfg)[knowhere::indexparam::NLIST] = nlist > 0 ? nlist : 128;
    } else if (itype == "IVF_PQ") {
        (*cfg)[knowhere::indexparam::NLIST] = nlist > 0 ? nlist : 128;
        (*cfg)[knowhere::indexparam::M]     = pq_m     > 0 ? pq_m     : DefaultIVFPQM(dim);
        (*cfg)[knowhere::indexparam::NBITS] = pq_nbits > 0 ? pq_nbits : 8;
    } else if (itype == "IVF_SQ8") {
        (*cfg)[knowhere::indexparam::NLIST] = nlist > 0 ? nlist : 128;
        (*cfg)[knowhere::indexparam::SQ_TYPE] = stype;
    } else if (itype == "DISKANN") {
        (*cfg)["index_prefix"] = diskann_prefixes_[segment_id];
        (*cfg)["data_path"]    = diskann_prefixes_[segment_id] + ".raw_data.bin";
        (*cfg)["max_degree"]       = HNSWM();
        (*cfg)["search_list_size"]  = HNSWEfSearch();
        (*cfg)["pq_code_budget_gb"]  = 0.0f;
        (*cfg)["build_dram_budget_gb"] = 0.0f;
        (*cfg)["disk_pq_dims"]  = 0;
        (*cfg)["accelerate_build"] = false;
        (*cfg)["shuffle_build"]   = false;
        (*cfg)["vec_field_size_gb"] = float(n) * dim * sizeof(float) / (1024*1024*1024);
    } else {
        std::fprintf(stderr, "plasmod: unsupported segment index_type=%s\n", itype.c_str());
        delete cfg;
        return kErrInvalidParam;
    }

    auto result = knowhere::IndexFactory::Instance().Create<float>(
        itype, knowhere::Version::GetCurrentVersion().VersionNumber());
    if (!result.has_value()) {
        delete cfg;
        return kErrBuildFailed;
    }
    using KnowhereIndex = knowhere::Index<knowhere::IndexNode>;
    auto* idx = new KnowhereIndex(std::move(result.value()));
    auto ds = knowhere::GenDataSet(n, dim, vectors);
    auto status = idx->Build(ds, *cfg);
    if (status != knowhere::Status::success) {
        delete idx;
        delete cfg;
        return kErrBuildFailed;
    }
    auto entry = std::make_shared<Entry>();
    entry->index_ptr   = idx;
    entry->config_ptr  = cfg;
    entry->dim          = dim;
    entry->num_vectors  = n;
    entry->index_type   = itype;
    entry->metric_type  = metric_type;
    entry->ivf_nlist   = nlist > 0 ? nlist : 128;
    entry->ivf_nprobe  = nprobe > 0 ? nprobe : 32;
    entry->ivf_pq_m     = pq_m > 0 ? pq_m : DefaultIVFPQM(dim);
    entry->ivf_pq_nbits = pq_nbits > 0 ? pq_nbits : 8;
    entry->ivf_sq_type = stype;

    std::unique_lock lk(mu_);
    auto it = segments_.find(segment_id);
    if (it != segments_.end() && it->second) DestroyEntry(*it->second);
    segments_[segment_id] = entry;
    diskann_prefixes_.erase(segment_id);  // clear prefix after use
    return kOK;
}

// ── SetDiskANNPrefix ────────────────────────────────────────────────────
int SegmentIndexManager::SetDiskANNPrefix(
    const std::string& segment_id,
    const std::string& index_prefix) {
    std::unique_lock lk(mu_);
    diskann_prefixes_[segment_id] = index_prefix;
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

    // Build standard config (no OpenMP, no hot-path).
    // Per-index-type search params: EF for HNSW, NPROBE for IVF variants.
    knowhere::Json cfg;
    cfg[knowhere::meta::METRIC_TYPE] = entry.metric_type;
    cfg[knowhere::meta::DIM]        = entry.dim;
    cfg[knowhere::meta::TOPK]       = topk;

    if (entry.index_type == "HNSW") {
        cfg[knowhere::indexparam::HNSW_M]         = HNSWM();
        cfg[knowhere::indexparam::EFCONSTRUCTION] = HNSWEfConstruction();
        cfg[knowhere::indexparam::EF]             = std::max(topk * 2, HNSWEfSearch());
    } else if (entry.index_type == "IVF_FLAT") {
        cfg[knowhere::indexparam::NLIST]  = entry.ivf_nlist;
        cfg[knowhere::indexparam::NPROBE] = entry.ivf_nprobe;
    } else if (entry.index_type == "IVF_PQ") {
        cfg[knowhere::indexparam::NLIST]  = entry.ivf_nlist;
        cfg[knowhere::indexparam::M]      = entry.ivf_pq_m;
        cfg[knowhere::indexparam::NBITS]  = entry.ivf_pq_nbits;
        cfg[knowhere::indexparam::NPROBE] = entry.ivf_nprobe;
    } else if (entry.index_type == "IVF_SQ8") {
        cfg[knowhere::indexparam::NLIST]    = entry.ivf_nlist;
        cfg[knowhere::indexparam::SQ_TYPE]  = entry.ivf_sq_type;
        cfg[knowhere::indexparam::NPROBE]   = entry.ivf_nprobe;
    }

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
