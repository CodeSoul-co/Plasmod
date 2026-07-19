# Core Interfaces

## Event Backbone

- `eventbackbone.WAL`: Append、Scan、LatestLSN；
- `eventbackbone.ErrorAwareWAL`: 带 scan error；
- `eventbackbone.Bus`: publish/subscribe；
- `DerivationLogger`、`PolicyDecisionLogger`。

## Storage

- `SegmentStore`、`IndexStore`；
- `ObjectStore`；
- `GraphEdgeStore`；
- `SnapshotVersionStore`；
- `PolicyStore`、`ShareContractStore`；
- `AuditStore`、`MemoryAlgorithmStateStore`；
- `RuntimeStorage`：上述能力的聚合边界。

## Data And Query

- `dataplane.DataPlane`；
- `dataplane.EmbeddingGenerator`/`embedding.Generator`；
- `retrievalplane.SearchService`、`StorageService`、`CompactionService`；
- `semantic.QueryPlanner`。

## Runtime

- `worker.IngestWorker`；
- materialization/state/index/proof worker interfaces；
- `consistency.CheckpointStore`、`WatermarkAdvancer`；
- `transport.RuntimeAPI`。

## Substitution rules

接口实现不仅要满足方法集合，还要保留 context cancellation、not-found 语义、幂等/重放行为、资源关闭、并发安全
和错误可分类性。实现映射见 [`../generated/interface-implementation-map.md`](../generated/interface-implementation-map.md)。
