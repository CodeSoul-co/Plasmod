# Interface Implementation Map

| Interface | Implementations/adapters |
|---|---|
| `eventbackbone.WAL` | FileWAL, InMemoryWAL |
| `storage.RuntimeStorage` | memory runtime bundle, Badger-backed runtime bundle |
| `storage.ObjectStore` | in-memory store, Badger object store, tiered wrapper |
| `storage.ColdObjectStore` | in-memory cold store, S3/MinIO store |
| `dataplane.EmbeddingGenerator` | TF-IDF, configured ONNX/provider adapter |
| `retrievalplane.SearchService` | CGO native bridge, unavailable stub |
| `semantic.QueryPlanner` | active semantic planner implementation |
| `consistency.CheckpointStore` | file checkpoint, in-memory/test stores |
| `transport.RuntimeAPI` | worker Runtime through Gateway/service adapter |

构造和最终选择发生在 `app.BuildServer`、`storage/factory.go` 与 dataplane constructors。
