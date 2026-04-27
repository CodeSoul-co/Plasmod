// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Batch query optimizer plugin — C++ layer.
//
// This file defines the BatchQueryOptimizerPlugin interface. All plugins
// operate entirely inside the C++ layer; the Go CGO interface is unchanged.
//
// Usage:
//   - Default plugin (DefaultQueryClusteringPlugin) is always enabled.
//   - Third-party plugins call SetGlobalPlugin(&my_plugin) at process startup.
//   - Plugin is invoked ONLY on the nq > 1 batch path; nq==1 hot path is
//     untouched.

#pragma once

#include <cstdint>
#include <cstring>
#include <vector>

namespace plasmod {

// Chunk boundary: [start, end) range of query indices assigned to one thread.
struct QueryChunk {
    int64_t start;  // inclusive
    int64_t end;    // exclusive
};

// BatchQueryOptimizerPlugin — abstract base for batch query optimizers.
//
// Call sequence inside SegmentIndexManager::DoSearch (batch path):
//   1. ReorderQueryBatch  — once, SERIAL, before #pragma omp parallel
//   2. #pragma omp parallel + GetOptimizedChunks — per thread, parallel
//   3. OnChunkDone        — per thread, after its chunk finishes
//
// Thread safety:
//   - GetOptimizedChunks is read-only on plugin state (thread-safe).
//   - OnChunkDone must be thread-safe if the plugin accumulates per-call stats.
class BatchQueryOptimizerPlugin {
public:
    virtual ~BatchQueryOptimizerPlugin() = default;

    // ReorderQueryBatch — called once, serial, BEFORE the parallel region.
    //
    //   query     : row-major query matrix [nq × dim] (NOT modified)
    //   nq        : number of queries
    //   dim       : embedding dimension
    //   order_out : caller-allocated [nq] output array.
    //               order_out[i] = original query index for output slot i.
    //               Default (identity permutation): order_out[i] = i.
    virtual void ReorderQueryBatch(
        const float* query,
        int64_t      nq,
        int          dim,
        int64_t*     order_out
    ) = 0;

    // GetOptimizedChunks — called from each OpenMP thread to claim its range.
    //
    //   nq          : total number of queries (after reordering)
    //   num_threads : omp_get_max_threads()
    //   thread_idx  : 0-based OpenMP thread index (omp_get_thread_num())
    //
    // Returns the [start, end) range for this thread.
    // Default: static strided partition (ceil(nq/num_threads) per thread).
    virtual QueryChunk GetOptimizedChunks(
        int64_t nq,
        int     num_threads,
        int     thread_idx
    ) = 0;

    // OnChunkDone — called after each thread finishes processing its chunk.
    //
    // Allows the plugin to accumulate per-chunk statistics (visited-node
    // sets, cache hints, etc.) for future batches.
    // Default: no-op.
    virtual void OnChunkDone(
        int64_t chunk_start,
        int64_t chunk_end,
        int64_t chunk_idx
    ) = 0;

    // Name — human-readable plugin name for logging/debugging.
    virtual const char* Name() const = 0;
};

// Global plugin accessors.
//
// Default plugin is a static-duration DefaultQueryClusteringPlugin instance
// constructed at program startup (zero cost). Third-party plugins replace
// it by calling SetGlobalPlugin before any DoSearch call.
BatchQueryOptimizerPlugin& GetDefaultPlugin();
void SetGlobalPlugin(BatchQueryOptimizerPlugin* plugin);

// ── Default Plugin ─────────────────────────────────────────────────────────────

// DefaultBatchOptimizerPlugin — always-enabled default plugin.
//
// Implements L2-norm sorting + static chunking for batch HNSW search.
//
// Motivation: HNSW entry-point selection is proportional to the L2 norm of the
// query vector. Queries with similar norms traverse overlapping entry paths,
// so the visited-list and layer-0 neighbor table stay hot in L2/L3 cache.
//
// Complexity: O(nq * dim) + O(nq log nq) — negligible vs. search time.
//
// Graceful degradation:
//   - nq < 8: identity permutation (overhead exceeds benefit)
//   - HAVE_OMP == 0: plugin skipped, serial fallback path used
class DefaultBatchOptimizerPlugin final : public BatchQueryOptimizerPlugin {
public:
    explicit DefaultBatchOptimizerPlugin();
    ~DefaultBatchOptimizerPlugin() override = default;

    void ReorderQueryBatch(
        const float* query,
        int64_t      nq,
        int          dim,
        int64_t*     order_out
    ) override;

    QueryChunk GetOptimizedChunks(
        int64_t nq,
        int     num_threads,
        int     thread_idx
    ) override;

    void OnChunkDone(
        int64_t chunk_start,
        int64_t chunk_end,
        int64_t chunk_idx
    ) override;

    const char* Name() const override { return "DefaultBatchOptimizerPlugin"; }

private:
    int64_t                  min_nq_for_optimize_;
    std::vector<float>       norms_;    // [nq] L2 norms of each query
    std::vector<int64_t>     indices_;  // [nq] sorted query indices
};

}  // namespace plasmod
