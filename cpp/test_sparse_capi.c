/*
 * test_sparse_capi.c — Standalone C test for the Sparse retriever C API.
 *
 * Validates that the extern "C" sparse functions exported by
 * libplasmod_retrieval.so are linkable and behave correctly without
 * requiring the Go toolchain.
 *
 * Build:
 *   cc -o cpp/build/test_sparse_capi cpp/test_sparse_capi.c \
 *      -I cpp/include -L cpp/build -lplasmod_retrieval \
 *      -Wl,-rpath,$(pwd)/cpp/build
 *
 * Run:
 *   ./cpp/build/test_sparse_capi
 */

#include "plasmod/plasmod_c_api.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define ASSERT(cond, msg) \
    do { \
        if (!(cond)) { \
            fprintf(stderr, "FAIL: %s (line %d): %s\n", #cond, __LINE__, msg); \
            return 1; \
        } \
    } while (0)

#define PASS(name) printf("  PASS: %s\n", name)

static int test_create_destroy(void) {
    void* s = plasmod_sparse_create();
    ASSERT(s != NULL, "create returned NULL");
    plasmod_sparse_destroy(s);
    PASS("create/destroy");
    return 0;
}

static int test_init(void) {
    void* s = plasmod_sparse_create();
    ASSERT(s != NULL, "create");
    int ok = plasmod_sparse_init(s, "SPARSE_INVERTED_INDEX");
    ASSERT(ok == 1, "init failed");
    /* After Init() the retriever is "ready" (initialised) but holds no data.
       Callers should use count() to distinguish empty vs populated indexes. */
    ASSERT(plasmod_sparse_is_ready(s) == 1, "should be ready (initialised) after Init");
    ASSERT(plasmod_sparse_count(s)    == 0, "count should be 0 before build");
    plasmod_sparse_destroy(s);
    PASS("init");
    return 0;
}

static int test_build_search(void) {
    void* s = plasmod_sparse_create();
    plasmod_sparse_init(s, "SPARSE_INVERTED_INDEX");

    /* 3 docs, CSR-flattened.
       doc 0: {1:1.0, 7:0.1}
       doc 1: {2:1.0, 7:0.1}
       doc 2: {7:1.0}                                                    */
    int32_t  lens[]  = {2, 2, 1};
    uint32_t idx[]   = {1, 7, 2, 7, 7};
    float    vals[]  = {1.0f, 0.1f, 1.0f, 0.1f, 1.0f};

    int ok = plasmod_sparse_build(s, 3, lens, idx, vals);
    ASSERT(ok == 1, "build failed");
    ASSERT(plasmod_sparse_count(s) == 3, "count != 3");
    ASSERT(plasmod_sparse_is_ready(s) == 1, "should be ready after build");

    /* Query: pure term 7 → doc 2 wins. */
    uint32_t q_idx[] = {7};
    float    q_val[] = {1.0f};
    int64_t  out_ids[3]    = {0};
    float    out_scores[3] = {0};

    int n = plasmod_sparse_search(s, 1, q_idx, q_val, 3, NULL, 0, out_ids, out_scores);
    ASSERT(n > 0, "search returned 0 results");
    ASSERT(out_ids[0] == 2, "top doc should be 2 for term 7");

    /* Query: pure term 1 → doc 0 wins. */
    uint32_t q_idx2[] = {1};
    float    q_val2[] = {1.0f};
    n = plasmod_sparse_search(s, 1, q_idx2, q_val2, 3, NULL, 0, out_ids, out_scores);
    ASSERT(n > 0, "search 2 returned 0 results");
    ASSERT(out_ids[0] == 0, "top doc should be 0 for term 1");

    plasmod_sparse_destroy(s);
    PASS("build/search");
    return 0;
}

static int test_filter(void) {
    void* s = plasmod_sparse_create();
    plasmod_sparse_init(s, "SPARSE_INVERTED_INDEX");

    /* 3 docs all hitting term 42 with descending weight. */
    int32_t  lens[] = {1, 1, 1};
    uint32_t idx[]  = {42, 42, 42};
    float    vals[] = {1.0f, 0.5f, 0.25f};

    plasmod_sparse_build(s, 3, lens, idx, vals);

    uint32_t q_idx[] = {42};
    float    q_val[] = {1.0f};
    int64_t  out_ids[5]    = {0};
    float    out_scores[5] = {0};

    /* No filter: top is doc 0. */
    int n = plasmod_sparse_search(s, 1, q_idx, q_val, 5, NULL, 0, out_ids, out_scores);
    ASSERT(n > 0 && out_ids[0] == 0, "unfiltered top should be doc 0");

    /* Filter doc 0 (bit 0 of byte 0). */
    uint8_t mask[1] = {0x01};
    n = plasmod_sparse_search(s, 1, q_idx, q_val, 5, mask, sizeof(mask), out_ids, out_scores);
    ASSERT(n > 0, "filtered search returned 0 results");
    for (int i = 0; i < n; ++i) {
        ASSERT(out_ids[i] != 0, "doc 0 should have been filtered out");
    }
    ASSERT(out_ids[0] == 1, "top after filter should be doc 1");

    plasmod_sparse_destroy(s);
    PASS("filter bitset");
    return 0;
}

static int test_text_to_vector(void) {
    uint32_t idx[16];
    float    vals[16];
    int32_t  out_len = 0;

    /* "hello world hello" → 2 distinct tokens. */
    int ok = plasmod_sparse_text_to_vector("hello world hello", 16, idx, vals, &out_len);
    ASSERT(ok == 1, "text_to_vector failed");
    ASSERT(out_len == 2, "expected 2 distinct tokens");

    /* Empty string → 0 tokens, still success. */
    ok = plasmod_sparse_text_to_vector("", 16, idx, vals, &out_len);
    ASSERT(ok == 1, "empty text_to_vector failed");
    ASSERT(out_len == 0, "empty text should yield 0 tokens");

    /* Buffer too small → returns 0 and out_len = required. */
    ok = plasmod_sparse_text_to_vector("a b c d e f", 2, idx, vals, &out_len);
    ASSERT(ok == 0, "small buffer should fail");
    ASSERT(out_len > 2, "out_len should report required size");

    PASS("text_to_vector");
    return 0;
}

static int test_text_round_trip(void) {
    void* s = plasmod_sparse_create();
    plasmod_sparse_init(s, "SPARSE_INVERTED_INDEX");

    /* Index a single doc generated from text. */
    uint32_t doc_idx[64];
    float    doc_val[64];
    int32_t  doc_len = 0;
    plasmod_sparse_text_to_vector("the quick brown fox", 64, doc_idx, doc_val, &doc_len);
    ASSERT(doc_len > 0, "doc tokenisation produced no tokens");

    int32_t lens[] = {doc_len};
    int ok = plasmod_sparse_build(s, 1, lens, doc_idx, doc_val);
    ASSERT(ok == 1, "build failed");

    /* Query with overlapping text. */
    uint32_t q_idx[64];
    float    q_val[64];
    int32_t  q_len = 0;
    plasmod_sparse_text_to_vector("brown fox", 64, q_idx, q_val, &q_len);

    int64_t out_ids[1] = {-1};
    float   out_scores[1] = {0};
    int n = plasmod_sparse_search(s, q_len, q_idx, q_val, 1, NULL, 0, out_ids, out_scores);
    ASSERT(n == 1, "expected 1 result");
    ASSERT(out_ids[0] == 0, "round-trip search should hit doc 0");
    ASSERT(out_scores[0] > 0.0f, "round-trip score should be positive");

    plasmod_sparse_destroy(s);
    PASS("text round-trip");
    return 0;
}

static int test_save_load(void) {
    const char* path = "/tmp/plasmod_sparse_capi_test.idx";

    /* Build + save. */
    void* src = plasmod_sparse_create();
    plasmod_sparse_init(src, "SPARSE_INVERTED_INDEX");
    int32_t  lens[]  = {2, 2};
    uint32_t idx[]   = {1, 2, 2, 3};
    float    vals[]  = {0.7f, 0.3f, 0.5f, 0.5f};
    plasmod_sparse_build(src, 2, lens, idx, vals);
    int ok = plasmod_sparse_save(src, path);
    ASSERT(ok == 1, "save failed");
    plasmod_sparse_destroy(src);

    /* Load into a fresh handle. */
    void* dst = plasmod_sparse_create();
    plasmod_sparse_init(dst, "SPARSE_INVERTED_INDEX");
    ok = plasmod_sparse_load(dst, path);
    ASSERT(ok == 1, "load failed");
    ASSERT(plasmod_sparse_count(dst) == 2, "count mismatch after load");

    uint32_t q_idx[] = {1};
    float    q_val[] = {1.0f};
    int64_t  out_ids[2]    = {-1, -1};
    float    out_scores[2] = {0};
    int n = plasmod_sparse_search(dst, 1, q_idx, q_val, 2, NULL, 0, out_ids, out_scores);
    ASSERT(n > 0 && out_ids[0] == 0, "search after load: doc 0 should win on term 1");

    plasmod_sparse_destroy(dst);
    remove(path);
    PASS("save/load");
    return 0;
}

static int test_wand_variant(void) {
    void* s = plasmod_sparse_create();
    int ok = plasmod_sparse_init(s, "SPARSE_WAND");
    ASSERT(ok == 1, "WAND init failed");

    int32_t  lens[]  = {1};
    uint32_t idx[]   = {5};
    float    vals[]  = {1.0f};
    ok = plasmod_sparse_build(s, 1, lens, idx, vals);
    ASSERT(ok == 1, "WAND build failed");

    uint32_t q_idx[] = {5};
    float    q_val[] = {1.0f};
    int64_t  out_ids[1] = {-1};
    float    out_scores[1] = {0};
    int n = plasmod_sparse_search(s, 1, q_idx, q_val, 1, NULL, 0, out_ids, out_scores);
    ASSERT(n == 1 && out_ids[0] == 0, "WAND search failed");

    plasmod_sparse_destroy(s);
    PASS("SPARSE_WAND variant");
    return 0;
}

int main(void) {
    printf("plasmod sparse C API tests:\n");
    printf("  version: %s\n\n", plasmod_version());

    int failed = 0;
    failed += test_create_destroy();
    failed += test_init();
    failed += test_build_search();
    failed += test_filter();
    failed += test_text_to_vector();
    failed += test_text_round_trip();
    failed += test_save_load();
    failed += test_wand_variant();

    if (failed) {
        printf("\n%d test(s) FAILED\n", failed);
        return 1;
    }
    printf("\nAll tests PASSED\n");
    return 0;
}
