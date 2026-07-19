# Key Files

| File | Why it matters |
|---|---|
| `src/cmd/server/main.go` | 进程入口 |
| `src/internal/app/bootstrap.go` | 完整 dependency graph |
| `src/internal/access/gateway.go` | HTTP route 与协议边界 |
| `src/internal/schemas/dynamic_event.go` | Event wire compatibility |
| `src/internal/schemas/canonical.go` | canonical data contract |
| `src/internal/storage/contracts.go` | persistence abstraction |
| `src/internal/storage/factory.go` | active storage selection |
| `src/internal/storage/badger_stores.go` | persistent key layout |
| `src/internal/worker/runtime.go` | write orchestration |
| `src/internal/worker/consistency/controller.go` | visibility scheduling |
| `src/internal/materialization/service.go` | Event-derived objects |
| `src/internal/dataplane/retrievalplane/bridge.go` | Go/C++ boundary |
| `cpp/CMakeLists.txt` | native dependency feature switches |

修改这些文件时应补充调用链级测试和对应文档，而不只是局部 unit test。
