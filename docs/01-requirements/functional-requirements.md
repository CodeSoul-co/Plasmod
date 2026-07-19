# 功能需求

下表采用可追踪的精简格式；详细代码/测试映射见 [Requirements Traceability](requirements-traceability.md)。

## FR-ING-001 Event 接受

### Requirement
系统必须接受 Dynamic Event v0.4，并兼容已实现的旧扁平输入别名；缺失 event ID 时生成 ID。

### Rationale
统一新写入语义，同时保留迁移能力。

### Inputs
`schemas.Event` JSON。

### Expected Behavior
规范化后写入 WAL，返回 event ID、LSN 和 projection/visibility 结果。

### Failure Behavior
无效 JSON、无效 consistency 或写入 backpressure 返回非 2xx。

### Acceptance Criteria
gateway 与 dynamic event tests 通过。

### Current Status
Implemented。

### Related Code
`schemas/dynamic_event.go`, `access/gateway.go`, `worker/runtime_consistency.go`。

## FR-ORD-001 Event 顺序与 replay

### Requirement
每个已接受 Event 必须获得 LSN，并可按 LSN 扫描；disk mode 必须使用 file WAL。

### Rationale
为恢复与 visibility watermark 提供统一顺序。

### Inputs
Event 和 `from_lsn`。

### Expected Behavior
`Append`, `Scan`, `LatestLSN` 遵守 WAL contract。

### Failure Behavior
持久 WAL 解码/IO 错误必须传播。

### Acceptance Criteria
WAL recovery/corruption tests 通过。

### Current Status
Implemented。

### Related Code
`eventbackbone/contracts.go`, `wal.go`, `wal_file.go`。

## FR-MAT-001 Canonical materialization

### Requirement
Event 必须按类型派生 Memory、AgentState 或 Artifact，并保存相关 Edge 与 ObjectVersion。

### Rationale
避免把 agent 对象压缩为不可解释的向量行。

### Inputs
规范化 Event。

### Expected Behavior
canonical projection 在共享 Badger backend 时原子提交对象/edge/version 集合。

### Failure Behavior
projection 失败不得推进 visible checkpoint；controller 按配置重试。

### Acceptance Criteria
materialization、canonical projection 和 consistency tests 通过。

### Current Status
Implemented；部分 direct CRUD 不经过该路径。

### Related Code
`materialization/service.go`, `storage/canonical_projection.go`, worker materializers。

## FR-STA-001 State 更新

### Requirement
相同 agent/session/state key 的更新必须指向稳定 State ID 并递增版本。

### Rationale
查询“当前状态”时需要确定性覆盖语义。

### Inputs
state update/change/checkpoint Event。

### Expected Behavior
State materializer 更新 State，checkpoint 生成 ObjectVersion。

### Failure Behavior
缺少 state key 时不生成 State；不得生成重复的竞争 State ID。

### Acceptance Criteria
state materialization tests 通过。

### Current Status
Implemented，跨进程 state key map 恢复语义为 Partial。

### Related Code
`worker/materialization/state.go`。

## FR-RET-001 Retrieval 与结构化查询

### Requirement
系统必须支持 query text、scope、object/memory type、time window、target IDs、cold tier 和 precomputed embedding 等过滤/操作符。

### Rationale
agent 查询需要语义检索与精确 selector 共存。

### Inputs
`schemas.QueryRequest`。

### Expected Behavior
planner 生成 SearchInput；dataplane 返回 candidates；assembler 输出 objects、edges、versions、provenance 和 proof trace。

### Failure Behavior
无效 selector 或 backend 错误返回明确错误；零命中不是 transport failure。

### Acceptance Criteria
query、tiered adapter、evidence tests 通过。

### Current Status
Implemented；部分 access filter 为 Partial。

### Related Code
`semantic/operators.go`, `worker/runtime.go`, `evidence/assembler.go`。

## FR-CON-001 可控一致性

### Requirement
系统必须支持 strict、bounded staleness 和 eventual visibility，并允许 Event/Query 覆盖默认模式。

### Rationale
不同 agent 操作需要在 latency、throughput 和 freshness 之间明确选择。

### Inputs
runtime config、Event access、Query access consistency。

### Expected Behavior
controller 负责 admission、queue、retry、watermark、checkpoint 和 query wait。

### Failure Behavior
queue full、paused、deadline、accepted-not-visible 和 projection failure 必须可区分。

### Acceptance Criteria
consistency controller/tracker tests 通过。

### Current Status
Implemented。

### Related Code
`worker/consistency/`, `runtime_consistency.go`。

## FR-GOV-001 Governance 与共享

### Requirement
系统应保存 PolicyRecord、ShareContract 和 AuditRecord，并在 evidence/query 阶段应用基础约束。

### Rationale
多 agent 数据需要可追踪的共享与生命周期决策。

### Inputs
policy、contract、memory operation。

### Expected Behavior
append policy/audit，按 object/scope 读取，在 trace 中暴露治理说明。

### Failure Behavior
不得把基础 policy engine 当作完整身份认证或授权系统。

### Acceptance Criteria
storage/governance tests 通过。

### Current Status
Partial。

### Related Code
`semantic/policy.go`, storage policy/contract/audit stores。

## FR-OPS-001 删除、purge 与恢复

### Requirement
系统必须区分 logical delete、hard purge、data wipe 和 replay；跨 tier 删除应清理对象、edge、segment ref 与 cold data。

### Rationale
生命周期操作必须可审计且不留下可检索孤儿。

### Inputs
dataset/source/memory selector 和 admin credential。

### Expected Behavior
批处理删除、后台 hard-delete、audit/outbox 和状态查询。

### Failure Behavior
部分失败必须可观测，不得静默报告全量成功。

### Acceptance Criteria
purge、hard delete、wipe 和 replay tests 通过。

### Current Status
Implemented/Partial，取决于 backend 和操作。

### Related Code
`access/hard_delete_manager.go`, `storage/purge_warm.go`, admin handlers。

## FR-SDK-001 SDK 与 transport

### Requirement
核心 ingest/query 应通过 HTTP 与 gRPC 可用；SDK 字段必须与 schema 保持一致。

### Rationale
避免调用方依赖内部 Go package。

### Inputs
JSON、protobuf 或 row-major binary payload。

### Expected Behavior
transport 映射到同一 Gateway service methods。

### Failure Behavior
协议错误应映射为 transport-level error，不能 panic。

### Acceptance Criteria
gRPC、framing、Python/Node tests 通过。

### Current Status
HTTP Implemented；gRPC limited；SDK Partial。

### Related Code
`gateway_rpc.go`, `api/grpc/`, `transport/`, `sdk/`。
