// Copyright 2024 CogDB Authors
// SPDX-License-Identifier: Apache-2.0
//
// Dense retrieval interface backed by CogDB-internal Knowhere (HNSW, IP metric).

#ifndef PLASMOD_DENSE_H
#define PLASMOD_DENSE_H

#include "plasmod/types.h"
#include <cstdint>
#include <memory>

namespace plasmod {

// Forward-declare the implementation class (defined in dense.cpp).
class DenseRetrieverImpl;

// DenseRetriever wraps an HNSW index via the CogDB-internal Knowhere library.
//
// Lifecycle:
//   1. Init(cfg)              — create & configure the index
//   2. Build(vecs, n)         — bulk-insert + build graph
//   3. Add(vecs, n)           — incremental insert (after Build)
//   4. Search(...)            — ANN search, optional allow-list bitmask
class DenseRetriever {
public:
    DenseRetriever();
    ~DenseRetriever();

    DenseRetriever(const DenseRetriever&)            = delete;
    DenseRetriever& operator=(const DenseRetriever&) = delete;

    // Initialize the index with the given configuration.
    bool Init(const IndexConfig& cfg);

    // Build index from a row-major float matrix [num_vectors × dim].
    bool Build(const float* vectors, int64_t num_vectors);

    // Incrementally add vectors to an already-built index.
    bool Add(const float* vectors, int64_t num_vectors);

    // ANN search.
    //   query       : row-major float matrix [nq × dim]
    //   nq          : number of query vectors
    //   topk        : results per query
    //   allow_bits  : optional allow-list bitmask (bit=1 → candidate allowed)
    //   allow_count : total number of candidates the bitmask covers
    //   out_ids     : output IDs   [nq × topk]
    //   out_dists   : output distances [nq × topk]
    bool Search(const float* query, int64_t nq, int topk,
                const uint8_t* allow_bits, int64_t allow_count,
                int64_t* out_ids, float* out_dists);

    // Number of vectors currently in the index.
    int64_t Count() const;

    // Dimensionality of indexed vectors.
    int Dim() const;

private:
    std::unique_ptr<DenseRetrieverImpl> impl_;
};

}  // namespace plasmod

#endif  // PLASMOD_DENSE_H
