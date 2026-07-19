# Core Interfaces

本页用于快速定位接口族。方法、实现、constructor、bootstrap 选择、active/partial/defined-not-wired/contract-only 状态见 [Interface Implementation Registry](../../02-concepts-and-design/system-design/06-cross-reference/interface-implementation-registry.md)。

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

## Agent SDK

- `agent.MemoryManager`：有 active HTTP adapter；
- `agent.LLMProvider`、`agent.MASProvider`：仅扩展契约，核心没有生产实现；
- `schemas.MemoryManagementAlgorithm`：服务进程内的 memory plugin contract，与 `agent.MemoryManager` 不同层。

## Contract caveats

- `worker.IngestWorker.Accept`/`PipelineIngestWorker` 有实现与测试，但未接当前主 Runtime；
- `schemas.GraphExpander` 没有 active implementation，实际 QueryChain 使用 `SubgraphExecutorWorker`；
- protobuf/gRPC generated interfaces 是 wire contract，不代表 feature parity；
- 上游兼容目录中的接口不自动成为 active Plasmod extension point。

## Substitution rules

接口实现不仅要满足方法集合，还要保留 context cancellation、not-found 语义、幂等/重放行为、资源关闭、并发安全
和错误可分类性。实现映射见 [`../generated/interface-implementation-map.md`](../generated/interface-implementation-map.md)。
