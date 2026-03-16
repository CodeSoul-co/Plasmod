# Phase-2 Step 1: Segment Planning Path

Implemented in this step:

1. Segment metadata model
- Segment state, row count, min/max event timestamp
- Metadata snapshot for planner and trace output

2. Planner-driven candidate selection
- Namespace filtering
- Time-window overlap filtering
- Growing/sealed segment inclusion flag

3. Execution path upgrade
- Runtime now executes: planner -> candidate segments -> search executor
- Query response proof trace now includes planned segment entries

4. Ingest-to-segment timestamp propagation
- Event timestamp parsed and stored on rows
- Segment min/max timestamps updated during ingest

Primary code paths:
- `src/internal/dataplane/segmentstore/planner.go`
- `src/internal/dataplane/segmentstore/segment.go`
- `src/internal/dataplane/segmentstore/search.go`
- `src/internal/dataplane/segment_adapter.go`
- `src/internal/worker/runtime.go`
