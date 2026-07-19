# Interface Implementation Registry

本注册表只把 active Plasmod core 的显式 contract 列为可替换系统接口。生成的 protobuf/gRPC 接口单列为协议产物；`platformpkg`、`coordinator/controlplane`、`eventbackbone/streamplane` 和 `cpp/vendor` 中的上游内部接口不等于当前 Runtime 的 extension point。

状态含义：**active** 表示 composition root 或主调用链可达；**partial** 表示仅覆盖部分能力；**defined-not-wired** 表示有实现/测试但 bootstrap 主链不调用；**contract-only** 表示只有接口，核心库没有生产实现。

## Event and Consistency

| Interface | 方法 | Active implementation | Constructor/selection | 状态 |
|---|---|---|---|---|
| `eventbackbone.WAL` | `Append`, `Scan`, `LatestLSN` | `InMemoryWAL`, `FileWAL` | `app.BuildServer`; disk/WAL persistence 选择 FileWAL | 完整 |
| `eventbackbone.ErrorAwareWAL` | `ScanWithError` | `FileWAL` | type assertion during replay/recovery | 完整 |
| `eventbackbone.Bus` | publish/subscribe | `InMemoryBus` | `NewInMemoryBus` | 部分：进程内 |
| `eventbackbone.WatermarkReader` | `Current` | `WatermarkPublisher` | bootstrap creates shared watermark | active |
| `eventbackbone.DerivationLogger` | `Append`, `ForDerived`, `Since` | `DerivationLog` | `NewDerivationLog`/`NewDerivationLogWithStore` | active；可选 file append store |
| `eventbackbone.PolicyDecisionLogger` | `Append`, `ForObject`, `Since` | `PolicyDecisionLog` | `NewPolicyDecisionLog` | active；进程内 log |
| `eventbackbone.DerivationStore` | `Append`, `Load` | `FileDerivationStore` | optional constructor injection | partial；只有 file implementation |
| `WatermarkAdvancer` | `AdvanceTo` | `WatermarkPublisher` adapter | consistency configure | 完整 |
| `consistency.CheckpointStore` | `Load`, `Save`, `Reset` | `MemoryCheckpoint`, `FileCheckpoint`, `BufferedCheckpoint` decorator | `ConfigFromEnv`/Runtime configure | 完整 |

## Canonical Storage

| Interface | 主要方法 | Active implementation | Selection | 替换要求 |
|---|---|---|---|---|
| `SegmentStore` | upsert/list/delete by ref | memory, Badger | storage factory | 保留 namespace/ref 删除语义 |
| `IndexStore` | upsert/list | memory, Badger | storage factory | 保留 cumulative index metadata |
| `ObjectStore` | Agent/Session/Event/Memory/State/Artifact/User CRUD | memory, Badger | storage factory | not-found 返回 `(zero,false)`；并发安全 |
| `GraphEdgeStore` | put/get/delete/from/to/bulk/list/prune | memory, Badger | storage factory | 维护 source/destination secondary index |
| unexported `warmEdgeBulkDeleter` | `DeleteEdgesByObjectID` | memory and Badger graph edge stores | `PurgeMemoryWarmOnlyWithStats` type assertion | optional fast-path；仍需 residual verification |
| `SnapshotVersionStore` | put/history/latest | memory, Badger | storage factory | 保留版本排序和 latest 语义 |
| `PolicyStore` | append/get/list | memory, Badger | storage factory | append-only |
| `ShareContractStore` | put/get/by scope/list | memory, Badger | storage factory | scope 查询一致 |
| `AuditStore` | append/get/list/delete target | memory, Badger/composite | storage factory | append-only，硬删除例外需审计 |
| `MemoryAlgorithmStateStore` | put/get/list | memory, Badger/composite | storage factory | `(memory_id,algorithm_id)` 复合键 |
| `RuntimeStorage` | store accessors + `ApplyCanonicalProjection` | `MemoryRuntimeStorage`, composite Badger runtime | `BuildRuntimeFromEnv` | object/edge/version 同 backend 时保持原子 projection |
| `ColdTierDiagnosticsProvider` | `ColdTierDiagnostics` | none | no active caller | contract-only；诊断字段当前由 search output 直接携带 |
| `MemoryEmbedder` | `Generate(text)` | dataplane/embedding generators by structural typing | tiered object constructor | minimal local adapter |
| `ColdHNSWSearcher` | `ColdHNSWSearch` | cold implementations that opt in | type assertion | optional capability |
| `ColdObjectStore` | memory/agent/state/artifact/edge/embedding CRUD + lexical/vector search | `InMemoryColdStore`, `S3ColdStore` | S3 env complete 时选择 S3 | active；对象 ID、embedding family、诊断语义必须稳定 |

## Semantic, Retrieval and Evidence

| Interface | 方法 | Implementation | Bootstrap | 状态 |
|---|---|---|---|---|
| `semantic.QueryPlanner` | `Build(QueryRequest)` | `DefaultQueryPlanner` | `NewDefaultQueryPlanner` | 完整基础规划；高级 plan 类型未接主执行器 |
| `dataplane.DataPlane` | `Ingest`, `Search`, `Flush` | `SegmentDataPlane`, active `TieredDataPlane` | `NewTieredDataPlaneWithEmbedderAndConfig` | 完整基础路径 |
| `dataplane.EmbeddingGenerator` | `Generate`, `Dim`, `Reset` | `dataplane.TfidfEmbedder` and provider adapters | dataplane/embedder bootstrap | active |
| `dataplane.BatchEmbeddingGenerator` | single + `BatchGenerate` | TF-IDF wrapper and batch-capable providers | `VectorStore.AddTexts` type assertion | optional extension |
| `embedding.Generator` | embedding + `Close`, `Provider` | TF-IDF, OpenAI-compatible, Cohere, HuggingFace, Vertex AI, ONNX, TensorRT, GGUF adapters | `PLASMOD_EMBEDDER` | 后端依构建/凭据而异 |
| Native retrieval bridge | create/ingest/search/release index | CGO bridge; non-retrieval stub | build tag/link flags | 条件实现 |
| `retrievalplane.RuntimeContract` | search/storage/compaction descriptors | imported layout contract | 未作为 active Runtime 的执行接口 | 占位/兼容 |
| `schemas.GraphExpander` | `Expand(GraphExpandRequest)` | none | none | contract-only；active worker 使用不同的三参数接口 |
| Evidence assembly | `Assembler.Build` concrete API | cached or uncached Assembler | `NewCachedAssembler(...).With...` | 完整基础 evidence |
| Evidence cache | get/put/get-many/stats | in-memory bounded `evidence.Cache` | bootstrap config size | 部分：不持久化 |

## Worker Interfaces and Implementations

| Interface | Exact domain methods | Active implementation | Constructor | Direct caller |
|---|---|---|---|---|
| `DataNode` | `Info`; `HandleIngest(IngestRecord)` | `InMemoryDataNode` | `CreateInMemoryDataNode` | NodeManager |
| `IndexNode` | `Info`; `BuildIndex(IngestRecord)` | `InMemoryIndexNode` | `CreateInMemoryIndexNode` | NodeManager |
| `QueryNode` | `Info`; `Search(SearchInput) SearchOutput` | `InMemoryQueryNode` | `CreateInMemoryQueryNode` | Runtime via NodeManager |
| nodes `IngestWorker` | `Info`; `Process(Event) error` | `InMemoryIngestWorker` | `CreateInMemoryIngestWorker` | MainChain/Manager；主 Runtime 另有 normalize |
| `ObjectMaterializationWorker` | `Info`; `Materialize(Event) error` | `InMemoryObjectMaterializationWorker` | `CreateInMemoryObjectMaterializationWorker` | MainChain/subscriber |
| `StateMaterializationWorker` | `Info`; `Apply(Event)`; `Checkpoint(agent,session)` | `InMemoryStateMaterializationWorker` | `CreateInMemoryStateMaterializationWorker` | MainChain/subscriber |
| `ToolTraceWorker` | `Info`; `TraceToolCall(Event) error` | `InMemoryToolTraceWorker` | `CreateInMemoryToolTraceWorker` | MainChain/subscriber |
| `IndexBuildWorker` | `Info`; `IndexObject(id,type,namespace,text)` | `InMemoryIndexBuildWorker` | `CreateInMemoryIndexBuildWorker` | MainChain/subscriber |
| `GraphRelationWorker` | `Info`; `IndexEdge(src,dst,type,weight)` | `InMemoryGraphRelationWorker` | `CreateInMemoryGraphRelationWorker` | MainChain/Manager |
| `MemoryExtractionWorker` | `Info`; `Extract(event,agent,session,content)` | baseline `InMemoryMemoryExtractionWorker` | `CreateInMemoryMemoryExtractionWorker` | MemoryPipeline/subscriber |
| `MemoryConsolidationWorker` | `Info`; `Consolidate(agent,session)` | baseline `InMemoryMemoryConsolidationWorker` | `CreateInMemoryMemoryConsolidationWorker` | MemoryPipeline/subscriber |
| `SummarizationWorker` | `Info`; `Summarize(agent,session,maxLevel)` | baseline `InMemorySummarizationWorker` | `CreateInMemorySummarizationWorker` | MemoryPipeline |
| `ReflectionPolicyWorker` | `Info`; `Reflect(objectID,objectType)` | baseline `InMemoryReflectionPolicyWorker` | `CreateInMemoryReflectionPolicyWorker` | MemoryPipeline/subscriber |
| `ConflictMergeWorker` | `Info`; `Merge(left,right,objectType)` | `InMemoryConflictMergeWorker` | `CreateInMemoryConflictMergeWorker` | Runtime/Collaboration/subscriber |
| `CommunicationWorker` | `Info`; `Broadcast(from,to,memoryID)` | `InMemoryCommunicationWorker` | `CreateInMemoryCommunicationWorker` | Runtime/Collaboration |
| `MicroBatchScheduler` | `Info`; `Enqueue(queryID,payload)`; `Flush() []any` | `InMemoryMicroBatchScheduler` | `CreateInMemoryMicroBatchScheduler` | Collaboration/Manager；仅显式 flush |
| `ProofTraceWorker` | `Info`; `AssembleTrace(ids,maxDepth)` | `InMemoryProofTraceWorker` | `CreateInMemoryProofTraceWorker` | QueryChain |
| `SubgraphExecutorWorker` | `Info`; `Expand(req,nodes,edges)` | `InMemorySubgraphExecutorWorker` | `CreateInMemorySubgraphExecutorWorker` | QueryChain |
| `AlgorithmDispatchWorker` | `Info`; `Dispatch(operation,ids,query,nowTS,agentID,sessionID,signals)` | `InMemoryAlgorithmDispatchWorker` | `CreateAlgorithmDispatchWorker` | Runtime internal API |
| `Runnable` | `Run(WorkerInput) (WorkerOutput,error)` | 多数 concrete worker 的 typed adapter | concrete worker constructor | 当前不是统一主调度入口 |

此外存在另一组执行面接口 `worker.IngestWorker.Accept(Event)` 和实现 `PipelineIngestWorker`。它完整封装 WAL、materialization、canonical persistence、precompute、node fan-out 和 DataPlane ingest，并有单元测试，但 `app.BuildServer` 与当前 Runtime 主路径没有构造或调用它，状态是 **defined-not-wired**。不能把它描述为当前 authoritative ingest chain。

`schemas.WorkerInput` 与 `schemas.WorkerOutput` 是 typed worker message 的 marker interfaces；具体 `*Input`/`*Output` 类型定义在 `src/internal/schemas/worker_params.go`。它们约束 `Runnable.Run` 的消息形状，但没有统一 scheduler 强制所有 worker 只通过该入口执行。

`nodes.Manager` 对多数 dispatch 选择注册列表中的第一个 worker，不执行动态负载均衡、健康探测或资源感知选择。

## Memory Algorithm Plugins

| Interface | Implementations | 可用 operation | 状态 |
|---|---|---|---|
| `schemas.MemoryManagementAlgorithm` | baseline | ingest/update/recall/compress/decay/summarize/export/load | 完整基础实现 |
| same | MemoryBank-style | reinforcement、retention、summary/profile-oriented behavior | 部分，语义为仓库自有 style implementation |
| same | Zep-style | recall/compress/decay/summarize behavior | 部分，非外部 Zep 服务等价实现 |
| same | custom plugin | 由接口扩展 | 取决于实现和 bootstrap 注册 |

Dispatcher 只路由和持久化，不自行决定 lifecycle threshold。`SuggestedLifecycleState` 由 plugin 给出并原样应用。

## Agent SDK Extension Contracts

| Interface | Methods | Core implementation | Actual caller | 状态 |
|---|---|---|---|---|
| `agent.MemoryManager` | `Name`, `Recall`, `Ingest`, `Compress`, `Summarize`, `Decay` | `BaselineMemoryManager` HTTP adapter | `AgentSession` and AgentGateway | active when legacy-named `Config.CogDBEndpoint` is configured |
| `agent.LLMProvider` | `Complete`, `Provider` | none in core | attached through `AgentSession.WithLLM`; no core generation workflow consumes it yet | contract-only |
| `agent.MASProvider` | `Peers`, `Topology` | none in core | attached through `AgentSession.WithMAS`; collaboration methods do not enforce it | contract-only |

`MemoryManager` 是 Agent SDK 到 internal memory routes 的 adapter contract，不等同于底层 `schemas.MemoryManagementAlgorithm` plugin interface。前者跨 HTTP 调用，后者在服务进程内由 algorithm dispatcher 执行。

## Runtime and Transport

| Boundary | Concrete implementation | Notes |
|---|---|---|
| `worker.Runtime` | `CreateRuntime(...)` | 聚合 WAL、DataPlane、stores、policy、planner、materializer、evidence、NodeManager 和 consistency |
| `transport.RuntimeAPI` | `*worker.Runtime` | 只暴露 warm segment/native transport 方法，避免 import cycle |
| HTTP Gateway | `access.Gateway` | 主 Event/Query/canonical/admin/internal handlers |
| gRPC service | `api/grpc.Server` | 通过 Gateway/service adapter；覆盖面不等同 HTTP |
| `worker.Orchestrator` | concrete priority dispatcher | bootstrap 启动，但主 Gateway/Runtime 未提交任务，属于部分接线 |
| `coordinator.WorkerScheduler` | dispatch/active counter | 不是执行器，也未驱动 NodeManager 选择 |

`PlasmodAPIServiceClient`、`PlasmodAPIServiceServer` 和 stream client/server interfaces 由 protobuf 生成，只定义 wire contract。它们的存在不能证明 HTTP feature parity、scheduler 接线或 engine replacement point；服务实现与路由覆盖必须单独检查 `src/internal/api/grpc/server.go`。

## Interface Change Checklist

1. 更新 interface 和所有 memory/Badger/S3/native adapter；
2. 更新 `app.BuildServer` 的 constructor 参数和注册顺序；
3. 检查 nil/not-found、context、并发、关闭和错误分类语义；
4. 为替换实现增加 contract test，而不仅是 concrete test；
5. 更新本注册表、Engine 页面和 API/Chain 映射。
