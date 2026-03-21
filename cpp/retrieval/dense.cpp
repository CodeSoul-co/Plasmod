// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Dense retrieval implementation using Knowhere HNSW/IVF indexes.
// Uses real Knowhere when ANDB_USE_KNOWHERE=1, otherwise falls back to stub.

#include "andb/dense.h"
#include <algorithm>
#include <cmath>
#include <stdexcept>

#ifdef ANDB_USE_KNOWHERE
#include "knowhere/index/index_factory.h"
#include "knowhere/common/config.h"
#include "knowhere/common/dataset.h"
#include "knowhere/utils/bitset_view.h"
#endif

namespace andb {

#ifdef ANDB_USE_KNOWHERE
// Real Knowhere implementation
class KnowhereIndexWrapper {
public:
    KnowhereIndexWrapper() = default;
    ~KnowhereIndexWrapper() = default;
    
    bool Init(const IndexConfig& config) {
        config_ = config;
        
        // Create Knowhere index based on type
        knowhere::Json json_config;
        json_config["dim"] = config.dim;
        json_config["metric_type"] = config.metric_type;
        
        if (config.index_type == "HNSW") {
            json_config["M"] = 16;
            json_config["efConstruction"] = 200;
            json_config["ef"] = 100;
            index_ = knowhere::IndexFactory::Instance().Create(
                knowhere::IndexEnum::INDEX_HNSW, knowhere::Version::GetCurrentVersion());
        } else if (config.index_type == "IVF_FLAT") {
            json_config["nlist"] = 128;
            json_config["nprobe"] = 16;
            index_ = knowhere::IndexFactory::Instance().Create(
                knowhere::IndexEnum::INDEX_FAISS_IVFFLAT, knowhere::Version::GetCurrentVersion());
        } else {
            // Default to HNSW
            json_config["M"] = 16;
            json_config["efConstruction"] = 200;
            json_config["ef"] = 100;
            index_ = knowhere::IndexFactory::Instance().Create(
                knowhere::IndexEnum::INDEX_HNSW, knowhere::Version::GetCurrentVersion());
        }
        
        config_json_ = json_config;
        return index_ != nullptr;
    }
    
    bool Build(const float* vectors, int64_t num_vectors) {
        if (!index_ || !vectors || num_vectors <= 0) {
            return false;
        }
        
        auto dataset = knowhere::GenDataSet(num_vectors, config_.dim, vectors);
        auto status = index_->Build(dataset, config_json_);
        if (status != knowhere::Status::success) {
            return false;
        }
        
        num_vectors_ = num_vectors;
        return true;
    }
    
    bool Add(const float* vectors, int64_t num_vectors) {
        if (!index_ || !vectors || num_vectors <= 0) {
            return false;
        }
        
        auto dataset = knowhere::GenDataSet(num_vectors, config_.dim, vectors);
        auto status = index_->Add(dataset, config_json_);
        if (status != knowhere::Status::success) {
            return false;
        }
        
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
        
        if (!index_ || !query_vectors || num_queries <= 0 || top_k <= 0) {
            return result;
        }
        
        auto query_dataset = knowhere::GenDataSet(num_queries, config_.dim, query_vectors);
        
        knowhere::Json search_config = config_json_;
        search_config["k"] = top_k;
        
        // Apply filter bitset if provided
        knowhere::BitsetView bitset_view;
        if (filter_bitset && filter_size > 0) {
            bitset_view = knowhere::BitsetView(filter_bitset, num_vectors_);
        }
        
        auto search_result = index_->Search(query_dataset, search_config, bitset_view);
        if (!search_result.has_value()) {
            return result;
        }
        
        auto& res = search_result.value();
        auto ids = res->GetIds();
        auto distances = res->GetDistance();
        int64_t total = num_queries * top_k;
        
        for (int64_t i = 0; i < total; ++i) {
            if (ids[i] >= 0) {
                result.ids.push_back(ids[i]);
                result.distances.push_back(distances[i]);
            }
        }
        
        result.count = static_cast<int64_t>(result.ids.size());
        return result;
    }
    
    bool Serialize(std::vector<uint8_t>& output) const {
        if (!index_) return false;
        auto binary = index_->Serialize(config_json_);
        if (!binary.has_value()) return false;
        output = std::move(binary.value()->data);
        return true;
    }
    
    bool Deserialize(const std::vector<uint8_t>& input) {
        if (!index_) return false;
        knowhere::BinarySet binary_set;
        binary_set.Append("index", std::make_shared<knowhere::Binary>(input));
        return index_->Deserialize(binary_set, config_json_) == knowhere::Status::success;
    }
    
    int64_t Count() const { return num_vectors_; }
    int32_t Dim() const { return config_.dim; }
    std::string Type() const { return config_.index_type; }

private:
    IndexConfig config_;
    knowhere::Json config_json_;
    std::shared_ptr<knowhere::Index> index_;
    int64_t num_vectors_ = 0;
};

#else
// Stub implementation (brute-force search)
class KnowhereIndexWrapper {
public:
    KnowhereIndexWrapper() = default;
    ~KnowhereIndexWrapper() = default;
    
    bool Init(const IndexConfig& config) {
        config_ = config;
        return true;
    }
    
    bool Build(const float* vectors, int64_t num_vectors) {
        if (!vectors || num_vectors <= 0) {
            return false;
        }
        
        // Store vectors for brute-force search (stub implementation)
        int64_t total_size = num_vectors * config_.dim;
        vectors_.resize(total_size);
        std::copy(vectors, vectors + total_size, vectors_.begin());
        num_vectors_ = num_vectors;
        
        return true;
    }
    
    bool Add(const float* vectors, int64_t num_vectors) {
        if (!vectors || num_vectors <= 0) {
            return false;
        }
        
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
        
        if (!query_vectors || num_queries <= 0 || top_k <= 0 || num_vectors_ == 0) {
            return result;
        }
        
        // Brute-force search (stub implementation)
        std::vector<std::pair<float, int64_t>> scores;
        scores.reserve(num_vectors_);
        
        for (int64_t q = 0; q < num_queries; ++q) {
            scores.clear();
            const float* query = query_vectors + q * config_.dim;
            
            for (int64_t i = 0; i < num_vectors_; ++i) {
                // Check filter bitset (bit=1 means filtered out)
                if (filter_bitset && filter_size > 0) {
                    size_t byte_idx = i / 8;
                    size_t bit_idx = i % 8;
                    if (byte_idx < filter_size && (filter_bitset[byte_idx] & (1 << bit_idx))) {
                        continue;
                    }
                }
                
                // Compute inner product (IP metric)
                const float* vec = vectors_.data() + i * config_.dim;
                float score = 0.0f;
                for (int32_t d = 0; d < config_.dim; ++d) {
                    score += query[d] * vec[d];
                }
                
                scores.emplace_back(score, i);
            }
            
            // Sort by score descending
            std::partial_sort(
                scores.begin(),
                scores.begin() + std::min(static_cast<size_t>(top_k), scores.size()),
                scores.end(),
                [](const auto& a, const auto& b) { return a.first > b.first; }
            );
            
            // Collect results
            int32_t k = std::min(static_cast<int32_t>(scores.size()), top_k);
            for (int32_t j = 0; j < k; ++j) {
                result.ids.push_back(scores[j].second);
                result.distances.push_back(scores[j].first);
            }
        }
        
        result.count = static_cast<int64_t>(result.ids.size());
        return result;
    }
    
    bool Serialize(std::vector<uint8_t>& output) const {
        return true;
    }
    
    bool Deserialize(const std::vector<uint8_t>& input) {
        return true;
    }
    
    int64_t Count() const { return num_vectors_; }
    int32_t Dim() const { return config_.dim; }
    std::string Type() const { return config_.index_type; }

private:
    IndexConfig config_;
    std::vector<float> vectors_;
    int64_t num_vectors_ = 0;
};
#endif

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
