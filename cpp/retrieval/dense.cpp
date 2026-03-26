// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Dense retrieval implementation using Knowhere HNSW/IVF indexes.
//
// This file depends on the Knowhere vector-index library (zilliztech/knowhere v2.3.12).
// When ANDB_USE_KNOWHERE is defined (cmake -DANDB_WITH_KNOWHERE=ON, the default),
// KnowhereIndexWrapper calls the real Knowhere IndexFactory / Train / Search API.
// Without Knowhere (ANDB_WITH_KNOWHERE=OFF), a brute-force inner-product fallback
// is compiled in so the library remains functional for development/testing.

#include "andb/dense.h"
#include <algorithm>
#include <cmath>
#include <cstring>
#include <stdexcept>

#ifdef ANDB_USE_KNOWHERE
#include "knowhere/index/index_factory.h"
#include "knowhere/dataset.h"
#include "knowhere/binaryset.h"
#include "knowhere/version.h"
#endif

namespace andb {

#ifdef ANDB_USE_KNOWHERE

// Real Knowhere HNSW/IVF index wrapper.
class KnowhereIndexWrapper {
public:
    KnowhereIndexWrapper() = default;
    ~KnowhereIndexWrapper() = default;

    bool Init(const IndexConfig& config) {
        config_ = config;
        const std::string& idx_type =
            config_.index_type.empty() ? "HNSW" : config_.index_type;
        const int32_t ver =
            knowhere::Version::GetCurrentVersion().VersionNumber();
        auto res = knowhere::IndexFactory::Instance().Create<knowhere::fp32>(
            idx_type, ver);
        if (!res.has_value()) {
            return false;
        }
        index_ = res.value();
        build_cfg_["M"] = config_.hnsw_m > 0 ? config_.hnsw_m : 16;
        build_cfg_["efConstruction"] =
            config_.hnsw_ef_construction > 0 ? config_.hnsw_ef_construction : 256;
        build_cfg_["ef"] =
            config_.hnsw_ef_search > 0 ? config_.hnsw_ef_search : 64;
        build_cfg_["metric_type"] =
            config_.metric_type.empty() ? "IP" : config_.metric_type;
        return true;
    }

    bool Build(const float* vectors, int64_t num_vectors) {
        if (!vectors || num_vectors <= 0) return false;
        num_vectors_ = num_vectors;
        auto ds = knowhere::GenDataSet(num_vectors, config_.dim, vectors);
        if (index_.Train(*ds, build_cfg_) != knowhere::Status::success)
            return false;
        return index_.Add(*ds, build_cfg_) == knowhere::Status::success;
    }

    bool Add(const float* vectors, int64_t num_vectors) {
        if (!vectors || num_vectors <= 0) return false;
        auto ds = knowhere::GenDataSet(num_vectors, config_.dim, vectors);
        if (index_.Add(*ds, build_cfg_) != knowhere::Status::success)
            return false;
        num_vectors_ += num_vectors;
        return true;
    }

    SearchResult Search(
        const float* query_vectors,
        int64_t num_queries,
        int32_t top_k,
        const uint8_t* filter_bitset,
        size_t filter_size
    ) const {
        SearchResult result;
        if (!query_vectors || num_queries <= 0 || top_k <= 0 ||
                num_vectors_ == 0) {
            return result;
        }
        knowhere::Json search_cfg = build_cfg_;
        search_cfg["k"] = top_k;
        auto q_ds = knowhere::GenDataSet(num_queries, config_.dim,
                                         query_vectors);
        knowhere::BitsetView bv;
        if (filter_bitset && filter_size > 0) {
            bv = knowhere::BitsetView(filter_bitset, num_vectors_);
        }
        auto res = index_.Search(*q_ds, search_cfg, bv);
        if (!res.has_value()) return result;
        const auto& ds_out = res.value();
        const int64_t* ids   = ds_out->GetIds();
        const float*   dists = ds_out->GetDistance();
        const int64_t  total = num_queries * top_k;
        for (int64_t i = 0; i < total; ++i) {
            if (ids[i] >= 0) {
                result.ids.push_back(ids[i]);
                result.distances.push_back(dists[i]);
            }
        }
        result.count = static_cast<int64_t>(result.ids.size());
        return result;
    }

    bool Serialize(std::vector<uint8_t>& output) const {
        // Flatten BinarySet: [4B name_len][name][8B data_len][data] ...
        auto res = index_.Serialize(build_cfg_);
        if (!res.has_value()) return false;
        const auto& bset = res.value();
        for (const auto& [name, binary] : bset.binary_map_) {
            uint32_t nlen = static_cast<uint32_t>(name.size());
            const uint8_t* np = reinterpret_cast<const uint8_t*>(&nlen);
            output.insert(output.end(), np, np + sizeof(nlen));
            output.insert(output.end(), name.begin(), name.end());
            uint64_t dlen = static_cast<uint64_t>(binary->size);
            const uint8_t* dp = reinterpret_cast<const uint8_t*>(&dlen);
            output.insert(output.end(), dp, dp + sizeof(dlen));
            output.insert(output.end(),
                          binary->data.get(),
                          binary->data.get() + binary->size);
        }
        return true;
    }

    bool Deserialize(const std::vector<uint8_t>& input) {
        knowhere::BinarySet bset;
        size_t pos = 0;
        while (pos + sizeof(uint32_t) <= input.size()) {
            uint32_t nlen = 0;
            std::memcpy(&nlen, input.data() + pos, sizeof(nlen));
            pos += sizeof(nlen);
            if (pos + nlen > input.size()) return false;
            std::string name(reinterpret_cast<const char*>(input.data() + pos),
                             nlen);
            pos += nlen;
            if (pos + sizeof(uint64_t) > input.size()) return false;
            uint64_t dlen = 0;
            std::memcpy(&dlen, input.data() + pos, sizeof(dlen));
            pos += sizeof(dlen);
            if (pos + dlen > input.size()) return false;
            auto data = std::make_shared<uint8_t[]>(dlen);
            std::memcpy(data.get(), input.data() + pos, dlen);
            pos += dlen;
            bset.Append(name, data, static_cast<int64_t>(dlen));
        }
        return index_.Deserialize(bset, build_cfg_) ==
               knowhere::Status::success;
    }

    int64_t Count() const { return num_vectors_; }
    int32_t Dim()   const { return config_.dim; }
    std::string Type() const {
        return config_.index_type.empty() ? "HNSW" : config_.index_type;
    }

private:
    IndexConfig config_;
    knowhere::Index<knowhere::IndexNode> index_;
    knowhere::Json build_cfg_;
    int64_t num_vectors_ = 0;
};

#else  // ANDB_USE_KNOWHERE not defined — brute-force fallback

// Brute-force inner-product index (used when Knowhere is not available).
class KnowhereIndexWrapper {
public:
    KnowhereIndexWrapper() = default;
    ~KnowhereIndexWrapper() = default;

    bool Init(const IndexConfig& config) {
        config_ = config;
        return true;
    }

    bool Build(const float* vectors, int64_t num_vectors) {
        if (!vectors || num_vectors <= 0) return false;
        int64_t total = num_vectors * config_.dim;
        vectors_.resize(total);
        std::copy(vectors, vectors + total, vectors_.begin());
        num_vectors_ = num_vectors;
        return true;
    }

    bool Add(const float* vectors, int64_t num_vectors) {
        if (!vectors || num_vectors <= 0) return false;
        int64_t old_size = vectors_.size();
        int64_t add_size = num_vectors * config_.dim;
        vectors_.resize(old_size + add_size);
        std::copy(vectors, vectors + add_size, vectors_.begin() + old_size);
        num_vectors_ += num_vectors;
        return true;
    }

    SearchResult Search(
        const float* query_vectors,
        int64_t num_queries,
        int32_t top_k,
        const uint8_t* filter_bitset,
        size_t filter_size
    ) const {
        SearchResult result;
        if (!query_vectors || num_queries <= 0 || top_k <= 0 ||
                num_vectors_ == 0) {
            return result;
        }
        std::vector<std::pair<float, int64_t>> scores;
        scores.reserve(num_vectors_);
        for (int64_t q = 0; q < num_queries; ++q) {
            scores.clear();
            const float* query = query_vectors + q * config_.dim;
            for (int64_t i = 0; i < num_vectors_; ++i) {
                if (filter_bitset && filter_size > 0) {
                    size_t byte_idx = static_cast<size_t>(i) / 8;
                    size_t bit_idx  = static_cast<size_t>(i) % 8;
                    if (byte_idx < filter_size &&
                            (filter_bitset[byte_idx] & (1u << bit_idx)))
                        continue;
                }
                const float* vec = vectors_.data() + i * config_.dim;
                float score = 0.0f;
                for (int32_t d = 0; d < config_.dim; ++d)
                    score += query[d] * vec[d];
                scores.emplace_back(score, i);
            }
            std::partial_sort(
                scores.begin(),
                scores.begin() +
                    std::min(static_cast<size_t>(top_k), scores.size()),
                scores.end(),
                [](const auto& a, const auto& b) {
                    return a.first > b.first;
                });
            int32_t k = std::min(static_cast<int32_t>(scores.size()), top_k);
            for (int32_t j = 0; j < k; ++j) {
                result.ids.push_back(scores[j].second);
                result.distances.push_back(scores[j].first);
            }
        }
        result.count = static_cast<int64_t>(result.ids.size());
        return result;
    }

    bool Serialize(std::vector<uint8_t>& output) const { return true; }
    bool Deserialize(const std::vector<uint8_t>& input) { return true; }
    int64_t Count() const { return num_vectors_; }
    int32_t Dim()   const { return config_.dim; }
    std::string Type() const { return config_.index_type; }

private:
    IndexConfig config_;
    std::vector<float> vectors_;
    int64_t num_vectors_ = 0;
};

#endif  // ANDB_USE_KNOWHERE

// DenseRetriever implementation

DenseRetriever::DenseRetriever() : impl_(std::make_unique<KnowhereIndexWrapper>()) {}

DenseRetriever::~DenseRetriever() = default;

DenseRetriever::DenseRetriever(DenseRetriever&&) noexcept = default;
DenseRetriever& DenseRetriever::operator=(DenseRetriever&&) noexcept = default;

bool DenseRetriever::Init(const IndexConfig& config) {
    config_ = config;
    ready_ = impl_->Init(config);
    return ready_;
}

bool DenseRetriever::Build(const float* vectors, int64_t num_vectors) {
    if (!impl_->Build(vectors, num_vectors)) {
        return false;
    }
    ready_ = true;
    return true;
}

bool DenseRetriever::Add(const float* vectors, int64_t num_vectors) {
    return impl_->Add(vectors, num_vectors);
}

SearchResult DenseRetriever::Search(
    const float* query_vectors,
    int64_t num_queries,
    int32_t top_k,
    const uint8_t* filter_bitset,
    size_t filter_size
) const {
    if (!ready_) {
        return SearchResult{};
    }
    return impl_->Search(query_vectors, num_queries, top_k, filter_bitset, filter_size);
}

bool DenseRetriever::Serialize(std::vector<uint8_t>& output) const {
    return impl_->Serialize(output);
}

bool DenseRetriever::Deserialize(const std::vector<uint8_t>& input) {
    ready_ = impl_->Deserialize(input);
    return ready_;
}

bool DenseRetriever::Load(const std::string& path) {
    // TODO: Load from file
    return false;
}

bool DenseRetriever::Save(const std::string& path) const {
    // TODO: Save to file
    return false;
}

int64_t DenseRetriever::Count() const {
    return impl_->Count();
}

int32_t DenseRetriever::Dim() const {
    return impl_->Dim();
}

std::string DenseRetriever::Type() const {
    return impl_->Type();
}

bool DenseRetriever::IsReady() const {
    return ready_;
}

}  // namespace andb
