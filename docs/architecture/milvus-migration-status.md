# Milvus Migration Status (Integrated Layout)

This file answers: "Has Milvus core code and capability been fully migrated into ANDB?"

## Short Answer
No. The migration is **in progress**.

## Integrated Source Layout (No Standalone Third-Party Folder)
Milvus-derived source code is distributed into ANDB module areas:
- `src/internal/dataplane/retrievalplane`
- `src/internal/coordinator/controlplane`
- `src/internal/eventbackbone/streamplane`
- `src/internal/platformpkg/pkg`

This layout keeps source close to ANDB runtime layers for direct iterative porting.

## Current Completion Estimate
- Source integration readiness: 75%
- Runtime capability parity: 15%

## What Is Already Implemented in Runtime
- Embedded segment lifecycle baseline under `src/internal/dataplane/segmentstore`
- Search execution baseline and query planner seam
- Stable runtime contracts for WAL/Bus/DataPlane/Planner/Policy

## Remaining Gaps Before "Core Migration Complete"
1. Segment and compaction parity
2. Query execution plan and index-path parity
3. Index lifecycle and metadata parity
4. TSO and snapshot consistency semantics parity
5. Binlog/object storage materialization pipeline parity
