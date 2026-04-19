// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Core types for retrieval module.
// All interfaces are fully exposed, no internal hiding.

#ifndef PLASMOD_TYPES_H
#define PLASMOD_TYPES_H

#include <cstdint>
#include <string>
#include <vector>

namespace plasmod {

// Retrieval candidate with all scoring fields
// Aligned with Python Candidate dataclass in types.py
struct Candidate {
    std::string object_id;
    std::string object_type;
    
    // Scores
    float dense_score = 0.0f;
    float sparse_score = 0.0f;
    float rrf_score = 0.0f;
    float final_score = 0.0f;
    
    // Reranking factors (read from index metadata)
    float importance = 0.0f;
    float freshness_score = 1.0f;
    float confidence = 0.0f;
    
    // Seed marking for graph expansion
    bool is_seed = false;
    float seed_score = 0.0f;
    
    // Source channels tracking
    bool from_dense = false;
    bool from_sparse = false;
    bool from_filter = false;
    
    // Internal index ID (for deduplication)
    int64_t internal_id = -1;
};

// Search result from a single retrieval path
struct SearchResult {
    std::vector<int64_t> ids;      // Internal IDs
    std::vector<float> distances;  // Distances/scores
    int64_t count = 0;
};

// Retrieval request parameters
struct RetrievalRequest {
    // Query vectors
    const float* query_vector = nullptr;
    int32_t vector_dim = 0;
    
    // Query text for sparse search
    std::string query_text;
    
    // Retrieval control
    int32_t top_k = 10;
    bool enable_dense = true;
    bool enable_sparse = true;
    bool for_graph = false;  // Return top_k * 2 when true
    
    // Filter bitset (nullptr = no filter)
    const uint8_t* filter_bitset = nullptr;
    size_t filter_bitset_size = 0;
};

// Retrieval result
struct RetrievalResult {
    std::vector<Candidate> candidates;
    int64_t total_found = 0;
    
    // Per-path hit counts
    int64_t dense_hits = 0;
    int64_t sparse_hits = 0;
    int64_t filter_hits = 0;
    
    // Latency in milliseconds
    int64_t latency_ms = 0;
};

// Index configuration
struct IndexConfig {
    std::string index_type;  // "HNSW", "IVF_FLAT", "SPARSE_INVERTED_INDEX", etc.
    std::string metric_type; // "IP", "L2", "COSINE"
    int32_t dim = 0;
    
    // HNSW specific
    int32_t hnsw_m = 16;
    int32_t hnsw_ef_construction = 200;
    int32_t hnsw_ef_search = 100;
    
    // IVF specific
    int32_t ivf_nlist = 128;
    int32_t ivf_nprobe = 8;
};

// RRF merge configuration
struct MergeConfig {
    int32_t rrf_k = 60;  // RRF smoothing parameter
    float seed_threshold = 0.7f;  // Threshold for seed marking
};

}  // namespace plasmod

#endif  // PLASMOD_TYPES_H
