# 14. Implementation Status, Gaps, and Claim Boundaries

> Language: [中文](../14-implementation-status-gaps-and-claim-boundaries.md) | English

---

This chapter is the factual index for the documentation set. It connects public claims to active code paths, interfaces, state transitions, tests, known gaps, and the maturity labels used throughout the other chapters.

---

## 14.1. API to Engine Matrix

### 14.1.1. Public/Application Routes

| Method/route                   | Handler/Runtime entry                                           | Chain         | Primary Engines                                                                    | Key boundaries                                             |
| ------------------------------ | --------------------------------------------------------------- | ------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------ |
| `POST /v1/ingest/events`       | `handleIngest` -> `SubmitIngestContext`                         | Ingest        | Execution Coordination, Object Derivation, Canonical Graph, Retrieval, Consistency | Full WAL/canonical/visibility entry                   |
| `POST /v1/ingest/vectors`      | `handleIngestVectors` -> warm ingest methods                    | none          | Adaptive Retrieval                                                                 | Writes retrieval segments only; no Event or canonical semantics |
| `POST /v1/ingest/document`     | document assembler -> repeated `SubmitIngest`                   | Ingest        | Object Derivation, Retrieval                                                       | Internal document adapter; not as stable as the Event API |
| `POST /v1/query`               | `handleQuery` -> `ServiceQueryContext` -> `ExecuteQueryContext` | Query         | Retrieval, Evidence, Canonical Graph, Governance                                   | Primary structured evidence endpoint |
| `POST /v1/query/batch`         | `ServiceQueryBatch`                                             | none          | Adaptive Retrieval                                                                 | Accepts `VectorWarmBatchQueryRequest`, not a QueryRequest array. |
| `GET/POST /v1/agents`          | canonical handler/ObjectCoordinator                             | none          | Canonical Graph                                                                    | POST writes a version snapshot; bypasses Event/WAL/retrieval/access gates |
| `GET/POST /v1/sessions`        | canonical handler/ObjectCoordinator                             | none          | Canonical Graph                                                                    | Same boundary as direct Agent POST |
| `GET/POST /v1/memory`          | canonical handler/ObjectCoordinator                             | none          | Canonical Graph                                                                    | POST writes a version snapshot but no WAL or retrieval projection |
| `GET/POST /v1/states`          | canonical handler/ObjectCoordinator                             | none          | Canonical Graph                                                                    | POST writes a version snapshot without Event replay or the monotonic State guard |
| `GET/POST /v1/artifacts`       | canonical handler/ObjectCoordinator                             | none          | Canonical Graph                                                                    | POST writes a version snapshot but no WAL or retrieval projection |
| `GET/POST /v1/edges`           | canonical handler                                               | none          | Canonical Graph                                                                    | Does not enforce full referential or semantic integrity at both endpoints |
| `GET/POST /v1/policies`        | canonical handler                                               | none          | Governance, Canonical Graph                                                        | Stores policy records; not a general policy language |
| `GET/POST /v1/share-contracts` | canonical handler                                               | none          | Governance, Canonical Graph                                                        | Stores contracts; query and referenced share enforce them, other writes remain partial |
| `GET /v1/traces/{id}`          | `handleTraces`                                                  | Query-like    | Evidence, Canonical Graph                                                          | Assembles recorded evidence and provenance for an object |
| `GET /v1/agent/list`           | internal agent listing                                          | Collaboration | Canonical Graph/Governance                                                         | Experimental internal route |

### 14.1.2. Internal Runtime Routes

| Route group | Runtime entry | Engine | State mutation |
|---|---|---|---|
| `/v1/internal/memory/recall` | `DispatchRecall` | Memory Evolution + Governance | Recall result; reinforcement may update algorithm state depending on the plugin |
| `/v1/internal/memory/ingest` | `DispatchAlgorithm("ingest")` | Memory Evolution | algorithm state, Memory ref, audit |
| `/v1/internal/memory/compress` | algorithm dispatch | Memory Evolution + Canonical Graph | derived Memory, state/audit |
| `/v1/internal/memory/summarize` | algorithm dispatch | Memory Evolution + Canonical Graph | summary Memory, audit |
| `/v1/internal/memory/decay` | algorithm dispatch | Memory Evolution | lifecycle/algorithm state/audit |
| `/v1/internal/memory/share` | `DispatchShareWithContract`/`DispatchShare` | Collaboration + Governance + Ingest | validates owner/contract, then writes a derived Event/WAL, shared Memory, Version, Edge, and retrieval projection |
| `/v1/internal/memory/conflict/resolve` | `DispatchConflictResolve` | Collaboration | winner/loser mutation, conflict edge/audit |
| `/v1/internal/memory/stale` | handler direct store mutation | Memory Evolution | `stale`, inactive, audit/metric path |
| `/v1/internal/memory/conflict/inject` | handler direct canonical setup | Collaboration | controlled conflict records; internal only |
| `/v1/internal/task/start\|complete\|tokens\|claim\|stage` | metric/state handlers | Execution Coordination | session metrics/canonical events depending handler |
| `/v1/internal/plan/step\|repair` | plan handlers | Execution Coordination/Evidence | plan state/metrics |
| `/v1/internal/mas/answer-consistency\|aggregate` | MAS handlers | Collaboration | metrics/aggregate response |
| `/v1/internal/tool-state` | tool state handler -> ingest/query | Ingest + Query | State/Event/projection |
| `/v1/internal/agent/handoff` | handoff handler -> ingest/share | Collaboration + Ingest | Event/shared object |
| `/v1/internal/session/context` | session context query | Query/Evidence | read only |
| `/v1/internal/warm-segment/register` | Runtime register | Adaptive Retrieval | in-memory/native segment mapping |
| `/v1/internal/eval/ground-truth` | internal handler | outside stable core contract | Must not be presented as a stable core capability |

Internal routes are not automatically protected by admin middleware, and deployment layers must restrict network access.

### 14.1.3. Management Routes

| Route | Primary Engine/Manager | Behaviour |
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
| `/v1/admin/rollback` | Version/Storage | Partial implementation; not full system point-in-time rollback |
| `/v1/admin/consistency-mode` | Consistency Controller | inspect/change default mode |
| `/v1/admin/replay` | Consistency/Runtime | preview or apply WAL replay |
| `/v1/admin/metrics` | metrics collector | in-process counters/histograms/status |
| `/v1/admin/governance-mode`, `/runtime-mode` | Runtime/Governance | toggle in-memory runtime flags |
| `/v1/admin/memory/providers/mode\|health` | Memory Evolution | provider router mode/health |

### 14.1.4. Internal Transport Routes

| Route | Contract | Engine | Evidence/canonical semantics |
|---|---|---|---|
| `POST /v1/internal/rpc/ingest_batch` | binary vector batch | Adaptive Retrieval | None |
| `POST /v1/internal/rpc/unload_segment` | segment ID | Adaptive Retrieval | None |
| `POST /v1/internal/rpc/query_warm` | binary query vector | Adaptive Retrieval | None |
| `POST /v1/internal/rpc/query_warm_batch` | binary NQ batch | Adaptive Retrieval | None |
| `POST /v1/internal/rpc/query_warm_serial_batch` | serial reference | Adaptive Retrieval | None |
| `POST /v1/internal/rpc/query_warm_batch_raw` | raw native path | Adaptive Retrieval | None |
| `POST /v1/internal/rpc/register_warm` | JSON mapping | Adaptive Retrieval | None |
| `GET /v1/wal/stream` | SSE | Event backbone | WAL observation only |

### 14.1.5. API Design Rules

1. Use Event ingest when you need durability, replay, canonical object and visibility guarantee;
2. direct canonical POST is a management/migration interface and should not be described as an equivalent Ingest Chain;
3. native warm routes only return ANN candidates and cannot claim to return evidence-bearing response;
4. the request structure and stability of the internal route are determined by the current commit;
5. Every new route must document its Chain, Engine, transaction boundary, and any bypassed canonical or WAL semantics.

---

## 14.2. Claim and Test Boundary

This section binds every supported core-system claim to current code entry points and test evidence.

### 14.2.1. Supported Claims

| Core claim | Code path | Primary tests | Boundary |
|---|---|---|---|
| Event-first durable ingest with replayable LSN | Gateway -> Runtime -> consistency -> WAL | `eventbackbone/*_test.go`, `worker/consistency/*_test.go`, runtime tests | File-backed WAL survives restart in disk mode |
| Event derives canonical Memory/State/Artifact/Edge/Version | materialization service + specialized workers | materialization and worker materialization tests | `materialization.targets/enabled` is not a universal dispatch gate |
| Canonical truth is separated from retrieval projection | storage contracts + DataPlane | storage projection tests, dataplane tests | No cross-system ACID transaction spans both planes |
| strict/bounded/eventual visibility modes | consistency controller/tracker/checkpoint | consistency mode/controller/tracker tests | Guarantees apply within the active single-process controller |
| hybrid/tiered retrieval with optional native ANN | TieredDataPlane + vector/sparse/segment/native bridge | dataplane/retrievalplane tests | Native ANN depends on build tags/libraries; Cold is queried explicitly |
| evidence-bearing, access-filtered query response | planner -> retrieval -> canonical access gate -> assembler -> graph endpoint filter | evidence, semantic, runtime access/e2e query tests | Response objects are IDs; caller identity still requires external authentication binding |
| pluggable memory management algorithm | interface + dispatcher + baseline/MemoryBank-style/Zep-style | cognitive tests | “Style” plugins are repository implementations, not external product services |
| canonical graph and provenance records | Edge/Version/derivation stores + proof worker | schema graph, evidence, coordination tests | graph validity is application-enforced, not referential constraint |
| contract-backed derived sharing | ShareContractStore + `DispatchShareWithContract` + Ingest Chain | semantic policy and runtime share/access tests | Share enforces derive/read; raw CRUD, lifecycle, and conflict writes are not uniform |
| hot/warm/cold object management | TieredObjectStore + S3/InMemory cold | storage tiered/S3 tests | promotion/demotion mostly explicit/policy worker driven |

### 14.2.2. Claims Requiring Qualification

| Phrase | Required qualification |
|---|---|
| Four chains are scheduled through one router | Not established. Four chain abstractions exist, but the main Gateway path does not pass through the Orchestrator |
| Intelligent resource-aware scheduling | Not established. Current components provide consistency queues, a fixed-priority Orchestrator, and a WorkerScheduler that tracks counters |
| Unified reconciliation manager | Not established; only decentralized replay, reindex, purge, and checkpoint capabilities exist. |
| ACL is enforced on every path | Policy/share schemas, filtering, and detection exist, but enforcement is not uniform across all paths |
| Event targets configure arbitrary object derivation | Current materialization uses explicit deterministic rules; targets are not a general rule engine |
| Complete durable evidence reconstruction | Edge, Version, and derivation data are durable; evidence cache and proof responses are rebuildable but not durable themselves |
| One transaction spans all storage layers | Badger canonical projection can be atomic; native indexes, S3, and caches remain outside that transaction |
| "distributed multi-tenant scheduler" | Active core is a single process worker/node manager architecture. |

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

Before a new system declaration is made, the following must be provided:

1. The active bootstrap constructs the component, or an active entry point calls it.
2. The interface, type, and method path are identified.
3. State mutations and failure boundaries are documented.
4. A primary test covers the claimed behavior.
5. This registry and the corresponding Architecture, Chain, Mechanism, or Engine section are updated together.

---

## 14.3. Execution State and Failure Matrix

### 14.3.1. Active Write Stages

There is no single repository-wide `WriteState` enum. The following vocabulary maps several runtime and tracker states into one documentation model; it must not be described as one implemented state machine.

| Unified stage | Code event | Durable or in-memory location | ACK relationship |
|---|---|---|---|
| `RECEIVED` | Gateway decode and normalization | Request memory | No acknowledgement yet |
| `WAL_COMMITTED` | `WAL.Append` returns an LSN | FileWAL or InMemoryWAL | All modes have accepted the Event |
| `PROJECTING` | tracker `MarkProjecting` | Tracker memory and checkpoint context | Strict waits; weaker modes may already have returned |
| `PERSISTED` | `ApplyCanonicalProjection` succeeds | Object, edge, and version stores | Required by the current strict projection path |
| `INDEXED` | `DataPlane.Ingest` succeeds | Warm retrieval structures/native index | Required by the current strict projection path |
| `EVIDENCE_READY` | The precompute cache fragment is written | in-memory evidence cache | Not a visibility gate; may be skipped |
| `VISIBLE` | tracker `MarkVisible` plus checkpoint/watermark | Tracker and checkpoint | Strict returns after this; weaker modes may have returned earlier |
| `MAINTAINED` | subscriber/reflection/consolidation/flush | multiple stores/cache/index | No |

The active `projectWALEntry` order is canonical projection, retrieval ingest, then hot promotion, conflict/precompute work, and worker dispatch. Canonical-first ordering preserves recoverable facts. If retrieval fails, the controller does not advance the visible watermark and query hides the incomplete mutation using `MutationLSN` and `ReadWatermarkLSN`. Retrieval and canonical persistence remain separate transactions.

### 14.3.2. Consistency Modes

| Mode | Submit response | Read gate | Guarantee |
|---|---|---|---|
| strict/strict_visible | Projection + visible; failure returns accepted-not-visible | waits through accepted prefix | request scoped read-after-write |
| bounded/bounded_staleness | WAL after pending ACK;sharded queue | waits within configured lag/through current accepted prefix | bounded lag, SLA breach observed |
| eventual/eventual_visibility | WAL pending after ACK | minimal ordering gate | eventual projection |

### 14.3.3. Sync/Async Boundary

| Operation | Synchronous section | Asynchronous parts |
|---|---|---|
| Event ingest | validation, WAL, strict projection wait | bounded/eventual projection, subscriber maintenance, periodic flush |
| Canonical projection | Event/Memory/stable State/optional Artifact/edge/version transaction followed by retrieval ingest | tool trace, consolidation, reflection, and specialized State checkpoints through the subscriber |
| Query | planner, retrieval, filters, assembler, QueryChain | no durable background completion promised |
| Memory algorithm API | selected plugin dispatch and store writes | provider shadow path may be external; no generic job tracker |
| Collaboration | conflict/share call | microbatch queue only processes when explicitly flushed |
| Archive/purge | request validation and task enqueue/start | hard delete/export stages where handler creates task |

### 14.3.4. Failure Windows

| Window | Possible state | Detection | Current recovery |
|---|---|---|---|
| WAL append fails | no accepted write | returned error | caller retry with same event ID |
| WAL succeeds, projection fails | durable Event, object not visible | tracker failure/status, 503 for strict | bounded retry; restart recovery scans WAL; admin replay |
| Canonical commit succeeds, retrieval ingest fails | authoritative object exists but its LSN is not visible | projection error/tracker retry; query watermark gate | retry the same WAL LSN or reindex; no automatic cross-plane rollback |
| Canonical commit succeeds, precompute fails/skips | object visible without cached fragment | cache miss stats | query-time delta evidence |
| Canonical succeeds, subscriber worker fails | main object visible, auxiliary state/audit/index may lag | logs/ErrorCh/in-memory DLQ/overflow; limited per-worker status | WAL subscriber re-scan/restart, manual replay; no durable dead-letter queue |
| Checkpoint save fails | object may be visible but durable progress uncertain | `checkpointVisibilityError`, buffered checkpoint last error | retry/flush, restart WAL scan |
| Cold archive partially fails | warm remains authoritative if deletion order respected | admin response/diagnostics | retry export; no global transaction |
| Hard purge partially fails | stores diverge | purge task stage/error | resume/retry task/manual cleanup |

### 14.3.5. Idempotency and Ordering

| Area | Current mechanism | Limitation |
|---|---|---|
| Event replay | deterministic IDs from event ID | direct workers with alternate IDs must preserve same rule |
| WAL ordering | monotonic LSN and serialized append section | global distributed ordering is not implemented by active single-process core |
| Canonical projection | same-backend transaction and upsert | retrieval/S3/cache are outside transaction |
| State version | Runtime/worker read store history, deduplicate mutation event, increment, and snapshot | single-process mutex; cross-process conditional-write coordination absent |
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
| Subscriber | WAL polling, context cancellation, retry/overflow and in-memory DLQ; no persistent dead-letter queue |

### 14.3.7. Recovery Capability Matrix

| Capability | Implementation | Maturity |
|---|---|---|
| startup WAL recovery | consistency controller `recoverFromWAL` from checkpoint | Active primary path |
| admin replay preview/apply | Runtime WAL scan and resubmit | Active basic path |
| canonical re-materialization | replay through deterministic materialization | Partial; bounded by worker idempotency coverage |
| embedding/retrieval reindex | `Runtime.ReindexEmbeddings`, warm prebuild | Dedicated operations implemented |
| relation repair | replay/build edges or manual operations | Partial; no independent scanner/planner |
| evidence rebuild | query-time assembly plus ingest precompute | Partial; no complete durable evidence rebuild manager |
| divergence scanner | no unified active component | Planned |
| dead-letter handler | no active durable queue | Planned |
| post-repair verification | tests/admin summaries, no unified verifier | Planned |

### 14.3.8. Correctness Review Rule

Any new phase must indicate who holds the status, whether it was completed before ACK, whether the checkpoint failed to advance, whether it can be re-entered, how to observe, how to fix it, and whether it is a visibility guarantee.

---

## 14.4. Interface Implementation Registry

This registry lists explicit, replaceable interfaces in the active Plasmod core. Generated protobuf/gRPC interfaces are protocol products. Internal upstream interfaces under `platformpkg`, `coordinator/controlplane`, `eventbackbone/streamplane`, and `cpp/vendor` are not automatically Runtime extension points.

Status meanings: **active** means reachable from the composition root or a primary call path; **partial** means only part of the declared capability is implemented; **defined-not-wired** means an implementation and tests exist but the active bootstrap path does not call it; **contract-only** means the interface has no production implementation in the core runtime.

### 14.4.1. Event and Consistency

| Interface | Methods | Active implementation | Constructor/selection | Status |
|---|---|---|---|---|
| `eventbackbone.WAL` | `Append`, `Scan`, `LatestLSN` | `InMemoryWAL`, `FileWAL` | `app.BuildServer`; disk/WAL persistence selects FileWAL | Complete |
| `eventbackbone.ErrorAwareWAL` | `ScanWithError` | `FileWAL` | type assertion during replay/recovery | Complete |
| `eventbackbone.Bus` | publish/subscribe | `InMemoryBus` | `NewInMemoryBus` | Partial: process-local only |
| `eventbackbone.WatermarkReader` | `Current` | `WatermarkPublisher` | bootstrap creates shared watermark | active |
| `eventbackbone.DerivationLogger` | `Append`, `ForDerived`, `Since` | `DerivationLog` | `NewDerivationLog`/`NewDerivationLogWithStore` | active; file append store option |
| `eventbackbone.PolicyDecisionLogger` | `Append`, `ForObject`, `Since` | `PolicyDecisionLog` | `NewPolicyDecisionLog` | Active; process-local log |
| `eventbackbone.DerivationStore` | `Append`, `Load` | `FileDerivationStore` | optional constructor injection | partial; only file implementation |
| `WatermarkAdvancer` | `AdvanceTo` | `WatermarkPublisher` adapter | consistency configure | Complete |
| `consistency.CheckpointStore` | `Load`, `Save`, `Reset` | `MemoryCheckpoint`, `FileCheckpoint`, `BufferedCheckpoint` decorator | `ConfigFromEnv`/Runtime configure | Complete |

### 14.4.2. Canonical Storage

| Interface | Principal methods | Active implementation | Selection | Replacement requirements |
|---|---|---|---|---|
| `SegmentStore` | upsert/list/delete by reference | memory, Badger | storage factory | Preserve namespace and reference deletion semantics |
| `IndexStore` | upsert/list | memory, Badger | storage factory | Preserve accumulated index metadata |
| `ObjectStore` | Agent/Session/Event/Memory/State/Artifact/User CRUD | memory, Badger | storage factory | not-found returns `(zero,false)`; concurrency-safe |
| `GraphEdgeStore` | put/get/delete/from/to/bulk/list/prune | memory, Badger | storage factory | Maintain source and destination secondary indexes |
| unexported `warmEdgeBulkDeleter` | `DeleteEdgesByObjectID` | memory and Badger graph edge stores | `PurgeMemoryWarmOnlyWithStats` type assertion | optional fast-path; residual verification is still required |
| `SnapshotVersionStore` | put/history/latest | memory, Badger | storage factory | Preserve latest-version ordering and history semantics |
| `PolicyStore` | append/get/list | memory, Badger | storage factory | append-only |
| `ShareContractStore` | put/get/by scope/list | memory, Badger | storage factory | Preserve scope-query semantics |
| `AuditStore` | append/get/list/delete target | memory, Badger/composite | storage factory | Append-only by default; audited exceptions are needed for hard deletion |
| `MemoryAlgorithmStateStore` | put/get/list | memory, Badger/composite | storage factory | Preserve the `(memory_id, algorithm_id)` composite identity |
| `RuntimeStorage` | store accessors + `ApplyCanonicalProjection` | `MemoryRuntimeStorage`, composite Badger runtime | `BuildRuntimeFromEnv` | Keep object/edge/version on one backend for atomic projection |
| `ColdTierDiagnosticsProvider` | `ColdTierDiagnostics` | none | no active caller | Contract-only; search output currently carries diagnostics directly |
| `MemoryEmbedder` | `Generate(text)` | dataplane/embedding generators by structural typing | tiered object constructor | minimal local adapter |
| `ColdHNSWSearcher` | `ColdHNSWSearch` | cold implementations that opt in | type assertion | optional capability |
| `ColdObjectStore` | memory/agent/state/artifact/edge/embedding CRUD + lexical/vector search | `InMemoryColdStore`, `S3ColdStore` | S3 selected when required environment is complete | Active; object ID, embedding family, and diagnostics must remain stable |

### 14.4.3. Semantic, Retrieval and Evidence

| Interface | Methods | Implementation | Bootstrap | Status |
|---|---|---|---|---|
| `semantic.QueryPlanner` | `Build(QueryRequest)` | `DefaultQueryPlanner` | `NewDefaultQueryPlanner` | Active basic planning; advanced descriptors do not all alter execution |
| `dataplane.DataPlane` | `Ingest`, `Search`, `Flush` | `SegmentDataPlane`, active `TieredDataPlane` | `NewTieredDataPlaneWithEmbedderAndConfig` | Active primary path |
| `dataplane.EmbeddingGenerator` | `Generate`, `Dim`, `Reset` | `dataplane.TfidfEmbedder` and provider adapters | dataplane/embedder bootstrap | active |
| `dataplane.BatchEmbeddingGenerator` | single + `BatchGenerate` | TF-IDF wrapper and batch-capable providers | `VectorStore.AddTexts` type assertion | optional extension |
| `embedding.Generator` | embedding + `Close`, `Provider` | TF-IDF, OpenAI-compatible, Cohere, HuggingFace, Vertex AI, ONNX, TensorRT, GGUF adapters | `PLASMOD_EMBEDDER` | Backend availability depends on build and configuration. |
| Native retrieval bridge | create/ingest/search/release index | CGO bridge; non-retrieval stub | build tag/link flags | Available only when the native library is built and linked |
| `retrievalplane.RuntimeContract` | search/storage/compaction descriptors | imported layout contract | not used by active Runtime | Compatibility/positioning only |
| `retrievalplane.SearchService` | `QueryPath`, `SupportsSegmentPlanning` | none | only returned by `RuntimeContract` | contract-only; describes imported query layout, not running current queries |
| `retrievalplane.StorageService` | `ObjectStorePath`, `SharedStoragePath` | none | only returned by `RuntimeContract` | contract-only; describe imported storage layout |
| `retrievalplane.CompactionService` | `CompactionPath` | none | only returned by `RuntimeContract` | contract-only; describe imported compaction layout |
| `schemas.GraphExpander` | `Expand(GraphExpandRequest)` | none | none | Contract-only; the active worker uses a separate multi-argument method |
| Evidence assembly | `Assembler.Build` concrete API | cached or uncached Assembler | `NewCachedAssembler(...).With...` | Complete basic evidence |
| Evidence cache | get/put/get-many/stats | bounded in-memory `evidence.Cache` | bootstrap cache-size configuration | Partial: not persistent |

### 14.4.4. Worker Interfaces and Implementations

| Interface | Exact domain methods | Active implementation | Constructor | Direct caller |
|---|---|---|---|---|
| `DataNode` | `Info`; `HandleIngest(IngestRecord)` | `InMemoryDataNode` | `CreateInMemoryDataNode` | NodeManager |
| `IndexNode` | `Info`; `BuildIndex(IngestRecord)` | `InMemoryIndexNode` | `CreateInMemoryIndexNode` | NodeManager |
| `QueryNode` | `Info`; `Search(SearchInput) SearchOutput` | `InMemoryQueryNode` | `CreateInMemoryQueryNode` | Runtime via NodeManager |
| nodes `IngestWorker` | `Info`; `Process(Event) error` | `InMemoryIngestWorker` | `CreateInMemoryIngestWorker` | MainChain/Manager; main runtime is normalize |
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
| `MicroBatchScheduler` | `Info`; `Enqueue(queryID,payload)`; `Flush() []any` | `InMemoryMicroBatchScheduler` | `CreateInMemoryMicroBatchScheduler` | Collaboration/Manager; only explicit flush |
| `ProofTraceWorker` | `Info`; `AssembleTrace(ids,maxDepth)` | `InMemoryProofTraceWorker` | `CreateInMemoryProofTraceWorker` | QueryChain |
| `SubgraphExecutorWorker` | `Info`; `Expand(req,nodes,edges)` | `InMemorySubgraphExecutorWorker` | `CreateInMemorySubgraphExecutorWorker` | QueryChain |
| `AlgorithmDispatchWorker` | `Info`; `Dispatch(operation,ids,query,nowTS,agentID,sessionID,signals)` | `InMemoryAlgorithmDispatchWorker` | `CreateAlgorithmDispatchWorker` | Runtime internal API |
| `Runnable` | `Run(WorkerInput) (WorkerOutput,error)` | Typed adapter for most concrete workers | concrete worker constructor | Currently not a unified master scheduling entry |

There is also a `worker.IngestWorker.Accept(Event)` contract with a `PipelineIngestWorker` implementation. It packages WAL, materialization, canonical persistence, precomputation, node fan-out, and DataPlane ingest and has unit tests. However, `app.BuildServer` and the active Runtime path neither construct nor call it. Its status is **defined-not-wired**, so it must not be described as the authoritative ingest chain.

`schemas.WorkerInput` and `schemas.WorkerOutput` are typed markers for worker messages. Concrete `*Input` and `*Output` types in `src/internal/schemas/worker_params.go` constrain `Runnable.Run` payloads, but no unified scheduler forces every worker invocation through this envelope.

For most dispatches, `nodes.Manager` selects the first registered worker. It does not implement dynamic load balancing, health-aware selection, or resource-aware routing.

### 14.4.5. Memory Algorithm Plugins

| Interface | Implementations | Available for operation | Status |
|---|---|---|---|
| `schemas.MemoryManagementAlgorithm` | baseline | ingest/update/recall/compress/decay/summarize/export/load | Active reference implementation |
| same | MemoryBank-style | reinforcement, retention, summary/profile-oriented behavior | Partial repository-owned style implementation |
| same | Zep-style | recall/compress/decay/summarize behavior | Partial repository-owned implementation; not the external Zep service |
| same | custom plugin | operations implemented by the plugin | Available only after implementation and bootstrap registration |

The dispatcher routes operations and persists results; it does not independently choose lifecycle thresholds. Plugins provide `SuggestedLifecycleState`, which the dispatcher applies.

### 14.4.6. Agent SDK Extension Contracts

| Interface | Methods | Core implementation | Actual caller | Status |
|---|---|---|---|---|
| `agent.MemoryManager` | `Name`, `Recall`, `Ingest`, `Compress`, `Summarize`, `Decay` | `BaselineMemoryManager` HTTP adapter | `AgentSession` and AgentGateway | active when legacy-named `Config.CogDBEndpoint` is configured |
| `agent.LLMProvider` | `Complete`, `Provider` | none in core | attached through `AgentSession.WithLLM`; no core generation workflow consumes it yet | contract-only |
| `agent.MASProvider` | `Peers`, `Topology` | none in core | attached through `AgentSession.WithMAS`; collaboration methods do not enforce it | contract-only |

`MemoryManager` is the adapter contract between Agent SDKs and the internal memory routes. It is distinct from the lower-level `schemas.MemoryManagementAlgorithm` plugin interface: the former calls the service over HTTP, while the algorithm dispatcher executes the latter inside the service process.

### 14.4.7. Runtime and Transport

| Boundary | Concrete implementation | Notes |
|---|---|---|
| `worker.Runtime` | `CreateRuntime(...)` | Aggregates WAL, DataPlane, stores, policies, planners, materializers, evidence, nodes, and consistency |
| `transport.RuntimeAPI` | `*worker.Runtime` | Exposes only warm-segment/native transport methods to avoid an import cycle |
| HTTP Gateway | `access.Gateway` | Primary Event, Query, canonical, admin, and internal handlers |
| gRPC service | `api/grpc.Server` | Uses Gateway/service adapters; coverage is not identical to HTTP |
| `worker.Orchestrator` | concrete priority dispatcher | Started by bootstrap, but primary Gateway/Runtime work is not submitted through it |
| `coordinator.WorkerScheduler` | dispatch/active counter | Counts dispatches; does not execute tasks or drive NodeManager selection |

Generated `PlasmodAPIServiceClient`, `PlasmodAPIServiceServer`, and stream interfaces define wire contracts only. Their existence does not prove HTTP feature parity, scheduler integration, or an engine replacement point; verify the service implementation and route coverage in `src/internal/api/grpc/server.go`.

### 14.4.8. Interface Change Checklist

1. Update the interface and every in-memory, Badger, S3, or native adapter.
2. Update constructor parameters and registration order in `app.BuildServer`.
3. Verify nil/not-found, context, concurrency, close, and error-classification semantics.
4. Extend replaceable contract tests, not only tests for one concrete implementation.
5. Update this registry, the relevant Engine section, and the API/Chain mapping.

---

## 14.5. Object and Message Registry

This registry summarizes objects and messages exchanged between Engines. Engine-specific sections reference it and identify the fields they actually read or mutate.

### 14.5.1. Dynamic Event v0.4

Code: `src/internal/schemas/dynamic_event.go`, `src/internal/schemas/canonical.go`.

| Group | Fields | Major consumers |
|---|---|---|
| `schema_version` | `schema_version` | normalize/validation |
| `identity` | `trace_id`, `event_id`, `tenant_id`, `workspace_id`, `source`, `dataset`, `import_batch_id`, `ingest_mode`, `file_name`, `replay_order` | WAL, scope, dataset, management, replay |
| `actor` | `session_id`, `agent_id`, `role_profile`, `team_id`, `parent_agent_id`, `agent_generation`, `agent_type` | materialization, governance, collaboration |
| `time` | `event_time`, `logical_ts`, `wal_lsn`, `ingest_time`, `visible_time` | WAL, version, consistency, time filter |
| `event` | `event_type`, `event_subtype`, `action`, `importance`, `confidence` | materializer, worker routing, salience |
| `object` | `object_id`, `object_type`, `object_subtype`, `version`, `lifecycle_state`, `state_type`, `state_key`, `artifact_name`, `artifact_uri`, `uri`, `mime_type` | object derivation |
| `causality` | `parent_event_id`, `causal_refs`, `provenance_refs`, `call_event_id`, source/target object IDs, `edge_kind`, `edge_weight`, `reason`, `hooks` | Edge, Artifact, proof/provenance |
| `access` | `consistency`, `visibility`, visible agent/role lists, `ttl_ms`, `freshness_sla_ms`, `policy_tags`, `share_contract_id`, `hooks` | consistency, policy, scope |
| `materialization` | `enabled`, `targets`, `mode`, `planned_object_ids`, `status`, `materialized_at_ms`, `hooks` | projection metadata; not a universal dispatch gate |
| `retrieval` | `index_text`, `has_embedding`, `embedding_dim`, `embedding_vector`, `embedding_ref`, `index_fields`, `retrieval_namespace`, `sparse_terms`, `hooks` | retrieval projection |
| `payload` | `map[string]any`; commonly `text`, `content`, `state_value`, or `artifact` | materializer and specialized workers |
| `data` | `payload_size_bytes`, `record_size_bytes`, `payload_hash`, `canonicalization`, `schema_name`, `schema_ref` | validation/metadata |
| `runtime` | receive/validation/WAL/queue/materialization/index/persistence/visibility/ack/error timestamps and state | runtime status and observability; some values originate in the request and others are updated by consistency processing |
| `extensions` | `custom`, `labels`, `hooks` | extension hooks/filters |

Legacy flat fields are `json:"-"` compatibility aliases. `NormalizeDynamicEventV04` folds them into the nested v0.4 model; they are not canonical output fields.

### 14.5.2. Canonical Objects

#### 14.5.2.1. Agent and Session

| Object | Fields |
|---|---|
| `Agent` | `agent_id`, `tenant_id`, `workspace_id`, `agent_type`, `role_profile`, `policy_ref`, `capability_set`, `default_memory_policy`, `created_at`, `status` |
| `Session` | `session_id`, `agent_id`, `parent_session_id`, `task_type`, `goal`, `context_ref`, `start_ts`, `end_ts`, `status`, `budget_token`, `budget_time_ms` |

#### 14.5.2.2. Memory

| Field groups | Fields | Ownership |
|---|---|---|
| Identity/type | `memory_id`, `memory_type`, `tenant_id`, `workspace_id`, `agent_id`, `session_id`, `owner_type`, `scope`, `level` | canonical Memory |
| Content | `content`, `summary` | canonical Memory |
| Provenance | `source_event_ids`, `provenance_ref` | canonical Memory; detailed relationships live in Edge and derivation records |
| Quality | `confidence`, `importance`, `freshness_score` | canonical Memory |
| Validity | `ttl`, `valid_from`, `valid_to`, `version`, `mutation_lsn`, `materialized_at`, `is_active`, `lifecycle_state` | canonical Memory |
| External references | `embedding_ref`, `algorithm_state_ref` | References Embedding and MemoryAlgorithmState records; persistence coverage depends on the active path |
| Governance | `CanonicalAccess{tenant/workspace/team/owner/session/visibility/agents/roles/tags/contract}`, `policy_tags`, `scope` | Memory + PolicyRecord/ShareContract |
| Ingest lineage | `dataset_name`, `source_file_name`, `import_batch_id` | delete/query selectors |

#### 14.5.2.3. State, Artifact, Relation and Version

| Object | Fields |
|---|---|
| `State`/`AgentState` | `state_id`, tenant/workspace/agent/session, type/key/value, derived event, checkpoint, version, `mutation_lsn`, `access` |
| `Artifact` | `artifact_id`, tenant/workspace/session/owner/type, URI/content/mime/metadata/hash, produced event, version, `mutation_lsn`, `materialized_at`, `access` |
| `Edge` | source/type/relation/destination/type/weight/provenance/time/properties/expiry, `mutation_lsn`, `access` |
| `ObjectVersion` | object/type/version, mutation event/LSN, valid from/to, snapshot tag, complete `snapshot`, `access` |

#### 14.5.2.4. Governance and Retrieval Records

| Object | Fields |
|---|---|
| `User` | `user_id`, `user_name`, `user_tenant_id`, `user_workspace_id`, `default_visibility` |
| `Embedding` | `vector_id`, `vector_context`, `original_text`, `embedding_type`, `dim`, `model_id`, `vector_ref`, `created_ts` |
| `Policy` | `policy_id`, `policy_version`, start/end time, publisher type/id, policy type |
| `PolicyRecord` | policy ID/version/context, target object/type, salience/TTL/decay/confidence, verified/quarantine/visibility, reason/source/event ID |
| `ShareContract` | contract/tenant/workspace/scope, legacy read/write/derive ACL, typed agent/role grants, TTL/consistency/merge/quarantine/audit policy |
| `RetrievalSegment` | segment/object/namespace/time bucket, embedding family, storage/index refs, row count, min/max TS, tier |
| `AuditRecord` | record/target/operation/actor/policy snapshot/decision/reason/time/downstream request |
| `MemoryAlgorithmState` | memory/algorithm ID, strength, recall time/count, retention, portrait state, summary refs, suggested lifecycle, update time |

### 14.5.3. Retrieval Plane Messages

| Type | Input and output fields |
|---|---|
| `dataplane.IngestRecord` | object ID, text, namespace, attributes, event Unix TS, embedding family/dim/vector, skip-vector flag |
| `dataplane.SearchInput` | query text/vector, TopK, namespace, constraints, time range, growing/cold flags, object/memory types |
| `dataplane.SearchOutput` | object IDs, scanned/planned segments, tier, cold mode/IDs/candidate count/request/fallback |
| `semantic.QueryPlan` | normalized TopK/namespace/time/types/tier plus access/materialization/runtime/hook descriptors |

### 14.5.4. Query API Messages

| Type | Fields |
|---|---|
| `QueryRequest` | query/scope, tenant/workspace/team/session/agent selectors, `requester_agent_id/requester_roles`, TopK/time, object/target/memory/edge/relation filters, response mode, dataset lineage, access/policy/share/materialization/runtime/extension filters, hooks, warm/cold/vector fields |
| `QueryResponse` | object IDs, graph nodes/edges, provenance, versions, filters, proof trace, `access_decisions`, `read_watermark_lsn`, four chain traces, cache/retrieval summary, status/hint |
| `GraphExpandRequest` | seeds/types, session/agent, hops/time/edges, node/edge limits, props/provenance/response mode |
| `GraphExpandResponse` | `EvidenceSubgraph`, applied filters |

`QueryResponse.Objects` contains object IDs. Query execution hydrates canonical records internally for type checks and Node, Edge, Version, and Provenance assembly; callers that need complete canonical payloads must use the canonical API or an adapter that performs that lookup.

### 14.5.5. Worker Typed I/O

Code: `src/internal/schemas/worker_params.go`.

| Worker | Input | Output | Key side effects |
|---|---|---|---|
| Ingest | `IngestInput{Event}` | `IngestOutput{Valid,Error}` | validation/normalization only in the nodes worker contract |
| Object materialization | `ObjectMaterializationInput{Event}` | object ID/type/materialized | object store/edge/version |
| State apply/checkpoint | Event or agent/session | state ID/version/checkpoint | state/version store |
| Tool trace | Event | artifact ID/traced | artifact/derivation |
| Memory extraction | event/agent/session/content | memory ID/extracted | memory/derivation |
| Consolidation | agent/session | produced IDs/count | derived Memory |
| Summarization | agent/session/max level | produced IDs/count | summary Memory |
| Reflection policy | object ID/type | policy applied | Memory/Policy/Audit/tier |
| Index build | object ID/type/namespace/text | segment/count | segment/index/dataplane |
| Graph relation | source/destination/type/weight | edge ID | Edge store |
| Subgraph expand | request + prefetched nodes/edges | graph response | read-only expansion |
| Conflict merge | two IDs/object type | winner/loser/resolved | Memory/Edge/Audit |
| Proof trace | object IDs/depth | proof steps/hops | read-only proof assembly |
| Microbatch | query ID/opaque payload | items/count | in-memory queue |
| Algorithm dispatch | operation, IDs, query/time/signals/scope | updated/produced/scored refs | Memory/algorithm state/audit |
| Communication | source/target agent/memory ID plus optional contract | shared memory ID | Runtime derives Event/WAL/Memory/Version/Edge; legacy worker copies scoped Memory |

### 14.5.6. ID and Version Invariants

| Record | Default rules |
|---|---|
| Memory | default `mem_<event_id>`; typed Memory may use an explicit object ID |
| State | `state_<24-hex sha256 prefix of tenant/workspace/agent/session/state_key>`; default key is `last_memory_id` |
| Artifact | explicit `object.object_id` takes precedence; otherwise use the deterministic default |
| Shared Memory | `shared_<memory_id>_to_<agent_id>` |
| Edge | implementation-specific deterministic source/type/destination composition |
| Version | Memory/Artifact preserve object versions; State increments from store history; all associate mutation event/LSN and store snapshots |

These rules affect replay and storage compatibility. Any change requires migration and replay tests.

---

## 14.6. Feature Status

| Capability | Status | Evidence/qualification |
|---|---|---|
| Dynamic Event v0.4 ingest | Implemented | Gateway + schemas + Runtime/WAL |
| File/In-memory WAL | Implemented | eventbackbone + storage factory |
| Memory, State, Artifact, Edge, and Version materialization | Implemented | materialization service + canonical projection |
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
belong here.

### 14.7.3. Partial

Only some backends/platforms/operations are complete. Example: gRPC parity, Node SDK, compile-dependent native indexes.

### 14.7.4. Not Confirmed

No current code evidence for a reliable contract. Documentation must not infer the capability from directory names or
upstream snapshots.

### 14.7.5. Deprecated

Still accepted for compatibility but new callers should not use. Examples include selected `ANDB_*` environment aliases and
legacy flat Event fields.

Status change requires code link, tests, migration impact and documentation review.

See [System Design Reference](14-implementation-status-gaps-and-claim-boundaries.md) and [Claim and Test Boundary](14-implementation-status-gaps-and-claim-boundaries.md) for the maturity and claim boundary of each of the 30 design items. An interface that exists but is not wired must be labeled `defined-not-wired` or Placeholder; it must not be classified as Implemented.

---

## 14.8. Known Limitations

1. The default runtime is a single-process composition; imported control-plane packages do not form a complete default cluster.
2. A Badger data directory cannot be shared directly by multiple Plasmod writer processes.
3. The admin key protects only `/v1/admin/*`; data and internal routes require external authentication and network isolation.
4. `/healthz` is a liveness check, not a complete readiness check.
5. The public API does not have a unified Idempotency-Key, cursor pagination or ETag/optimistic lock.
6. Canonical CRUD does not automatically enter the Event/WAL/replay causal path.
7. Cold tiering is explicit; not every write is automatically copied to cold storage.
8. IVF, DiskANN, GPU/TensorRT depend on the build and platform.
9. gRPC and the Node SDK do not have complete parity with HTTP and the Python SDK.
10. Some State-worker version tracking is process-local; recovery depends on durable versions and replay validation.
11. A formal compatibility policy across API `v1` and Dynamic Event v0.4 releases is still required.
12. Some YAML files exist but are not read by active startup.

---

## 14.9. Release Notes

The repository does not currently maintain a historical release log in this file. Future releases should use the following format so compatibility decisions are not inferred from commit messages alone.

Each release entry should include:

- version/date/commit;
- Added, Changed, Fixed, Deprecated, Removed;
- API/schema/storage/config changes;
- native dependency/index compatibility;
- migration/rollback steps;
- security changes;
- verified platforms and commands;
- known limitations.

Release notes in this repository describe core code and deployment behavior, not external experimental results.
