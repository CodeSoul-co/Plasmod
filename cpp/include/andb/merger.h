// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// RRF merge and reranking for multi-path retrieval results.
// All interfaces are fully exposed for extensibility.

#ifndef ANDB_MERGER_H
#define ANDB_MERGER_H

#include "andb/types.h"
#include <vector>

namespace andb {

// RRF (Reciprocal Rank Fusion) merger
// Combines results from dense, sparse, and filter paths
class Merger {
public:
    explicit Merger(const MergeConfig& config = MergeConfig());
    ~Merger();
    
    // Set merge configuration
    void SetConfig(const MergeConfig& config);
    const MergeConfig& GetConfig() const;
    
    // Merge results from multiple paths
    // dense_results: results from dense retrieval (can be empty)
    // sparse_results: results from sparse retrieval (can be empty)
    // top_k: number of final results to return
    // for_graph: if true, return top_k * 2 results
    // Returns merged and reranked candidates
    std::vector<Candidate> Merge(
        const SearchResult& dense_results,
        const SearchResult& sparse_results,
        int32_t top_k,
        bool for_graph = false
    ) const;
    
    // Compute RRF score for a single candidate
    // rank: 1-based rank in the result list
    // Returns: 1.0 / (k + rank)
    float ComputeRRFScore(int32_t rank) const;
    
    // Apply reranking formula to candidates
    // formula: final_score = rrf_score * max(importance, 0.01) * max(freshness_score, 0.01) * max(confidence, 0.01)
    // Modifies candidates in place
    void Rerank(std::vector<Candidate>& candidates) const;
    
    // Mark seed candidates for graph expansion
    // Candidates with final_score >= seed_threshold are marked as seeds
    // Modifies candidates in place
    void MarkSeeds(std::vector<Candidate>& candidates) const;

private:
    MergeConfig config_;
    
    // Deduplicate candidates by internal_id, accumulating RRF scores
    std::vector<Candidate> Deduplicate(
        const std::vector<Candidate>& candidates
    ) const;
};

// Standalone RRF score computation (for custom implementations)
inline float ComputeRRF(int32_t rank, int32_t k = 60) {
    return 1.0f / static_cast<float>(k + rank);
}

// Standalone reranking formula (for custom implementations)
inline float ComputeFinalScore(
    float rrf_score,
    float importance,
    float freshness_score,
    float confidence
) {
    return rrf_score 
        * std::max(importance, 0.01f)
        * std::max(freshness_score, 0.01f)
        * std::max(confidence, 0.01f);
}

}  // namespace andb

#endif  // ANDB_MERGER_H
