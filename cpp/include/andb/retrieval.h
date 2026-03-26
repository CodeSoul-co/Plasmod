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

// C API for FFI compatibility (CGO bridge)
#ifdef __cplusplus
extern "C" {
#endif

const char* andb_version();

// ── Per-process flat retriever (legacy, single-segment usage) ────────────────
void* andb_retriever_create();
void  andb_retriever_destroy(void* retriever);

int andb_retriever_init(
    void*       retriever,
    const char* dense_index_type,
    const char* metric_type,
    int         dim,
    const char* sparse_index_type,
    int         rrf_k
);

int andb_retriever_build(
    void*        retriever,
    const float* dense_vectors,
    int64_t      num_vectors,
    int          dim
);

int andb_retriever_search(
    void*          retriever,
    const float*   query_vector,
    int            dim,
    int            top_k,
    int            for_graph,
    const uint8_t* filter_bitset,
    size_t         filter_size,
    int64_t*       out_ids,
    float*         out_scores,
    int            max_results
);

// ── SegmentIndexManager API ───────────────────────────────────────────────────
// segment_id format: "object_type.memory_type.time_bucket.agent"
// Matches the retrieval_segments table primary key.

// Build (or rebuild) a segment HNSW index.
// Returns 0 on success, negative on error.
int andb_segment_build(
    const char*  segment_id,
    const float* vectors,
    int64_t      n,
    int          dim
);

// ANN search within a segment — no filter.
// out_ids and out_dists must be caller-allocated with at least nq*topk elements.
int andb_segment_search(
    const char*  segment_id,
    const float* query,
    int64_t      nq,
    int          topk,
    int64_t*     out_ids,
    float*       out_dists
);

// ANN search within a segment — with allow-list bitmask filter.
// allow_bits  : bitmask where bit i=1 means vector i is a valid candidate
// allow_count : total number of vectors the bitmask covers (in bits, not bytes)
int andb_segment_search_filter(
    const char*    segment_id,
    const float*   query,
    int64_t        nq,
    int            topk,
    const uint8_t* allow_bits,
    int64_t        allow_count,
    int64_t*       out_ids,
    float*         out_dists
);

// Remove a segment from memory.  Returns 0 or -1 (not found).
int andb_segment_unload(const char* segment_id);

// Check if a segment is loaded.  Returns 1=yes, 0=no.
int andb_segment_exists(const char* segment_id);

// Returns number of vectors in segment, or -1 if not found.
int64_t andb_segment_size(const char* segment_id);

#ifdef __cplusplus
}
#endif

#endif  // ANDB_RETRIEVAL_H
