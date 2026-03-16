#include "andb/retrieval.h"

static const char* kVersion = "andb-retrieval-0.1.0";
static const char* kMockIDs[] = {"mem_001", "mem_002", "mem_003", "mem_004"};

const char* andb_version() {
  return kVersion;
}

int andb_dense_search(const char* /*query*/, int top_k, const char** out_ids, int max_ids) {
  if (out_ids == nullptr || max_ids <= 0 || top_k <= 0) {
    return 0;
  }
  int n = top_k < max_ids ? top_k : max_ids;
  if (n > 4) {
    n = 4;
  }
  for (int i = 0; i < n; ++i) {
    out_ids[i] = kMockIDs[i];
  }
  return n;
}
