// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Dense retrieval — backed by CogDB-internal Knowhere (cpp/knowhere/).
// Knowhere is source-level integrated: no external git clone or FetchContent.
//
// GPU path (ANDB_WITH_GPU=1, Linux + CUDA):
//   NVIDIA RAFT — "GPU_CAGRA" (HNSW-equivalent on GPU, NVIDIA nv-tabular/raft 44.00.00).
//   Falls back to CPU "HNSW" automatically at runtime if no CUDA device is found.
//   GPU index selected at Init() time; Build/Search/Add always route to the active index.
//
// CPU path (default, ANDB_WITH_GPU=0):
//   hnswlib HNSW — always used, no CUDA required.
//
// Internal engines (cpp/vendor/):
//   engines/hnsw_engine/   ← hnswlib HNSW (CPU, MIT)
//   engines/faiss_engine/  ← Meta faiss slim (CPU, MIT)
//   engines/diskann_engine/ ← Microsoft DiskANN (opt-in)
//   src/index/gpu_raft/    ← NVIDIA RAFT wrappers: CAGRA/brute_force/ivf_flat/ivf_pq (opt-in)

#include "plasmod/dense.h"

// Knowhere public API
#include "knowhere/index/index_factory.h"
#include "knowhere/dataset.h"
#include "knowhere/config.h"
#include "knowhere/comp/thread_pool.h"

#ifdef ANDB_WITH_GPU
#include <cuda_runtime_api.h>
#endif

#include <algorithm>
#include <cassert>
#include <cerrno>
#include <cstring>
#include <mutex>
#include <stdexcept>
#include <vector>

namespace plasmod {

// ── Default index config ─────────────────────────────────────────────────────
static constexpr int kDefaultM              = 16;
static constexpr int kDefaultEfConstruction = 256;
static constexpr int kDefaultEfSearch        = 64;

// ── GPU detection (runtime) ──────────────────────────────────────────────────
// Returns true if at least one CUDA GPU is visible to the driver.
// Called at Init() time so the GPU check is a one-time cost, not per-search.
static bool cudaAvailable() {
#ifdef ANDB_WITH_GPU
    int count = 0;
    if (cudaGetDeviceCount(&count) != cudaSuccess) return false;
    return count > 0;
#else
    return false;
#endif
}

// ── tryCreateGpuIndex ─────────────────────────────────────────────────────────
// Attempts to create a GPU RAFT index via Knowhere's IndexFactory.
// Returns nullptr if the GPU path is unavailable or the factory call fails.
static std::unique_ptr<knowhere::Index<knowhere::IndexNode>>
tryCreateGpuIndex(const IndexConfig& cfg, const knowhere::Json& cfg_obj) {
#ifdef ANDB_WITH_GPU
    if (!cudaAvailable()) return nullptr;

    // GPU_CAGRA: CUDA-accelerated HNSW-equivalent via NVIDIA RAFT.
    // Falls back gracefully at the factory level if CUDA is absent at runtime.
    auto result = knowhere::IndexFactory::Instance().Create<float>(
        "GPU_CAGRA", knowhere::Version::GetCurrentVersion().VersionNumber());
    if (!result.has_value()) {
        // GPU not usable (no CUDA device or RAFT not compiled in) — fall through.
        return nullptr;
    }
    return std::make_unique<knowhere::Index<knowhere::IndexNode>>(std::move(result.value()));
#else
    (void)cfg; (void)cfg_obj;
    return nullptr;
#endif
}

// ── HNSWIndexWrapper ─────────────────────────────────────────────────────────
// Wraps a single Knowhere HNSW index (CPU fallback, always available).
class HNSWIndexWrapper {
public:
    HNSWIndexWrapper() = default;
    ~HNSWIndexWrapper() = default;

    bool Init(const IndexConfig& cfg) {
        cfg_ = cfg;
        if (cfg_.dim <= 0) return false;

        int M   = cfg_.hnsw_m > 0              ? cfg_.hnsw_m              : kDefaultM;
        int efC = cfg_.hnsw_ef_construction > 0 ? cfg_.hnsw_ef_construction : kDefaultEfConstruction;

        // Use metric from config, default to IP if not specified
        std::string metric = cfg_.metric_type.empty() ? "IP" : cfg_.metric_type;
        build_cfg_[knowhere::meta::METRIC_TYPE]         = metric;
        build_cfg_[knowhere::indexparam::M]             = M;
        build_cfg_[knowhere::indexparam::EFCONSTRUCTION] = efC;
        build_cfg_[knowhere::meta::DIM]                 = cfg_.dim;
        build_cfg_[knowhere::meta::TOPK]                = kDefaultEfSearch;

        auto result = knowhere::IndexFactory::Instance().Create<float>(
            "HNSW", knowhere::Version::GetCurrentVersion().VersionNumber());
        if (!result.has_value()) return false;
        index_ = std::make_unique<knowhere::Index<knowhere::IndexNode>>(
            std::move(result.value()));
        dim_ = cfg_.dim;
        return true;
    }

    bool Build(const float* vectors, int64_t n) {
        if (!index_ || !vectors || n <= 0) return false;
        std::lock_guard<std::mutex> lk(mu_);
        auto ds     = knowhere::GenDataSet(n, dim_, vectors);
        auto status = index_->Build(ds, build_cfg_);
        if (status != knowhere::Status::success) return false;
        num_vectors_ = n;
        return true;
    }

    bool Add(const float* vectors, int64_t n) {
        if (!index_ || !vectors || n <= 0) return false;
        std::lock_guard<std::mutex> lk(mu_);
        auto ds     = knowhere::GenDataSet(n, dim_, vectors);
        auto status = index_->Add(ds, build_cfg_);
        if (status != knowhere::Status::success) return false;
        num_vectors_ += n;
        return true;
    }

    bool Search(const float* query, int64_t nq, int topk,
                const uint8_t* allow_bits, int64_t allow_count,
                int64_t* out_ids, float* out_dists) {
        if (!index_ || !query) return false;
        std::lock_guard<std::mutex> lk(mu_);

        knowhere::Json search_cfg = build_cfg_;
        search_cfg[knowhere::meta::TOPK]     = topk;
        search_cfg[knowhere::indexparam::EF] = std::max(topk * 2, kDefaultEfSearch);

        auto qds = knowhere::GenDataSet(nq, dim_, query);

        knowhere::expected<knowhere::DataSetPtr> res;
        if (allow_bits && allow_count > 0) {
            knowhere::BitsetView bitset(allow_bits, allow_count);
            res = index_->Search(qds, search_cfg, bitset);
        } else {
            res = index_->Search(qds, search_cfg, knowhere::BitsetView());
        }

        if (!res.has_value()) return false;
        const int64_t total = nq * topk;
        std::copy_n(res.value()->GetIds(),      total, out_ids);
        std::copy_n(res.value()->GetDistance(), total, out_dists);
        return true;
    }

    int64_t num_vectors() const { return num_vectors_; }
    int     dim()         const { return dim_; }
    bool    is_gpu()      const { return false; }

private:
    IndexConfig cfg_;
    int         dim_         = 0;
    int64_t     num_vectors_ = 0;
    knowhere::Json  build_cfg_;
    std::unique_ptr<knowhere::Index<knowhere::IndexNode>> index_;
    mutable std::mutex mu_;
};

// ── GpuIndexWrapper ──────────────────────────────────────────────────────────
// Wraps a GPU RAFT index (CAGRA) obtained from tryCreateGpuIndex.
// Only instantiated when ANDB_WITH_GPU=1 and a CUDA device is visible.
class GpuIndexWrapper {
public:
    GpuIndexWrapper(std::unique_ptr<knowhere::Index<knowhere::IndexNode>> idx,
                    const IndexConfig& cfg)
        : index_(std::move(idx)), cfg_(cfg) {
        if (index_) {
            knowhere::Json init_cfg;
            init_cfg[knowhere::meta::DIM] = cfg.dim;
            (void)init_cfg; // RAFT GPU index may not need explicit Build-time config
            dim_ = cfg.dim;
        }
    }

    bool Build(const float* vectors, int64_t n) {
        if (!index_ || !vectors || n <= 0) return false;
        std::lock_guard<std::mutex> lk(mu_);
        auto ds     = knowhere::GenDataSet(n, dim_, vectors);
        knowhere::Json build_cfg;
        build_cfg[knowhere::meta::DIM]    = dim_;
        build_cfg[knowhere::meta::TOPK]    = kDefaultEfSearch;
        auto status = index_->Build(ds, build_cfg);
        if (status != knowhere::Status::success) return false;
        num_vectors_ = n;
        return true;
    }

    bool Add(const float* vectors, int64_t n) {
        if (!index_ || !vectors || n <= 0) return false;
        std::lock_guard<std::mutex> lk(mu_);
        auto ds     = knowhere::GenDataSet(n, dim_, vectors);
        auto status = index_->Add(ds, knowhere::Json{});
        if (status != knowhere::Status::success) return false;
        num_vectors_ += n;
        return true;
    }

    bool Search(const float* query, int64_t nq, int topk,
                const uint8_t* allow_bits, int64_t allow_count,
                int64_t* out_ids, float* out_dists) {
        if (!index_ || !query) return false;
        std::lock_guard<std::mutex> lk(mu_);
        knowhere::Json search_cfg;
        search_cfg[knowhere::meta::TOPK] = topk;
        auto qds = knowhere::GenDataSet(nq, dim_, query);
        knowhere::expected<knowhere::DataSetPtr> res;
        if (allow_bits && allow_count > 0) {
            knowhere::BitsetView bitset(allow_bits, allow_count);
            res = index_->Search(qds, search_cfg, bitset);
        } else {
            res = index_->Search(qds, search_cfg, knowhere::BitsetView());
        }
        if (!res.has_value()) return false;
        const int64_t total = nq * topk;
        std::copy_n(res.value()->GetIds(),      total, out_ids);
        std::copy_n(res.value()->GetDistance(), total, out_dists);
        return true;
    }

    int64_t num_vectors() const { return num_vectors_; }
    int     dim()         const { return dim_; }
    bool    is_gpu()      const { return true; }

private:
    std::unique_ptr<knowhere::Index<knowhere::IndexNode>> index_;
    IndexConfig cfg_;
    int         dim_         = 0;
    int64_t     num_vectors_ = 0;
    mutable std::mutex mu_;
};

// ── DenseRetrieverImpl ─────────────────────────────────────────────────────
// Dual-path retriever: GPU (CAGRA/RAFT) when available, CPU (HNSW) otherwise.
// Init() probes GPU availability once; Build/Search/Add always route to the
// selected backend without any per-call overhead.
class DenseRetrieverImpl {
public:
    bool Init(const IndexConfig& cfg) {
        // GPU path: attempt RAFT CAGRA. If CUDA unavailable or factory rejects
        // (no GPU compiled in), fall through to CPU HNSW.
        gpu_index_ = tryCreateGpuIndex(cfg, knowhere::Json{});
        if (gpu_index_) {
            gpu_wrapper_ = std::make_unique<GpuIndexWrapper>(std::move(gpu_index_), cfg);
            return true;
        }
        // CPU fallback: always available.
        hnsw_wrapper_ = std::make_unique<HNSWIndexWrapper>();
        return hnsw_wrapper_->Init(cfg);
    }

    bool Build(const float* vecs, int64_t n) {
        if (gpu_wrapper_) return gpu_wrapper_->Build(vecs, n);
        return hnsw_wrapper_ && hnsw_wrapper_->Build(vecs, n);
    }

    bool Add(const float* vecs, int64_t n) {
        if (gpu_wrapper_) return gpu_wrapper_->Add(vecs, n);
        return hnsw_wrapper_ && hnsw_wrapper_->Add(vecs, n);
    }

    bool Search(const float* query, int64_t nq, int topk,
                const uint8_t* allow_bits, int64_t allow_count,
                int64_t* ids, float* dists) {
        if (gpu_wrapper_) return gpu_wrapper_->Search(query, nq, topk, allow_bits, allow_count, ids, dists);
        return hnsw_wrapper_ && hnsw_wrapper_->Search(query, nq, topk, allow_bits, allow_count, ids, dists);
    }

    int64_t Count() const {
        if (gpu_wrapper_) return gpu_wrapper_->num_vectors();
        return hnsw_wrapper_ ? hnsw_wrapper_->num_vectors() : 0;
    }
    int Dim() const {
        if (gpu_wrapper_) return gpu_wrapper_->dim();
        return hnsw_wrapper_ ? hnsw_wrapper_->dim() : 0;
    }
    bool IsGpu() const { return gpu_wrapper_ != nullptr; }

private:
    // GPU path (non-null when CUDA available at Init time)
    std::unique_ptr<knowhere::Index<knowhere::IndexNode>> gpu_index_;   // owned by wrapper
    std::unique_ptr<GpuIndexWrapper> gpu_wrapper_;
    // CPU fallback
    std::unique_ptr<HNSWIndexWrapper> hnsw_wrapper_;
};

// ── DenseRetriever public API ───────────────────────────────────────────────
DenseRetriever::DenseRetriever()  = default;
DenseRetriever::~DenseRetriever() = default;

bool DenseRetriever::Init(const IndexConfig& cfg) {
    impl_ = std::make_unique<DenseRetrieverImpl>();
    return impl_->Init(cfg);
}
bool DenseRetriever::Build(const float* v, int64_t n) {
    return impl_ && impl_->Build(v, n);
}
bool DenseRetriever::Add(const float* v, int64_t n) {
    return impl_ && impl_->Add(v, n);
}
bool DenseRetriever::Search(const float* q, int64_t nq, int topk,
                            const uint8_t* allow_bits, int64_t allow_count,
                            int64_t* out_ids, float* out_dists) {
    return impl_ && impl_->Search(q, nq, topk, allow_bits, allow_count,
                                  out_ids, out_dists);
}
int64_t DenseRetriever::Count() const { return impl_ ? impl_->Count() : 0; }
int     DenseRetriever::Dim()   const { return impl_ ? impl_->Dim()   : 0; }

}  // namespace plasmod
