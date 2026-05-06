// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0

#include "plasmod/batch_optimizer.h"

#include <algorithm>
#include <cmath>
#include <cstring>
#include <vector>

namespace plasmod {

// ── Global plugin registry ─────────────────────────────────────────────────────

namespace {
BatchQueryOptimizerPlugin* g_plugin = nullptr;
}

BatchQueryOptimizerPlugin* GetActivePlugin() {
    return g_plugin;
}

void SetGlobalPlugin(BatchQueryOptimizerPlugin* plugin) {
    g_plugin = plugin;
}

// ── L2NormSortPlugin ───────────────────────────────────────────────────────────

L2NormSortPlugin::L2NormSortPlugin() : min_nq_(8) {}

void L2NormSortPlugin::ReorderQueryBatch(
    const float* query,
    int64_t      nq,
    int          dim,
    int64_t*     order_out
) {
    if (nq < min_nq_) {
        for (int64_t i = 0; i < nq; ++i) order_out[i] = i;
        return;
    }

    norms_.resize(nq);
    for (int64_t i = 0; i < nq; ++i) {
        float sum = 0.0f;
        const float* v = query + static_cast<size_t>(i) * static_cast<size_t>(dim);
        for (int d = 0; d < dim; ++d) { float x = v[d]; sum += x * x; }
        norms_[i] = std::sqrt(sum);
    }

    indices_.resize(nq);
    for (int64_t i = 0; i < nq; ++i) indices_[i] = i;
    std::sort(indices_.begin(), indices_.end(),
              [this](int64_t a, int64_t b) { return norms_[a] < norms_[b]; });

    for (int64_t i = 0; i < nq; ++i) order_out[i] = indices_[i];
}

QueryChunk L2NormSortPlugin::GetOptimizedChunks(
    int64_t nq,
    int     num_threads,
    int     thread_idx
) {
    if (num_threads <= 0) num_threads = 1;
    const int64_t chunk_size = (nq + num_threads - 1) / num_threads;
    QueryChunk c;
    c.start = static_cast<int64_t>(thread_idx) * chunk_size;
    c.end   = std::min(c.start + chunk_size, nq);
    return c;
}

// ── VisitedListSharingPlugin ───────────────────────────────────────────────────

VisitedListSharingPlugin::VisitedListSharingPlugin(int64_t num_vectors,
                                                   bool    approx_skip)
    : min_nq_(2),
      approx_skip_(approx_skip),
      prev_top1_(-1),
      num_vectors_(num_vectors) {
    if (num_vectors_ > 0) {
        visited_bits_.assign((num_vectors_ + 7) / 8, 0);
    }
}

void VisitedListSharingPlugin::ResizeForSegment(int64_t num_vectors) {
    num_vectors_ = num_vectors;
    visited_bits_.assign((num_vectors_ + 7) / 8, 0);
}

void VisitedListSharingPlugin::ReorderQueryBatch(
    const float* query,
    int64_t      nq,
    int          dim,
    int64_t*     order_out
) {
    // Reset per-batch state
    prev_top1_ = -1;
    if (num_vectors_ > 0) {
        std::fill(visited_bits_.begin(), visited_bits_.end(), 0);
    }

    if (nq < min_nq_) {
        for (int64_t i = 0; i < nq; ++i) order_out[i] = i;
        return;
    }

    // Sort by L2 norm so consecutive queries are spatially close
    norms_.resize(nq);
    for (int64_t i = 0; i < nq; ++i) {
        float sum = 0.0f;
        const float* v = query + static_cast<size_t>(i) * static_cast<size_t>(dim);
        for (int d = 0; d < dim; ++d) { float x = v[d]; sum += x * x; }
        norms_[i] = std::sqrt(sum);
    }

    indices_.resize(nq);
    for (int64_t i = 0; i < nq; ++i) indices_[i] = i;
    std::sort(indices_.begin(), indices_.end(),
              [this](int64_t a, int64_t b) { return norms_[a] < norms_[b]; });

    for (int64_t i = 0; i < nq; ++i) order_out[i] = indices_[i];
}

int64_t VisitedListSharingPlugin::GetWarmEntryPoint(int64_t /*query_slot*/) {
    return prev_top1_;  // -1 on first query → DoSearch uses default entry
}

void VisitedListSharingPlugin::OnQueryVisited(
    int64_t        /*query_slot*/,
    const int64_t* result_ids,
    const float*   /*result_dists*/,
    int            topk,
    const int64_t* visited_ids,
    int64_t        visited_count
) {
    // Update warm entry point for next query
    if (topk > 0 && result_ids && result_ids[0] >= 0) {
        prev_top1_ = result_ids[0];
    }

    // Merge visited nodes into shared bitset
    if (!visited_ids || visited_count <= 0 || visited_bits_.empty()) return;
    for (int64_t i = 0; i < visited_count; ++i) {
        int64_t id = visited_ids[i];
        if (id >= 0 && id < num_vectors_) {
            visited_bits_[id >> 3] |= static_cast<uint8_t>(1u << (id & 7));
        }
    }
}

bool VisitedListSharingPlugin::IsVisited(int64_t node_id) const {
    if (node_id < 0 || node_id >= num_vectors_ || visited_bits_.empty()) {
        return false;
    }
    return (visited_bits_[node_id >> 3] >> (node_id & 7)) & 1u;
}

}  // namespace plasmod
