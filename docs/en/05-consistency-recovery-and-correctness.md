# 05. Consistency, Recovery, and Correctness Model

> Language: [中文](../05-consistency-recovery-and-correctness.md) | English

---

This chapter defines consistency and failure boundaries across WAL, retrieval projection, canonical storage, evidence construction, and asynchronous workers.

---

## 05.1. Consistency and Recovery Model

### 05.1.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Correctness Perspective |
| Question | How do WAL, canonical state, retrieval projections, evidence, and asynchronous workers remain verifiably aligned? |
| Maturity | Write visibility and replay foundations are Implemented; cross-module reconciliation is Partial. |

### 05.1.2. Code Entry Points

| Concern | Package / file | Interface / method |
|---|---|---|
| WAL durability/replay | `src/internal/eventbackbone/` | `WAL.Append`, `ScanWAL`, File/InMemory WAL |
| write scheduling | `src/internal/worker/consistency/controller.go` | `Submit`, projection loop, `Start` recovery |
| write state | `src/internal/worker/consistency/tracker.go` | accepted/projecting/retrying/visible/failed tracking |
| checkpoint | `src/internal/worker/consistency/checkpoint.go` | `CheckpointStore.Load/Save/Reset` |
| canonical commit | `src/internal/storage/contracts.go` and backend | `RuntimeStorage.ApplyCanonicalProjection` |
| subscriber maintenance | `src/internal/worker/subscriber.go` | queue, retry, overflow, in-memory DLQ |
| admin recovery | `src/internal/worker/runtime.go`, `src/internal/access/` admin handlers | replay/reindex/reset/purge operations |

### 05.1.3. Inputs and Outputs

| Operation | Input | Success output | Failure output/state |
|---|---|---|---|
| submit event | Event + visibility mode/deadline | LSN + accepted/visible status | admission, WAL or visibility error |
| projection | WAL entry | projection callback success + watermark/checkpoint | retrying or failed-prefix state |
| startup recovery | checkpoint + WAL scan | replayed visible prefix | startup/recovery error |
| subscriber maintenance | published event/message | derived state/index/trace/lifecycle updates | ErrorCh, overflow, in-memory DLQ |
| admin replay/reindex | range/options or canonical scan | preview/apply/rebuilt count | partial operation error |

### 05.1.4. Internal Components

#### 05.1.4.1. Source of truth and boundaries

| Data | Authority | Recovery source |
|---|---|---|
| accepted Event/order | WAL + LSN | FileWAL scan |
| canonical object graph | RuntimeStorage | WAL re-materialization/backups |
| retrieval projection | DataPlane/native/cold index | canonical scan/reindex or replay |
| evidence | Edge/Version/derivation/policy | query rebuild; cache disposable |
| async lifecycle | canonical/algorithm/audit stores | partial replay/manual operation |

#### 05.1.4.2. Write state mapping

The [Execution State and Failure Matrix](14-implementation-status-gaps-and-claim-boundaries.md) maps conceptual stages to actual Tracker state and ACK conditions. There is no single `RECEIVED..MAINTAINED` enum; Controller explicitly tracks accepted, projecting, retrying, visible, and failed prefixes.

#### 05.1.4.3. Transaction and failure model

- FileWAL append is a durability boundary.
- The backend canonical projection can be applied transactionally to Event/Memory/checkpoint State/optional Artifact/Edges/Versions.
- Canonical transaction, DataPlane ingest, cache, S3, and subscriber workers are separate failure domains; the primary callback commits canonical state before retrieval ingest.
- strict mode visibility failure does not delete the submitted WAL, but returns accepted-not-visible.
- bounded/eventual ACK indicates pending, not all queryable objects completed.

### 05.1.5. Call Relationships

The Gateway passes the Event to Runtime and the consistency Controller. The Controller appends WAL first, then invokes the Runtime projection callback synchronously or through a queue according to the selected mode. Projection commits the canonical bundle first, writes the DataPlane second, and advances watermark/checkpoint only after both succeed. The primary canonical bundle now includes stable State; the subscriber State worker primarily supports specialized apply/checkpoint calls, while tool-trace, index, and lifecycle maintenance remains asynchronous.

Strict mode waits for the main projection to become visible, but it does not wait for every subscriber worker. Bounded and eventual modes may return before projection completes. Read gates compare the observed or required LSN with the visible watermark.

### 05.1.6. Data and State

| State | Location | Persistence |
|---|---|---|
| WAL entries/LSN | Event backbone | FileWAL or memory WAL |
| accepted/visible/failed prefix | consistency tracker | process memory |
| replay checkpoint/generation | checkpoint store | file/memory/buffered |
| canonical object graph | RuntimeStorage | memory/Badger |
| projection/index | DataPlane/native/cold | Multi-backend, rebuilt |
| retry queues, slots, DLQ | Controller/subscriber | process memory |
| derivation/policy/audit | dedicated logs/stores | backend-dependent |

### 05.1.7. Correctness

Recovery APIs:

| Capability | Entry |
|---|---|
| startup recovery | `Controller.Start -> recoverFromWAL` |
| checkpoint | File/Memory/Buffered checkpoint stores |
| admin replay | `/v1/admin/replay` preview/apply |
| reindex | `/v1/admin/embeddings/reindex`, warm prebuild |
| reset/wipe | Runtime pause subscribers/controller, reset stores/WAL/checkpoint |
| purge resume | hard-delete/purge task state where applicable |

#### 05.1.7.1. Idempotency, retry and audit

Deterministic IDs and upserts support replay. State history additionally deduplicates mutation events and prevents replay of an old mutation from rolling current State backward. If canonical commit succeeds and retrieval ingest fails, retrying the same LSN reuses canonical identity and completes projection; the query gate hides records beyond the watermark. Submitting the same event ID under a new LSN returns a duplicate result without another version. The Controller retries projection/checkpoint work, while the subscriber has process-local overflow, error, and dead-letter handling. There is no durable universal dead-letter queue, divergence scanner, repair planner, or post-repair verifier.

### 05.1.8. Claim Boundaries

There are three visibility modes, checkpointed WAL recovery, deterministic replay, and dedicated reindex/purge recovery.

Do not claim ACID semantics across WAL, Badger, native indexes, and S3. A strict ACK also does not prove that every subscriber has completed.

### 05.1.9. Gaps

- No unified `WriteState` schema or durable per-stage record;
- No durable DLQ, cross-restart retry intent, or dead-letter replay API;
- No canonical/projection/evidence divergence scanner;
- No generic repair planner, durable repair progress, or post-repair verifier;
- Incomplete fault-injection coverage across all failure windows;
- The target design remains the [Reconciliation Manager](#053-reconciliation-manager).

---

## 05.2. Cross-module Consistency Mechanism

### 05.2.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Mechanism |
| Goals | Keep WAL, materialization, canonical state, projection, evidence, and maintenance aligned |
| Maturity | Visibility and replay foundations are Implemented; continuous repair is Partial/Planned. |

### 05.2.2. Source of truth and markers

| Concern | Marker/authority |
|---|---|
| accepted order | WAL LSN |
| visible prefix | consistency Tracker + checkpoint/watermark |
| canonical mutation | deterministic object ID + ObjectVersion |
| projection compatibility | object ID + embedding family/dim |
| evidence lineage | Edge/Version/derivation/policy refs |
| async processing | subscriber cursor/count/DLQ memory |

### 05.2.3. Idempotency and checks

Deterministic IDs, store upserts, and replay provide basic idempotency. Embedding compatibility checks prevent obvious vector-space mismatches. There is no unified per-object stage record, cross-store checksum, or divergence table.

### 05.2.4. Failure handling

The Controller detects projection and checkpoint failures, the Gateway maps strict-mode errors, and the subscriber captures panics in its in-memory dead-letter path. Administrative replay, reindex, and purge operations repair selected failure classes. Canonical visibility may still be reported when an auxiliary worker fails later.

### 05.2.5. Recovery planning/execution

Current repair is operation-specific: WAL replay, embedding reindex, warm prebuild, hard-delete tasks, data wipe, and startup recovery. No component computes a generic repair plan from a detected divergence type.

### 05.2.6. Audit and verification

Policy, Audit, and Derivation logs record selected semantic changes. Metrics expose queue, latency, and error counters. Post-repair verification currently relies on administrative summaries, tests, and manual queries rather than a persisted verifier result.

### 05.2.7. Correctness boundary

See the [Execution State and Failure Matrix](14-implementation-status-gaps-and-claim-boundaries.md). Visibility means that the projection callback and checkpoint have advanced; it does not imply completion of every asynchronous maintenance step.

### 05.2.8. Claim Boundaries

Supported claim: Plasmod provides WAL-based recovery, a checkpointed visible prefix, and deterministic rebuild operations for covered data.

Do not claim automatic cross-module reconciliation, a durable dead-letter store, distributed transactions, or verified self-healing.

### 05.2.9. Gaps

Add StageRecord/idempotency key registry, divergence scanner, repair planner/executor, durable DLQ, relation/evidence rebuild, verification query/checksum and audit trail for repairs.

---

## 05.3. Reconciliation Manager

### 05.3.1. Positioning

| Attribute | Definition |
|---|---|
| Type | Engine |
| Goals | Detect canonical, projection, evidence, and tier divergence; plan, execute, and verify repairs |
| Critical-path role | recovery/admin only |
| Current Maturity | Placeholder/Partial: there is no active `ReconciliationManager` type |

### 05.3.2. Existing code fragments

| Capability | Existing entry | Maturity |
|---|---|---|
| WAL recovery | `consistency.Controller.recoverFromWAL` | Full write-prefix recovery |
| replay | `Runtime.AdminReplayPreview/Apply` | Complete foundation |
| re-materialization | replay -> Runtime ingest/project | Partial |
| embedding/index rebuild | `Runtime.ReindexEmbeddings`, warm prebuild/reset | Dedicated operations implemented |
| purge repair | hard delete manager/task + tier cleanup | Partial |
| subscriber DLQ | in-memory DLQ/overflow/ErrorCh | Partial and non-durable |
| relation/evidence rebuild | replay/query delta/manual edges | Partial |
| divergence scan/repair plan/verifier | no unified implementation | Planned |

### 05.3.3. Current inputs/outputs

| Operation | Input | Output |
|---|---|---|
| replay preview/apply | from LSN + limit | counts/range/errors summary |
| reindex | canonical Memory scan + embedding spec | indexed count/error |
| purge task | dataset/source/object selectors | stage/status/counts/errors |
| startup recovery | checkpoint LSN + WAL tail | queued projection tasks |

### 05.3.4. Required target fields

The following are design requirements, not currently implemented fields:

| Field | Purpose |
|---|---|
| `scannerRegistry` | canonical/projection/relation/evidence/tier scanners |
| `checkpointReader`, `wal` | recovery boundary |
| `repairPlanner` | classify divergence and dependency order |
| `executors` | replay/re-materialize/reindex/rebuild/delete workers |
| `deadLetterStore` | durable failed repair records |
| `verificationRunner` | post-repair checks |
| `auditStore`, `metrics` | traceability/observability |
| `locks/generation` | prevent conflicting repair/reset |

### 05.3.5. Required interface

```text
Scan(scope) -> DivergenceReport
Plan(report) -> RepairPlan
Execute(ctx, plan) -> RepairResult
Verify(ctx, result) -> VerificationResult
Resume(repairID) / Cancel(repairID)
```

`RepairPlan` needs object IDs/LSN range, source of truth, ordered actions, idempotency keys, retry policy and expected postconditions.

### 05.3.6. Call relationships and state

Bootstrap, the consistency Controller, and administrative Gateway routes invoke the current recovery operations separately. They do not share a durable repair record. WAL and canonical state remain recovery authorities; projections, caches, and tiers are repair targets.

### 05.3.7. Correctness/failure

Replay with deterministic IDs is largely idempotent, but auxiliary workers and direct mutations can violate that assumption. Reindex resets in-memory retrieval and scans canonical records; an interruption requires an explicit rerun. No unified lock prevents every conflicting combination of administrative repairs.

### 05.3.8. Claim Boundaries

Supported claim: startup WAL recovery, replay, reindex, and staged purge operations are implemented.

Do not describe these operation-specific tools as a general Reconciliation Manager, automatic divergence detection, durable dead-letter repair, or verified self-healing.

### 05.3.9. Missing implementation

This Engine remains a required new design: implement durable divergence/repair schemas, scanner registry, planner/executor, relation/evidence rebuilders, DLQ, operation locking, resumability, verification and failure-injection tests before marking partial/complete system-level reconciliation.

---

## 05.4. Concurrency Model

| Area | Mechanism | Purpose |
|---|---|---|
| Gateway writes | bounded channel semaphore | Returns `503` when the write-admission limit is reached |
| WAL | implementation mutex/file append | Assign process-local LSN order and protect records |
| Consistency admission | read/write gates + append mutex | Mode switching and append ordering |
| Projection | worker-sharded queues + global slots | Parallelism is bounded by backpressure controls. |
| Bounded mode | per-shard reservation | Avoid queueing work that would already violate the shard SLA |
| Tracker/checkpoint | mutex + buffered checkpoint | Advance a contiguous visible prefix and persist progress |
| EventSubscriber | polling goroutine + drain mutex | Drain Events in visible-LSN order |
| Node manager | RWMutex | worker registration and dispatch |
| Orchestrator | four priority channels | Feed a fixed worker pool |
| Hot cache | RWMutex | cache get/put/eviction |
| Badger | transactions | concurrent KV read/write |
| Segment/index | package-level locks | Coordinate build, search, and unload |
| Shutdown | context cancellation + WaitGroup | Stop admission, drain worker, close resources |

### 05.4.1. Key Invariants

- Do not hold the write gate while performing unbounded external I/O.
- Compute bounded-mode deadlines from WAL acceptance time, not from queue-head time.
- A failed projection worker must not advance the Tracker.
- The subscriber must not consume entries beyond the Controller's visible watermark.
- A worker panic must be isolated and recorded in the dead-letter path without terminating the drain loop.

### 05.4.2. Background goroutine ownership

The Gateway owns a hard-delete manager. Runtime owns the flush loop, subscriber, consistency lifecycle, and selected outboxes. `ServerBundle.Shutdown` stops these components in dependency order before closing Badger, WAL, and the derivation store.

---

## 05.5. Consistency Model

### 05.5.1. State Definitions

- **Accepted Event**: appended to WAL and assigned an LSN.
- **Canonical visible**: the required object/edge/version projection succeeded and the Tracker marked the LSN visible.
- **Retrieval visible**: the object is reachable through the target retrieval path. Background flush or index build may make this later than canonical visibility.
- **Evidence visible**: the edges, versions, and cache fragments required by the query have been assembled.

### 05.5.2. Modes

| Mode | Writing back | Reading behavior | Applicable scenarios |
|---|---|---|---|
| `strict_visible` / `strict` | Wait for projection to become visible; return accepted-not-visible or projection errors explicitly | Wait for target visibility | The next decision must read its own write. |
| `bounded_staleness` / `bounded` | Reserve a shard slot and project within the freshness SLA | Wait up to the configured limit | Bounded stale reads are acceptable. |
| `eventual_visibility` / `eventual` | Project asynchronously after WAL acceptance | Do not wait for the latest LSN | Throughput is prioritized and callers can retry. |

### 05.5.3. Configuration

The default mode is strict. Configuration covers `PLASMOD_CONSISTENCY_DEFAULT_MODE`, `BOUNDED_MAX_LAG`, `QUEUE_SIZE`, `WORKERS`, retry intervals, query and shutdown timeouts, checkpoint paths, and flush intervals.

### 05.5.4. Concurrency and Ordering

The Controller uses an admission gate, mode gate, append mutex, global slots, and sharded worker queues. Bounded mode allows one reserved task per shard to avoid accepting a queue that cannot meet its deadline.

### 05.5.5. Mode Switching

The admin consistency-mode operation changes the runtime default without rewriting the resolved mode stored on existing Events. During a switch, the Controller uses gate-and-drain behavior to prevent mixed-mode admission.

### 05.5.6. Recovery

In disk mode, the default checkpoint lives in the data directory. A fresh durable deployment initializes to the current WAL tail; subsequent startups scan from checkpoint + 1. When a checkpoint exists, later entries must be replayed, and scan or decode errors terminate recovery.

### 05.5.7. Limitations

The model restricts Event ingest main path; direct canonical CRUD does not automatically obtain the same WAL/visibility assurance.

---

## 05.6. Failure Model

### 05.6.1. Before WAL

JSON decoding, schema/consistency validation, or Gateway semaphore admission can fail before WAL. The Event has no LSN at this point, so the client may correct the request or retry with the same Event ID.

### 05.6.2. WAL append

File I/O, encoding, or sync can fail during WAL append. The server cannot report the Event as accepted. Operators should inspect data-directory permissions, disk space, and WAL health.

### 05.6.3. After WAL, before visibility

The Event has an LSN, but its projection queue, worker, canonical write, or retrieval ingest failed. For `AcceptedNotVisibleError` or `ProjectionFailureError`, the client must retain the event ID and inspect or replay the accepted Event rather than generating a new ID blindly.

### 05.6.4. Canonical projection failure

When objects, edges, and versions share the Badger backend, an encoding or write failure rolls back their canonical projection transaction. The storage factory prohibits splitting these stores across backends because that configuration cannot preserve the same atomicity guarantee.

### 05.6.5. Retrieval projection failure

Canonical data may be durable while the retrieval index is stale. The system should expose the error or visibility state and must not treat the index as the only source of truth.

### 05.6.6. Evidence failure

A query may return canonical object IDs even when optional evidence enrichment is incomplete. Callers that require evidence must inspect proof and provenance fields rather than relying on HTTP `200` alone.

### 05.6.7. Async worker failure

A subscriber-handler panic is captured and placed in the process-local dead-letter or overflow path. Ordinary error behavior remains worker-specific.

### 05.6.8. Shutdown during operation

The Controller stops admission, cancels queued work, and waits for active workers and checkpoint flush. A timeout returns a shutdown error; Badger must remain open while dependent resources are still in use.

### 05.6.9. Replay failure

WAL scan/decode, legacy-schema, embedding-family, or projection errors may interrupt replay. Run preview first, back up the data directory, and then apply; never hide invalid records by deleting checkpoints.
