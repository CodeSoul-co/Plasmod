# Interface Index

| Domain | Interfaces |
|---|---|
| Event | WAL, ErrorAwareWAL, Bus, WatermarkReader, DerivationLogger |
| Storage | SegmentStore, IndexStore, ObjectStore, GraphEdgeStore, SnapshotVersionStore, RuntimeStorage |
| Query | DataPlane, QueryPlanner, SearchService |
| Embedding | EmbeddingGenerator, Generator, BatchEmbeddingGenerator |
| Runtime | IngestWorker, materialization workers, CheckpointStore |
| Transport | RuntimeAPI |

具体签名见各 package `contracts.go` 或接口声明文件。
