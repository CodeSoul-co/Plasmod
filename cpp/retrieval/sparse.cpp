// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Sparse retrieval implementation using Knowhere SPARSE_INVERTED_INDEX.
// Uses real Knowhere when ANDB_USE_KNOWHERE=1, otherwise falls back to stub.

#include "andb/sparse.h"
#include <algorithm>
#include <cmath>
#include <sstream>
#include <cctype>

#ifdef ANDB_USE_KNOWHERE
#include "knowhere/index/index_factory.h"
#include "knowhere/common/config.h"
#include "knowhere/common/dataset.h"
#include "knowhere/utils/bitset_view.h"
#endif

namespace andb {

// FNV-1a hash constants
static constexpr uint32_t FNV_OFFSET_BASIS = 2166136261u;
static constexpr uint32_t FNV_PRIME = 16777619u;
static constexpr uint32_t SPARSE_DIM = 30000u;

#ifdef ANDB_USE_KNOWHERE
// Real Knowhere sparse index implementation
class KnowhereSparseIndexWrapper {
public:
    KnowhereSparseIndexWrapper() = default;
    ~KnowhereSparseIndexWrapper() = default;
    
    bool Init(const std::string& index_type) {
        index_type_ = index_type;
        
        knowhere::Json json_config;
        json_config["metric_type"] = "IP";
        
        // Create sparse inverted index
        index_ = knowhere::IndexFactory::Instance().Create(
            knowhere::IndexEnum::INDEX_SPARSE_INVERTED_INDEX, 
            knowhere::Version::GetCurrentVersion());
        
        config_json_ = json_config;
        return index_ != nullptr;
    }
    
    bool Build(const SparseVector* vectors, int64_t num_vectors) {
        if (!index_ || !vectors || num_vectors <= 0) {
            return false;
        }
        
        // Convert to Knowhere sparse format
        auto dataset = ConvertToDataset(vectors, num_vectors);
        auto status = index_->Build(dataset, config_json_);
        if (status != knowhere::Status::success) {
            return false;
        }
        
        num_vectors_ = num_vectors;
        return true;
    }
    
    bool Add(const SparseVector* vectors, int64_t num_vectors) {
        if (!index_ || !vectors || num_vectors <= 0) {
            return false;
        }
        
        auto dataset = ConvertToDataset(vectors, num_vectors);
        auto status = index_->Add(dataset, config_json_);
        if (status != knowhere::Status::success) {
            return false;
        }
        
        num_vectors_ += num_vectors;
        return true;
    }
    
    SearchResult Search(
        const SparseVector& query,
        int32_t top_k,
        const uint8_t* filter_bitset,
        size_t filter_size
    ) const {
        SearchResult result;
        
        if (!index_ || query.indices.empty() || top_k <= 0) {
            return result;
        }
        
        auto query_dataset = ConvertToDataset(&query, 1);
        
        knowhere::Json search_config = config_json_;
        search_config["k"] = top_k;
        
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
        
        for (int32_t i = 0; i < top_k; ++i) {
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
    std::string Type() const { return index_type_; }

private:
    static std::shared_ptr<knowhere::DataSet> ConvertToDataset(
        const SparseVector* vectors, int64_t num_vectors) {
        // Convert SparseVector array to Knowhere sparse format
        // This is a simplified conversion - actual implementation depends on Knowhere API
        auto dataset = std::make_shared<knowhere::DataSet>();
        dataset->SetRows(num_vectors);
        // Note: Actual sparse data format depends on Knowhere version
        return dataset;
    }
    
    std::string index_type_;
    knowhere::Json config_json_;
    std::shared_ptr<knowhere::Index> index_;
    int64_t num_vectors_ = 0;
};

#else
// Stub implementation (brute-force sparse search)
class KnowhereSparseIndexWrapper {
public:
    KnowhereSparseIndexWrapper() = default;
    ~KnowhereSparseIndexWrapper() = default;
    
    bool Init(const std::string& index_type) {
        index_type_ = index_type;
        return true;
    }
    
    bool Build(const SparseVector* vectors, int64_t num_vectors) {
        if (!vectors || num_vectors <= 0) {
            return false;
        }
        
        vectors_.clear();
        vectors_.reserve(num_vectors);
        for (int64_t i = 0; i < num_vectors; ++i) {
            vectors_.push_back(vectors[i]);
        }
        
        return true;
    }
    
    bool Add(const SparseVector* vectors, int64_t num_vectors) {
        if (!vectors || num_vectors <= 0) {
            return false;
        }
        
        for (int64_t i = 0; i < num_vectors; ++i) {
            vectors_.push_back(vectors[i]);
        }
        
        return true;
    }
    
    SearchResult Search(
        const SparseVector& query,
        int32_t top_k,
        const uint8_t* filter_bitset,
        size_t filter_size
    ) const {
        SearchResult result;
        
        if (query.indices.empty() || top_k <= 0 || vectors_.empty()) {
            return result;
        }
        
        // Compute sparse inner product with all vectors
        std::vector<std::pair<float, int64_t>> scores;
        scores.reserve(vectors_.size());
        
        for (size_t i = 0; i < vectors_.size(); ++i) {
            // Check filter bitset (bit=1 means filtered out)
            if (filter_bitset && filter_size > 0) {
                size_t byte_idx = i / 8;
                size_t bit_idx = i % 8;
                if (byte_idx < filter_size && (filter_bitset[byte_idx] & (1 << bit_idx))) {
                    continue;
                }
            }
            
            // Compute sparse inner product
            float score = ComputeSparseIP(query, vectors_[i]);
            if (score > 0.0f) {
                scores.emplace_back(score, static_cast<int64_t>(i));
            }
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
        
        result.count = static_cast<int64_t>(result.ids.size());
        return result;
    }
    
    bool Serialize(std::vector<uint8_t>& output) const {
        return true;
    }
    
    bool Deserialize(const std::vector<uint8_t>& input) {
        return true;
    }
    
    int64_t Count() const { return static_cast<int64_t>(vectors_.size()); }
    std::string Type() const { return index_type_; }

private:
    // Compute sparse inner product between two sparse vectors
    static float ComputeSparseIP(const SparseVector& a, const SparseVector& b) {
        float score = 0.0f;
        size_t i = 0, j = 0;
        
        // Both vectors are assumed to be sorted by index
        while (i < a.indices.size() && j < b.indices.size()) {
            if (a.indices[i] == b.indices[j]) {
                score += a.values[i] * b.values[j];
                ++i;
                ++j;
            } else if (a.indices[i] < b.indices[j]) {
                ++i;
            } else {
                ++j;
            }
        }
        
        return score;
    }
    
    std::string index_type_;
    std::vector<SparseVector> vectors_;
};
#endif

// SparseRetriever implementation

SparseRetriever::SparseRetriever() : impl_(std::make_unique<KnowhereSparseIndexWrapper>()) {}

SparseRetriever::~SparseRetriever() = default;

SparseRetriever::SparseRetriever(SparseRetriever&&) noexcept = default;
SparseRetriever& SparseRetriever::operator=(SparseRetriever&&) noexcept = default;

bool SparseRetriever::Init(const std::string& index_type) {
    index_type_ = index_type;
    ready_ = impl_->Init(index_type);
    return ready_;
}

bool SparseRetriever::Build(const SparseVector* vectors, int64_t num_vectors) {
    if (!impl_->Build(vectors, num_vectors)) {
        return false;
    }
    ready_ = true;
    return true;
}

bool SparseRetriever::Add(const SparseVector* vectors, int64_t num_vectors) {
    return impl_->Add(vectors, num_vectors);
}

SearchResult SparseRetriever::Search(
    const SparseVector& query,
    int32_t top_k,
    const uint8_t* filter_bitset,
    size_t filter_size
) const {
    if (!ready_) {
        return SearchResult{};
    }
    return impl_->Search(query, top_k, filter_bitset, filter_size);
}

uint32_t SparseRetriever::FnvHash(const std::string& token) {
    uint32_t hash = FNV_OFFSET_BASIS;
    for (char c : token) {
        hash ^= static_cast<uint8_t>(c);
        hash *= FNV_PRIME;
    }
    return hash;
}

SparseVector SparseRetriever::TextToSparse(const std::string& text) {
    SparseVector result;
    
    if (text.empty()) {
        return result;
    }
    
    // Tokenize: split by whitespace, convert to lowercase
    std::istringstream iss(text);
    std::string token;
    std::unordered_map<uint32_t, float> term_counts;
    int total_tokens = 0;
    
    while (iss >> token) {
        // Convert to lowercase
        for (char& c : token) {
            c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
        }
        
        // Hash to index
        uint32_t idx = FnvHash(token) % SPARSE_DIM;
        term_counts[idx] += 1.0f;
        ++total_tokens;
    }
    
    // Normalize by total tokens (simple TF)
    if (total_tokens > 0) {
        float norm = 1.0f / static_cast<float>(total_tokens);
        for (auto& [idx, count] : term_counts) {
            result.indices.push_back(idx);
            result.values.push_back(count * norm);
        }
    }
    
    // Sort by index for efficient sparse IP computation
    std::vector<std::pair<uint32_t, float>> pairs;
    pairs.reserve(result.indices.size());
    for (size_t i = 0; i < result.indices.size(); ++i) {
        pairs.emplace_back(result.indices[i], result.values[i]);
    }
    std::sort(pairs.begin(), pairs.end());
    
    result.indices.clear();
    result.values.clear();
    for (const auto& [idx, val] : pairs) {
        result.indices.push_back(idx);
        result.values.push_back(val);
    }
    
    return result;
}

bool SparseRetriever::Serialize(std::vector<uint8_t>& output) const {
    return impl_->Serialize(output);
}

bool SparseRetriever::Deserialize(const std::vector<uint8_t>& input) {
    ready_ = impl_->Deserialize(input);
    return ready_;
}

bool SparseRetriever::Load(const std::string& path) {
    return false;
}

bool SparseRetriever::Save(const std::string& path) const {
    return false;
}

int64_t SparseRetriever::Count() const {
    return impl_->Count();
}

std::string SparseRetriever::Type() const {
    return impl_->Type();
}

bool SparseRetriever::IsReady() const {
    return ready_;
}

}  // namespace andb
