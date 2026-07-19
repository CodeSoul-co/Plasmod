# Test Map

| Behavior | Primary test areas |
|---|---|
| Route/method/error/auth/visibility | `src/internal/access/*_test.go` |
| Bootstrap/config/ports | `src/internal/app/*_test.go`, `config/*_test.go` |
| Event normalization and schema | `src/internal/schemas/*_test.go` |
| WAL/Bus/derivation | `src/internal/eventbackbone/*_test.go` |
| Badger/memory/S3/tiering | `src/internal/storage/*_test.go` |
| Materialization IDs/objects/edges | `src/internal/materialization/*_test.go`, `worker/materialization/*_test.go` |
| Runtime and end-to-end query | `src/internal/worker/runtime*_test.go`, `e2e_query_test.go` |
| Consistency modes/checkpoint | `src/internal/worker/consistency/*_test.go` |
| Retrieval/embedding | `src/internal/dataplane/*_test.go`, `retrievalplane` tests |
| Evidence/query planner | `src/internal/evidence/*_test.go`, `semantic/*_test.go` |
| SDK | `sdk/python/tests`, `sdk/nodejs/src/index.test.js` |
