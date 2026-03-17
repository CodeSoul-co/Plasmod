# CogDB Architecture Design

## 1. System Overview

CogDB is an **agent-native database** built for multi-agent systems (MAS).  Its
core goal is to unify agent memory, state, event, and artifact into a single
object system that makes memory *fast to activate*, *evidence-traceable*, and
*governance-controlled*.

```
┌─────────────────────────────────────────────────────┐
│                   HTTP Gateway                       │  /v1/ingest  /v1/query  /v1/agents …
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│               Worker Runtime                         │  SubmitIngest · ExecuteQuery · Topology
│  ┌─────────────────┐  ┌───────────────────────────┐ │
│  │  Materializer   │  │   PreComputeService        │ │  ← runs at ingest time
│  └────────┬────────┘  └──────────┬────────────────┘ │
│           │                      │ builds EvidenceFragment
│  ┌────────▼──────────────────────▼────────────────┐ │
│  │            EvidenceCache  (hot)                 │ │  pre-assembled proof chains
│  └────────────────────────────────────────────────┘ │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│              Tiered Data Plane                       │
│  ┌──────────┐  ┌──────────┐  ┌────────────────────┐ │
│  │ Hot Index│→ │ WarmPlane│→ │  ColdPlane (disk)  │ │
│  │(in-memory│  │(in-memory│  │  (file / RocksDB)  │ │
│  │ growing) │  │ all shards│  │  archived objects  │ │
│  └──────────┘  └──────────┘  └────────────────────┘ │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│              Storage Layer                           │
│  HotObjectCache │ ObjectStore │ GraphEdgeStore       │
│  PolicyStore    │ VersionStore│ ShareContractStore   │
└─────────────────────────────────────────────────────┘
```

---

## 2. Naming — Origin of Borrowed Code

Several sub-directories contain code ported or adapted from open-source
infrastructure projects.  Their **internal package names** are preserved in the
borrowed sub-trees, but the **integration boundary** files (always named
`contracts.go`) expose only CogDB/ANDB-native type names.

| Directory | Role | Build tag | CogDB boundary |
|---|---|---|---|
| `coordinator/controlplane/` | distributed control plane | `extended` | `controlplane.RuntimeContract` |
| `eventbackbone/streamplane/` | distributed stream plane | `extended` | `streamplane.RuntimeContract` |
| `dataplane/retrievalplane/` | C++ retrieval engine | CMake only | not imported by Go |
| `platformpkg/` | shared platform utilities | `extended` | not imported by Go |
| `dataplane/segmentstore/` | CogDB-native segment store | always | `segmentstore.Index` / `Shard` |

The `extended` build tag means those files are **never compiled** in a
standard `go build ./...`.  The segment-store code compiled by default
has been fully renamed to CogDB conventions:

| Old name | CogDB name |
|---|---|
| `Partition` | `Shard` |
| `PartitionState` | `ShardState` |
| `PartitionMeta` | `ShardMeta` |
| `NewGrowingPartition` | `NewGrowingShard` |
| `Row` | `ObjectRecord` |
| `SearchHit.Partition` | `SearchHit.ShardID` |
| `Plan.CandidatePartitions` | `Plan.CandidateShards` |

---

## 3. Memory Activation — Hot / Warm / Cold Tiers

### 3.1 Design Goal

> "Agent should be able to activate memories faster — the DB pre-computes
> evidence chains at ingest time, and the hot path is kept fully in-memory."

### 3.2 Three Tiers

```
Tier        Storage            Latency    Contents
────────────────────────────────────────────────────────────
Hot         HotObjectCache     ~0 µs      Current session memories, salience ≥ 0.5
            HotSegmentIndex               Growing shards of active namespaces

Warm        MemoryRuntimeStorage ~0 µs   All objects since process start
            SegmentDataPlane (warm)       All sealed and growing shards

Cold        InMemoryColdStore  simulated  Archived / TTL-expired objects
            SegmentDataPlane (cold)       Archived sealed shards
```

In production the cold tier would be backed by **RocksDB**, an object store
(S3 / GCS), or a file-based LSM tree.  The current `InMemoryColdStore` is a
structural placeholder that models the architectural boundary.

### 3.3 Ingest Path (hot path)

```
Event
  → WAL.Append()                           log + assign LSN
  → Materializer.ProjectEvent()            Event → IngestRecord
  → PreComputeService.Compute()            build EvidenceFragment, store in EvidenceCache
  → HotObjectCache.Put()                   if salience ≥ 0.5
  → NodeManager.DispatchIngest()           fan-out to data/index nodes
  → TieredDataPlane.Ingest()               write to hot index + warm plane
```

### 3.4 Query Path (tiered)

```
QueryRequest
  → QueryPlanner.Build()                   → QueryPlan
  → TieredDataPlane.Search()
      1. HotIndex.Search()                 fastest; only if result count ≥ TopK
      2. WarmPlane.Search()   (if needed)  merge with hot results
      3. ColdPlane.Search()   (if IncludeCold=true)
  → Assembler.Build(result, filters)
      1. EvidenceCache.GetMany(objectIDs)  pre-computed fragments (fast)
      2. assembleFromFragments()           merge salience + edges + policy filters
      3. append delta scanned-shard trace  fill any cache misses
  → QueryResponse
```

### 3.5 Tier Field in SearchOutput

Every `SearchOutput` carries a `Tier` string:

| Value | Meaning |
|---|---|
| `"hot"` | All results served from hot index |
| `"hot+warm"` | Hot insufficient; warm consulted |
| `"hot+warm+cold"` | Time-travel / historical query |

---

## 4. Pre-Computed Evidence (DB-Side Pre-Computation)

### 4.1 EvidenceFragment

Built by `PreComputeService.Compute()` at ingest time for every ingested object:

```go
type EvidenceFragment struct {
    ObjectID      string    // canonical object key
    ObjectType    string    // event_type / object_type
    Namespace     string
    TextTokens    []string  // tokenised, deduplicated text
    RelatedIDs    []string  // agent, session, causal refs
    EdgeTypes     []string  // "derived_from", "causal"
    PolicyFilters []string  // filters applied at ingest (agent_id:x, visibility:y)
    SalienceScore float64   // composite of Importance + recency + causal density
    Level         int       // memory level (0 = raw event, 1 = extracted, 2 = consolidated)
    ComputedAt    time.Time
    LogicalTS     int64     // WAL LSN at ingest time
}
```

### 4.2 EvidenceCache

Bounded LRU in-memory cache (default 10 000 entries).  Used by the Assembler
to serve pre-built proof chains without re-deriving from raw events.

At query time the Assembler calls `cache.GetMany(objectIDs)` and merges:
- **Pre-computed part**: fragment tokens, edges, policy filters (fast)
- **Delta part**: scanned shard IDs from the current search (current)

### 4.3 SalienceScore Formula

```
base  = ev.Importance  (0–1, default 0.5)
+0.1  if len(TextTokens) > 10
+0.1  if len(CausalRefs) > 0
+0.2  if Visibility == "global"
capped at 1.0
```

Objects with `SalienceScore ≥ 0.5` are immediately promoted to `HotObjectCache`.

---

## 5. Coordinator Hub

Nine coordinators wired in `coordinator.Hub`:

| Coordinator | Responsibility |
|---|---|
| `SchemaCoordinator` | Object type registry and field introspection |
| `ObjectCoordinator` | CRUD delegation + snapshot version recording |
| `PolicyCoordinator` | Policy record management + governance queries |
| `VersionCoordinator` | Logical clock, time-travel, visibility publication |
| `WorkerScheduler` | Worker type dispatch tracking and stats |
| `MemoryCoordinator` | Memory object lifecycle (level 0→1→2) |
| `IndexCoordinator` | Retrieval segment + index metadata management |
| `ShardCoordinator` | Ingest → WAL channel + namespace mapping |
| `QueryCoordinator` | Entry-point for semantic query plan building |

---

## 6. Event Backbone

```
WAL (InMemoryWAL)
  Append() → LSN assignment via HybridClock
  Scan(fromLSN) → bounded-staleness replay
  LatestLSN() → current watermark

Bus (InMemoryBus)
  Publish / Subscribe channels
  Used for fanout to materializer, workers

WatermarkPublisher → broadcasts advancing time-tick
DerivationLog      → records derivation steps for proof-trace assembly
PolicyDecisionLog  → governance audit log (append-only)
```

---

## 7. Worker Node Contracts (14 types)

Per spec section 16.4:

| ID | Type | Interface |
|---|---|---|
| 1 | DataNode | ingest, flush, metadata |
| 2 | IndexNode | build, update, metadata |
| 3 | QueryNode | search, metadata |
| 4 | MemoryExtractionWorker | extract memories from events |
| 5 | MemoryConsolidationWorker | merge/consolidate memory levels |
| 6 | ReflectionPolicyWorker | apply reflection policies |
| 7 | ConflictMergeWorker | resolve cross-agent conflicts |
| 8 | MaterializationWorker | project events → ingest records |
| 9 | EmbeddingWorker | compute vector embeddings |
| 10 | GraphRelationWorker | extract and persist edges |
| 11 | ProofTraceWorker | assemble and persist proof traces |
| 12 | SegmentSealWorker | seal growing shards on threshold |
| 13 | CompactionWorker | compact cold-tier shards |
| 14 | ReplayWorker | WAL replay from a given LSN |

---

## 8. HTTP API Surface

| Method | Path | Purpose |
|---|---|---|
| GET | /healthz | Liveness probe |
| POST | /v1/ingest/events | Submit event |
| POST | /v1/query | Semantic query |
| GET | /v1/admin/topology | Node topology |
| GET/POST | /v1/agents | List / create agents |
| GET/POST | /v1/sessions | List / create sessions |
| GET/POST | /v1/memory | List / create memories |
| GET/POST | /v1/states | List / create states |
| GET/POST | /v1/artifacts | List / create artifacts |
| GET/POST | /v1/edges | List / create edges |
| GET/POST | /v1/policies | List / append policies |
| GET/POST | /v1/share-contracts | List / create share contracts |

---

## 9. Extensibility Points

| Layer | Extension mechanism |
|---|---|
| Storage | Implement `RuntimeStorage` interface; swap `MemoryRuntimeStorage` |
| Data Plane | Implement `DataPlane` interface; swap `TieredDataPlane` for extended-plane adapter |
| Cold Store | Implement `ColdObjectStore`; replace `InMemoryColdStore` |
| Evidence | Implement `evidence.Cache`-compatible store (Redis, etc.) |
| Workers | Implement any worker interface; call `manager.Register*()` |
| Coordinators | Implement new coordinator; add to `Hub` struct |
| Query Operators | Add new `QueryPlan` subtypes in `semantic/operators.go` |
| Control Plane | Enable `extended` build tag to compile the distributed control plane |
