// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Dense retrieval interface using Knowhere HNSW/IVF indexes.
// All interfaces are fully exposed for extensibility.

#ifndef ANDB_DENSE_H
#define ANDB_DENSE_H

#include "andb/types.h"
#include <memory>
#include <string>

namespace andb {

// Forward declaration for Knowhere index wrapper
class KnowhereIndexWrapper;

// Dense retriever interface - wraps Knowhere dense vector indexes
class DenseRetriever {
public:
    DenseRetriever();
    ~DenseRetriever();
    
    // Disable copy, allow move
    DenseRetriever(const DenseRetriever&) = delete;
    DenseRetriever& operator=(const DenseRetriever&) = delete;
    DenseRetriever(DenseRetriever&&) noexcept;
    DenseRetriever& operator=(DenseRetriever&&) noexcept;
    
    // Initialize index with configuration
    // Returns true on success, false on failure
    bool Init(const IndexConfig& config);
    
    // Build index from vectors
    // vectors: row-major float array, shape [num_vectors, dim]
    // num_vectors: number of vectors
    // Returns true on success
    bool Build(const float* vectors, int64_t num_vectors);
    
    // Add vectors to existing index (for growable indexes)
    bool Add(const float* vectors, int64_t num_vectors);
    
    // Search with optional filter bitset
    // query_vectors: row-major float array, shape [num_queries, dim]
    // num_queries: number of query vectors
    // top_k: number of results per query
    // filter_bitset: optional filter (nullptr = no filter), bit=1 means filtered out
    // filter_size: size of filter bitset in bytes
    // Returns search results
    SearchResult Search(
        const float* query_vectors,
        int64_t num_queries,
        int32_t top_k,
        const uint8_t* filter_bitset = nullptr,
        size_t filter_size = 0
    ) const;
    
    // Serialize index to binary
    bool Serialize(std::vector<uint8_t>& output) const;
    
    // Deserialize index from binary
    bool Deserialize(const std::vector<uint8_t>& input);
    
    // Load index from file
    bool Load(const std::string& path);
    
    // Save index to file
    bool Save(const std::string& path) const;
    
    // Get index statistics
    int64_t Count() const;
    int32_t Dim() const;
    std::string Type() const;
    
    // Check if index is ready for search
    bool IsReady() const;

private:
    std::unique_ptr<KnowhereIndexWrapper> impl_;
    IndexConfig config_;
    bool ready_ = false;
};

}  // namespace andb

#endif  // ANDB_DENSE_H
