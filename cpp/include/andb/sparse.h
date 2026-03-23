// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Sparse retrieval interface using Knowhere SPARSE_INVERTED_INDEX/SPARSE_WAND.
// All interfaces are fully exposed for extensibility.

#ifndef ANDB_SPARSE_H
#define ANDB_SPARSE_H

#include "andb/types.h"
#include <memory>
#include <string>
#include <unordered_map>

namespace andb {

// Forward declaration for Knowhere sparse index wrapper
class KnowhereSparseIndexWrapper;

// Sparse vector representation (CSR-like format)
struct SparseVector {
    std::vector<uint32_t> indices;  // Non-zero indices
    std::vector<float> values;      // Corresponding values
};

// Sparse retriever interface - wraps Knowhere sparse vector indexes
class SparseRetriever {
public:
    SparseRetriever();
    ~SparseRetriever();
    
    // Disable copy, allow move
    SparseRetriever(const SparseRetriever&) = delete;
    SparseRetriever& operator=(const SparseRetriever&) = delete;
    SparseRetriever(SparseRetriever&&) noexcept;
    SparseRetriever& operator=(SparseRetriever&&) noexcept;
    
    // Initialize index with configuration
    // index_type: "SPARSE_INVERTED_INDEX" or "SPARSE_WAND"
    bool Init(const std::string& index_type = "SPARSE_INVERTED_INDEX");
    
    // Build index from sparse vectors
    // vectors: array of sparse vectors
    // num_vectors: number of vectors
    bool Build(const SparseVector* vectors, int64_t num_vectors);
    
    // Add sparse vectors to existing index
    bool Add(const SparseVector* vectors, int64_t num_vectors);
    
    // Search with sparse query vector
    // query: sparse query vector
    // top_k: number of results
    // filter_bitset: optional filter (nullptr = no filter)
    // filter_size: size of filter bitset in bytes
    SearchResult Search(
        const SparseVector& query,
        int32_t top_k,
        const uint8_t* filter_bitset = nullptr,
        size_t filter_size = 0
    ) const;
    
    // Convert text to sparse vector using BM25-style tokenization
    // Uses FNV-1a hash for token -> index mapping
    static SparseVector TextToSparse(const std::string& text);
    
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
    std::string Type() const;
    
    // Check if index is ready for search
    bool IsReady() const;

private:
    std::unique_ptr<KnowhereSparseIndexWrapper> impl_;
    std::string index_type_;
    bool ready_ = false;
    
    // FNV-1a hash for text tokenization
    static uint32_t FnvHash(const std::string& token);
};

}  // namespace andb

#endif  // ANDB_SPARSE_H
