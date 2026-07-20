# 03. Canonical Object Model and Memory Lifecycle

> Language: [中文](../03-canonical-data-model-and-memory-lifecycle.md) | English

---

This chapter defines the canonical objects derived from Events and details the object-derivation, memory-evolution, canonical-object-graph, and tiered-storage engines.

---

## 03.1. Memory Subsystem Architecture

### 03.1.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Perspective |
| Question | What objects, modules and capabilities make up the Memory subsystem? |
| Maturity | Ranges from Partial to Implemented, depending on the capability. |

### 03.1.2. Code Entry Points

| Role | Package / file | Constructor / public method |
|---|---|---|
| Event-derived Memory | `src/internal/materialization/service.go` | `materialization.NewService`, `MaterializeEvent` |
| Canonical storage | `src/internal/storage/contracts.go` | `RuntimeStorage.Objects`, `ApplyCanonicalProjection` |
| Algorithm dispatch | `src/internal/worker/cognitive/` | dispatcher constructor, `Dispatch`/`Run` |
| Agent-facing lifecycle adapter | `src/internal/agent/memory_manager.go` | `NewBaselineMemoryManager`; `Recall`, `Ingest`, `Compress`, `Summarize`, `Decay` |
| Tier placement | `src/internal/storage/tiered.go` | `NewTieredObjectStore`, promotion/archive/read methods |
| Retrieval projection | `src/internal/dataplane/` | `DataPlane.Ingest`, `Search`, `Flush` |
| Evidence and policy | `src/internal/evidence/`, `src/internal/semantic/policy.go`, cognitive reflection worker | `Assembler.Build`, policy/reflection methods |

See the [Interface Implementation Registry](14-implementation-status-gaps-and-claim-boundaries.md) for the complete interface, implementation, and construction map.

### 03.1.3. Inputs and Outputs

| Operation | Typed input | Output | Main mutation / side effect |
|---|---|---|---|
| materialize | `schemas.Event` | `MaterializationResult` | Memory, Version, Edge, and the State/Artifact candidate |
| lifecycle dispatch | `AlgorithmDispatchInput` | `AlgorithmDispatchOutput` | Memory lifecycle, algorithm state, audit; some operations generate new memory |
| recall | query/scope/topK or `AlgorithmRecallInput` | `MemoryView`/ranked IDs | A plugin may compute reinforcement; the primary recall paths do not persist it uniformly |
| tier transition | memory ID + placement signal | tier result | Hot cache, warm object, cold object/embedding changes |
| retrieval | `SearchInput` | `SearchOutput` | Read projection; tier/segment trace included |
| evidence | candidate IDs + query context | `EvidenceSubgraph`/`QueryResponse` | Read Edge/Version/Policy/derivation; write bounded cache |

See the [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md) for concrete field definitions.

### 03.1.4. Internal Components

| Concern | Canonical type/module | Maturity |
|---|---|---|
| Memory object | `schemas.Memory` | Complete field model |
| Event materialization | `materialization.Service`, extraction worker | Complete basic rules |
| Canonical store | `ObjectStore` memory methods | Complete |
| Algorithm state | `MemoryAlgorithmStateStore` | Full storage interface |
| Algorithm dispatch | plugin interface + dispatcher | Core infrastructure exists; profile migration is incomplete. |
| Lifecycle | fields + plugins + reflection worker | Partial |
| Tiering | Hot cache/Warm ObjectStore/Cold store | Partial automation |
| Retrieval projection | DataPlane Ingest/Search | Complete foundation |
| Governance | PolicyRecord/ShareContract/Audit/filters | Partial enforcement |
| Evidence/provenance | Edge/Version/derivation/Assembler | Complete foundation |

The separation of memory properties, algorithmic states, and cross-object information is as follows:

Memory itself contains identity/type/content/scope/quality/validity/lifecycle and external references; algorithm-specific strength/retention/profile exists in an independent state store; relationships, versions, policies, audits also exist in an independent store. [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md).

This separation allows algorithms to be replaced without modifying the canonical Memory schema. However, `AlgorithmStateRef` is currently a single string; multi-state models still need a more precise reference contract for each `(memory, algorithm)` pair.

The object relationship is:

```text
Event -> Memory
Event -> State / Artifact
Memory -> source Event
Memory -> summary/compressed/shared Memory
Memory <-> Edge graph
Memory -> ObjectVersion / PolicyRecord / AuditRecord / AlgorithmState
Memory -> Retrieval projection
```

### 03.1.5. Call Relationships

- Event ingest creates the baseline Memory.
- `/v1/memory` provides direct CRUD.
- `/v1/internal/memory/*` invokes algorithm, lifecycle, and sharing operations.
- `/v1/query` retrieves memories and constructs evidence.
- Admin delete, purge, archive, and reindex operations manage lifecycle and storage projections.

The synchronous main chain mainly covers event materialization, query read and explicit internal lifecycle commands; subscriber, reflection, summarization, index build and partial tier maintenance are asynchronous or explicit trigger paths.

### 03.1.6. Data and State

| State class | Location | Persistence |
|---|---|---|
| canonical Memory | `ObjectStore` | memory or Badger backend |
| lifecycle | `Memory.LifecycleState`, `IsActive`, validity fields | Persisted with Memory. |
| algorithm state | `MemoryAlgorithmStateStore` | memory/Badger composite |
| graph/version/policy/audit | dedicated stores | backend-dependent persistent state |
| retrieval projection | segments/vector/sparse/native index | Rebuildable acceleration state. |
| hot/evidence cache | in-process bounded cache | Volatile. |
| cold object/embedding | `ColdObjectStore` | In-memory simulation or S3/MinIO adapter |

### 03.1.7. Correctness

Canonical Memory is authoritative; retrieval records, hot caches, and evidence fragments are rebuildable. Direct Memory mutations that bypass the Event chain are a major consistency boundary because they do not automatically synchronize versions, projections, and evidence.

Event-derived IDs and store upserts make replay idempotent for the covered records. The algorithm dispatcher writes Memory, algorithm state, and audit data, but does not combine lifecycle mutation, ObjectVersion, Edge, and projection refresh into one transaction. Recovery relies on WAL replay, reindexing, explicit lifecycle operations, or manual repair; there is no unified Memory reconciliation loop.

### 03.1.8. Claim Boundaries

Supported claim: Plasmod provides a Memory subsystem composed of canonical objects, algorithm state, tiering, retrieval, governance, and evidence.

Do not claim that every component is coordinated by one Memory service, or that algorithm recommendations, policy annotations, and cold-tier placement form a globally consistent lifecycle state machine.

### 03.1.9. Gaps

- No unified `MemoryMutation` or transition-command interface.
- No atomic contract spanning lifecycle, version, edge, and projection updates.
- `LLMProvider` and `MASProvider` are SDK contracts without core production implementations.
- ACL enforcement is not consistent across every Memory read and write entry point.
- Profile migration, cache generations, and a cross-plane repair manager are missing.
- Cross-backend contract tests and invalid-transition tests are still required.

---

## 03.2. Memory Lifecycle State Machine

### 03.2.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Behavior Perspective |
| Question | What are the real states of memory and who triggers and executes the conversion |
| Maturity | There is enum and plugin transition logic, but no unified state machine. |

### 03.2.2. Code Entry Points

| Concern | Package / file | Main API |
|---|---|---|
| enum and canonical fields | `src/internal/schemas/canonical.go` | `MemoryLifecycle`, `Memory.LifecycleState`, `IsActive` |
| algorithm suggestion | `src/internal/schemas/memory_management.go` | `MemoryManagementAlgorithm`, `AlgorithmDispatchOutput` |
| transition writer | `src/internal/worker/cognitive/` | dispatcher lifecycle/state/audit writes |
| policy-driven decay/quarantine | `src/internal/worker/cognitive/` reflection worker | `Reflect`/typed `Run` path |
| explicit stale/archive/delete | `src/internal/access/` admin handlers, `src/internal/worker/runtime.go`, `src/internal/storage/tiered.go` | internal/admin handlers and tier methods |
| agent adapter | `src/internal/agent/memory_manager.go` | `Compress`, `Summarize`, `Decay` |

### 03.2.3. Inputs and Outputs

| Trigger input | Decision output | State mutation | Additional output |
|---|---|---|---|
| new Event | default/event lifecycle | new Memory lifecycle | Version/Edge/projection candidate |
| algorithm operation + Memory + state | `SuggestedLifecycleState` | Memory + algorithm state | produced IDs, audit metadata |
| recall query | score/reinforcement candidate | plugin-specific; not uniformly persisted | ranked memory view |
| reflection policy/TTL/confidence | retain/decay/archive/quarantine | Memory/tier/audit depending path | policy decision |
| admin delete/purge | logical or hard deletion | `IsActive`/state or physical removal | cleanup/audit result |

### 03.2.4. Internal Components

#### 03.2.4.1. Actual enum

`schemas.MemoryLifecycle` defines:

| State | Current meaning/producer |
|---|---|
| `active` | materialization/baseline/default active object |
| `candidate` | MemoryBank-style ingest/evaluation |
| `reinforced` | MemoryBank-style recall/update signal |
| `compressed` | plugin-derived or transitioned memory |
| `decayed` | baseline/reflection decay |
| `stale` | MemoryBank/Zep/internal stale route |
| `archived` | plugin/tier lifecycle suggestion |
| `quarantined` | plugin/reflection policy |
| `hidden` | retrieval exclusion state |
| `deleted_logically` | logical deletion state |

`Created`, `Weakened`, `Summarized`, `Reactivating`, `Reactivated`, and physical `Deleted` are not current enum values. Summary and compression typically create a new Memory rather than only changing the original object's state.

#### 03.2.4.2. Transition ownership

| Trigger | Decision | Writer |
|---|---|---|
| Event ingest | Event object lifecycle or default | materializer |
| algorithm ingest/update/decay | plugin returns `SuggestedLifecycleState` | dispatcher applies verbatim |
| recall | A plugin may score or reinforce internally; the dispatcher does not uniformly persist recall state. | plugin/none |
| reflection | TTL/quarantine/confidence policy | reflection worker |
| internal stale | handler command | Gateway/store |
| archive/delete | admin/tier operation | storage/governance path |

#### 03.2.4.3. Guards and actions

MemoryBank-style lifecycle code contains guards for candidate, active, reinforced, compressed, stale, archived, and quarantined states. Other plugins use different rules; no mandatory transition table applies across all plugins. Retrieval checks `IsActive` and `LifecycleState` through different paths, while some policy tags and states are evaluated separately.

### 03.2.5. Call Relationships

Event ingest initializes Memory. Internal memory routes or `AgentSession.MemoryManager` invoke the algorithm dispatcher. Subscriber and reflection workers may update lifecycle asynchronously, while admin and tier paths perform archive/delete operations. These paths mutate `Memory` fields without a unified transition service.

Synchronization depends on the entry point: internal algorithm routes wait for dispatcher results, subscriber maintenance runs after the ingest acknowledgement, and query recall does not guarantee that reinforcement has already been persisted.

### 03.2.6. Data and State

- Lifecycle state in canonical memory; algorithm score/profile in `MemoryAlgorithmStateStore`;
- `IsActive`, validity intervals, and policy tags also affect visibility.
- Hot/Warm/Cold is a physical placement, not a lifecycle enum.
- The transition audit exists in part on the dispatcher/reflection/admin path;
- Summary/compression can generate a new memory, which the original object typically retains and expresses a derivative relationship through Edge/Version.

Reverse transition and current behavior of tiering:

- A plugin can suggest leaving quarantine, but there is no unified authorization flow for that transition.
- There is no general archived -> active/reactivated transition; a cold query is not reactivation.
- The lifecycle state is not tied to the Hot/Warm/Cold; the placement policy can refer to the lifecycle, but the tier is the physical state.
- Logical delete is separated from hard purge.

### 03.2.7. Correctness

The dispatcher writes Memory, algorithm state, and audit records for lifecycle updates, but it does not uniformly write ObjectVersion, Edge, and refreshed retrieval projections. Reflection and administrative paths have additional side effects.

### 03.2.8. Claim Boundaries

Supported claim: lifecycle enums, plugin-driven transition suggestions, policy decay/quarantine, and archive/delete operations are implemented.

Do not claim a fully verified state machine, uniform reverse transitions, mandatory audit coverage for every path, or strict binding between lifecycle state and storage tier.

### 03.2.9. Gaps

- No unified transition command, guard/action registry, or invalid-transition error.
- No single visibility invariant combines `IsActive`, lifecycle, validity, and policy.
- No unified authorized workflow for archive-to-reactivate transitions or quarantine release.
- transition, `ObjectVersion`, Edge, audit, and projection updates are not atomic or uniformly recoverable;
- Cross-plugin transition contracts, reverse-transition tests, and concurrent-mutation tests are missing.

---

## 03.3. Event-derived Memory Construction Mechanism

### 03.3.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Mechanism |
| Goals | Raw Event -> normalized Event -> typed canonical objects/relations/versions/projection |
| Critical-path role | Event ingest |
| Maturity | Basic rules are Implemented; semantic and configurable derivation remains Partial. |

### 03.3.2. Code Entry Points

| Function/type | Role |
|---|---|
| `Event.NormalizeDynamicEventV04` | merge nested/legacy wire fields |
| `MaterializeEvent` | produce Memory, candidate State, optional Artifact, edges, versions, IngestRecord |
| Event helper methods | text/scope/state/artifact/causality extraction |
| specialized materialization workers | keyed State, Artifact/tool trace, graph/index side effects |
| `ApplyCanonicalProjection` | commit selected outputs |

### 03.3.3. Inputs and Outputs

The input is a complete Dynamic Event; see the [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md). An Event produces a Memory, Memory version, stable keyed or `last_memory_id` State, State version, retrieval record, and base or causal edges. Artifact and ArtifactVersion are added when Artifact derivation rules match. The original Event and the derived records are assembled into the canonical projection write set.

### 03.3.4. Decision rules

| Signal | Decision |
|---|---|
| event type | resolve Memory type; tool/artifact/state route |
| `retrieval.index_text`/payload text | Memory content and index text |
| workspace/retrieval/session | scope and namespace |
| causality refs | typed edges/provenance |
| object descriptor | explicit object/artifact/state metadata |
| embedding vector/ref/flag | projection vector or skip-vector behavior |

Go helpers implement the current rules directly; there is no declarative rule registry or semantic analyzer.

### 03.3.5. Call Relationships and Synchronous Boundaries

Runtime calls the Materializer synchronously. The consistency worker submits retrieval and canonical writes, while the subscriber invokes specialized workers asynchronously. `MainChain` packages another ordered worker sequence but is not the active Runtime write path.

### 03.3.6. State Changes

WAL/LSN is the derivative input sequence; canonical store preserves the object/relationship/version; DataPlane preserves the projection; derivation log/audit/cache preserves the auxiliary evidence.

### 03.3.7. Correctness

- Deterministic primary IDs support replay;
- duplicate Event ID currently tends to upsert/cover, with no universal payload hash conflict rejection;
- Multi-store writes are transactional only within the shared canonical backend;
- The algorithm compresses/summarizes directly generates Memory without automatically re-entering the mechanism as an Event.

### 03.3.8. Claim Boundaries

Supported claim: Events drive typed-object construction plus provenance, version, and retrieval projections.

Do not claim arbitrary configuration-driven rules, LLM semantic classification, a universal deduplication/conflict engine, or a derived Event for every derived Memory.

### 03.3.9. Gaps

DerivationRule interface/registry, duplicate policy, payload hash conflict, derived event contract, per-output validation, dedicated worker, output merger protocols and fault-injection tests are still required.

---

## 03.4. Canonical Memory Representation Mechanism

### 03.4.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Mechanism |
| Goals | Represent content, provenance, lifecycle, algorithms, governance, versions, and relationships around a stable canonical Memory identity |
| Maturity | Field model is complete; persistence and validation of some referenced objects are Partial. |

### 03.4.2. Code and schema entry

- `schemas.Memory`, `MemoryAlgorithmState`, `ObjectVersion`, `Edge`, `PolicyRecord`, `AuditRecord`;
- `ObjectStore`, `MemoryAlgorithmStateStore`, Version/Edge/Policy/Audit stores;
- `ObjectModelRegistry`, where Memory is marked as versionable and indexable.

### 03.4.3. Representation

```text
Memory = identity/type/scope/content/quality/validity/lifecycle
       + source/provenance references
       + embedding and algorithm-state references
       + dataset lineage
```

See full field [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md).

### 03.4.4. Internal vs external fields

| Concern | Stored in Memory | External record |
|---|---|---|
| content/summary/type/scope | yes | retrieval text/vector projection |
| provenance | source IDs/ref | Edge + derivation log |
| lifecycle | state/isActive/TTL/valid interval | Policy/Audit/Version |
| algorithm | single ref | `(memory,algorithm)` state records |
| embedding | ref only | vector index / optional Embedding object model |
| policy/share | tags/scope | PolicyRecord/ShareContract |
| version | current number | ObjectVersion history |

### 03.4.5. Input/output and calls

Materializer, plugin, collaboration, reflection, and admin handlers write Memory; query, algorithm, tier, governance, and evidence paths read it. No single `MemoryRepository` service validates every writer.

### 03.4.6. State semantics

`IsActive` is a coarse serving flag, `LifecycleState` is a finer lifecycle phase, and tier records physical placement; the three are related but not equivalent. `ValidFrom` and `ValidTo` define canonical validity, not merely retrieval-segment time.

### 03.4.7. Correctness

- Stable ID/version/source refs are replay boundaries.
- Direct Memory POST can bypass version/projection/audit.
- `AlgorithmStateRef` does not yet fully define references for a multi-algorithm state store.
- Embedding object type has been defined, but active paths mainly store embedding in the index metadata/vector store.

### 03.4.8. Claim Boundaries

Supported claim: canonical Memory is separated from algorithm state, governance records, and retrieval projections.

Do not claim database-level foreign-key enforcement for every reference or complete provenance, evidence, and embedding payloads within each Memory record.

### 03.4.9. Gaps

It requires a central memory mutation validator, reference integrity checks, multi-algorithm ref schema, versioned validity update API, embedding object persistence contract and writer inventory tests.

---

## 03.5. Memory Evolution Mechanism

### 03.5.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Mechanism |
| Goals | Recall/usage/time/policy/cost signals driving memory state, content, tier and projection evolution |
| Maturity | Partial |

### 03.5.2. Entry and decision owners

| Entry | Decision owner |
|---|---|
| algorithm dispatch ingest/update/recall/decay/compress/summarize | selected plugin |
| reflection policy | PolicyRecord + baseline worker rules |
| admin archive/delete/purge | handler/storage policy |
| hot cache promotion/eviction | salience/hotness/config |

### 03.5.3. Signals and outputs

| Signal | Possible output |
|---|---|
| recall/query | scored order, plugin reinforcement state |
| elapsed time/TTL | decayed/stale/archive suggestion |
| importance/confidence/policy | salience adjustment/quarantine |
| conflict penalty | quarantine/stale in MemoryBank-style |
| content set | compressed/summary derived Memory |
| storage pressure/access | hot eviction/promotion |

### 03.5.4. Interfaces and state

`MemoryManagementAlgorithm` is a decision plugin. The dispatcher persists `MemoryAlgorithmState` and Memory outputs; reflection writes Memory, PolicyDecision, and Audit records and may archive. Canonical lifecycle, algorithm state, and physical tier are distinct state domains.

### 03.5.5. Sync/async

Internal algorithm APIs are synchronous. Subscriber reflection and consolidation are asynchronous. Hot eviction occurs on cache insert; Cold archival is mostly explicit or reflection-driven. No central evolution loop schedules every operation.

### 03.5.6. Correctness

- Source Memory remains for compression/summary semantics unless explicit deletion.
- Derived edges/version/projection are not uniformly required by dispatcher.
- Recall dispatch does not persist generic reinforcement state.
- Archived read does not automatically produce reactivation transition.

### 03.5.7. Failure/recovery

A partial mutation can leave lifecycle, projection, and tier state out of sync. Current recovery relies on canonical inspection, reindex or archive retry, and audit records. There is no lifecycle transaction log or reconciler.

### 03.5.8. Claim Boundaries

Supported claim: Plasmod provides pluggable evolution algorithms plus policy and tier actions.

Do not claim a fully autonomous evolution engine, a uniform scoring function, automatic reactivation, or transactional lifecycle transitions.

### 03.5.9. Gaps

Define EvolutionCommand/Decision/Transition record, mandatory version/edge/audit/projection hooks, operation scheduler, reactivation and purge states, and transition metrics/tests.

---

## 03.6. Object Derivation Engine

### 03.6.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Engine |
| Original Module | Materializer + specialized materialization workers |
| Goals | Event -> canonical objects/relations/versions/retrieval record |
| Critical-path role | Yes |
| Maturity | Basic derivation rules are Implemented; general semantic/configurable derivation is Partial. |

### 03.6.2. Code Entry Points

| Item | Code |
|---|---|
| Package | `src/internal/materialization`, `src/internal/worker/materialization` |
| Main files | `service.go`, `pre_compute.go`, `object.go`, `state.go`, `tool_trace.go` |
| Constructors | `NewService`, `NewPreComputeServiceWithConfig`, `CreateInMemory*MaterializationWorker` |
| Public methods | `MaterializeEvent`, compatibility `ProjectEvent`; worker `Run/Materialize/Apply/Checkpoint/TraceToolCall` |
| Interfaces | Object/State/Tool worker interfaces, `Runnable` |

### 03.6.3. Engine fields

| Type | Field | Type/meaning |
|---|---|---|
| `Service` | none | stateless deterministic transformer |
| `MaterializationResult` | `Record` | retrieval `IngestRecord` |
| same | `Memory`, `Version`, `Edges` | always-derived core output |
| same | `State`, `StateVersion` | ingest checkpoint candidate |
| same | `Artifact`, `ArtifactVersion` | optional output |
| Object worker | `id`, object/edge/version stores, derivation logger | specialized Artifact route |
| State worker | `id`, object/version stores, derivation logger, mutex | store-backed keyed State version tracking and checkpoints |
| Tool worker | ID/object/version/log dependencies | tool Artifact trace plus recoverable version |
| PreCompute | cache/config | evidence fragment precomputation |

### 03.6.4. Input/output and field access

| Input | Reads | Output |
|---|---|---|
| `schemas.Event` | identity, actor, time, event/object/causality/access/materialization/retrieval/payload | `MaterializationResult` |
| `StateApplyInput` | actor IDs, event type, state key/value | State ID/version |
| `StateCheckpointInput` | agent/session | snapshot count + ObjectVersions |
| `ToolTraceInput` | tool event/object/payload | Artifact ID |

See full field [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md).

### 03.6.5. Internal components

| Suggested component | Actual implementation |
|---|---|
| Event Normalizer | Event helper/normalize methods, complete |
| Event Parser | Go JSON decode + helper accessors, complete |
| Semantic Analyzer | There is no general analyzer; event-type switch/heuristics |
| Event Classifier | The event/object helper rules, part |
| Object Deriver | `MaterializeEvent` + workers, complete foundation |
| Relation Generator | `deriveEdges`, schema edge builders, graph worker |
| Version Generator | materializer + state/object workers |
| Projection Builder | `IngestRecord` construction |
| Deduplicator | Definitional ID/upsert only, with no conflict-aware deduplicator |
| Validator | verifies required identity, object, scope, payload, and derivation constraints |

### 03.6.6. Calls and APIs

Upstream callers are Runtime `projectWALEntry`, `MainChain`, and the subscriber. Downstream dependencies are DataPlane, RuntimeStorage, the evidence cache, and the derivation log. The public entry point is `POST /v1/ingest/events`; direct canonical POST routes do not call this engine.

### 03.6.7. State, correctness and failure

- The Service is stateless. The State worker reads current State and version history from ObjectStore/VersionStore instead of relying on a process-local key map for version assignment.
- Memory IDs, scope-safe `CanonicalStateID`, and mutation-event identity support idempotent replay.
- Runtime submits the Event, Memory, stable keyed or `last_memory_id` State, optional Artifact, relations, and complete snapshot versions in one canonical projection. Dedicated tool-trace and lifecycle-worker outputs remain outside that transaction.
- A Runtime mutex serializes State version resolution inside one process; cross-process writers still require an external single-writer or conditional-write protocol.
- Worker store writes can overlap Runtime Artifact writes. Upsert semantics hide some duplication, but version histories can still diverge.
- `enabled/targets` is not a hard routing gate.

### 03.6.8. Claim Boundaries

Typed deterministic object derivation with relation/version/projection records.

Do not claim semantic LLM analysis, a declarative derivation DSL, global deduplication/conflict handling, atomicity across every output, or arbitrary object plugins.

### 03.6.9. Gaps

1. single DerivationPlan listing every output and commit status;
2. configured rule registry and custom object derivation interface;
3. duplicate payload conflict policy;
4. merge specialized worker outputs into one commit/replay contract;
5. per-output validation/error propagation;
6. tests proving same Event produces byte/semantic-equivalent outputs across replay.

---

## 03.7. Memory Evolution Engine

### 03.7.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Engine |
| Original Module | Memory Algorithm Dispatcher + plugins |
| Goals | Execute memory ingest/update/recall/decay/compress/summarize and persist the results |
| Critical-path role | Internal memory operations; common queries can be accessed via a recall adapter |
| Maturity | Plugin and dispatcher foundations are Implemented; unified lifecycle transactions are Partial. |

### 03.7.2. Code entry

| Item | Code |
|---|---|
| Interface | `schemas.MemoryManagementAlgorithm` |
| Dispatcher | `worker/cognitive/algorithm_dispatcher.go` |
| Plugins | `cognitive/baseline`, `memorybank`, `zep` |
| Constructor | `CreateAlgorithmDispatchWorker` |
| Runtime | `DispatchAlgorithm`, `DispatchRecall` |
| HTTP | `/v1/internal/memory/*` |

### 03.7.3. Dispatcher fields

| Field | Type | Meaning |
|---|---|---|
| `id` | string | worker/node identity |
| `algo` | `MemoryManagementAlgorithm` | active plugin |
| `objStore` | ObjectStore | canonical Memory read/write |
| `algoStore` | MemoryAlgorithmStateStore | plugin state persistence |
| `auditStore` | AuditStore | algorithm update audit |

Plugins additionally hold their own configuration and optional in-memory exported state maps; their durable portability depends on dispatcher state persistence/load usage.

### 03.7.4. Interface methods

| Method | Input | Output |
|---|---|---|
| `AlgorithmID` | none | stable ID |
| `Ingest` | Memories + context | initial states |
| `Update` | Memories + signals | updated states |
| `Recall` | query + candidates + context | scored Memories |
| `Compress` | Memories + context | derived Memories |
| `Decay` | Memories + timestamp | updated states |
| `Summarize` | Memories + context | summary Memories |
| `ExportState/LoadState` | memory ID/state | plugin state portability |

Dispatcher `Dispatch` supports operation strings `ingest|decay|recall|compress|summarize|update` and returns `AlgorithmDispatchOutput`.

### 03.7.5. Internal behavior

- fetch IDs from ObjectStore;
- construct AlgorithmContext;
- call exactly one selected operation;
- persist states and apply `SuggestedLifecycleState` verbatim;
- store derived Memory as returned;
- append algorithm-update AuditRecord;
- return counts/IDs/scored refs.

Dispatcher deliberately has no threshold/business decision logic.

### 03.7.6. State mutations and side effects

| Operation | Canonical Memory | Algorithm state | Audit | Version/Edge/Projection |
|---|---|---|---|---|
| ingest/update/decay | ref/lifecycle may change | yes | yes | not uniformly |
| recall | no generic mutation | no dispatcher persistence | no | none |
| compress/summarize | new Memory | plugin-dependent | yes | not uniformly |

### 03.7.7. Correctness/failure

- Unknown operation returns typed error string.
- Missing Memory IDs are silently omitted from input set.
- Store methods do not return errors, so persistence failure visibility depends implementation.
- Profile switch does not migrate/validate old algorithm states.
- Derived Memory may be absent from retrieval until separately indexed.

### 03.7.8. Claim Boundaries

Supported claim: Plasmod provides a pluggable algorithm interface, algorithm-specific state independent of the canonical schema, and baseline, MemoryBank-style, and Zep-style implementations.

Undeclared plugins are externally equivalent to named products, recall always reinforces, or lifecycle/derived mutations are transactionally versioned/reindexed.

### 03.7.9. Gaps

Capability registry/versioning, plugin state migration, missing-ID/error reporting, lifecycle transition transaction, mandatory derived edges/versions/projection, operation metrics and deterministic plugin contract tests.

---

## 03.8. Canonical Object Graph Engine

### 03.8.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Engine |
| Original Module | Canonical Object Store / RuntimeStorage |
| Goals | Persist and query canonical objects, relationships, versions, policies, contracts, audits, and algorithm state. |
| Critical-path role | Yes |
| Maturity | Single-process in-memory and Badger core is Implemented; graph queries and constraints are Partial. |

### 03.8.2. Code entry

| Item | Code |
|---|---|
| Interfaces | `src/internal/storage/contracts.go` |
| Memory implementation | `storage/memory.go` |
| Persistent implementation | `badger_stores.go`, `composite.go` |
| Factory | `BuildRuntimeFromEnv` |
| Canonical transaction | `ApplyCanonicalProjection` |
| Graph adapters | `schemas/graph_*`, `worker/indexing/graph.go` |
| Coordinators | Object/Memory/Version/Policy coordinators |

### 03.8.3. Engine fields and stores

The Engine is an aggregate boundary, not one `CanonicalObjectGraphEngine` struct.

| RuntimeStorage accessor | Record family |
|---|---|
| `Segments`, `Indexes` | retrieval metadata |
| `Objects` | Agent/Session/Event/Memory/State/Artifact/User |
| `Edges` | graph relations + src/dst indexes |
| `Versions` | object histories/latest |
| `Policies` | append-only PolicyRecord |
| `Contracts` | ShareContract |
| `Audits` | append-only AuditRecord |
| `AlgorithmStates` | per memory/algorithm state |
| `HotCache` | volatile object cache |

`CanonicalProjection` fields: Memory, State, Artifact, Versions, Edges, flags for base edges.

### 03.8.4. Interfaces and API surface

| Interface | Main methods |
|---|---|
| ObjectStore | put/get/list per object type, delete Memory |
| GraphEdgeStore | put/get/delete/from/to/bulk/list/prune |
| SnapshotVersionStore | put/history/latest |
| PolicyStore | append/get/list |
| ShareContractStore | put/get/by-scope/list |
| AuditStore | append/get/list/delete target |
| AlgorithmStateStore | put/get/list |
| RuntimeStorage | accessors, canonical projection, base-edge helpers |

External direct APIs include the canonical collection routes. Event and Query paths use the engine through Runtime and evidence assembly. See the [API to Engine Matrix](14-implementation-status-gaps-and-claim-boundaries.md).

### 03.8.5. Input/output and data model

Inputs are canonical structs and mutations. Outputs are hydrated structs, edge neighborhoods, versions, policies, contracts, audits, and collection results. Complete object fields are listed in the [Object and Message Registry](14-implementation-status-gaps-and-claim-boundaries.md).

### 03.8.6. Internal composition and indexes

Badger uses typed key prefixes for objects, edges, versions, policies, contracts, and source/destination edge indexes. The in-memory implementation uses maps protected by locks. `ObjectModelRegistry` describes primary-key, versioning, and indexing metadata, but does not automatically enforce those properties across every store.

### 03.8.7. Correctness/failure

- Same Badger backend can atomically apply canonical projection.
- Store interfaces mostly omit `error` on writes, limiting backend failure propagation.
- Direct CRUD can bypass Event/WAL/version/projection/audit.
- Edge endpoints have no mandatory foreign-key or schema constraint validation.
- Latest version depends on store ordering; rollback API is partial.

### 03.8.8. Claim Boundaries

Supported claim: Plasmod provides a canonical multi-object graph with version, policy, sharing, and audit records, backed by in-memory and Badger implementations.

Do not claim a graph query language, universal referential integrity, distributed transactions, arbitrary historical snapshots, or Event sourcing for every mutation path.

### 03.8.9. Gaps

Remaining gaps include error-returning mutation interfaces, a transaction abstraction for all canonical writes, referential and type constraints, a graph-traversal service, version-at-time snapshots, an Event-only public mutation policy, and shared storage-contract tests across implementations.

---

## 03.9. Tiered Storage Engine

### 03.9.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Engine |
| Original Module | Tiered Storage |
| Goals | Read, upgrade, archive, clean and rebuild objects/embedding between Hot/Warm/Cold/Archive |
| Critical-path role | ingest promotion/query fallback/archive/delete |
| Maturity | All three tiers have concrete implementations; autonomous migration and resource optimization are Partial. |

### 03.9.2. Code entry

| Item | Code |
|---|---|
| Object tiering | `storage/tiered.go` |
| Hot policy | `hot_cache_policy.go`, memory tier config |
| Cold implementations | `InMemoryColdStore`, `S3ColdStore` |
| Retrieval tiering | `dataplane.TieredDataPlane` |
| Constructor | `NewTieredObjectStoreWithThreshold`, `NewTieredDataPlaneWithEmbedderAndConfig` |
| API | S3 export/snapshot/cold purge, delete/purge, query include_cold |

### 03.9.3. Engine fields

| Type/field | Meaning |
|---|---|
| `HotEntry` | object ID/type/payload, salience, access count, last access, insert time |
| `HotObjectCache.mu/entries/order/maxSize/policy` | bounded concurrent cache and eviction policy |
| `TieredObjectStore.hot` | Hot cache |
| `.warm` | canonical ObjectStore |
| `.warmEdge` | warm graph edge store |
| `.cold` | S3 or in-memory cold store |
| `.embedder` | archive/reindex embedding generation |
| `.hotThreshold` | promotion threshold |
| `TieredDataPlane` fields | hot index, warm plane, embedder, cold search functions, RRF K |

### 03.9.4. Methods/input/output

| Operation | Input | Output/side effect |
|---|---|---|
| Hot `Put/Get/Contains/Evict/Clear/Len` | object metadata | volatile cache state |
| promote/get | Memory/ID + salience | Hot/Warm read/write |
| `ArchiveMemory` | Memory ID | cold Memory/embedding/edges and warm/hot transition per method |
| cold search | text/vector/TopK | object IDs + diagnostics |
| soft delete cleanup | Memory ID | hot eviction |
| hard delete | Memory ID | hot/warm/cold/edge cleanup |
| export/purge | selectors | S3/cold mutations/admin summary |

### 03.9.5. Placement logic

The Hot cache has a default capacity of 2,000. Its score combines salience, recency, and access count, while `HotCachePolicy` exposes weighted recency, frequency, semantic, and tier-class controls. Warm is the default canonical serving tier. Cold is written by explicit archive or reflection actions, not by every ingest, and it is queried only when requested.

Lifecycle and tier are not identical: archived lifecycle can guide cold placement, but Hot/Warm/Cold is physical state and may temporarily diverge.

### 03.9.6. Calls and sync/async

Runtime promotes synchronously after canonical commit; flush loop asynchronously persists retrieval index; reflection may archive asynchronously; admin export/purge runs request/task-specific logic; query cold path synchronous when requested.

### 03.9.7. Correctness/failure

- Cold backend selected only when required S3 env is valid, otherwise in-memory simulation.
- Archive spans object/edge/embedding stores and lacks distributed transaction.
- Soft delete leaves cold aligned until hard purge; query filtering must avoid serving stale hot payload.
- S3/network errors are operation-level and retry is caller/admin responsibility.
- Hot cache is disposable and not authoritative.

### 03.9.8. Claim Boundaries

Supported claim: real Hot/Warm/Cold routing, a bounded hot cache, an S3/MinIO cold backend, and explicit archive/retrieval/delete operations are implemented.

Do not claim a fully autonomous tier optimizer, zero-copy migration, transactional tier movement, or learned-cost reactivation and prefetch.

### 03.9.9. Gaps

Persistent tier metadata/state machine, migration job/status/retry, warm-delete-after-cold-verify invariant, resource pressure feedback, rehydration API, per-object location diagnostics and cross-tier reconciliation tests.

---

## 03.10. Canonical Object Model

The structure of authority is defined in `src/internal/schemas/canonical.go`.

| Object | Primary ID | Key relationships | Versionable | Indexable |
|---|---|---|---:|---:|
| Agent | `agent_id` | tenant/workspace/policy/capability | Yes | No |
| Session | `session_id` | agent/parent session/task | Yes | No |
| Event | `identity.event_id` | actor/causality/access/materialization | Yes | Yes |
| Memory | `memory_id` | source events/scope/provenance/lifecycle | Yes | Yes |
| AgentState | `state_id` | agent/session/state key/value | Yes | No |
| Artifact | `artifact_id` | session/owner/producer event | Yes | Yes |
| Edge | `edge_id` | source/type/target/provenance | No | No |
| ObjectVersion | object ID + version | mutation event/valid interval | N/A | No |
| PolicyRecord | `policy_id` | object/decision/visibility | No | No |
| ShareContract | `contract_id` | ACL/consistency/merge/audit policy | No | No |
| RetrievalSegment | `segment_id` | namespace/index/storage/tier | No | Physical metadata |

`semantic.ObjectModelRegistry` stores type metadata in Go. `AgentState` is an alias for `State`; new code should use the object type name `agent_state`, while `state` remains for compatibility.

### 03.10.1. IDs and Versions

When an Event omits its ID, Gateway generates an `evt_*` ID. Memory commonly uses `mem_ + event_id`; state materializers use `state_ + agent_id + state_key`; Artifact IDs may come from the Event object or a default rule. Deterministic IDs let replay target the same canonical key, but do not guarantee exactly-once external side effects.

### 03.10.2. Atomic Projection

`storage.CanonicalProjection` may include Memory, State, Artifact, Versions, Edges, and base-edge flags. `storage.factory` binds object, edge, and version stores to the same backend; Badger can therefore commit this write set in one transaction.

### 03.10.3. Direct CRUD Boundaries

`/v1/agents`, `/v1/sessions`, `/v1/memory`, `/v1/states`, `/v1/artifacts`, `/v1/edges` POST routes are used to write store/coordinator directly, mainly for management and compatibility. Business writing that requires audit/replay/consistency should be done using Event ingest.

---

## 03.11. Event-to-Object Model

### 03.11.1. Input Normalization

`schemas.Event.UnmarshalJSON` accepts Dynamic Event v0.4 and legacy flat aliases. `NormalizeDynamicEventV04()` normalizes identity, actor, time, event, object, causality, access, materialization, retrieval, payload, runtime, and extension fields. Canonical JSON output uses the nested v0.4 form.

### 03.11.2. Default Routing

| Features of the event | Main canonical output |
|---|---|
| tool call / tool result / artifact-like | Artifact + base edges + version |
| state update / state change / checkpoint | AgentState;checkpoint can generate state versions |
| Other materializable events | Memory + base/causal edges + version |

An Event can specify `object.object_type` and `object.object_id`. `materialization.enabled` and `materialization.targets` are normalized, recorded, and available for filtering, but the active `materialization.Service` does not yet treat them as universal hard gates. Each Event ingest still creates the default Memory, ObjectVersion, and stable State. An explicit State key updates that key; otherwise `last_memory_id` is updated. Specialized State and Artifact workers perform additional actions based on event/object type and payload. Retrieval fields are normalized separately.

### 03.11.3. Memory materialization

`materialization.Service.MaterializeEvent` derives text, scope, memory type, confidence, importance, source Event, and lifecycle, then produces Memory and derivation edges. Runtime writes the canonical projection to storage before writing the retrieval record to the data plane.

### 03.11.4. State materialization

Primary Runtime and `InMemoryStateMaterializationWorker.Apply` read the State key/value and derive `CanonicalStateID` from a hash of tenant, workspace, agent, session, and key. Version increments from current ObjectStore/VersionStore records, and an existing mutation event is idempotent. The primary Runtime mutex serializes only one process, so direct CRUD and cross-process writers still require external coordination.

### 03.11.5. Artifact materialization

A tool or artifact Event derives Artifact URI, MIME type, name, and body from the Event object and payload. `content_ref=inline` records inline content metadata.

### 03.11.6. Edges and Derivation

Default memory edges include caused_by event, belongs_to_session, owned_by_agent and derived_from causal refs. Artifact also generates producer/base edges. DerivationLog saves the operation of event -> object for trace use.
