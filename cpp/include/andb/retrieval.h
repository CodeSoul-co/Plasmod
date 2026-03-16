#ifndef ANDB_RETRIEVAL_H
#define ANDB_RETRIEVAL_H

#ifdef __cplusplus
extern "C" {
#endif

const char* andb_version();
int andb_dense_search(const char* query, int top_k, const char** out_ids, int max_ids);

#ifdef __cplusplus
}
#endif

#endif
