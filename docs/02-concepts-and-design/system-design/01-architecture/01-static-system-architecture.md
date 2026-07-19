# 1. Static System Architecture

## 1. 定位

| 项 | 结论 |
|---|---|
| 类型 | Architecture |
| 设计目标 | 说明 active Plasmod 进程由哪些 layer、subsystem、module 和 canonical object 构成 |
| 关键路径 | 是，覆盖 bootstrap、读写、存储和后台维护 |
| 当前成熟度 | 部分：核心单进程组合完整；统一分布式 control plane 不是 active 默认路径 |

## 2. 代码入口

| 入口 | 代码 |
|---|---|
| Process main | `src/cmd/server/main.go` |
| Composition root | `src/internal/app/bootstrap.go: BuildServer` |
| HTTP boundary | `src/internal/access/gateway.go` |
| Runtime | `src/internal/worker/runtime.go`, `runtime_consistency.go` |
| Shared schemas | `src/internal/schemas/` |
| Shutdown | `ServerBundle.Shutdown`, `app.RunServers` |

## 3. Layer 与模块

| Layer | Active packages/modules | 性质 | Runtime/Bootstrap ownership |
|---|---|---|---|
| Interface | `access.Gateway`, gRPC server, `transport.Server`, SDK | 独立协议边界 | Bootstrap 构造 server；Gateway 持有 Runtime/store |
| Runtime & Control | `worker.Runtime`, consistency Controller, `nodes.Manager`, active `coordinator.Hub`, Orchestrator | concrete service + registry | Runtime 持有核心依赖；Orchestrator 单独注册 |
| Event & Causality | WAL, Bus, clock/watermark, derivation/policy decision log, subscriber | interface + concrete implementation | Runtime/Controller/Subscriber 持有 |
| Canonical Object | schemas, materialization service/workers, graph/version/policy records | type + derivation logic | Runtime 调 materializer；workers 处理辅助对象 |
| Storage & Retrieval | RuntimeStorage, Badger/memory stores, TieredObjectStore, TieredDataPlane, native bridge | replaceable interfaces + adapters | Bootstrap 按 env 选择 |
| Cross-cutting | policy, evidence, metrics, consistency, auth/visibility, algorithm plugins | shared services | Runtime/Gateway/worker 持有 |
| Shared Object Model | Event, Agent, Session, Memory, State, Artifact, Edge, Version, Policy, ShareContract | low-level schema contract | 被所有层引用，不持有行为 |

## 4. 内部组成与依赖方向

```text
cmd/server
  -> app
     -> access / grpc / transport
     -> worker Runtime / consistency / nodes
     -> coordinator Hub
     -> semantic / materialization / evidence
     -> dataplane -> retrievalplane -> C++ bridge
     -> storage -> schemas
     -> eventbackbone -> schemas
```

`schemas` 位于低层；`app` 是唯一 composition root。Active package graph 未发现由 lower layer 反向 import Gateway 的循环。`transport.RuntimeAPI` 用最小 interface 避免 transport/worker import cycle。

## 5. 可替换边界

| Capability | Replacement interface | 当前选择点 |
|---|---|---|
| WAL | `eventbackbone.WAL` | storage mode/WAL persistence |
| Canonical stores | `storage.RuntimeStorage` 及子接口 | `storage.BuildRuntimeFromEnv` |
| Cold store | cold object contract | S3 env 是否完整 |
| Retrieval | `dataplane.DataPlane` | bootstrap constructor |
| Embedder | embedding generator interfaces | `PLASMOD_EMBEDDER` |
| Query planning | `semantic.QueryPlanner` | bootstrap constructor |
| Memory algorithm | `MemoryManagementAlgorithm` | algorithm config/profile |
| Worker | `nodes` interfaces/`Runnable` | NodeManager registration |

## 6. 数据与状态

| State class | Source of truth | Volatile derivatives |
|---|---|---|
| Event order | FileWAL/InMemoryWAL LSN | subscriber cursor, controller queues |
| Canonical objects | RuntimeStorage object/edge/version/policy/contract stores | Hot cache, Query nodes |
| Retrieval | disposable projection over canonical/Event | segment/native index handles |
| Evidence | Edge/Version/derivation/policy records | evidence cache and response proof trace |
| Algorithm | MemoryAlgorithmStateStore + Memory fields | plugin in-memory state |
| Scheduling | controller/tracker/checkpoint | queues, slots, counters |

完整字段见 [Object and Message Registry](../06-cross-reference/object-and-message-registry.md)。

## 7. 正确性

- 同一 Badger backend 内 canonical projection 可原子写 object/edge/version；native index、S3 和 cache 不在该事务中。
- WAL + deterministic IDs 提供 replay 基础。
- consistency Controller 是写入可见性真实 gate。
- 上游/兼容目录不因存在大量代码而自动成为 active architecture。

## 8. 声明边界

可声明：Plasmod 是 Event、canonical object graph、retrieval projection、evidence 和 tiered storage 组合的单进程 agent-native data runtime。

不可声明：默认进程已完整启用 imported distributed controlplane/streamplane，或所有 layer 都是独立部署服务。

## 9. 缺口

| Gap | Required work |
|---|---|
| logical layer 和 deployable subsystem 混用 | 明确 service/process boundary 后再拆分 |
| Orchestrator 未接主请求路径 | 决定整合或移除 active claim |
| distributed ownership 未形成 | 增加 leader/shard/task lifecycle contract 与运行测试 |
| 部分 object type 注册但持久/查询能力有限 | 补 storage/API/materialization contract tests |
