# Claim and Test Boundary

本页把可声明的系统能力绑定到代码和测试。这里的“声明”是核心系统设计边界，不包含外部实验数据或结果。

## Supported Claims

| Core claim | Code path | Primary tests | Boundary |
|---|---|---|---|
| Event-first durable ingest with replayable LSN | Gateway -> Runtime -> consistency -> WAL | `eventbackbone/*_test.go`, `worker/consistency/*_test.go`, runtime tests | disk mode才是进程重启后持久 WAL |
| Event derives canonical Memory/State/Artifact/Edge/Version | materialization service + specialized workers | materialization and worker materialization tests | targets/enabled 不是全局硬 gate |
| Canonical truth is separated from retrieval projection | storage contracts + DataPlane | storage projection tests, dataplane tests | 两平面无跨系统 ACID transaction |
| strict/bounded/eventual visibility modes | consistency controller/tracker/checkpoint | consistency mode/controller/tracker tests | guarantee 由单进程 controller 范围定义 |
| hybrid/tiered retrieval with optional native ANN | TieredDataPlane + vector/sparse/segment/native bridge | dataplane/retrievalplane tests | native index depends build tag/library；cold only explicit query |
| evidence-bearing query response | planner -> retrieval -> assembler -> QueryChain | evidence, semantic, worker e2e query tests | response objects 是 IDs；policy annotation 不等于完整 ACL enforcement |
| pluggable memory management algorithm | interface + dispatcher + baseline/MemoryBank-style/Zep-style | cognitive tests | style implementation 不等价于外部产品服务 |
| canonical graph and provenance records | Edge/Version/derivation stores + proof worker | schema graph, evidence, coordination tests | graph validity is application-enforced, not referential constraint |
| explicit share contract records and shared copies | ShareContractStore + communication/conflict worker | governance/coordination tests | read ACL主要用于 contamination observation；未统一强制所有读写/derive |
| hot/warm/cold object management | TieredObjectStore + S3/InMemory cold | storage tiered/S3 tests | promotion/demotion mostly explicit/policy worker driven |

## Claims Requiring Qualification

| Phrase | Required qualification |
|---|---|
| “四条 Chain 统一调度所有请求” | 不成立；四个 type 存在，但 Gateway 主路径不经过 Orchestrator |
| “智能资源调度” | 不成立；现有是 consistency queues、固定优先级 Orchestrator 和计数型 WorkerScheduler |
| “完整 Reconciliation Manager” | 不成立；只有 replay/reindex/purge/checkpoint 等分散能力 |
| “全链路 ACL 强制” | 不成立；存在 policy/share schema、过滤与检测，但非所有路径统一 enforce |
| “任意 Event target 可配置派生任意对象” | 不成立；materializer 是明确的 deterministic 规则 |
| “Evidence 完整可重放” | 需限定：Edge/Version/derivation 可持久，Evidence cache/proof response 可重算但 cache 不持久 |
| “跨存储事务一致” | 不成立；Badger canonical projection 可原子，native index/S3/cache 在边界之外 |
| “distributed multi-tenant scheduler” | 不成立；active core 是单进程 worker/node manager 架构 |

## Test Coverage Map

| Design area | Test directories/files | Remaining gaps |
|---|---|---|
| bootstrap/listeners/shutdown | `src/internal/app/*_test.go` | full process crash matrix |
| HTTP/auth/visibility/routes | `src/internal/access/*_test.go` | every internal route contract |
| schema/ID/graph | `src/internal/schemas/*_test.go` | migration compatibility corpus |
| WAL/bus/derivation/watermark | `src/internal/eventbackbone/*_test.go` | long-running corruption recovery |
| canonical storage/tiering/S3 | `src/internal/storage/*_test.go` | cross-store fault injection |
| materialization | `src/internal/materialization/*_test.go`, worker materialization tests | all event subtype combinations |
| Runtime/query/ingest | `src/internal/worker/runtime*_test.go`, `e2e_query_test.go` | multi-process and sustained failure |
| consistency | `src/internal/worker/consistency/*_test.go` | restart at every projection boundary |
| retrieval/embedding/native | `src/internal/dataplane/*_test.go`, retrievalplane tests, C++ tests | optional backend parity |
| evidence/planner/policy | evidence/semantic tests | complete scope/ACL matrix and evidence completeness metric |
| chains/orchestrator/nodes | chain, orchestrator, manager tests | prove Gateway integration if unified routing is added |
| algorithm plugins | cognitive tests | profile migration and projection refresh assertions |

## Documentation Review Gate

新增系统声明前必须同时提供：

1. active bootstrap 构造或明确的调用入口；
2. interface/type/method 的代码路径；
3. state mutation 和 failure boundary；
4. primary test；
5. 本页与对应 Architecture/Chain/Mechanism/Engine 的同步更新。
