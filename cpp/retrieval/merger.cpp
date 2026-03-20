// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// RRF merge and reranking implementation.

#include "andb/merger.h"
#include <algorithm>
#include <unordered_map>

namespace andb {

Merger::Merger(const MergeConfig& config) : config_(config) {}

Merger::~Merger() = default;

void Merger::SetConfig(const MergeConfig& config) {
    config_ = config;
}

const MergeConfig& Merger::GetConfig() const {
    return config_;
}

float Merger::ComputeRRFScore(int32_t rank) const {
    return 1.0f / static_cast<float>(config_.rrf_k + rank);
}

std::vector<Candidate> Merger::Merge(
    const SearchResult& dense_results,
    const SearchResult& sparse_results,
    int32_t top_k,
    bool for_graph
) const {
    // Step 1: Convert search results to candidates with RRF scores
    std::vector<Candidate> all_candidates;
    
    // Process dense results
    for (size_t i = 0; i < dense_results.ids.size(); ++i) {
        Candidate c;
        c.internal_id = dense_results.ids[i];
        c.dense_score = ComputeRRFScore(static_cast<int32_t>(i + 1));
        c.from_dense = true;
        all_candidates.push_back(c);
    }
    
    // Process sparse results
    for (size_t i = 0; i < sparse_results.ids.size(); ++i) {
        Candidate c;
        c.internal_id = sparse_results.ids[i];
        c.sparse_score = ComputeRRFScore(static_cast<int32_t>(i + 1));
        c.from_sparse = true;
        all_candidates.push_back(c);
    }
    
    // Step 2: Deduplicate by internal_id, accumulating RRF scores
    std::vector<Candidate> merged = Deduplicate(all_candidates);
    
    // Step 3: Compute RRF score (sum of per-channel scores)
    for (auto& c : merged) {
        c.rrf_score = c.dense_score + c.sparse_score;
    }
    
    // Step 4: Apply reranking formula
    Rerank(merged);
    
    // Step 5: Sort by final_score descending
    std::sort(merged.begin(), merged.end(),
        [](const Candidate& a, const Candidate& b) {
            return a.final_score > b.final_score;
        });
    
    // Step 6: Truncate to effective top_k
    int32_t effective_k = for_graph ? top_k * 2 : top_k;
    if (static_cast<int32_t>(merged.size()) > effective_k) {
        merged.resize(effective_k);
    }
    
    // Step 7: Mark seeds
    MarkSeeds(merged);
    
    return merged;
}

void Merger::Rerank(std::vector<Candidate>& candidates) const {
    for (auto& c : candidates) {
        c.final_score = ComputeFinalScore(
            c.rrf_score,
            c.importance,
            c.freshness_score,
            c.confidence
        );
    }
}

void Merger::MarkSeeds(std::vector<Candidate>& candidates) const {
    for (auto& c : candidates) {
        if (c.final_score >= config_.seed_threshold) {
            c.is_seed = true;
            c.seed_score = c.final_score;
        } else {
            c.is_seed = false;
            c.seed_score = 0.0f;
        }
    }
}

std::vector<Candidate> Merger::Deduplicate(
    const std::vector<Candidate>& candidates
) const {
    std::unordered_map<int64_t, Candidate> merged_map;
    
    for (const auto& c : candidates) {
        auto it = merged_map.find(c.internal_id);
        if (it == merged_map.end()) {
            merged_map[c.internal_id] = c;
        } else {
            // Accumulate scores
            it->second.dense_score += c.dense_score;
            it->second.sparse_score += c.sparse_score;
            it->second.from_dense = it->second.from_dense || c.from_dense;
            it->second.from_sparse = it->second.from_sparse || c.from_sparse;
            it->second.from_filter = it->second.from_filter || c.from_filter;
        }
    }
    
    std::vector<Candidate> result;
    result.reserve(merged_map.size());
    for (auto& [id, candidate] : merged_map) {
        result.push_back(std::move(candidate));
    }
    
    return result;
}

}  // namespace andb
