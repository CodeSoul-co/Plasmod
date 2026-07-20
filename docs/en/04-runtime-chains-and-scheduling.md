# 04. Runtime, Four Execution Chains, and Scheduling

> Language: [中文](../04-runtime-chains-and-scheduling.md) | English

---

This chapter reconstructs the actual runtime paths for Ingest, Memory, Query, and Collaboration. A Chain denotes a dynamic execution path, not a static collection of modules.

---

## 04.1. Dynamic System Runtime

### 04.1.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Architecture |
| Design goals | Real functional paths after external requests/internal events enter the system |
| Critical-path role | Yes |
| Current Maturity | Complete Event/Query main path; Unified Chain Router only partially connected |

### 04.1.2. Request Entry Points

| Boundary | Entry | Calls Runtime directly? |
|---|---|---|
| HTTP Event/Query | `Gateway.RegisterAPIRoutes` handlers | Yes |
| HTTP canonical CRUD | Gateway handlers | No, direct store/coordinator |
| Admin | Gateway management handlers | Runtime/store calls by operation |
| Internal transport | `transport.Server` | Calls the `RuntimeAPI` warm/native methods |
| gRPC | gRPC server -> Gateway/service adapter | Yes/Indirectly |
| WAL background | consistency recovery, EventSubscriber | Controller project callback / NodeManager |

### 04.1.3. Actual Runtime Path

#### 04.1.3.1. Event write

```text
HTTP trigger
-> Gateway.handleIngest
-> Runtime.SubmitIngestContext
-> consistency.Controller.Submit
-> WAL.Append + LSN
-> projection queue
-> Runtime.projectWALEntry
-> Materializer
-> DataPlane.Ingest
-> RuntimeStorage.ApplyCanonicalProjection
-> tracker/checkpoint/watermark visible
-> strict response or pending response
-> EventSubscriber auxiliary maintenance
```

#### 04.1.3.2. Query

```text
HTTP trigger
-> Gateway.ServiceQueryContext
-> Runtime.ExecuteQueryContext
-> consistency read gate
-> QueryPlanner.Build
-> NodeManager.DispatchQuery -> DataPlane.Search
-> structured/canonical filters and supplements
-> Evidence.Assembler.Build
-> QueryChain.Run (proof + subgraph)
-> provenance/metrics/visibility filtering
-> QueryResponse
```

#### 04.1.3.3. Direct canonical mutation

```text
HTTP canonical POST -> handler -> Object/Edge/Policy/Contract store
```

The path does not automatically execute WAL, projection, version, evidence precompute or replay semantics.

### 04.1.4. Chain Selection Reality

| Chain | Concrete object | How the main pathway enters |
|---|---|---|
| MainChain | `chain.MainChain` | Orchestrator task can be called; Event Runtime masterwriter does not call it |
| MemoryPipelineChain | concrete | Orchestrator task can be called; subscriber is usually dispatched directly by worker |
| QueryChain | Runtime holds | `executeQuery` Sync calls after retrieval/assembler |
| CollaborationChain | concrete | Orchestrator task can be called; internal API is usually runtime dispatch. |

There is therefore no central Chain Router for all requests. `Orchestrator.execute` contains a type switch, but Gateway and Runtime do not submit their primary requests through it.

### 04.1.5. Inputs, Outputs, and State Changes

| Path | Input | Output | Durable mutations |
|---|---|---|---|
| Event | Dynamic Event | ACK with LSN/status/event/memory/state/artifact IDs | WAL, retrieval projection, Event/Memory/checkpoint State, and Artifact/Edge/Version are optional. |
| Query | QueryRequest | QueryResponse | metrics/cache reads; usually no canonical mutation |
| Memory operation | AlgorithmDispatchInput-like body | AlgorithmDispatchOutput/MemoryView | Memory, algorithm state, audit depending operation |
| Collaboration | agent/memory/conflict IDs | shared/winner result | Memory/Edge; WAL/version/projection not unified |
| Admin recovery | LSN/range/action | summary/status | replay/reindex/reset/purge dependent |

### 04.1.6. Synchronous/Asynchronous Boundaries and Return Conditions

| Mode/path | Return condition |
|---|---|
| strict ingest | WAL + projection + tracker visible; support subscriber not inside gate |
| bounded/eventual ingest | WAL accepted and task enqueued |
| query | read gate + current retrieval/evidence build complete |
| Orchestrator task | `Submit` returns only whether the task was queued; no result future is returned |
| subscriber | background polling; panic goes to in-memory DLQ/overflow/error channel |

### 04.1.7. Failure Handling

- Context cancel/deadline is distributed in Gateway -> Runtime -> consistency read/write gate.
- Projection retries are performed by the Controller; exhausted strict-mode retries return accepted-not-visible.
- Several NodeManager dispatch helpers return no error and therefore cannot propagate worker failures to the caller.
- `DataPlane.Search` returns a value rather than an error. Backend failures are observed primarily through empty results, metrics, or logs.
- See partial windows and recovery [Execution State and Failure Matrix](14-implementation-status-gaps-and-claim-boundaries.md).

### 04.1.8. Claim Boundaries

Event and Query have a clear Runtime main chain, canonical state and projection merge in Runtime, and background maintenance is performed by subscriber/flush loops.

Do not claim that Gateway always calls Orchestrator first, or that all four Chains provide a uniform task-result, retry, and cancellation protocol for every stage.

### 04.1.9. Gaps

1. Define one explicit relationship between direct Runtime orchestration and the Orchestrator.
2. Add result and error propagation for auxiliary workers.
3. Add a consistent WAL, version, and projection path for collaboration mutations.
4. Add a typed error contract for retrieval failures.
5. Propagate one request/task trace ID across modules.

---

## 04.2. Scheduler Design

### 04.2.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Architecture |
| Design goals | Chain, worker, write visibility and how resources are routed/restricted/recovered |
| Critical-path role | The consistency scheduler is; the Orchestrator and WorkerScheduler are not the master request gate |
| Current Maturity | Partial |

### 04.2.2. Three Existing Scheduling Components

| Component | File/type | Real Responsibilities | Main-path reachability |
|---|---|---|---|
| Consistency scheduler | `worker/consistency.Controller` | write admission, mode gate, sharded queue, slot, retry, deadline, checkpoint | Event ingest key path |
| Chain Orchestrator | `worker.Orchestrator` | 4 priority queues + fixed worker pool + Chain type switch | bootstrap started but no main path submit caller |
| WorkerScheduler | `coordinator.WorkerScheduler` | dispatched/active counter | Registry and counters only; it does not execute tasks |
| NodeManager | `nodes.Manager` | registration + first-worker synchronous dispatch | Runtime/Chain is a real call |
| Microbatch | `InMemoryMicroBatchScheduler` | opaque FIFO buffer + explicit flush | Collaboration enqueue; timeless drain |

### 04.2.3. Tasks and Classification

| `worker.Task` field | Meaning |
|---|---|
| `ID` | caller-provided task identity |
| `Type` | ingest/memory/query/collaboration |
| `Priority` | 0 low, 1 normal, 2 high, 3 urgent |
| `Payload` | chain-specific `any` |
| `Submitted` | queue timestamp |

Orchestrator has no dependency, deadline, tenant, cost, resource request, retry count, cancellation token or result channel fields.

### 04.2.4. Routing and Ordering

| Feature | Current behavior |
|---|---|
| Chain selection | Orchestrator `TaskType` Switch; main Runtime self-routing function |
| Priority | urgent -> high -> normal -> low; non-preemptive |
| Dependency | Non-uniform DAG; function call sequence implied expression |
| Worker selection | NodeManager takes the first implementation of the registry list |
| Load balancing | No active resource-aware selection |
| Fairness/tenant isolation | None |
| SLA/deadline | consistency bounded lag + request context; Orchestrator has no task deadline |
| Rate limit | Consistency slots/queue + Gateway write semaphore; no tenant token bucket |
| Microbatch | Buffer/explicit flush; not a general query batching scheduler |

### 04.2.5. Backpressure, Cancellation and Recovery

| Component | Backpressure | Cancellation | Failure/retry |
|---|---|---|---|
| Controller | bounded slots/queues, blocks admission | request/root/admission context | configured projection/checkpoint retry |
| Orchestrator | queue send blocks up to 30 s then returns false | worker exits on context; queued task without individual cancellation | no task retry/result |
| NodeManager | synchronous call | Caller context is not available in most worker interfaces | many errors discarded by dispatch helper |
| Microbatch | unbounded until caller flush relative to batchSize behavior | none | none |

### 04.2.6. Metrics and State

| Metrics | Source | Limitation |
|---|---|---|
| Controller status | queue/active/tracker/checkpoint/mode | write-specific |
| Orchestrator stats | submitted/completed/dropped/in-flight/depth | Limited representation when the task is not submitted from the main path |
| WorkerScheduler stats | dispatched/active by worker type | Callers must invoke `Dispatch` and `Complete`; the counters do not represent actual NodeManager activity automatically |
| global metrics | query/write latency, visibility, counters | in-process, not scheduler feedback loop |

### 04.2.7. Correctness

- The controller uses generation, pause/reset, mode gate and tracker to ensure reset/shutdown does not interfere with old tasks.
- Orchestrator `execute` does not propagate a chain result or error. `Completed` means the call ended; it does not prove business success.
- Strict priority queues may starve low-priority tasks; no aging or fairness mechanism is implemented.
- Most NodeManager registrations are not equivalent to redundancy or load balancing.

### 04.2.8. Claim Boundaries

The implemented scheduling surface consists of consistency-aware write scheduling, a fixed-priority Chain dispatcher, and worker registration/dispatch primitives.

Do not claim a unified intelligent scheduler, resource-cost optimization, tenant fairness, dependency DAGs, deadline-aware scheduling, or automatic feedback optimization.

### 04.2.9. Gaps and Target Interfaces

A future `ScheduledTask` contract would need, at minimum, task and Chain type, priority, dependencies, deadline, tenant/scope, consistency requirement, resource estimate, attempt count, idempotency key, context, and result channel.

Before the [Intelligent Scheduler](#049-intelligent-scheduler) can be considered implemented, it needs a classifier, dependency resolver, cost/SLA evaluator, resource allocator, worker health and load inputs, fair queues, cancellation, retry policy, and trace propagation. Its current status is Partial/Planned.

---

## 04.3. Ingest Chain

### 04.3.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Chain |
| Goals | Turn Dynamic Event into a WAL record, canonical objects and retrieval projection |
| Critical-path role | Yes |
| Maturity | The primary write chain is implemented; the concrete `MainChain` abstraction is only partially wired. |

### 04.3.2. Code Entry Points

| Layer | Entry |
|---|---|
| HTTP | `Gateway.handleIngest` |
| Runtime | `SubmitIngestContext`, `projectWALEntry` |
| Consistency | `Controller.Submit`, `projectWithRetry`, `Tracker` |
| Derivation | `materialization.Service.MaterializeEvent` |
| Persistence | `RuntimeStorage.ApplyCanonicalProjection` |
| Conceptual chain type | `chain.MainChain.Run` |
| Background | `EventSubscriber.addBuiltinHandlers` |

### 04.3.3. Actual Stages, Inputs, Outputs, and Ordering

| # | Stage | Input | Output/mutation | Sync rule |
|---|---|---|---|---|
| 1 | decode/normalize | Event JSON | normalized Event | sync |
| 2 | admission/mode | Event + context | slot/shard/mode/deadline | sync |
| 3 | WAL append | Event | WALEntry/LSN | sync all modes |
| 4 | materialize in memory | WALEntry.Event | IngestRecord, Memory, stable State, optional Artifact, Edge, and Version records | projection worker |
| 5 | canonical commit | Event/Memory/State/optional Artifact/Edges/Versions | store transaction/upserts | authoritative write |
| 6 | retrieval ingest | IngestRecord | lexical/vector segment state | after canonical commit |
| 7 | hot/conflict/precompute | Memory/Event | cache, conflict edge, evidence fragment | same projection callback but not all are gate-critical |
| 8 | tracker visible | LSN | checkpoint/watermark | strict waits |
| 9 | subscriber maintenance | WAL scan | keyed State, tool trace, reflection, conflict, consolidation | async |

`Runtime.projectWALEntry` now includes `MaterializationResult.State` and State history in the same `CanonicalProjection`. Explicit `state_key/state_value` fields update a stable keyed State; other Events update the scope's `last_memory_id` State. Before commit, Runtime reads current State/version history, assigns the monotonic next version, and closes the previous interval. The asynchronous `StateMaterializationWorker` still serves specialized apply/checkpoint chains, but it is no longer the only source of primary-write State visibility.

Canonical commit precedes retrieval ingest. If retrieval ingest fails, the controller does not advance the visible watermark and retries the same WAL LSN. Query also rejects canonical objects whose `MutationLSN` is beyond `ReadWatermarkLSN`, so canonical persistence alone does not make a failed projection visible.

### 04.3.4. MainChain abstraction

`MainChain.Run(MainChainInput)` invokes object, State, tool, index, and graph workers. It assumes WAL has already been written. The active Runtime write path does not call `MainChain`; only the Orchestrator does, and primary Gateway requests are not submitted to that Orchestrator.

### 04.3.5. Canonical object rules

| Event signal | Output |
|---|---|
| every accepted Event | default `mem_<event_id>` (or an explicit typed Memory object ID), Memory version, stable `last_memory_id` State, base/causal edges, retrieval record |
| artifact/tool-like | Artifact + version + artifact edges; worker may also generate tool trace |
| payload with state key | `CanonicalStateID(tenant, workspace, agent, session, state_key)`, committed in the primary canonical write |
| parent/causal/source/target refs | typed Edge according to builders |

`materialization.enabled/targets` is currently normalized metadata and hook input. It does not universally disable or reroute the default Memory projection.

### 04.3.6. State and side effects

| Plane | Mutation |
|---|---|
| Event | WAL append, LSN/logical time |
| Canonical | Event, Memory, State, optional Artifact, Edge, and complete ObjectVersion snapshots; tool traces may remain asynchronous |
| Projection | DataPlane ingest after canonical commit, dirty flush flag |
| Evidence | in-memory fragment cache, derivation log through workers |
| Metrics | write latency/visible, counts, session step |

### 04.3.7. Correctness and failure

- Deterministic IDs support replay upsert/idempotency.
- Projection retry is LSN-based; strict returns only after tracker visible.
- WAL committed but projection failed is an accepted-not-visible state, not a clean rollback.
- Retrieval success followed by canonical failure can leave projection/canonical divergence; replay/reindex repairs it.
- Auxiliary worker errors do not consistently fail the original ACK; subscriber uses in-memory DLQ/overflow/error reporting, not durable dead-letter storage.

### 04.3.8. Claim Boundaries

Event-first ingest, WAL LSN, deterministic canonical derivation, retrieval projection, three visibility modes and replay.

Do not claim that ACK waits for keyed State, tool-trace, lifecycle, and evidence maintenance to finish, or that WAL, retrieval, canonical storage, and S3 participate in one ACID transaction.

### 04.3.9. Gaps

1. Include keyed State and StateVersion in a defined visibility contract and return the resulting `state_id`.
2. Merge MainChain with the active Runtime path or rename it to avoid implying that it is authoritative.
3. Propagate worker failures into acknowledgement or maintenance status.
4. Add a projection/canonical divergence marker.
5. Define reject, overwrite, or conflict behavior for duplicate Event IDs with different payloads.

---

## 04.4. Memory Lifecycle Chain

### 04.4.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Chain |
| Goals | Canonical Memory is used to extract, amplify, decrease, compress, summarize, archive, govern and retrieve |
| Critical-path role | Query recall/internal memory API accessible; complete pipeline not every time gate is written |
| Maturity | Partial |

### 04.4.2. Code Entry Points and Interfaces

| Entry | API/type |
|---|---|
| Chain | `MemoryPipelineChain.Run(MemoryPipelineInput)` |
| Runtime API | `DispatchAlgorithm`, `DispatchRecall` |
| HTTP | `/v1/internal/memory/{recall,ingest,compress,summarize,decay,stale}` |
| Plugin | `schemas.MemoryManagementAlgorithm` |
| Dispatcher | `InMemoryAlgorithmDispatchWorker.Dispatch` |
| Async | EventSubscriber direct extraction/reflection/consolidation dispatch |

### 04.4.3. Input/output

| Operation | Input | Output | Mutation |
|---|---|---|---|
| extraction | event/agent/session/content | level-0 Memory ID | Memory + derivation |
| ingest algorithm | Memory IDs + context | updated count | MemoryAlgorithmState, Memory ref, audit |
| recall | query + candidate IDs + context | scored refs/MemoryView | The baseline dispatcher does not persist recall state uniformly. |
| update | signals keyed by memory ID | states | algorithm state/lifecycle/audit |
| decay | IDs + timestamp | states | lifecycle/strength/retention/audit |
| compress | source IDs/context | produced IDs | derived Memory + state/audit |
| summarize | source IDs/context | produced IDs | summary Memory + audit |
| reflection | object/policies/time | applied result | Memory lifecycle/policy/tier |

For the complete typed field definitions, see the [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md).

### 04.4.4. Internal composition

| Component | Responsibility | Decision ownership |
|---|---|---|
| MemoryPipelineChain | fixed extraction -> optional consolidation/summary -> reflection | caller flags determine optional stages |
| Algorithm dispatcher | load objects, call plugin, persist state/output/audit | Not the lifecycle threshold |
| baseline plugin | simple strength/decay/recall/compress/summarize | plugin |
| MemoryBank-style | candidate/active/reinforced/stale/compressed/archive/quarantine logic | plugin |
| Zep-style | repository-local Zep-inspired behavior | plugin |
| TieredObjectStore/reflection | hot/cold movement and policy handling | policy/config |

### 04.4.5. Call relationship

`MemoryPipelineChain` combines legacy/baseline workers with the service plugin API. No lifecycle coordinator currently commits recall signals, plugin decisions, tier movement, projection refresh, versioning, and audit as one operation.

### 04.4.6. Data and state

| State | Location |
|---|---|
| canonical content/scope/lifecycle | `schemas.Memory` in ObjectStore |
| plugin-specific strength/retention/profile | MemoryAlgorithmStateStore |
| versions/relations | Version/Edge stores, but dispatcher lifecycle update does not always write version/edge |
| audit | AuditStore for dispatcher/reflection operations |
| projection | DataPlane; derived/lifecycle updates do not guarantee that each path is simultaneously refreshed |
| hot/cold placement | TieredObjectStore/cache/cold store |

### 04.4.7. Correctness

- The dispatcher applies a plugin's `SuggestedLifecycleState` directly; it does not independently adjudicate the content-level decision.
- Compress and Summarize store plugin outputs while usually retaining the source Memory.
- Plugin profile switching does not automatically migrate the historical algorithm state.
- Lifecycle mutation, Version, projection, and tier movement do not share a consistent transaction or reconciliation flow.
- There is no general archived-reactivation operation or state flow; reading from Cold is not canonical reactivation.

### 04.4.8. Claim Boundaries

Supported claim: Plasmod provides pluggable recall, decay, compression, summarization, and lifecycle-suggestion operations.

Do not claim that every recall produces durable reinforcement, that a fully automatic archive/reactivate state machine exists, or that every lifecycle change atomically updates versions, indexes, tiers, and audit records.

### 04.4.9. Gaps

1. One typed lifecycle command/result contract.
2. A lifecycle transition guard plus coordinated Version, Audit, and projection updates.
3. Explicit archive, reactivate, quarantine-release, and hard-purge APIs.
4. Plugin-profile state migration.
5. Source edges for derived Memory and a mandatory projection-refresh contract.
6. Lifecycle lag and transition-failure metrics.

---

## 04.5. Query and Evidence Chain

### 04.5.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Chain |
| Goals | Converting Query to a candidate object and returning objects, relations, versions, provenance, policy and proof |
| Critical-path role | Yes |
| Maturity | Basic structured evidence is Implemented; advanced operators, ACL enforcement, and hydration are Partial. |

### 04.5.2. Entry and interfaces

| Layer | Entry/interface |
|---|---|
| HTTP | `POST /v1/query`, `Gateway.ServiceQueryContext` |
| Runtime | `ExecuteQueryContext`, `executeQuery` |
| Planner | `QueryPlanner.Build` |
| Retrieval | `DataPlane.Search` through `NodeManager.DispatchQuery` |
| Assembly | `Assembler.Build` |
| Reasoning | `QueryChain.Run` |
| Response | `schemas.QueryResponse` + visibility middleware |

### 04.5.3. Stages

| # | Stage | Concrete behavior |
|---|---|---|
| 1 | read consistency | strict/bounded waits; eventual skips |
| 2 | plan | defaults TopK, namespace, time/types/tier and carries descriptor filters |
| 3 | candidate retrieval | Hot/Warm, optional Cold; lexical/vector/sparse/native depending config |
| 4 | candidate correction | CJK fallback, type/selector/target filters, inactive filtering |
| 5 | canonical supplement | State/Artifact/other requested types from ObjectStore |
| 6 | policy filters | `PolicyEngine.ApplyQueryFilters` generates filters unless minimal mode |
| 7 | evidence assembly | edges, versions, provenance, policy annotations, cache fragments, proof skeleton |
| 8 | QueryChain | prefetch graph nodes/edges, BFS proof trace, one-hop subgraph, edge merge |
| 9 | provenance/metrics | embedding provenance, evidence support, contamination observation |
| 10 | output filtering | APP_MODE visibility strips internal/debug fields in production |

### 04.5.4. Inputs and outputs

`QueryRequest` with `QueryResponse` See full fields [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md).

| Output section | Source |
|---|---|
| `objects` | retrieval IDs + canonical supplements |
| `nodes` | QueryChain hydrated Memory/Event/Artifact graph nodes |
| `edges` | assembler BulkEdges + subgraph merge |
| `versions` | latest version lookup |
| `provenance` | Memory source refs, State/Artifact event refs, Edge refs, versions, embedding annotations |
| `proof_trace` | assembler stages/cache fragments + BFS/derivation steps |
| `applied_filters` | PolicyEngine descriptor output |
| `retrieval` | tier/cold diagnostics and hit/add counts |

### 04.5.5. Retrieval details

- Default search covers Hot and Warm; Cold is searched only when `include_cold=true`.
- The Go layer owns candidate fusion, filtering, and evidence; the C++ engine owns physical ANN operations only.
- RRF/score normalization in the dataplane/retrieval implementation; the default RRF constant is defined by the code.
- `objects_only` uses a fast path that skips evidence and QueryChain work.
- `/v1/query/batch` executes direct warm ANN and is not part of this evidence chain.

### 04.5.6. Data and state

Query typically reads only canonical/retrieval stores; it updates metrics, hot cache observation or evidence cache stats.

Inactive memories are filtered, while explicit cold queries may preserve archived IDs from Cold. Quarantine and retraction are currently represented mainly through Assembler annotations; complete deny and mask enforcement is not centralized.

### 04.5.7. Correctness and failure

- `query_status` distinguishes retrieval hits from canonical supplements.
- Candidate hydration uses deterministic ID prefixes and ObjectStore lookups. Unknown prefixes may fall back to Memory inference and can therefore be misclassified.
- Graph expansion is based on existing edges and does not prove graph completeness.
- `DataPlane.Search` has no error return, so backend failures may appear as empty results.
- Evidence cache miss is degraded to query-time evidence and should not change canonical answer contract.

### 04.5.8. Claim Boundaries

Supported claim: QueryChain returns structured candidates, canonical supplements, graph context, versions, provenance, and proof steps.

Do not claim that EvidenceAssembler enforces every policy, every object is fully hydrated to payload, graph/proof output is a formal proof, or evidence completeness is quantified.

### 04.5.9. Gaps

1. typed retrieval error;
2. full object hydration contract or explicitly return only ID;
3. Policy deni/mask separated from annotation;
4. The advanced response mode executor connects;
5. configurable graph depth/edge filters from QueryRequest;
6. Evidence completeness/confidence formula and tests.

---

## 04.6. Collaboration Chain

### 04.6.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Chain |
| Goals | Support between agents share, conflict resolution,handoff and aggregate |
| Critical-path role | Internal collaboration operations are available; no normal Event/Query must be followed |
| Maturity | Partial |

### 04.6.2. Entry points

| Entry | Concrete call |
|---|---|
| Chain | `CollaborationChain.Run(CollaborationChainInput)` |
| Runtime | `DispatchShare`, `DispatchShareWithContract`, `DispatchConflictResolve` |
| HTTP | memory share/conflict, agent handoff, MAS aggregate/consistency, agent list |
| Workers | ConflictMergeWorker, CommunicationWorker, MicroBatchScheduler |
| Stores | ObjectStore, EdgeStore, ShareContractStore, PolicyStore |

### 04.6.3. Input/output

| Operation | Input | Output | Mutation |
|---|---|---|---|
| conflict merge | left/right Memory IDs, object type | winner/loser | loser inactive + conflict edge |
| share/broadcast | from/to agent + Memory ID + optional contract ID | shared Memory ID | derived Event creates Memory/Version/Edge/projection through WAL |
| CollaborationChain | conflict input + agent IDs | winner/shared IDs | merge, microbatch enqueue, optional broadcast |
| handoff | source/target/session/task context | handler response | Event/share path depending body |
| aggregate/consistency | agent answers/results | score/aggregate | mainly response/metrics |

### 04.6.4. Scope and contract behavior

| Concern | Current behavior |
|---|---|
| Agent/session resolution | request fields and Memory fields |
| Share contract storage | canonical CRUD with typed agent/role grants and legacy ACL fields |
| Read ACL | `/v1/query` enforces canonical access on candidates and revalidates evidence graph endpoints |
| Write/derive ACL | contract-backed sharing enforces source `derive` and target `read`; other direct mutations are not yet uniform |
| Shared object model | derives `shared_<source>_to_<target>`, preserves source ownership, and grants the target explicitly |
| Handoff event | internal handler may submit Event; not a single canonical collaboration transaction |
| Conflict detection | same agent+session active Memories; LWW by Version |
| Conflict preservation | loser remains stored but inactive, edge points winner -> loser |

### 04.6.5. Chain and API relationship

`CollaborationChain.Run` resolves conflicts, enqueues the result in the in-memory microbatch, and broadcasts selected Memory. The internal HTTP share/conflict handlers usually call Runtime or NodeManager directly rather than invoking `CollaborationChain`.

### 04.6.6. Data/state

- Canonical: the share path writes a derived Event, shared Memory, complete ObjectVersion snapshot, and `derived_from` Edge; ShareContract and Policy records remain independent.
- Access: the shared object carries owner, scope, target-agent grant, policy tags, and contract ID.
- Provenance: source/target causality on the share Event produces a traversable `derived_from` Edge.
- Projection: share calls `SubmitIngest`, so it follows the ordinary retrieval-projection and watermark path.
- Boundary: the legacy CommunicationWorker and conflict merge still contain direct store mutations without the same transaction/version/audit semantics.

### 04.6.7. Correctness and failure

- Missing source, owner mismatch, and contract scope/derive/read denial return errors; same-agent sharing remains a no-op.
- Conflict precondition is no-op when not satisfied; LWW is the same version as left.
- A WAL-derived share commits canonical Memory/Edge/Version together; retrieval follows, while Audit is not in that transaction.
- Query candidates and evidence endpoints use prevention-based access filtering; contamination metrics are additional observation.
- Raw canonical CRUD, legacy direct workers, and every lifecycle write do not yet share one write-policy gate.

### 04.6.8. Claim Boundaries

Supported claim: explicit ShareContract storage, contract-backed WAL-derived sharing, canonical access filtering for query/evidence, same-session LWW conflict preservation, and handoff/MAS adapters.

Do not claim complete multi-agent transactions, authenticated identity binding, comprehensive ACLs for every management/lifecycle write, automatic semantic conflict detection, a unified merge-policy plugin, or a security-audited zero-leakage guarantee.

### 04.6.9. Gaps

1. Move conflict, merge, and handoff through policy-authorized derived Events and WAL.
2. Add Audit and a retrieval-completion marker to the recoverable share/merge workflow.
3. Apply one contract/policy write gate to raw CRUD, lifecycle, and all write operations.
4. Semantic conflict detector and pluggable merge strategy;
5. durable microbatch/job status;
6. authenticated principal binding and adversarial security tests.

---

## 04.7. Runtime Coordination Mechanism

### 04.7.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Mechanism |
| Goals | Trigger -> routing/dependencies/modules/workers/state/result/replay |
| Maturity | Direct Runtime coordination is Implemented; unified task orchestration is Partial. |

### 04.7.2. Code entry

The main code entries are `worker.Runtime`, the consistency Controller, NodeManager, the Chain types, Orchestrator, the coordinator Hub and registry, Gateway, and EventSubscriber.

### 04.7.3. Input/output

Runtime receives Event, Query, administrative, algorithm, and collaboration requests. It returns acknowledgements, `QueryResponse` values, algorithm or collaboration results, and administrative summaries. Side effects span the WAL, canonical stores, retrieval projections, caches, metrics, and workers.

### 04.7.4. Internal composition

| Role | Concrete owner |
|---|---|
| request lifecycle | Gateway + Runtime methods |
| write state | consistency Controller/tracker |
| dependency order | explicit function sequence |
| worker invocation | NodeManager |
| chain composition | four chain types |
| async maintenance | EventSubscriber/flush loop |
| module discovery | coordinator registry |
| optional queued routing | Orchestrator |

### 04.7.5. Sync/async boundary

Strict writes and query assembly are synchronous. Bounded/eventual projection, subscriber work, and flushes are asynchronous. Orchestrator tasks are asynchronous but are not used by the main API. NodeManager dispatch is often synchronous and does not consistently propagate a request context.

### 04.7.6. State tracking

LSN, tracker, and checkpoint state record write progress. Runtime fields hold mode flags, the latest-Memory map, embedding specification, and flush-dirty state. Orchestrator and scheduler expose independent counters. No unified `ExecutionRecord` or dependency DAG is persisted.

### 04.7.7. Failure/replay

The Controller retries projection and checkpoint operations and recovers from WAL. Runtime administrative replay resubmits Events. NodeManager worker errors may be lost, and Orchestrator statistics ignore `ChainResult`. APIs do not share one envelope for partial results.

### 04.7.8. Claim Boundaries

Supported claim: Plasmod provides a coordinated Event/Query runtime with consistency control and worker composition.

Do not claim that every trigger uses a dependency DAG, resource-aware scheduling, durable task state, or a uniform partial-result protocol.

### 04.7.9. Gaps

Introduce ExecutionPlan/ExecutionRecord, unified context/trace/error/result, explicit sync/async stage contract, worker futures, Orchestrator integration decision and persisted retry/replay metadata.

---

## 04.8. Execution Coordination Engine

### 04.8.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Engine |
| Original Module | `worker.Runtime` |
| Goals | Module calls and status of coordinated ingest/query/memory/collaboration/admin |
| Critical-path role | Yes |
| Maturity | Direct coordination is Implemented; the unified task/plan engine is Partial. |

### 04.8.2. Code entry

| Item | Code |
|---|---|
| Package | `src/internal/worker` |
| Main files | `runtime.go`, `runtime_consistency.go`, `orchestrator.go`, `subscriber.go` |
| Constructor | `CreateRuntime(...)` |
| Bootstrap | `app.BuildServer` constructs and configures Runtime |
| External callers | Gateway, transport RuntimeAPI, consistency project callback, admin handlers |

### 04.8.3. Runtime fields

| Field group | Fields | Responsibility |
|---|---|---|
| Event | `wal`, `bus`, derivation/policy logs | accepted order and semantic logs |
| Query/data | `plane`, `planner`, `policy`, `assembler`, `evCache` | plan/retrieve/evidence |
| Derivation | `materializer`, `preCompute` | Event projection/evidence fragment |
| Control | `coord`, `nodeManager`, `queryChain` | module discovery/worker/query reasoning |
| Storage | `storage`, `tieredObjects` | canonical and tier access |
| Conflict | `lastMem`, mutex | latest per agent/session |
| Consistency | controller/config/watermark, mutex | mode/queue/checkpoint/read gate |
| Background | subscribers + mutex, flush ticker/channels/dirty/once | async maintenance/index flush |
| Runtime modes | vector-only/minimal/governance-disabled | in-memory behavior switches |
| Memory provider | backend router | primary/shadow provider mode |
| Embedding | spec + mutex | family/dimension compatibility |
| Admin | wipe mutex | destructive operation serialization |

### 04.8.4. Constructor dependencies and APIs

`CreateRuntime` requires WAL, Bus, DataPlane, Hub, PolicyEngine, QueryPlanner, Materializer, PreCompute, Assembler, Cache, logs, NodeManager, RuntimeStorage and TieredObjectStore.

| API group | Main methods |
|---|---|
| ingest/query | `SubmitIngestContext`, `ExecuteQueryContext`, internal `projectWALEntry/executeQuery` |
| consistency | configure/start/shutdown/mode/status/pause/reset/resume |
| warm/native | ingest/search/register/unload/batch variants |
| retrieval maintenance | warm prebuild, embedding spec/reindex, flush loop |
| evidence | fragment/derivation/policy decision getters |
| memory/collaboration | algorithm dispatch/recall/share/conflict/provider mode |
| recovery/admin | wipe, replay preview/apply, topology |

### 04.8.5. Inputs, outputs and state mutation

| Request | Output | Mutation |
|---|---|---|
| Event | ACK map | WAL/canonical/projection/cache/metrics |
| QueryRequest | QueryResponse | metrics/cache observations |
| algorithm op | AlgorithmDispatchOutput/MemoryView | Memory/state/audit |
| share/conflict | IDs | Memory/Edge |
| admin replay/reset/reindex | summary | multiple subsystems |

### 04.8.6. Internal components and routing

The request manager is distributed across Gateway and Runtime methods. Task classification and Chain routing use method-level switches rather than one router object. Dependency planning is expressed by code order. Module routing uses direct fields and NodeManager; NodeManager also dispatches workers. The consistency Controller and administrative replay paths provide failure monitoring and replay. Consistency, runtime, and provider flags implement mode management, while the global collector supplies metrics.

### 04.8.7. Correctness

- Runtime is long-lived and concurrency-safe only where fields have mutex/atomic protection; mode booleans are direct mutable flags.
- Main ingest consistency is delegated to Controller.
- Runtime's direct function path is authoritative; Orchestrator registration does not intercept it.
- Partial failures across plane/store/cache/subscriber are not one transaction.
- Admin reset coordinates pause/subscriber/controller but remains high-risk multi-component mutation.

### 04.8.8. Claim Boundaries

Supported claim: the active single-process Runtime provides a concrete execution-coordination engine.

Do not claim a durable generic task DAG, Chain routing for every request, distributed resource allocation, or uniform partial-result handling.

### 04.8.9. Gaps

Define Runtime service interfaces, ExecutionContext/Plan/Result, typed errors, context-aware worker dispatch, mode synchronization, durable task records, Orchestrator integration and component-level health/dependency checks.

---

## 04.9. Intelligent Scheduler

### 04.9.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Engine |
| Original Module | Scheduler / Orchestrator / consistency scheduler |
| Goals | Create and execute plans based on task, dependency, resource, consistency and SLA |
| Critical-path role | The consistency scheduler is; the unified intelligent scheduler is not |
| Current Maturity | Parts and Plans |

### 04.9.2. Existing code entry

| Component | Constructor | Methods |
|---|---|---|
| `worker.Orchestrator` | `CreateOrchestrator` | `Submit`, convenience submits, `Run`, `Stats` |
| consistency Controller | `NewController` | start/submit/read wait/mode/status/pause/reset/resume/shutdown |
| NodeManager | `CreateManager` | register/dispatch/topology |
| WorkerScheduler | `NewWorkerScheduler` | `Dispatch`, `Complete`, `Stats` |
| Microbatch | `CreateInMemoryMicroBatchScheduler` | enqueue/flush/run/info |

### 04.9.3. Existing fields

| Type | Fields |
|---|---|
| `Task` | ID, Type, Priority, Payload, Submitted |
| Orchestrator | Manager, four queues, concurrency/waitgroup, atomic stats, four Chain pointers |
| Controller task | WALEntry, mode, lag, accepted/deadline, generation, strict result channel, bounded shard reservation |
| Controller | WAL/project/config/checkpoint/tracker/mode/admission gates/slots/sharded queues/lifecycle contexts/active count/workers |
| WorkerScheduler | mutex + map worker type -> dispatched/active |
| Microbatch | ID, mutex, opaque queue, batch size |

### 04.9.4. Current input/output

| Scheduler | Input | Output |
|---|---|---|
| Orchestrator | Task with opaque chain payload | queued bool; no result future |
| Controller | Event + context + consistency mode/SLA | visible/pending ACK or error |
| NodeManager | concrete domain arguments | direct worker result or void |
| WorkerScheduler | worker type events | counters |
| Microbatch | opaque payload | flushed FIFO items |

### 04.9.5. Capability comparison

| Required intelligent component | Current status |
|---|---|
| Task profiler/classifier | fixed TaskType / Event consistency mode |
| Chain selector | Orchestrator type switch, not main path |
| Dependency resolver | missing; code order only |
| Candidate plan generator | missing |
| Priority scorer | caller integer only |
| Cost estimator | missing |
| SLA evaluator | bounded freshness only |
| Consistency evaluator | implemented for Event/read gate |
| Resource allocator/worker pool selector | fixed pool/first worker |
| Feedback optimizer | metrics exist, no feedback loop |

### 04.9.6. Call relationship and state

Bootstrap starts and registers the Orchestrator, but Gateway and Runtime do not submit primary tasks to it. The consistency Controller directly schedules the critical Event-ingest path. NodeManager dispatch bypasses WorkerScheduler counters unless a caller integrates them explicitly.

### 04.9.7. Correctness/failure

- Orchestrator priority is non-preemptive and can starve low priority.
- submit times out at 30 s and increments dropped; no durable queue/retry/result/cancel.
- `Completed` increments even if payload type mismatch or ChainResult failed.
- Controller has robust generation/pause/retry/checkpoint semantics but only for write visibility.
- No fairness, tenant isolation, resource health or deadline scheduling beyond write SLA.

### 04.9.8. Claim Boundaries

Supported claim: Plasmod provides a fixed-priority Chain dispatcher, consistency-aware sharded write scheduling, a worker registry, and microbatch primitives.

Do not claim an intelligent, resource-aware unified scheduler, dependency DAG, cost optimization, tenant fairness, deadline scheduling, or automatic tuning.

### 04.9.9. Required target interface and fields

```text
Classify(Request) -> ScheduledTask
Plan(Task, ResourceSnapshot) -> ExecutionPlan
Submit(ctx, plan) -> Future[ExecutionResult]
Cancel(taskID) / Retry(taskID) / Resume(taskID)
```

The target Task model must include type, Chain, priority, dependencies, deadline, SLA, consistency, tenant, scope, idempotency, resource estimates, attempt, and trace. Before this Engine can be marked complete, the scheduler needs durable state, fair queues, worker health and load inputs, result/error propagation, cancellation, retry policy, metrics feedback, and integration tests proving that all Gateway and Runtime paths use it.
