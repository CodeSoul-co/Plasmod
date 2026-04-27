// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// SegmentIndexManager — manages per-segment Knowhere HNSW indexes.
//
// segment_id format: "object_type.memory_type.time_bucket.agent"
// This matches the primary key of the retrieval_segments table.
//
// Thread safety: shared_mutex — concurrent reads, exclusive writes.
// All external calls are safe to make from multiple goroutines.

#pragma once

#include <cstdint>
#include <memory>
#include <string>
#include <unordered_map>
#include <shared_mutex>
#include <vector>

namespace plasmod {

class SegmentIndexManager {
public:
    // Global singleton (one manager per process).
    static SegmentIndexManager& Instance();

    // Build (or rebuild) a segment's HNSW index.
    // segment_id : "object_type.memory_type.time_bucket.agent"
    // vectors    : row-major float32 matrix  [n × dim]
    // n          : number of vectors
    // dim        : embedding dimension
    // Returns 0 on success, negative error code on failure.
    int BuildSegment(const std::string& segment_id,
                     const float*       vectors,
                     int64_t            n,
                     int                dim);

    // ANN search within a segment — no filter.
    // query    : row-major float32 matrix  [nq × dim]
    // nq       : number of query vectors
    // topk     : results per query
    // out_ids  : caller-allocated int64 array  [nq × topk]
    // out_dists: caller-allocated float array  [nq × topk]
    // Returns 0 on success.
    int Search(const std::string& segment_id,
               const float*       query,
               int64_t            nq,
               int                topk,
               int64_t*           out_ids,
               float*             out_dists);

    // ANN search with allow-list filter (BitsetView).
    // allow_bits  : bitmask — bit i=1 means vector i is a valid candidate
    // allow_count : total number of vectors the bitmask covers (bits, not bytes)
    int SearchWithFilter(const std::string& segment_id,
                         const float*       query,
                         int64_t            nq,
                         int                topk,
                         const uint8_t*     allow_bits,
                         int64_t            allow_count,
                         int64_t*           out_ids,
                         float*             out_dists);

    // Remove a segment's index from memory.
    // Returns 0 on success, -1 if the segment was not found.
    int UnloadSegment(const std::string& segment_id);

    // True if the segment is currently loaded.
    bool HasSegment(const std::string& segment_id) const;

    // Returns the list of currently loaded segment IDs.
    std::vector<std::string> ListSegments() const;

    // Returns the number of vectors in a segment, or -1 if not found.
    int64_t SegmentSize(const std::string& segment_id) const;

    // RegisterWarmSegment stores object IDs for a segment so the Go layer can
    // map int search results back to object IDs.
    // object_ids: flat list of object ID strings, index i = vector i in the segment.
    // Returns 0 on success, kErrNotFound if segment does not exist.
    int RegisterWarmSegment(const std::string&              segment_id,
                            const std::vector<std::string>& object_ids);

private:
    SegmentIndexManager() = default;
    ~SegmentIndexManager() = default;

    // Non-copyable, non-movable singleton.
    SegmentIndexManager(const SegmentIndexManager&)            = delete;
    SegmentIndexManager& operator=(const SegmentIndexManager&) = delete;

    struct Entry {
        // Opaque pointers to internal index types — concrete types are in
        // segment_index.cpp only and are never exposed through this header.
        void*   index_ptr   = nullptr;  // internal: andb index instance
        void*   config_ptr  = nullptr;  // internal: andb index config
        int     dim         = 0;
        int64_t num_vectors = 0;
        std::vector<std::string> object_ids; // Go-visible IDs for SearchWarmSegment
    };

    mutable std::shared_mutex                                   mu_;
    std::unordered_map<std::string, std::shared_ptr<Entry>>     segments_;

    // Internal helpers (defined in segment_index.cpp).
    int  DoSearch(Entry& entry, const float* query, int64_t nq, int topk,
                  const uint8_t* allow_bits, int64_t allow_count,
                  int64_t* out_ids, float* out_dists);
    void DestroyEntry(Entry& e);
};

}  // namespace plasmod
