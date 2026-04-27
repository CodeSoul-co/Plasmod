// Copyright 2024 Plasmod
// SPDX-License-Identifier: Apache-2.0
//
// Fast-path entry into HnswIndexNode that skips the Index<T>::Search wrapper
// (CreateConfig + LoadConfig + per-call BitsetView reconstruction).  Suitable
// for hot online retrieval where the search parameters (top-k, ef, metric)
// are already known by the caller and the index is known to be the HNSW
// implementation.
//
// Returns 0 on success; a negative error code on failure.  The result
// buffers must hold at least `k` int64 ids and `k` float distances.  When
// `bitset_bits == 0`, no allow-list filtering is applied.

#pragma once

#include <cstddef>
#include <cstdint>

#include "knowhere/index/index_node.h"

namespace knowhere {

int
HnswFastSearchFloat(IndexNode* node,
                    const float* query,
                    int          k,
                    int          ef,
                    const uint8_t* bitset_data,
                    size_t       bitset_bits,
                    int64_t*     out_ids,
                    float*       out_dists);

}  // namespace knowhere
