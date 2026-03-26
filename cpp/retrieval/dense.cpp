// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Dense retrieval — backed by CogDB-internal Knowhere (cpp/knowhere/).
// Knowhere is source-level integrated: no external git clone or FetchContent.
// Internal engines:
//   cpp/knowhere/engines/hnsw_engine/   ← hnswlib HNSW algorithm
//   cpp/knowhere/engines/faiss_engine/  ← Meta faiss (slim build, no GPU)
//   cpp/knowhere/engines/diskann_engine/← Microsoft DiskANN (opt-in)

#include "andb/dense.h"

// Knowhere public API
#include "knowhere/index/index_factory.h"
#include "knowhere/dataset.h"
#include "knowhere/config.h"
#include "knowhere/comp/thread_pool.h"

#include <algorithm>
#include <cassert>
#include <mutex>
#include <stdexcept>
#include <vector>

namespace andb {

// ── Default index config ─────────────────────────────────────────────────────
static constexpr int kDefaultM              = 16;
static constexpr int kDefaultEfConstruction = 256;
static constexpr int kDefaultEfSearch       = 64;

// ── HNSWIndexWrapper ─────────────────────────────────────────────────────────
// Wraps a single Knowhere HNSW index.
class HNSWIndexWrapper {
public:
    HNSWIndexWrapper() = default;
    ~HNSWIndexWrapper() = default;

    bool Init(const IndexConfig& cfg) {
        cfg_ = cfg;
        if (cfg_.dim <= 0) return false;

        int M   = cfg_.hnsw_m > 0              ? cfg_.hnsw_m              : kDefaultM;
        int efC = cfg_.hnsw_ef_construction > 0 ? cfg_.hnsw_ef_construction : kDefaultEfConstruction;

        build_cfg_[knowhere::meta::METRIC_TYPE]         = "IP";
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
        // GenDataSet returns DataSetPtr (shared_ptr<DataSet>); pass directly.
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
        search_cfg[knowhere::meta::TOPK]        = topk;
        search_cfg[knowhere::indexparam::EF]    = std::max(topk * 2, kDefaultEfSearch);

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

private:
    IndexConfig cfg_;
    int         dim_         = 0;
    int64_t     num_vectors_ = 0;
    knowhere::Json  build_cfg_;
    std::unique_ptr<knowhere::Index<knowhere::IndexNode>> index_;
    mutable std::mutex mu_;
};

// ── DenseRetrieverImpl ───────────────────────────────────────────────────────
class DenseRetrieverImpl {
public:
    bool Init(const IndexConfig& cfg) {
        wrapper_ = std::make_unique<HNSWIndexWrapper>();
        return wrapper_->Init(cfg);
    }
    bool Build(const float* vecs, int64_t n) {
        return wrapper_ && wrapper_->Build(vecs, n);
    }
    bool Add(const float* vecs, int64_t n) {
        return wrapper_ && wrapper_->Add(vecs, n);
    }
    bool Search(const float* query, int64_t nq, int topk,
                const uint8_t* allow_bits, int64_t allow_count,
                int64_t* ids, float* dists) {
        return wrapper_ && wrapper_->Search(
            query, nq, topk, allow_bits, allow_count, ids, dists);
    }
    int64_t Count() const { return wrapper_ ? wrapper_->num_vectors() : 0; }
    int     Dim()   const { return wrapper_ ? wrapper_->dim()         : 0; }

private:
    std::unique_ptr<HNSWIndexWrapper> wrapper_;
};

// ── DenseRetriever public API ────────────────────────────────────────────────
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

}  // namespace andb
