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

}  // namespace plasmod
