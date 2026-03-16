# Milvus Source Map to ANDB Modules

## Source Integration Map

- Data plane related Milvus-derived sources:
  - `src/internal/dataplane/retrievalplane/core`
  - `src/internal/dataplane/retrievalplane/queryruntime`
  - `src/internal/dataplane/retrievalplane/storage*`
  - `src/internal/dataplane/retrievalplane/storageshared`
  - `src/internal/dataplane/retrievalplane/objectstore`
  - `src/internal/dataplane/retrievalplane/compaction`

- Coordinator related Milvus-derived sources:
  - `src/internal/coordinator/controlplane/coordinator`
  - `src/internal/coordinator/controlplane/metacontrol`
  - `src/internal/coordinator/controlplane/datacontrol`
  - `src/internal/coordinator/controlplane/querycontrol`
  - `src/internal/coordinator/controlplane/accessproxy`

- Event backbone related Milvus-derived sources:
  - `src/internal/eventbackbone/streamplane/clockservice`
  - `src/internal/eventbackbone/streamplane/streamcoord`
  - `src/internal/eventbackbone/streamplane/streamnode`
  - `src/internal/eventbackbone/streamplane/flushpipeline`

- Shared platform package sources:
  - `src/internal/platformpkg/pkg/*`

## Build Isolation
Integrated source subtrees are isolated from the main ANDB module using nested `go.mod` files:
- `src/internal/dataplane/retrievalplane/go.mod`
- `src/internal/coordinator/controlplane/go.mod`
- `src/internal/eventbackbone/streamplane/go.mod`
- `src/internal/platformpkg/go.mod`

## Porting Rule
As code is adapted into ANDB runtime modules, move implementation behind ANDB contracts:
- `src/internal/dataplane/contracts.go`
- `src/internal/eventbackbone/contracts.go`
- `src/internal/dataplane/retrievalplane/contracts.go`
- `src/internal/coordinator/controlplane/contracts.go`
- `src/internal/eventbackbone/streamplane/contracts.go`
