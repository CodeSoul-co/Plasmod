# Package Dependency Graph

```text
cmd/server
  -> internal/app
      -> access -> worker/coordinator/schemas/storage
      -> transport -> worker runtime API
      -> worker -> eventbackbone/materialization/dataplane/storage
      -> semantic -> schemas
      -> evidence -> storage/schemas
      -> dataplane -> storage/retrievalplane/schemas
      -> storage -> schemas/eventbackbone
```

`schemas` 位于低层，不依赖 Gateway。`app` 是 composition root。C++ library 只通过 retrievalplane bridge 进入
Go graph。
