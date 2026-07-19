# Repository Tree

```text
Plasmod/
├── src/
│   ├── cmd/server/main.go
│   └── internal/
│       ├── access/          # HTTP gateway, auth, response visibility
│       ├── app/             # dependency wiring and server lifecycle
│       ├── coordinator/     # active lightweight coordinators + upstream snapshot
│       ├── dataplane/       # embedding, retrieval, tiered query
│       ├── eventbackbone/   # WAL/bus/derivation + upstream streamplane
│       ├── evidence/        # evidence assembly
│       ├── materialization/ # event to canonical objects
│       ├── schemas/         # wire and canonical types
│       ├── semantic/        # query planning/operators
│       ├── storage/         # Badger, memory, S3, tiering
│       ├── transport/       # internal RPC/WAL stream
│       └── worker/          # runtime, materializers, consistency
├── cpp/                     # native retrieval and vendored source
├── sdk/
├── configs/
├── scripts/
├── docs/
├── Makefile
├── Dockerfile
└── docker-compose*.yml
```

运行产生的 `.andb_data`、`.gocache`、`cpp/build` 和 `bin` 不是源码模块。
