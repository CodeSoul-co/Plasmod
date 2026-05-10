// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Batch query optimizer plugin — C++ layer.
//
// Supported plugin modes:
//
//   L2_NORM_SORT (PluginMode::L2_NORM_SORT):
//     Sort queries by L2 norm, then dispatch one-query-per-thread via OpenMP.
//     DoSearch calls ReorderQueryBatch + GetOptimizedChunks + OnChunkDone.
//     Each thread calls HnswFastSearchFloat for its assigned query range.
//
// The Go CGO interface is unchanged; plugin selection is via SetGlobalPlugin.

#pragma once

#include <atomic>
#include <cstdint>
#include <cstring>
#include <mutex>
#include <vector>

namespace plasmod {

// Chunk boundary: [start, end) range of query indices assigned to one thread.
struct QueryChunk {
    int64_t start;  // inclusive
    int64_t end;    // exclusive
};

// Plugin execution mode — determines which DoSearch code path is taken.
enum class PluginMode {
    NONE           = 0,  // No plugin: direct full-batch Knowhere call
    L2_NORM_SORT    = 1   // L2-norm sort + OpenMP per-query dispatch
};

// ── Base interface ─────────────────────────────────────────────────────────────

class BatchQueryOptimizerPlugin {
public:
    virtual ~BatchQueryOptimizerPlugin() = default;

    virtual PluginMode Mode() const = 0;
    virtual const char* Name() const = 0;

    // ReorderQueryBatch — called once, serial, before any parallel/sequential
    // dispatch. Fills order_out[i] = original query index for slot i.
    // Identity permutation is always a valid implementation.
    virtual void ReorderQueryBatch(
        const float* query,
        int64_t      nq,
        int          dim,
        int64_t*     order_out
    ) = 0;

    // ── L2_NORM_SORT path ──────────────────────────────────────────────────────

    // GetOptimizedChunks — called from each OpenMP thread to claim its range.
    // Only used when Mode() == L2_NORM_SORT.
    virtual QueryChunk GetOptimizedChunks(
        int64_t nq,
        int     num_threads,
        int     thread_idx
    ) = 0;

    // OnChunkDone — called after each OpenMP thread finishes its chunk.
    // Only used when Mode() == L2_NORM_SORT.
    virtual void OnChunkDone(
        int64_t chunk_start,
        int64_t chunk_end,
        int64_t chunk_idx
    ) = 0;
};

// Global plugin accessors.
BatchQueryOptimizerPlugin* GetActivePlugin();   // nullptr = NONE mode
void SetGlobalPlugin(BatchQueryOptimizerPlugin* plugin);  // nullptr = disable

// ── L2_NORM_SORT plugin ────────────────────────────────────────────────────────

// L2NormSortPlugin — sort queries by L2 norm, dispatch one-per-thread via OMP.
//
// Hypothesis: HNSW entry-point traversal correlates with query L2 norm, so
// norm-adjacent queries share visited nodes in L2/L3 cache.
//
// Complexity: O(nq*dim) norm + O(nq log nq) sort — negligible vs. search.
// Minimum batch: 8 queries (below this, identity permutation is used).
class L2NormSortPlugin final : public BatchQueryOptimizerPlugin {
public:
    explicit L2NormSortPlugin();
    ~L2NormSortPlugin() override = default;

    PluginMode  Mode() const override { return PluginMode::L2_NORM_SORT; }
    const char* Name() const override { return "L2NormSortPlugin"; }

    void ReorderQueryBatch(
        const float* query, int64_t nq, int dim, int64_t* order_out
    ) override;

    QueryChunk GetOptimizedChunks(
        int64_t nq, int num_threads, int thread_idx
    ) override;

    void OnChunkDone(int64_t, int64_t, int64_t) override {}

private:
    int64_t              min_nq_;
    std::vector<float>   norms_;
    std::vector<int64_t> indices_;
};

}  // namespace plasmod
