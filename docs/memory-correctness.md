# Native retrieval and Memory Bank correctness

The production bootstrap now rebuilds its process-local ANN index from canonical storage at startup. Embedding-spec changes still require the existing explicit reindex opt-in. A configured vector store fails to initialize if its native backend is unavailable; it no longer silently becomes lexical-only. HNSW uses runtime SIMD dispatch rather than requiring AVX-512 on every x86 host.

Native scoped search uses Knowhere exclusion bits and the actual indexed bit count. WAL replay does not rematerialize an existing inactive memory.

When bootstrap selects Memory Bank (including its default selection), strict canonical ingest and scoped query invoke that algorithm. The secondary baseline dispatcher is removed: an empty governance result remains empty. Memory Bank YAML configuration is preserved; invalid YAML stops startup instead of silently using defaults.

The algorithm dispatcher persists governance in scoped canonical State records, reloads it before operations, and persists recall reinforcement. Repeated ingest does not reset existing algorithm state, and writing AlgorithmStateRef preserves the lifecycle selected by the algorithm. Calls on one dispatcher serialize the state read/modify/write sequence. This is not a multi-process transaction guarantee.

Conflict comparison excludes inactive and cross-tenant/workspace memories. Independent source events in a workspace are not treated as contradictions merely because their memory types match. This source-based rule does not implement general cross-message semantic contradiction detection.

## Validation

Build the native backend, then run from the repository root:

```sh
cmake -S cpp -B cpp/build -DCMAKE_BUILD_TYPE=Release -DANDB_KNOWHERE_FAISS=ON
cmake --build cpp/build -j 4
export CGO_LDFLAGS="-L$PWD/cpp/build -lplasmod_retrieval -Wl,-rpath,$PWD/cpp/build"
go test -tags retrieval ./...
go test -race -tags retrieval ./src/internal/worker/cognitive -run TestMemoryBankDurableRecallAndRepeatedIngest
```

Regression tests cover scoped native filtering (including non-byte-aligned counts and empty scope), production HTTP bootstrap/restart with disk storage, logical deletion/WAL replay, durable recall counts, repeated ingest, concurrent recall, lifecycle preservation, and governance exclusion. HTTP tests use the explicit TF-IDF test embedder and require no cloud credentials.

No automatic algorithm migration, semantic summary, scheduled decay, unknown-write retry, or cloud-provider fallback is introduced. Logical forgetting does not remove historical bytes from existing WAL files or backups.
