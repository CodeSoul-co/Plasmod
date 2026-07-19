# 14. 实现状态、缺口与可声明边界

> Language: 中文 | [English](en/14-implementation-status-gaps-and-claim-boundaries.md)

---

汇总系统设计核对、接口接线、失败矩阵、成熟度、限制和代码测试证据。

---

## 14.1. API to Engine Matrix

### 14.1.1. Public/Application Routes

| Method/route | Handler/Runtime entry | Chain | Primary Engines | 关键边界 |
|---|---|---|---|---|
| `POST /v1/ingest/events` | `handleIngest` -> `SubmitIngestContext` | Ingest | Execution Coordination, Object Derivation, Canonical Graph, Retrieval, Consistency | 完整 WAL/canonical/visibility 入口 |
| `POST /v1/ingest/vectors` | `handleIngestVectors` -> warm ingest methods | none | Adaptive Retrieval | 只写 retrieval segment，无 Event/canonical 语义 |
| `POST /v1/ingest/document` | document assembler -> repeated `SubmitIngest` | Ingest | Object Derivation, Retrieval | internal document adapter；稳定性不等同 Event API |
| `POST /v1/query` | `handleQuery` -> `ServiceQueryContext` -> `ExecuteQueryContext` | Query | Retrieval, Evidence, Canonical Graph, Governance | structured evidence 主入口 |
| `POST /v1/query/batch` | `ServiceQueryBatch` | none | Adaptive Retrieval | `VectorWarmBatchQueryRequest`，不是 QueryRequest 数组 |
| `GET/POST /v1/agents` | canonical handler | none | Canonical Graph | 直接 store CRUD，绕过 WAL/Chain |
| `GET/POST /v1/sessions` | canonical handler | none | Canonical Graph | 同上 |
| `GET/POST /v1/memory` | canonical handler | none | Canonical Graph | 同上；不会自动写 projection/version |
| `GET/POST /v1/states` | canonical handler | none | Canonical Graph | 同上 |
| `GET/POST /v1/artifacts` | canonical handler | none | Canonical Graph | 同上 |
| `GET/POST /v1/edges` | canonical handler | none | Canonical Graph | 不验证两端对象语义完整性 |
| `GET/POST /v1/policies` | canonical handler | none | Governance, Canonical Graph | record storage；不是通用 policy language |
| `GET/POST /v1/share-contracts` | canonical handler | none | Governance, Canonical Graph | contract storage；执行覆盖有限 |
| `GET /v1/traces/{id}` | `handleTraces` | Query-like | Evidence, Canonical Graph | 按对象组装已有 evidence/provenance |
| `GET /v1/agent/list` | internal agent listing | Collaboration | Canonical Graph/Governance | experimental |

### 14.1.2. Internal Runtime Routes

| Route group | Runtime entry | Engine | State mutation |
|---|---|---|---|
| `/v1/internal/memory/recall` | `DispatchRecall` | Memory Evolution + Governance | 通常只读；plugin recall 不持久化 state |
| `/v1/internal/memory/ingest` | `DispatchAlgorithm("ingest")` | Memory Evolution | algorithm state, Memory ref, audit |
| `/v1/internal/memory/compress` | algorithm dispatch | Memory Evolution + Canonical Graph | derived Memory, state/audit |
| `/v1/internal/memory/summarize` | algorithm dispatch | Memory Evolution + Canonical Graph | summary Memory, audit |
| `/v1/internal/memory/decay` | algorithm dispatch | Memory Evolution | lifecycle/algorithm state/audit |
| `/v1/internal/memory/share` | `DispatchShare` | Collaboration + Governance | shared Memory copy；contract enforcement 有限 |
| `/v1/internal/memory/conflict/resolve` | `DispatchConflictResolve` | Collaboration | winner/loser mutation, conflict edge/audit |
| `/v1/internal/memory/stale` | handler direct store mutation | Memory Evolution | `stale`, inactive, audit/metric path |
| `/v1/internal/memory/conflict/inject` | handler direct canonical setup | Collaboration | controlled conflict records；internal only |
| `/v1/internal/task/start\|complete\|tokens\|claim\|stage` | metric/state handlers | Execution Coordination | session metrics/canonical events depending handler |
| `/v1/internal/plan/step\|repair` | plan handlers | Execution Coordination/Evidence | plan state/metrics |
| `/v1/internal/mas/answer-consistency\|aggregate` | MAS handlers | Collaboration | metrics/aggregate response |
| `/v1/internal/tool-state` | tool state handler -> ingest/query | Ingest + Query | State/Event/projection |
| `/v1/internal/agent/handoff` | handoff handler -> ingest/share | Collaboration + Ingest | Event/shared object |
| `/v1/internal/session/context` | session context query | Query/Evidence | read only |
| `/v1/internal/warm-segment/register` | Runtime register | Adaptive Retrieval | in-memory/native segment mapping |
| `/v1/internal/eval/ground-truth` | internal handler | outside stable core contract | 不作为核心系统能力声明 |

Internal routes 未被 admin middleware 自动保护，部署层必须限制网络访问。

### 14.1.3. Management Routes

| Route | Primary Engine/Manager | 行为 |
|---|---|---|
| `/healthz`, `/v1/system/mode` | app/access | process/mode status |
| `/v1/admin/topology` | Execution Coordination | coordinator registry + NodeManager topology |
| `/v1/admin/storage`, `/config/effective` | Tiered Storage/app | effective runtime config |
| `/v1/admin/s3/export`, `/snapshot-export`, `/cold-purge` | Tiered Storage | archive/export/purge |
| `/v1/admin/warm/prebuild` | Adaptive Retrieval | build/load default warm segment |
| `/v1/admin/embeddings/reindex` | Retrieval + Reconciliation fragments | scan canonical memories and rebuild retrieval projection |
| `/v1/admin/dataset/delete`, `/memory/delete-by-source` | Governance + Canonical Graph | logical deletion |
| `/v1/admin/dataset/purge`, `/memory/purge-by-source`, `/dataset/purge/task` | Reconciliation fragments + Storage | staged hard deletion; no distributed transaction |
| `/v1/admin/data/wipe` | Runtime/Storage/Consistency | pause/reset stores, WAL/checkpoint/index |
| `/v1/admin/rollback` | Version/Storage | 部分实现；不是全系统 point-in-time rollback |
| `/v1/admin/consistency-mode` | Consistency Controller | inspect/change default mode |
| `/v1/admin/replay` | Consistency/Runtime | preview or apply WAL replay |
| `/v1/admin/metrics` | metrics collector | in-process counters/histograms/status |
| `/v1/admin/governance-mode`, `/runtime-mode` | Runtime/Governance | toggle in-memory runtime flags |
| `/v1/admin/memory/providers/mode\|health` | Memory Evolution | provider router mode/health |

### 14.1.4. Internal Transport Routes

| Route | Contract | Engine | Evidence/canonical semantics |
|---|---|---|---|
| `POST /v1/internal/rpc/ingest_batch` | binary vector batch | Adaptive Retrieval | 无 |
| `POST /v1/internal/rpc/unload_segment` | segment ID | Adaptive Retrieval | 无 |
| `POST /v1/internal/rpc/query_warm` | binary query vector | Adaptive Retrieval | 无 |
| `POST /v1/internal/rpc/query_warm_batch` | binary NQ batch | Adaptive Retrieval | 无 |
| `POST /v1/internal/rpc/query_warm_serial_batch` | serial reference | Adaptive Retrieval | 无 |
| `POST /v1/internal/rpc/query_warm_batch_raw` | raw native path | Adaptive Retrieval | 无 |
| `POST /v1/internal/rpc/register_warm` | JSON mapping | Adaptive Retrieval | 无 |
| `GET /v1/wal/stream` | SSE | Event backbone | WAL observation only |

### 14.1.5. API Design Rules

1. 需要 durability、replay、canonical object 和 visibility guarantee 时使用 Event ingest；
2. direct canonical POST 是管理/迁移接口，不应被描述为等价 Ingest Chain；
3. native warm routes只返回 ANN 候选，不能宣称返回 evidence-bearing response；
4. internal route 的 request struct 和稳定性由当前 commit 决定；
5. 新接口必须在本表明确其 Chain、Engine、事务边界和绕过的机制。

---

## 14.2. Claim and Test Boundary

本节把可声明的核心系统能力绑定到当前代码入口和测试证据。

### 14.2.1. Supported Claims

| Core claim | Code path | Primary tests | Boundary |
|---|---|---|---|
| Event-first durable ingest with replayable LSN | Gateway -> Runtime -> consistency -> WAL | `eventbackbone/*_test.go`, `worker/consistency/*_test.go`, runtime tests | disk mode才是进程重启后持久 WAL |
| Event derives canonical Memory/State/Artifact/Edge/Version | materialization service + specialized workers | materialization and worker materialization tests | targets/enabled 不是全局硬 gate |
| Canonical truth is separated from retrieval projection | storage contracts + DataPlane | storage projection tests, dataplane tests | 两平面无跨系统 ACID transaction |
| strict/bounded/eventual visibility modes | consistency controller/tracker/checkpoint | consistency mode/controller/tracker tests | guarantee 由单进程 controller 范围定义 |
| hybrid/tiered retrieval with optional native ANN | TieredDataPlane + vector/sparse/segment/native bridge | dataplane/retrievalplane tests | native index depends build tag/library；cold only explicit query |
| evidence-bearing query response | planner -> retrieval -> assembler -> QueryChain | evidence, semantic, worker e2e query tests | response objects 是 IDs；policy annotation 不等于完整 ACL enforcement |
| pluggable memory management algorithm | interface + dispatcher + baseline/MemoryBank-style/Zep-style | cognitive tests | style implementation 不等价于外部产品服务 |
| canonical graph and provenance records | Edge/Version/derivation stores + proof worker | schema graph, evidence, coordination tests | graph validity is application-enforced, not referential constraint |
| explicit share contract records and shared copies | ShareContractStore + communication/conflict worker | governance/coordination tests | read ACL主要用于 contamination observation；未统一强制所有读写/derive |
| hot/warm/cold object management | TieredObjectStore + S3/InMemory cold | storage tiered/S3 tests | promotion/demotion mostly explicit/policy worker driven |

### 14.2.2. Claims Requiring Qualification

| Phrase | Required qualification |
|---|---|
| “四条 Chain 统一调度所有请求” | 不成立；四个 type 存在，但 Gateway 主路径不经过 Orchestrator |
| “智能资源调度” | 不成立；现有是 consistency queues、固定优先级 Orchestrator 和计数型 WorkerScheduler |
| “完整 Reconciliation Manager” | 不成立；只有 replay/reindex/purge/checkpoint 等分散能力 |
| “全链路 ACL 强制” | 不成立；存在 policy/share schema、过滤与检测，但非所有路径统一 enforce |
| “任意 Event target 可配置派生任意对象” | 不成立；materializer 是明确的 deterministic 规则 |
| “Evidence 完整可重放” | 需限定：Edge/Version/derivation 可持久，Evidence cache/proof response 可重算但 cache 不持久 |
| “跨存储事务一致” | 不成立；Badger canonical projection 可原子，native index/S3/cache 在边界之外 |
| “distributed multi-tenant scheduler” | 不成立；active core 是单进程 worker/node manager 架构 |

### 14.2.3. Test Coverage Map

| Design area | Test directories/files | Remaining gaps |
|---|---|---|
| bootstrap/listeners/shutdown | `src/internal/app/*_test.go` | full process crash matrix |
| HTTP/auth/visibility/routes | `src/internal/access/*_test.go` | every internal route contract |
| schema/ID/graph | `src/internal/schemas/*_test.go` | migration compatibility corpus |
| WAL/bus/derivation/watermark | `src/internal/eventbackbone/*_test.go` | long-running corruption recovery |
| canonical storage/tiering/S3 | `src/internal/storage/*_test.go` | cross-store fault injection |
| materialization | `src/internal/materialization/*_test.go`, worker materialization tests | all event subtype combinations |
| Runtime/query/ingest | `src/internal/worker/runtime*_test.go`, `e2e_query_test.go` | multi-process and sustained failure |
| consistency | `src/internal/worker/consistency/*_test.go` | restart at every projection boundary |
| retrieval/embedding/native | `src/internal/dataplane/*_test.go`, retrievalplane tests, C++ tests | optional backend parity |
| evidence/planner/policy | evidence/semantic tests | complete scope/ACL matrix and evidence completeness metric |
| chains/orchestrator/nodes | chain, orchestrator, manager tests | prove Gateway integration if unified routing is added |
| algorithm plugins | cognitive tests | profile migration and projection refresh assertions |

### 14.2.4. Documentation Review Gate

新增系统声明前必须同时提供：

1. active bootstrap 构造或明确的调用入口；
2. interface/type/method 的代码路径；
3. state mutation 和 failure boundary；
4. primary test；
5. 本页与对应 Architecture/Chain/Mechanism/Engine 的同步更新。

---

## 14.3. Execution State and Failure Matrix

### 14.3.1. Active Write Stages

代码没有一个覆盖全系统的 `WriteState` enum。下表把实际 tracker/runtime 状态映射到统一术语，不能将统一术语误写成已存在的单一状态机。

| 统一阶段 | 代码事件 | 持久/内存位置 | 是否 ACK 前完成 |
|---|---|---|---|
| `RECEIVED` | Gateway decode/normalize | request memory | 是 |
| `WAL_COMMITTED` | `WAL.Append` 返回 LSN | FileWAL 或 InMemoryWAL | 所有模式是 |
| `PROJECTING` | tracker `MarkProjecting` | tracker memory/checkpoint context | strict 等待；其他异步 |
| `INDEXED` | `DataPlane.Ingest` 成功 | warm retrieval structures/native index | strict 是；其他最终完成 |
| `PERSISTED` | `ApplyCanonicalProjection` 成功 | object/edge/version stores | strict 是；其他最终完成 |
| `EVIDENCE_READY` | precompute cache fragment 写入 | in-memory evidence cache | 非 visibility gate；可能跳过 |
| `VISIBLE` | tracker `MarkVisible` + checkpoint/watermark | tracker + checkpoint | strict 是；bounded/eventual ACK 后 |
| `MAINTAINED` | subscriber/reflection/consolidation/flush | multiple stores/cache/index | 否 |

当前 `projectWALEntry` 实际顺序是 retrieval ingest -> canonical projection -> hot promotion/conflict/precompute/worker dispatch。它通过“先检索失败再避免 canonical mutation”缩小 partial window，但 retrieval 与 canonical store 不属于同一跨系统事务。

### 14.3.2. Consistency Modes

| Mode | Submit response | Read gate | Guarantee |
|---|---|---|---|
| strict/strict_visible | 等 projection + visible；失败返回 accepted-not-visible | waits through accepted prefix | request scoped read-after-write |
| bounded/bounded_staleness | WAL 后 pending ACK；sharded queue | waits within configured lag/through current accepted prefix | bounded lag，SLA breach 可观测 |
| eventual/eventual_visibility | WAL 后 pending ACK | minimal ordering gate | eventual projection |

### 14.3.3. Sync/Async Boundary

| Operation | 同步部分 | 异步部分 |
|---|---|---|
| Event ingest | validation, WAL, strict projection wait | bounded/eventual projection, subscriber maintenance, periodic flush |
| Canonical projection | retrieval ingest；Event/Memory/checkpoint State/可选 Artifact/edge/version transaction；hot promotion | keyed state/tool/consolidation/reflection via subscriber |
| Query | planner, retrieval, filters, assembler, QueryChain | no durable background completion promised |
| Memory algorithm API | selected plugin dispatch and store writes | provider shadow path may be external; no generic job tracker |
| Collaboration | conflict/share call | microbatch queue only processes when explicitly flushed |
| Archive/purge | request validation and task enqueue/start | hard delete/export stages where handler creates task |

### 14.3.4. Failure Windows

| Window | Possible state | Detection | Current recovery |
|---|---|---|---|
| WAL append fails | no accepted write | returned error | caller retry with same event ID |
| WAL succeeds, projection fails | durable Event, object not visible | tracker failure/status, 503 for strict | bounded retry; restart recovery scans WAL; admin replay |
| Retrieval ingest succeeds, canonical projection fails | candidate may exist without canonical record | projection error; query hydration/filter anomalies | replay/reindex; no automatic two-plane rollback |
| Canonical commit succeeds, precompute fails/skips | object visible without cached fragment | cache miss stats | query-time delta evidence |
| Canonical succeeds, subscriber worker fails | main object visible, auxiliary state/audit/index may lag | logs/ErrorCh/in-memory DLQ/overflow；limited per-worker status | WAL subscriber re-scan/restart, manual replay；无 durable dead-letter queue |
| Checkpoint save fails | object may be visible but durable progress uncertain | `checkpointVisibilityError`, buffered checkpoint last error | retry/flush, restart WAL scan |
| Cold archive partially fails | warm remains authoritative if deletion order respected | admin response/diagnostics | retry export; no global transaction |
| Hard purge partially fails | stores diverge | purge task stage/error | resume/retry task/manual cleanup |

### 14.3.5. Idempotency and Ordering

| Area | Current mechanism | Limitation |
|---|---|---|
| Event replay | deterministic IDs from event ID | direct workers with alternate IDs must preserve same rule |
| WAL ordering | monotonic LSN and serialized append section | global distributed ordering is not implemented by active single-process core |
| Canonical projection | same-backend transaction and upsert | retrieval/S3/cache are outside transaction |
| State version | state worker reads current state and increments | in-memory `stateKeys` plus store lookup; cross-process coordination absent |
| Edge generation | deterministic source/type/destination IDs in builders | direct Edge POST can introduce duplicates/semantic inconsistency |
| Algorithm state | composite memory/algorithm key | switching algorithm profile does not migrate old state |

### 14.3.6. Retry, Cancellation and Backpressure

| Component | Behavior |
|---|---|
| Consistency Controller | bounded queues, global slots, bounded shard slots, exponential/configured retry, context cancellation, pause/reset/resume/shutdown |
| Gateway | request context passed to Runtime for ingest/query; write semaphore controls HTTP concurrency |
| Orchestrator | four priority queues, fixed worker pool, 30 s submit timeout; no task result/future/cancellation handle |
| NodeManager | synchronous first-worker dispatch; worker errors often not propagated by void dispatch helpers |
| DataPlane flush loop | periodic retry by leaving dirty flag set and logging failure |
| Subscriber | WAL polling, context cancellation, retry/overflow and in-memory DLQ；no persistent dead-letter queue |

### 14.3.7. Recovery Capability Matrix

| Capability | Implementation | Maturity |
|---|---|---|
| startup WAL recovery | consistency controller `recoverFromWAL` from checkpoint | 完整主路径 |
| admin replay preview/apply | Runtime WAL scan and resubmit | 完整基础路径 |
| canonical re-materialization | replay deterministic materializer | 部分：受 worker/idempotency 边界约束 |
| embedding/retrieval reindex | `Runtime.ReindexEmbeddings`, warm prebuild | 完整专用操作 |
| relation repair | replay/build edges or manual operations | 部分，无独立 scanner/planner |
| evidence rebuild | query delta + ingest precompute | 部分，无全量 durable evidence rebuild manager |
| divergence scanner | no unified active component | 规划 |
| dead-letter handler | no active durable queue | 规划 |
| post-repair verification | tests/admin summaries, no unified verifier | 规划 |

### 14.3.8. Correctness Review Rule

任何新阶段都必须说明：谁持有状态、是否在 ACK 前完成、失败是否推进 checkpoint、是否可重入、如何观察、如何修复，以及它是否属于 visibility guarantee。

---

## 14.4. Interface Implementation Registry

本注册表只把 active Plasmod core 的显式 contract 列为可替换系统接口。生成的 protobuf/gRPC 接口单列为协议产物；`platformpkg`、`coordinator/controlplane`、`eventbackbone/streamplane` 和 `cpp/vendor` 中的上游内部接口不等于当前 Runtime 的 extension point。

状态含义：**active** 表示 composition root 或主调用链可达；**partial** 表示仅覆盖部分能力；**defined-not-wired** 表示有实现/测试但 bootstrap 主链不调用；**contract-only** 表示只有接口，核心库没有生产实现。

### 14.4.1. Event and Consistency

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

### 14.4.2. Canonical Storage

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

### 14.4.3. Semantic, Retrieval and Evidence

| Interface | 方法 | Implementation | Bootstrap | 状态 |
|---|---|---|---|---|
| `semantic.QueryPlanner` | `Build(QueryRequest)` | `DefaultQueryPlanner` | `NewDefaultQueryPlanner` | 完整基础规划；高级 plan 类型未接主执行器 |
| `dataplane.DataPlane` | `Ingest`, `Search`, `Flush` | `SegmentDataPlane`, active `TieredDataPlane` | `NewTieredDataPlaneWithEmbedderAndConfig` | 完整基础路径 |
| `dataplane.EmbeddingGenerator` | `Generate`, `Dim`, `Reset` | `dataplane.TfidfEmbedder` and provider adapters | dataplane/embedder bootstrap | active |
| `dataplane.BatchEmbeddingGenerator` | single + `BatchGenerate` | TF-IDF wrapper and batch-capable providers | `VectorStore.AddTexts` type assertion | optional extension |
| `embedding.Generator` | embedding + `Close`, `Provider` | TF-IDF, OpenAI-compatible, Cohere, HuggingFace, Vertex AI, ONNX, TensorRT, GGUF adapters | `PLASMOD_EMBEDDER` | 后端依构建/凭据而异 |
| Native retrieval bridge | create/ingest/search/release index | CGO bridge; non-retrieval stub | build tag/link flags | 条件实现 |
| `retrievalplane.RuntimeContract` | search/storage/compaction descriptors | imported layout contract | 未作为 active Runtime 的执行接口 | 占位/兼容 |
| `retrievalplane.SearchService` | `QueryPath`, `SupportsSegmentPlanning` | none | only returned by `RuntimeContract` | contract-only；描述 imported query layout，不执行当前查询 |
| `retrievalplane.StorageService` | `ObjectStorePath`, `SharedStoragePath` | none | only returned by `RuntimeContract` | contract-only；描述 imported storage layout |
| `retrievalplane.CompactionService` | `CompactionPath` | none | only returned by `RuntimeContract` | contract-only；描述 imported compaction layout |
| `schemas.GraphExpander` | `Expand(GraphExpandRequest)` | none | none | contract-only；active worker 使用不同的三参数接口 |
| Evidence assembly | `Assembler.Build` concrete API | cached or uncached Assembler | `NewCachedAssembler(...).With...` | 完整基础 evidence |
| Evidence cache | get/put/get-many/stats | in-memory bounded `evidence.Cache` | bootstrap config size | 部分：不持久化 |

### 14.4.4. Worker Interfaces and Implementations

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

### 14.4.5. Memory Algorithm Plugins

| Interface | Implementations | 可用 operation | 状态 |
|---|---|---|---|
| `schemas.MemoryManagementAlgorithm` | baseline | ingest/update/recall/compress/decay/summarize/export/load | 完整基础实现 |
| same | MemoryBank-style | reinforcement、retention、summary/profile-oriented behavior | 部分，语义为仓库自有 style implementation |
| same | Zep-style | recall/compress/decay/summarize behavior | 部分，非外部 Zep 服务等价实现 |
| same | custom plugin | 由接口扩展 | 取决于实现和 bootstrap 注册 |

Dispatcher 只路由和持久化，不自行决定 lifecycle threshold。`SuggestedLifecycleState` 由 plugin 给出并原样应用。

### 14.4.6. Agent SDK Extension Contracts

| Interface | Methods | Core implementation | Actual caller | 状态 |
|---|---|---|---|---|
| `agent.MemoryManager` | `Name`, `Recall`, `Ingest`, `Compress`, `Summarize`, `Decay` | `BaselineMemoryManager` HTTP adapter | `AgentSession` and AgentGateway | active when legacy-named `Config.CogDBEndpoint` is configured |
| `agent.LLMProvider` | `Complete`, `Provider` | none in core | attached through `AgentSession.WithLLM`; no core generation workflow consumes it yet | contract-only |
| `agent.MASProvider` | `Peers`, `Topology` | none in core | attached through `AgentSession.WithMAS`; collaboration methods do not enforce it | contract-only |

`MemoryManager` 是 Agent SDK 到 internal memory routes 的 adapter contract，不等同于底层 `schemas.MemoryManagementAlgorithm` plugin interface。前者跨 HTTP 调用，后者在服务进程内由 algorithm dispatcher 执行。

### 14.4.7. Runtime and Transport

| Boundary | Concrete implementation | Notes |
|---|---|---|
| `worker.Runtime` | `CreateRuntime(...)` | 聚合 WAL、DataPlane、stores、policy、planner、materializer、evidence、NodeManager 和 consistency |
| `transport.RuntimeAPI` | `*worker.Runtime` | 只暴露 warm segment/native transport 方法，避免 import cycle |
| HTTP Gateway | `access.Gateway` | 主 Event/Query/canonical/admin/internal handlers |
| gRPC service | `api/grpc.Server` | 通过 Gateway/service adapter；覆盖面不等同 HTTP |
| `worker.Orchestrator` | concrete priority dispatcher | bootstrap 启动，但主 Gateway/Runtime 未提交任务，属于部分接线 |
| `coordinator.WorkerScheduler` | dispatch/active counter | 不是执行器，也未驱动 NodeManager 选择 |

`PlasmodAPIServiceClient`、`PlasmodAPIServiceServer` 和 stream client/server interfaces 由 protobuf 生成，只定义 wire contract。它们的存在不能证明 HTTP feature parity、scheduler 接线或 engine replacement point；服务实现与路由覆盖必须单独检查 `src/internal/api/grpc/server.go`。

### 14.4.8. Interface Change Checklist

1. 更新 interface 和所有 memory/Badger/S3/native adapter；
2. 更新 `app.BuildServer` 的 constructor 参数和注册顺序；
3. 检查 nil/not-found、context、并发、关闭和错误分类语义；
4. 为替换实现增加 contract test，而不仅是 concrete test；
5. 更新本注册表、Engine 页面和 API/Chain 映射。

---

## 14.5. Object and Message Registry

本页集中定义 Engine 之间传递的对象和字段。各 Engine 页面不重复复制完整 schema，而是引用本注册表并说明本 Engine 实际读写的字段。

### 14.5.1. Dynamic Event v0.4

代码：`src/internal/schemas/dynamic_event.go`, `src/internal/schemas/canonical.go`。

| Group | 字段 | 主要消费者 |
|---|---|---|
| `schema_version` | `schema_version` | normalize/validation |
| `identity` | `trace_id`, `event_id`, `tenant_id`, `workspace_id`, `source`, `dataset`, `import_batch_id`, `ingest_mode`, `file_name`, `replay_order` | WAL、scope、dataset 管理、replay |
| `actor` | `session_id`, `agent_id`, `role_profile`, `team_id`, `parent_agent_id`, `agent_generation`, `agent_type` | materialization、governance、collaboration |
| `time` | `event_time`, `logical_ts`, `wal_lsn`, `ingest_time`, `visible_time` | WAL、version、consistency、time filter |
| `event` | `event_type`, `event_subtype`, `action`, `importance`, `confidence` | materializer、worker routing、salience |
| `object` | `object_id`, `object_type`, `object_subtype`, `version`, `lifecycle_state`, `state_type`, `state_key`, `artifact_name`, `artifact_uri`, `uri`, `mime_type` | object derivation |
| `causality` | `parent_event_id`, `causal_refs`, `provenance_refs`, `call_event_id`, source/target object IDs, `edge_kind`, `edge_weight`, `reason`, `hooks` | Edge、Artifact、proof/provenance |
| `access` | `consistency`, `visibility`, visible agent/role lists, `ttl_ms`, `freshness_sla_ms`, `policy_tags`, `share_contract_id`, `hooks` | consistency、policy、scope |
| `materialization` | `enabled`, `targets`, `mode`, `planned_object_ids`, `status`, `materialized_at_ms`, `hooks` | projection metadata；当前不是通用硬 gate |
| `retrieval` | `index_text`, `has_embedding`, `embedding_dim`, `embedding_vector`, `embedding_ref`, `index_fields`, `retrieval_namespace`, `sparse_terms`, `hooks` | retrieval projection |
| `payload` | `map[string]any`；常用 `text/content/state_value/artifact` | materializer 和专用 worker |
| `data` | `payload_size_bytes`, `record_size_bytes`, `payload_hash`, `canonicalization`, `schema_name`, `schema_ref` | validation/metadata |
| `runtime` | created/write/materialized/visible/query 时间、三类 latency、write/materialization/visibility status | 运行状态和观测；部分值来自输入或 consistency 更新 |
| `extensions` | `custom`, `labels`, `hooks` | extension hooks/filters |

旧平铺字段是 `json:"-"` 兼容 alias，经过 `NormalizeDynamicEventV04` 汇入嵌套模型，不是 canonical 输出字段。

### 14.5.2. Canonical Objects

#### 14.5.2.1. Agent and Session

| Object | 字段 |
|---|---|
| `Agent` | `agent_id`, `tenant_id`, `workspace_id`, `agent_type`, `role_profile`, `policy_ref`, `capability_set`, `default_memory_policy`, `created_at`, `status` |
| `Session` | `session_id`, `agent_id`, `parent_session_id`, `task_type`, `goal`, `context_ref`, `start_ts`, `end_ts`, `status`, `budget_token`, `budget_time_ms` |

#### 14.5.2.2. Memory

| 字段组 | 字段 | 所有权 |
|---|---|---|
| Identity/type | `memory_id`, `memory_type`, `agent_id`, `session_id`, `owner_type`, `scope`, `level` | canonical Memory |
| Content | `content`, `summary` | canonical Memory |
| Provenance | `source_event_ids`, `provenance_ref` | canonical Memory；详细关系在 Edge/derivation log |
| Quality | `confidence`, `importance`, `freshness_score` | canonical Memory |
| Validity | `ttl`, `valid_from`, `valid_to`, `version`, `is_active`, `lifecycle_state` | canonical Memory |
| External references | `embedding_ref`, `algorithm_state_ref` | 指向 Embedding/MemoryAlgorithmState；当前 embedding object 持久化接线有限 |
| Governance | `policy_tags`, `scope` | Memory + PolicyRecord/ShareContract |
| Ingest lineage | `dataset_name`, `source_file_name`, `import_batch_id` | delete/query selectors |

#### 14.5.2.3. State, Artifact, Relation and Version

| Object | 字段 |
|---|---|
| `State`/`AgentState` | `state_id`, `agent_id`, `session_id`, `state_type`, `state_key`, `state_value`, `derived_from_event_id`, `checkpoint_ts`, `version` |
| `Artifact` | `artifact_id`, `session_id`, `owner_agent_id`, `artifact_type`, `uri`, `content_ref`, `mime_type`, `metadata`, `hash`, `produced_by_event_id`, `version` |
| `Edge` | `edge_id`, `src_object_id`, `src_type`, `edge_type`, `dst_object_id`, `dst_type`, `weight`, `provenance_ref`, `created_ts`, `properties`, `expires_at` |
| `ObjectVersion` | `object_id`, `object_type`, `version`, `mutation_event_id`, `valid_from`, `valid_to`, `snapshot_tag` |

#### 14.5.2.4. Governance and Retrieval Records

| Object | 字段 |
|---|---|
| `User` | `user_id`, `user_name`, `user_tenant_id`, `user_workspace_id`, `default_visibility` |
| `Embedding` | `vector_id`, `vector_context`, `original_text`, `embedding_type`, `dim`, `model_id`, `vector_ref`, `created_ts` |
| `Policy` | `policy_id`, `policy_version`, start/end time, publisher type/id, policy type |
| `PolicyRecord` | policy ID/version/context, target object/type, salience/TTL/decay/confidence, verified/quarantine/visibility, reason/source/event ID |
| `ShareContract` | `contract_id`, `scope`, read/write/derive ACL, TTL/consistency/merge/quarantine/audit policy |
| `RetrievalSegment` | segment/object/namespace/time bucket, embedding family, storage/index refs, row count, min/max TS, tier |
| `AuditRecord` | record/target/operation/actor/policy snapshot/decision/reason/time/downstream request |
| `MemoryAlgorithmState` | memory/algorithm ID, strength, recall time/count, retention, portrait state, summary refs, suggested lifecycle, update time |

### 14.5.3. Retrieval Plane Messages

| Type | 输入/输出字段 |
|---|---|
| `dataplane.IngestRecord` | object ID, text, namespace, attributes, event Unix TS, embedding family/dim/vector, skip-vector flag |
| `dataplane.SearchInput` | query text/vector, TopK, namespace, constraints, time range, growing/cold flags, object/memory types |
| `dataplane.SearchOutput` | object IDs, scanned/planned segments, tier, cold mode/IDs/candidate count/request/fallback |
| `semantic.QueryPlan` | normalized TopK/namespace/time/types/tier plus access/materialization/runtime/hook descriptors |

### 14.5.4. Query API Messages

| Type | 字段 |
|---|---|
| `QueryRequest` | query/scope/session/agent/tenant/workspace, TopK/time, object/target/memory/edge/relation filters, response mode, dataset lineage, access/policy/share/materialization/runtime/extension filters, query hooks, warm segment, cold flag, query vector |
| `QueryResponse` | object IDs, graph nodes/edges, provenance, versions, filters, proof trace, four chain trace slots, cache stats, retrieval summary, query status/hint |
| `GraphExpandRequest` | seeds/types, session/agent, hops/time/edges, node/edge limits, props/provenance/response mode |
| `GraphExpandResponse` | `EvidenceSubgraph`, applied filters |

`QueryResponse.Objects` 是 ID 列表，不是 hydrated object payload。Canonical hydration 当前用于类型判断、Node/Edge/Version/Provenance 组装；完整对象需通过 canonical API 或后续 adapter 读取。

### 14.5.5. Worker Typed I/O

代码：`src/internal/schemas/worker_params.go`。

| Worker | Input | Output | 关键副作用 |
|---|---|---|---|
| Ingest | `IngestInput{Event}` | `IngestOutput{Valid,Error}` | 无持久化 |
| Object materialization | `ObjectMaterializationInput{Event}` | object ID/type/materialized | object store/edge/version |
| State apply/checkpoint | Event 或 agent/session | state ID/version/checkpoint | state/version store |
| Tool trace | Event | artifact ID/traced | artifact/derivation |
| Memory extraction | event/agent/session/content | memory ID/extracted | memory/derivation |
| Consolidation | agent/session | produced IDs/count | derived Memory |
| Summarization | agent/session/max level | produced IDs/count | summary Memory |
| Reflection policy | object ID/type | policy applied | Memory/Policy/Audit/tier |
| Index build | object ID/type/namespace/text | segment/count | segment/index/dataplane |
| Graph relation | source/destination/type/weight | edge ID | Edge store |
| Subgraph expand | request + prefetched nodes/edges | graph response | 无持久化 |
| Conflict merge | two IDs/object type | winner/loser/resolved | Memory/Edge/Audit |
| Proof trace | object IDs/depth | proof steps/hops | 无持久化 |
| Microbatch | query ID/opaque payload | items/count | in-memory queue |
| Algorithm dispatch | operation, IDs, query/time/signals/scope | updated/produced/scored refs | Memory/algorithm state/audit |
| Communication | source/target agent/memory ID | shared memory ID | copied Memory |

### 14.5.6. ID and Version Invariants

| Record | 默认规则 |
|---|---|
| Memory | `mem_<event_id>` |
| Ingest checkpoint State | `state_<session_id>_<event_id>` |
| Keyed State worker | `state_<agent_id>_<state_key>` |
| Artifact | explicit `object.object_id` 优先，否则 deterministic default |
| Shared Memory | `shared_<memory_id>_to_<agent_id>` |
| Edge | implementation-specific deterministic source/type/destination composition |
| Version | 由 logical TS 或 state worker 当前版本递增；必须关联 mutation event |

这些规则直接影响 replay 幂等和存储兼容，修改时必须提供迁移与重放测试。

---

## 14.6. Feature Status

| Capability | Status | Evidence/qualification |
|---|---|---|
| Dynamic Event v0.4 ingest | Implemented | Gateway + schemas + Runtime/WAL |
| File/In-memory WAL | Implemented | eventbackbone + storage factory |
| Event/Memory/checkpoint State/可选 Artifact/Edge/Version materialization | Implemented | materialization service + canonical projection |
| AgentState materialization | Implemented | state worker; recovery requires version/replay checks |
| strict/bounded/eventual | Implemented | consistency controller |
| Canonical CRUD | Implemented | Gateway/coordinators; not full WAL semantics |
| Structured query/evidence | Implemented | semantic/dataplane/evidence |
| Hot/Warm tiers | Implemented | cache + canonical/retrieval stores |
| S3/MinIO Cold tier | Implemented | explicit archive/query/purge |
| Native HNSW | Implemented | build-dependent |
| IVF/DiskANN | Partial | compile feature/platform dependent |
| gRPC/transport | Partial | not full HTTP API parity |
| Python SDK | Implemented | core ingest/query/vector/admin helpers |
| Node SDK | Partial | legacy naming and limited methods |
| Admin key | Implemented | admin prefix only |
| End-user/IAM authentication | Not Confirmed | deployment gateway required |
| Multi-process shared Badger HA | Not Confirmed | default is single active runtime |
| Universal idempotency protocol | Not Confirmed | no general Idempotency-Key |

---

## 14.7. Implementation Maturity

### 14.7.1. Implemented

Registered in active composition root, has concrete backend/handler and tests for primary behavior.

### 14.7.2. Experimental

Code is usable for controlled integration, but payload/name/lifecycle may change. Most `/v1/internal/*` runtime bridges
belong here。

### 14.7.3. Partial

Only some backends/platforms/operations are complete. Example: gRPC parity, Node SDK, compile-dependent native indexes。

### 14.7.4. Not Confirmed

No current code evidence for a reliable contract. Documentation must not infer the capability from directory names or
upstream snapshots。

### 14.7.5. Deprecated

Still accepted for compatibility but new callers should not use. Examples include selected `ANDB_*` environment aliases and
legacy flat Event fields。

Status change requires code link, tests, migration impact and documentation review。

30 个设计项的逐项成熟度和可声明边界见 [System Design Reference](14-implementation-status-gaps-and-claim-boundaries.md) 与 [Claim and Test Boundary](14-implementation-status-gaps-and-claim-boundaries.md)。接口存在但未接线时必须标记为 `defined-not-wired` 或“占位”，不能归入 Implemented。

---

## 14.8. Known Limitations

1. 默认 runtime 是单进程组合；大体量 upstream controlplane 不是默认完整集群。
2. Badger data directory 不支持多个 Plasmod 进程直接共享写入。
3. Admin key 只保护 `/v1/admin/*`；数据/internal route 需要外部认证和网络隔离。
4. `/healthz` 不是完整 readiness。
5. 公共 API 没有统一 Idempotency-Key、cursor pagination 或 ETag/乐观锁。
6. Canonical CRUD 不自动获得完整 Event/WAL/replay 因果链。
7. Cold tier 是显式归档/查询，不是所有写入自动复制。
8. IVF、DiskANN、GPU/TensorRT 依赖构建和平台。
9. gRPC/Node SDK 与 HTTP/Python SDK 不完全等价。
10. State worker 的部分 version tracking 在进程内，恢复需依赖持久化版本和 replay 校验。
11. `v1` 与 Dynamic Event v0.4 仍需要正式 release compatibility policy。
12. 某些 YAML 文件存在但未由 active startup 读取。

---

## 14.9. Release Notes

仓库当前没有在本文件中维护历史 release 日志。本页定义后续格式，避免从 commit message 猜兼容影响。

每个 release 应记录：

- version/date/commit；
- Added、Changed、Fixed、Deprecated、Removed；
- API/schema/storage/config changes；
- native dependency/index compatibility；
- migration/rollback steps；
- security changes；
- verified platforms and commands；
- known limitations。

Release note 只描述核心代码和部署行为，不混入外部测量结果。
