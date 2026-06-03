// Copyright 2024 Plasmod
// SPDX-License-Identifier: Apache-2.0
//
// Fast-path entry into IvfIndexNode for online single-query search. This keeps
// FAISS IVF search semantics intact while bypassing Knowhere's per-row future
// scheduling and DataSet result allocation overhead when nq == 1.

#pragma once

#include <cstdint>

#include "knowhere/index/index_node.h"

namespace knowhere {

int
IvfFastSearchFloat(IndexNode* node,
                   const float* query,
                   int          k,
                   int          nprobe,
                   int64_t*     out_ids,
                   float*       out_dists);

int
IvfFastSearchBatchFloat(IndexNode* node,
                        const float* queries,
                        int64_t      nq,
                        int          k,
                        int          nprobe,
                        int64_t*     out_ids,
                        float*       out_dists);

}  // namespace knowhere
