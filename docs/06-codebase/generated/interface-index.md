# Interface Index

完整接口到实现和接线状态见 [Interface Implementation Registry](../../02-concepts-and-design/system-design/06-cross-reference/interface-implementation-registry.md)。

| Domain | Interfaces |
|---|---|
| Event | WAL, ErrorAwareWAL, Bus, WatermarkReader, DerivationLogger |
| Storage | SegmentStore, IndexStore, ObjectStore, GraphEdgeStore, SnapshotVersionStore, RuntimeStorage |
| Query | DataPlane, QueryPlanner, SearchService |
| Embedding | EmbeddingGenerator, Generator, BatchEmbeddingGenerator |
| Runtime | IngestWorker, materialization workers, CheckpointStore |
| Transport | RuntimeAPI |
| Agent SDK | MemoryManager, LLMProvider, MASProvider |
| Memory algorithms | MemoryManagementAlgorithm |
| Optional capabilities | BatchEmbeddingGenerator, ColdHNSWSearcher, ColdTierDiagnosticsProvider |

具体签名见各 package `contracts.go` 或接口声明文件。
