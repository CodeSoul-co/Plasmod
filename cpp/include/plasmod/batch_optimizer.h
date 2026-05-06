// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Batch query optimizer plugin — C++ layer.
//
// Two parallel plugin paths are supported:
//
//   L2_NORM_SORT (PluginMode::L2_NORM_SORT):
//     Sort queries by L2 norm, then dispatch one-query-per-thread via OpenMP.
//     DoSearch calls ReorderQueryBatch + GetOptimizedChunks + OnChunkDone.
//     Each thread calls HnswFastSearchFloat for its assigned query range.
//
//   VISITED_SHARING (PluginMode::VISITED_SHARING):
//     Run queries sequentially. After each query, expose its visited nodes
//     to the plugin via OnQueryVisited. The plugin can then provide a warm
//     entry point for the next query via GetWarmEntryPoint.
//     DoSearch calls ReorderQueryBatch, then for each query in order:
//       GetWarmEntryPoint → HnswFastSearchFloat → OnQueryVisited.
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
    L2_NORM_SORT   = 1,  // L2-norm sort + OpenMP per-query dispatch
    VISITED_SHARING = 2  // Sequential with visited-list sharing
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

    // ── VISITED_SHARING path ───────────────────────────────────────────────────

    // GetWarmEntryPoint — called before each query in the sequential loop.
    // Returns the node ID to use as the HNSW entry point, or -1 for default.
    // Only used when Mode() == VISITED_SHARING.
    virtual int64_t GetWarmEntryPoint(int64_t query_slot) = 0;

    // OnQueryVisited — called after each query completes.
    //   query_slot   : position in the reordered batch (0-based)
    //   result_ids   : top-k result IDs (may contain -1 for unfilled slots)
    //   result_dists : corresponding distances
    //   topk         : length of result_ids / result_dists
    //   visited_ids  : node IDs visited during this query's HNSW traversal
    //   visited_count: number of visited nodes
    // Only used when Mode() == VISITED_SHARING.
    virtual void OnQueryVisited(
        int64_t        query_slot,
        const int64_t* result_ids,
        const float*   result_dists,
        int            topk,
        const int64_t* visited_ids,
        int64_t        visited_count
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

    // VISITED_SHARING methods — unused, return safe defaults
    int64_t GetWarmEntryPoint(int64_t) override { return -1; }
    void OnQueryVisited(int64_t, const int64_t*, const float*,
                        int, const int64_t*, int64_t) override {}

private:
    int64_t              min_nq_;
    std::vector<float>   norms_;
    std::vector<int64_t> indices_;
};

// ── VISITED_SHARING plugin ─────────────────────────────────────────────────────

// VisitedListSharingPlugin — sequential batch search with cross-query warm start.
//
// Algorithm:
//   1. ReorderQueryBatch: sort queries by L2 norm (same as L2NormSortPlugin)
//      so consecutive queries are likely to be spatially close.
//   2. For each query i (sequential):
//      a. GetWarmEntryPoint: return top-1 result of query i-1 as entry hint.
//      b. HnswFastSearchFloat runs with that entry hint.
//      c. OnQueryVisited: record top-1 result + visited nodes for next query.
//
// The visited-node set is accumulated across the batch. Nodes visited by
// earlier queries are tracked in a bitset; the plugin exposes this to DoSearch
// so it can optionally skip already-visited nodes (approximate mode).
//
// Thread safety: NOT thread-safe across concurrent DoSearch calls.
// Each DoSearch call must use its own plugin instance, or the caller must
// serialize calls. The global singleton is safe for single-threaded benchmarks.
class VisitedListSharingPlugin final : public BatchQueryOptimizerPlugin {
public:
    // num_vectors: total vectors in the segment (for visited bitset sizing).
    // approx_skip: if true, DoSearch may skip nodes in the shared visited set
    //              (faster but lower recall). If false, visited set is used
    //              only for warm-start entry point selection.
    explicit VisitedListSharingPlugin(int64_t num_vectors = 0,
                                      bool    approx_skip = false);
    ~VisitedListSharingPlugin() override = default;

    PluginMode  Mode() const override { return PluginMode::VISITED_SHARING; }
    const char* Name() const override { return "VisitedListSharingPlugin"; }

    // Called once per batch before the sequential loop.
    // Sorts by L2 norm and resets per-batch state.
    void ReorderQueryBatch(
        const float* query, int64_t nq, int dim, int64_t* order_out
    ) override;

    // L2_NORM_SORT methods — unused
    QueryChunk GetOptimizedChunks(int64_t, int, int) override {
        return {0, 0};
    }
    void OnChunkDone(int64_t, int64_t, int64_t) override {}

    // Returns the top-1 result of the previous query as warm entry point.
    // Returns -1 for the first query in a batch.
    int64_t GetWarmEntryPoint(int64_t query_slot) override;

    // Records top-1 result and merges visited nodes into the shared bitset.
    void OnQueryVisited(
        int64_t        query_slot,
        const int64_t* result_ids,
        const float*   result_dists,
        int            topk,
        const int64_t* visited_ids,
        int64_t        visited_count
    ) override;

    // Resize the visited bitset for a new segment size.
    void ResizeForSegment(int64_t num_vectors);

    // Returns true if node_id was visited by any prior query in this batch.
    bool IsVisited(int64_t node_id) const;

    bool ApproxSkip() const { return approx_skip_; }

private:
    int64_t              min_nq_;
    bool                 approx_skip_;
    std::vector<float>   norms_;
    std::vector<int64_t> indices_;

    // Per-batch state (reset in ReorderQueryBatch)
    int64_t              prev_top1_;       // top-1 result of previous query
    std::vector<uint8_t> visited_bits_;    // bitset over segment nodes
    int64_t              num_vectors_;     // segment size (for bitset bounds)
};

}  // namespace plasmod
