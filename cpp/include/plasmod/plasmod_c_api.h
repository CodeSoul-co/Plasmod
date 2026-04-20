/*
 * plasmod_c_api.h — Pure C header for CGO / FFI consumers.
 *
 * This file declares only the extern "C" functions that Go (CGO) needs to call.
 * It intentionally avoids ALL C++ headers so it can be #included by a C compiler.
 *
 * The full C++ API is in plasmod/retrieval.h (include that from C++ code only).
 */
#ifndef PLASMOD_C_API_H
#define PLASMOD_C_API_H

#include <stddef.h>   /* size_t */
#include <stdint.h>   /* int64_t, uint8_t */

#ifdef __cplusplus
extern "C" {
#endif

/* Library version */
const char* plasmod_version(void);

/* ── Legacy flat-retriever (single segment) ─────────────────────────────── */

void* plasmod_retriever_create(void);
void  plasmod_retriever_destroy(void* retriever);

int   plasmod_retriever_init(
    void*       retriever,
    const char* dense_index_type,
    const char* metric_type,
    int         dim,
    const char* sparse_index_type,
    int         rrf_k
);

int   plasmod_retriever_build(
    void*        retriever,
    const float* dense_vectors,
    int64_t      num_vectors,
    int          dim
);

int   plasmod_retriever_search(
    void*          retriever,
    const float*   query_vector,
    int            dim,
    int            top_k,
    int            for_graph,
    const uint8_t* filter_bitset,
    size_t         filter_size,
    int64_t*       out_ids,
    float*         out_scores,
    int            max_results
);

/* ── SegmentIndexManager (multi-segment, production) ──────────────────────
 * segment_id format: "object_type.memory_type.time_bucket.agent"
 * Matches retrieval_segments table primary key.
 * Returns 0 on success, negative on error.
 */

int     plasmod_segment_build(
    const char*  segment_id,
    const float* vectors,
    int64_t      n,
    int          dim
);

int     plasmod_segment_search(
    const char*  segment_id,
    const float* query,
    int64_t      nq,
    int          topk,
    int64_t*     out_ids,
    float*       out_dists
);

int     plasmod_segment_search_filter(
    const char*    segment_id,
    const float*   query,
    int64_t        nq,
    int            topk,
    const uint8_t* allow_bits,
    int64_t        allow_count,   /* in bits, not bytes */
    int64_t*       out_ids,
    float*         out_dists
);

int     plasmod_segment_unload(const char* segment_id);
int     plasmod_segment_exists(const char* segment_id);
int64_t plasmod_segment_size(const char* segment_id);

#ifdef __cplusplus
}
#endif

#endif /* PLASMOD_C_API_H */
