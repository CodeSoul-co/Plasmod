// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Unified retrieval interface combining dense, sparse, and filter paths.
// All interfaces are fully exposed for extensibility.

#ifndef ANDB_RETRIEVAL_H
#define ANDB_RETRIEVAL_H

#include "andb/types.h"
#include "andb/dense.h"
#include "andb/sparse.h"
#include "andb/filter.h"
#include "andb/merger.h"
#include <memory>
#include <string>

namespace andb {

// Unified retriever combining all three paths
class Retriever {
public:
    Retriever();
    ~Retriever();
    
    // Disable copy, allow move
    Retriever(const Retriever&) = delete;
    Retriever& operator=(const Retriever&) = delete;
    Retriever(Retriever&&) noexcept;
    Retriever& operator=(Retriever&&) noexcept;
    
    // Initialize with index configurations
    bool Init(
        const IndexConfig& dense_config,
        const std::string& sparse_index_type = "SPARSE_INVERTED_INDEX",
        const MergeConfig& merge_config = MergeConfig()
    );
    
    // Build indexes from data
    // dense_vectors: row-major float array, shape [num_vectors, dim]
    // sparse_vectors: array of sparse vectors
    // num_vectors: number of vectors
    bool Build(
        const float* dense_vectors,
        const SparseVector* sparse_vectors,
        int64_t num_vectors
    );
    
    // Execute retrieval with all enabled paths
    RetrievalResult Retrieve(const RetrievalRequest& request) const;
    
    // Execute retrieval for benchmark (no truncation)
    RetrievalResult BenchmarkRetrieve(const RetrievalRequest& request) const;
    
    // Access individual retrievers for custom usage
    DenseRetriever& GetDenseRetriever();
    const DenseRetriever& GetDenseRetriever() const;
    SparseRetriever& GetSparseRetriever();
    const SparseRetriever& GetSparseRetriever() const;
    Merger& GetMerger();
    const Merger& GetMerger() const;
    
    // Serialize all indexes
    bool Serialize(std::vector<uint8_t>& output) const;
    
    // Deserialize all indexes
    bool Deserialize(const std::vector<uint8_t>& input);
    
    // Load indexes from directory
    bool Load(const std::string& dir_path);
    
    // Save indexes to directory
    bool Save(const std::string& dir_path) const;
    
    // Check if retriever is ready
    bool IsReady() const;

private:
    std::unique_ptr<DenseRetriever> dense_;
    std::unique_ptr<SparseRetriever> sparse_;
    std::unique_ptr<Merger> merger_;
    bool ready_ = false;
};

// Version string
const char* Version();

}  // namespace andb

// C API for FFI compatibility
#ifdef __cplusplus
extern "C" {
#endif

const char* andb_version();

// Create/destroy retriever
void* andb_retriever_create();
void andb_retriever_destroy(void* retriever);

// Initialize retriever
int andb_retriever_init(
    void* retriever,
    const char* dense_index_type,
    const char* metric_type,
    int dim,
    const char* sparse_index_type,
    int rrf_k
);

// Build indexes
int andb_retriever_build(
    void* retriever,
    const float* dense_vectors,
    int64_t num_vectors,
    int dim
);

// Search
int andb_retriever_search(
    void* retriever,
    const float* query_vector,
    int dim,
    int top_k,
    int for_graph,
    const uint8_t* filter_bitset,
    size_t filter_size,
    int64_t* out_ids,
    float* out_scores,
    int max_results
);

#ifdef __cplusplus
}
#endif

#endif  // ANDB_RETRIEVAL_H
