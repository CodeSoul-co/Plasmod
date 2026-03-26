// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Dense retrieval — HNSW index backed by vendored hnswlib.
//
// hnswlib (nmslib/hnswlib v0.8.0, MIT) is vendored at cpp/include/hnswlib/.
// No external downloads or network access required at build time.
//
// Search uses Inner Product (IP) distance, consistent with the original
// Knowhere HNSW configuration (metric_type="IP").

#include "andb/dense.h"
#include "hnswlib/hnswlib.h"

#include <algorithm>
#include <cstring>
#include <mutex>
#include <stdexcept>
#include <vector>

namespace andb {

// ---------------------------------------------------------------------------
// HNSWIndexWrapper — thin RAII wrapper around hnswlib::HierarchicalNSW<float>
// ---------------------------------------------------------------------------
class HNSWIndexWrapper {
public:
    HNSWIndexWrapper() = default;
    ~HNSWIndexWrapper() = default;

    bool Init(const IndexConfig& cfg) {
        cfg_ = cfg;
        if (cfg_.dim <= 0) return false;
        space_ = std::make_unique<hnswlib::InnerProductSpace>(
            static_cast<size_t>(cfg_.dim));
        return true;
    }

    // Build: create the HNSW graph from a batch of dense vectors.
    bool Build(const float* vectors, int64_t num_vectors) {
        if (!space_ || !vectors || num_vectors <= 0) return false;
        std::lock_guard<std::mutex> lock(mu_);

        int M              = cfg_.hnsw_m > 0              ? cfg_.hnsw_m              : 16;
        int ef_construction= cfg_.hnsw_ef_construction > 0? cfg_.hnsw_ef_construction: 256;

        index_ = std::make_unique<hnswlib::HierarchicalNSW<float>>(
            space_.get(),
            static_cast<size_t>(num_vectors), // max_elements
            static_cast<size_t>(M),
            static_cast<size_t>(ef_construction));

        for (int64_t i = 0; i < num_vectors; ++i) {
            index_->addPoint(
                vectors + i * cfg_.dim,
                static_cast<hnswlib::labeltype>(i));
        }
        num_vectors_ = num_vectors;
        return true;
    }

    // Add: incrementally insert vectors (resizes capacity as needed).
    bool Add(const float* vectors, int64_t num_vectors) {
        if (!space_ || !vectors || num_vectors <= 0) return false;
        std::lock_guard<std::mutex> lock(mu_);

        if (!index_) {
            // First call: treat like Build
            int M = cfg_.hnsw_m > 0 ? cfg_.hnsw_m : 16;
            int ef = cfg_.hnsw_ef_construction > 0 ? cfg_.hnsw_ef_construction : 256;
            index_ = std::make_unique<hnswlib::HierarchicalNSW<float>>(
                space_.get(),
                static_cast<size_t>(num_vectors),
                static_cast<size_t>(M),
                static_cast<size_t>(ef));
        } else {
            index_->resizeIndex(
                static_cast<size_t>(num_vectors_ + num_vectors));
        }

        for (int64_t i = 0; i < num_vectors; ++i) {
            index_->addPoint(
                vectors + i * cfg_.dim,
                static_cast<hnswlib::labeltype>(num_vectors_ + i));
        }
        num_vectors_ += num_vectors;
        return true;
    }

    SearchResult Search(
        const float* query_vectors,
        int64_t       num_queries,
        int32_t       top_k,
        const uint8_t* filter_bitset,
        size_t         filter_size
    ) const {
        SearchResult result;
        if (!index_ || !query_vectors || num_queries <= 0 ||
                top_k <= 0 || num_vectors_ == 0) {
            return result;
        }

        int ef_search = cfg_.hnsw_ef_search > 0 ? cfg_.hnsw_ef_search : 64;
        // ef must be >= k
        index_->setEf(static_cast<size_t>(std::max(ef_search, top_k)));

        for (int64_t q = 0; q < num_queries; ++q) {
            const float* qvec = query_vectors + q * cfg_.dim;

            // hnswlib filter: return true to *exclude* an element
            hnswlib::BaseFilterFunctor* filter_fn = nullptr;
            struct BitsetFilter : hnswlib::BaseFilterFunctor {
                const uint8_t* bits;
                size_t         size;
                BitsetFilter(const uint8_t* b, size_t s) : bits(b), size(s) {}
                bool operator()(hnswlib::labeltype id) override {
                    size_t byte_idx = static_cast<size_t>(id) / 8;
                    size_t bit_idx  = static_cast<size_t>(id) % 8;
                    return byte_idx < size &&
                           (bits[byte_idx] & (1u << bit_idx)) != 0;
                }
            };
            BitsetFilter bf(filter_bitset,
                            filter_bitset ? filter_size : 0);
            if (filter_bitset && filter_size > 0) {
                filter_fn = &bf;
            }

            auto pq = index_->searchKnn(
                qvec,
                static_cast<size_t>(top_k),
                filter_fn);

            // searchKnn returns a max-heap (largest distance first for IP →
            // largest inner-product first).  Drain it in descending order.
            std::vector<std::pair<float, hnswlib::labeltype>> hits;
            hits.reserve(pq.size());
            while (!pq.empty()) {
                hits.push_back(pq.top());
                pq.pop();
            }
            // Reverse so best (highest IP) comes first
            std::reverse(hits.begin(), hits.end());
            for (auto& [dist, label] : hits) {
                result.ids.push_back(static_cast<int64_t>(label));
                result.distances.push_back(dist);
            }
        }
        result.count = static_cast<int64_t>(result.ids.size());
        return result;
    }

    // Serialize / Deserialize — hnswlib supports saveIndex / loadIndex.
    bool Serialize(std::vector<uint8_t>& output) const {
        if (!index_) return false;
        // Save to a temp path and read back; hnswlib has no in-memory stream API.
        const std::string tmp = "/tmp/_andb_hnsw_ser.bin";
        index_->saveIndex(tmp);
        FILE* f = std::fopen(tmp.c_str(), "rb");
        if (!f) return false;
        std::fseek(f, 0, SEEK_END);
        size_t sz = static_cast<size_t>(std::ftell(f));
        std::rewind(f);
        output.resize(sz);
        std::fread(output.data(), 1, sz, f);
        std::fclose(f);
        std::remove(tmp.c_str());
        return true;
    }

    bool Deserialize(const std::vector<uint8_t>& input) {
        if (!space_ || input.empty()) return false;
        const std::string tmp = "/tmp/_andb_hnsw_deser.bin";
        FILE* f = std::fopen(tmp.c_str(), "wb");
        if (!f) return false;
        std::fwrite(input.data(), 1, input.size(), f);
        std::fclose(f);
        index_ = std::make_unique<hnswlib::HierarchicalNSW<float>>(
            space_.get(), tmp);
        num_vectors_ = static_cast<int64_t>(index_->getCurrentElementCount());
        std::remove(tmp.c_str());
        return true;
    }

    int64_t     Count() const { return num_vectors_; }
    int32_t     Dim()   const { return cfg_.dim; }
    std::string Type()  const {
        return cfg_.index_type.empty() ? "HNSW" : cfg_.index_type;
    }

private:
    IndexConfig cfg_;
    std::unique_ptr<hnswlib::InnerProductSpace>      space_;
    std::unique_ptr<hnswlib::HierarchicalNSW<float>> index_;
    int64_t num_vectors_ = 0;
    mutable std::mutex mu_;
};

// ---------------------------------------------------------------------------
// DenseRetriever — public interface (delegates to HNSWIndexWrapper)
// ---------------------------------------------------------------------------

DenseRetriever::DenseRetriever()
    : impl_(std::make_unique<HNSWIndexWrapper>()) {}

DenseRetriever::~DenseRetriever() = default;
DenseRetriever::DenseRetriever(DenseRetriever&&) noexcept = default;
DenseRetriever& DenseRetriever::operator=(DenseRetriever&&) noexcept = default;

bool DenseRetriever::Init(const IndexConfig& config) {
    config_ = config;
    ready_  = impl_->Init(config);
    return ready_;
}

bool DenseRetriever::Build(const float* vectors, int64_t num_vectors) {
    if (!impl_->Build(vectors, num_vectors)) return false;
    ready_ = true;
    return true;
}

bool DenseRetriever::Add(const float* vectors, int64_t num_vectors) {
    return impl_->Add(vectors, num_vectors);
}

SearchResult DenseRetriever::Search(
    const float*  query_vectors,
    int64_t       num_queries,
    int32_t       top_k,
    const uint8_t* filter_bitset,
    size_t         filter_size
) const {
    if (!ready_) return SearchResult{};
    return impl_->Search(query_vectors, num_queries, top_k,
                         filter_bitset, filter_size);
}

bool DenseRetriever::Serialize(std::vector<uint8_t>& output) const {
    return impl_->Serialize(output);
}

bool DenseRetriever::Deserialize(const std::vector<uint8_t>& input) {
    ready_ = impl_->Deserialize(input);
    return ready_;
}

bool DenseRetriever::Load(const std::string&) { return false; }
bool DenseRetriever::Save(const std::string&) const { return false; }

int64_t     DenseRetriever::Count() const { return impl_->Count(); }
int32_t     DenseRetriever::Dim()   const { return impl_->Dim();   }
std::string DenseRetriever::Type()  const { return impl_->Type();  }
bool        DenseRetriever::IsReady() const { return ready_; }

}  // namespace andb
