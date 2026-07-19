# Interface Implementation Map

本页保留 compact map；完整方法、constructor 和接线状态见 [Interface Implementation Registry](../../02-concepts-and-design/system-design/06-cross-reference/interface-implementation-registry.md)。

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
| `agent.MemoryManager` | BaselineMemoryManager HTTP adapter |
| `agent.LLMProvider`, `agent.MASProvider` | core contract only; no production implementation |
| `schemas.MemoryManagementAlgorithm` | baseline, MemoryBank-style, Zep-style plugins |
| `worker.IngestWorker.Accept` | PipelineIngestWorker; defined and tested, not wired into active Runtime |
| `schemas.GraphExpander` | no active implementation; worker chain uses SubgraphExecutorWorker |

构造和最终选择发生在 `app.BuildServer`、`storage/factory.go` 与 dataplane constructors。
