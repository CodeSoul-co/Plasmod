# Package Index

| Package | Active role | Entry files |
|---|---|---|
| `internal/app` | 组装依赖、启动/关闭 server | `bootstrap.go`, `ports.go`, `run.go` |
| `internal/access` | HTTP Gateway、安全和输出可见性 | `gateway.go`, `admin_auth.go`, `visibility.go` |
| `internal/schemas` | Event、canonical、query schema | `dynamic_event.go`, `canonical.go`, `query.go` |
| `internal/eventbackbone` | WAL、Bus、derivation log | `contracts.go`, file/memory WAL |
| `internal/worker` | Event runtime、物化、consistency | `runtime.go`, `consistency/*`, `nodes/*` |
| `internal/materialization` | 默认 Memory/Artifact/Edge 派生 | `service.go` |
| `internal/storage` | RuntimeStorage、Badger、S3、tier | `contracts.go`, `factory.go`, `badger_stores.go` |
| `internal/dataplane` | embedding、warm/cold retrieval | `contracts.go`, `vectorstore.go`, `retrievalplane/*` |
| `internal/semantic` | query planning/operator | `operators.go` |
| `internal/evidence` | evidence/proof 组装 | assembler files |
| `internal/coordinator` | active object/index/policy coordinators | `hub.go`, top-level coordinator files |
| `internal/transport` | 组件 RPC 和 WAL stream | `server.go` |

更细说明见 [`packages/README.md`](packages/README.md)。
