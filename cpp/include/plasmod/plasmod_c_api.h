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

/* DiskANN-specific init.  DiskANN is on-disk: it writes a vamana graph,
 * PQ shards and metadata files prefixed by `index_prefix` (the directory
 * containing the prefix must already exist and be writable).  After this
 * succeeds, follow the normal plasmod_retriever_build / _search flow.
 *
 * metric_type must be one of: "L2", "IP", "COSINE".
 * Returns 1 on success, 0 on failure.
 */
int   plasmod_retriever_init_diskann(
    void*       retriever,
    const char* metric_type,
    int         dim,
    const char* index_prefix
);

/* IVF_FLAT-specific init with explicit (nlist, nprobe) tuning.
 *
 * nlist  : number of coarse Voronoi cells the dataset is partitioned into
 *          at build time.  Rule of thumb: nlist ≈ 4 * sqrt(N).
 * nprobe : number of cells visited per query.  Higher → higher recall,
 *          lower QPS.  Pass <=0 to use the C++ default (nprobe=8).
 *
 * metric_type must be one of: "L2", "IP", "COSINE".
 * Returns 1 on success, 0 on failure.
 */
int   plasmod_retriever_init_ivf(
    void*       retriever,
    const char* metric_type,
    int         dim,
    int         nlist,
    int         nprobe
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

int     plasmod_segment_search_raw(
    const char*  segment_id,
    const float* query,
    int64_t      nq,
    int          topk,
    int64_t*     out_ids,
    float*       out_dists
);

int     plasmod_segment_unload(const char* segment_id);
int     plasmod_segment_exists(const char* segment_id);
int64_t plasmod_segment_size(const char* segment_id);

/* Register a warm segment's object-ID list with the Go SegmentDataPlane.
 * After calling plasmod_segment_build, call this to make the segment
 * visible to the HTTP server's SearchWarmSegment path.
 * object_ids: flat array of object ID strings (char* pointers)
 * n_ids: number of object IDs
 * Returns 0 on success, negative on error. */
int plasmod_segment_register_warm(
    const char*       segment_id,
    const char* const object_ids[],
    int64_t           n_ids
);

/* ── Sparse retriever (SPARSE_INVERTED_INDEX / SPARSE_WAND) ───────────────
 * Wraps plasmod::SparseRetriever. Sparse vectors are passed in CSR-flattened
 * form: doc_lengths[i] = nnz of doc i; indices_flat / values_flat are
 * concatenated arrays whose total length equals sum(doc_lengths).
 *
 * All functions return 1 on success / 0 on failure unless noted otherwise.
 * Functions returning int counts (e.g. search) return number of results
 * found (>=0) or a negative error code.
 */

void* plasmod_sparse_create(void);
void  plasmod_sparse_destroy(void* sparse);

/* index_type: "SPARSE_INVERTED_INDEX" (default) or "SPARSE_WAND" */
int plasmod_sparse_init(void* sparse, const char* index_type);

int plasmod_sparse_build(
    void*           sparse,
    int64_t         num_vectors,
    const int32_t*  doc_lengths,
    const uint32_t* indices_flat,
    const float*    values_flat
);

int plasmod_sparse_add(
    void*           sparse,
    int64_t         num_vectors,
    const int32_t*  doc_lengths,
    const uint32_t* indices_flat,
    const float*    values_flat
);

/*
 * Search with a sparse query vector.
 *   q_len            : nnz of query
 *   q_indices        : query non-zero indices (length q_len)
 *   q_values         : query non-zero values  (length q_len)
 *   top_k            : maximum results
 *   filter_bitset    : optional ban-list bitmask (NULL = no filter)
 *   filter_size      : bitset length in BYTES
 *   out_ids/out_scores : caller buffers, length >= top_k
 * Returns number of results filled (0..top_k), or negative on bad args.
 */
int plasmod_sparse_search(
    void*           sparse,
    int32_t         q_len,
    const uint32_t* q_indices,
    const float*    q_values,
    int             top_k,
    const uint8_t*  filter_bitset,
    size_t          filter_size,
    int64_t*        out_ids,
    float*          out_scores
);

/* Number of indexed documents, -1 on bad handle. */
int64_t plasmod_sparse_count(void* sparse);

/* 1 if plasmod_sparse_init has been called successfully (regardless of
 * whether data has been added). Use plasmod_sparse_count > 0 to distinguish
 * empty from populated indexes. */
int plasmod_sparse_is_ready(void* sparse);

/*
 * Tokenise text -> sparse vector via FNV-1a hashing.
 *   out_len_max   : capacity of out_indices/out_values (caller allocates)
 *   out_len       : actual length written (may be 0 for empty text)
 * Returns 1 on success, 0 on overflow/error. When the produced vector
 * exceeds out_len_max, returns 0 and *out_len = required length.
 */
int plasmod_sparse_text_to_vector(
    const char*  text,
    int32_t      out_len_max,
    uint32_t*    out_indices,
    float*       out_values,
    int32_t*     out_len
);

/* Persistence: save/load index to/from a file path. */
int plasmod_sparse_save(void* sparse, const char* path);
int plasmod_sparse_load(void* sparse, const char* path);

/* ── FAISS HNSW baseline ──────────────────────────────────────────────────────
 * Mirrors the SegmentIndexManager API but uses FAISS directly.
 * Used for fair kernel-level baseline comparison (same algorithm, different lib).
 * create/destroy return/accept handle; build/search return 0 on success, <0 on error. */
void* plasmod_faiss_create(void);
void  plasmod_faiss_destroy(void* handle);
int   plasmod_faiss_build(void* handle, const float* vectors, int64_t n, int dim,
                          int m, int ef_construction);
int   plasmod_faiss_search(void* handle, const float* queries, int64_t nq,
                           int topk, int64_t* out_ids, float* out_dists);
#ifdef __cplusplus
}
#endif

#endif /* PLASMOD_C_API_H */
