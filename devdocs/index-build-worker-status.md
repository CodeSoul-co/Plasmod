# IndexBuildWorker Integration Status

> **Owner:** Member B (Retrieval / Indexing)
> **Date:** 2026-03-26
> **File:** `src/internal/worker/indexing/build.go`

---

## What IndexBuildWorker Does

`InMemoryIndexBuildWorker` writes a materialised object into two metadata stores:

| Store | Type | Purpose |
|---|---|---|
| `storage.SegmentStore` | Metadata | Records which segment/time-bucket an object belongs to |
| `storage.IndexStore` | Metadata | Tracks per-namespace indexed object count |

**It is a segment metadata tracker, not a secondary retrieval index.**

The worker does **not** write to `TieredDataPlane` or the CGO Knowhere/HNSW vector index. Its output (`SegmentID`, `IndexedCount`) is used for topology reporting (`/v1/admin/topology`) and segment bookkeeping only.

---

## Where It Is Called

`IndexBuildWorker` is registered in the worker `Manager` and dispatched through the **Main Chain** (`src/internal/worker/chain/chain.go`):

```
IngestWorker
  → WAL.Append
  → ObjectMaterializationWorker
  → ToolTraceWorker
  → IndexBuildWorker   ← here (segment metadata write)
  → GraphRelationWorker
```

In `Runtime.SubmitIngest` (`src/internal/worker/runtime.go`), it is called via `DispatchIndexBuild` after materialisation completes.

---

## The Actual Retrieval Path (TieredDataPlane)

Queries do **not** go through `IndexBuildWorker`. The query path is:

```
Runtime.ExecuteQuery
  → TieredDataPlane.Search(SearchInput)
       ├── Hot tier:  segmentstore.Index  (in-memory lexical, fast)
       └── Warm tier: SegmentDataPlane.Search
                          ├── VectorStore.Search()     ← CGO Knowhere/HNSW (primary)
                          └── segmentstore.Index       ← lexical fallback
```

`TieredDataPlane` is populated at ingest time by `Runtime.SubmitIngest → plane.Ingest(record)`, which calls `SegmentDataPlane.Ingest()` → writes to `segmentstore.Index` and `VectorStore.AddText()`.

---

## Does IndexBuildWorker Need to Feed the Retrieval Path?

**No. Current architecture is correct.**

The separation is intentional:

| Component | Role |
|---|---|
| `IndexBuildWorker` | Segment **metadata** tracker (bookkeeping, topology) |
| `TieredDataPlane.Ingest` | Actual **retrieval index** population (lexical + vector) |

`TieredDataPlane` is already populated synchronously in `SubmitIngest` (line ~181 in `runtime.go`) before `IndexBuildWorker` is even called. Routing `IndexBuildWorker` output into `TieredDataPlane` would create a double-write with no benefit.

---

## Vector Search Integration (E3 Status)

As of 2026-03-26, the CGO Knowhere/HNSW retriever **is** wired into the query path via:

```
SegmentDataPlane.Search()
  └── VectorStore.Search(queryVec, topK)
        └── retrievalplane.Retriever.Search()   ← CGO bridge
```

When `CGO_ENABLED=0` or the C++ library is not built, `VectorStore.Ready()` returns `false` and search falls back to `segmentstore.Index` (lexical) transparently.

**E3 is resolved.** The `bridge_stub.go` in `src/internal/dataplane/retrievalplane/` provides compile-time safety without CGO.

---

## Summary

| Question | Answer |
|---|---|
| Is IndexBuildWorker called in SubmitIngest? | ✅ Yes, via MainChain after materialisation |
| Does it write to TieredDataPlane? | ❌ No — it writes segment/index metadata only |
| Is it a secondary retrieval index? | ❌ No — it is a bookkeeping tracker |
| Should it be wired into ExecuteQuery? | ❌ No — TieredDataPlane handles retrieval directly |
| Is vector search (Knowhere) wired in? | ✅ Yes — via VectorStore → CGO retrievalplane.Retriever |
