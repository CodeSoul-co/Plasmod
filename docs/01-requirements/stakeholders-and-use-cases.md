# Stakeholders 与 Use Cases

## UC-001: Framework 写入 runtime event

### Actor
Agent framework developer。

### Goal
将 observation、tool result、state update 或 artifact 作为 Event 写入，并获得接受/可见状态。

### Preconditions
服务健康；Event 至少有 actor、event type 和 payload；所选 consistency mode 有效。

### Main Flow
Framework 调用 `/v1/ingest/events`；Gateway 规范化 v0.4；WAL 分配 LSN；controller 执行 projection；返回 event/object/visibility ack。

### Alternative Flow
eventual 模式先返回 WAL 接受；projection 后台完成。队列满、runtime paused 或 projection 失败时返回可重试错误。

### Data Written
Event、Memory/State/Artifact、Edge、ObjectVersion、retrieval projection。

### Data Queried
Query、canonical CRUD、trace。

### Consistency Requirement
由 Event `access.consistency` 覆盖 runtime default。

### Failure Expectation
WAL 接受后但未可见必须以明确错误/状态区分，不能伪装成未写入。

### Related API
`POST /v1/ingest/events`, `GET/POST /v1/admin/consistency-mode`。

## UC-002: Tool-use agent 查询最新状态

### Actor
Tool-use agent。

### Goal
在连续 tool result 和 state update 后读取指定 agent/session 的最新 State。

### Preconditions
state event 带 `object.state_key` 或兼容 payload 字段。

### Main Flow
写入 state update；State materializer 按 agent + state key 生成稳定 state ID 并递增 version；客户端查询 `/v1/states` 或结构化 query selector。

### Alternative Flow
eventual 模式下客户端等待/重试或使用 strict read。

### Data Written
Event、State、derivation、可选 ObjectVersion checkpoint。

### Data Queried
State list/latest selector。

### Consistency Requirement
决策关键路径使用 strict；容忍旧值时使用 bounded/eventual。

### Failure Expectation
不得把“未找到”与“已接受但尚未物化”混为一类。

### Related API
`POST /v1/ingest/events`, `GET /v1/states`, `POST /v1/query`。

## UC-003: Research agent 获取 evidence

### Actor
Research agent。

### Goal
查询 memory，同时获得来源 event、关系、版本与 proof trace。

### Preconditions
查询 scope 与 object filter 合法；相关对象已进入 canonical/retrieval store。

### Main Flow
调用 `/v1/query`；planner 生成 SearchInput；tiered dataplane 检索；assembler 扩展 edge/version/provenance。

### Alternative Flow
设置 `target_object_ids` 走 canonical selector；设置 `include_cold` 扩展到归档层。

### Data Written
无；evidence cache 可在 ingest 阶段预计算。

### Data Queried
Memory、Edge、ObjectVersion、PolicyRecord、EvidenceFragment。

### Consistency Requirement
查询 mode 决定是否等待可见 watermark。

### Failure Expectation
零 retrieval hit 与 canonical supplement 必须由 `query_status` 区分。

### Related API
`POST /v1/query`, `GET /v1/traces/{object_id}`。

## UC-004: Multi-agent 共享与冲突处理

### Actor
Multi-agent runtime。

### Goal
表达 owner、visibility、share contract 与冲突关系。

### Preconditions
agent、workspace 和 contract 标识稳定。

### Main Flow
写入共享 memory/contract；policy 与 access filter 参与查询；冲突通过 internal memory route 和 edge/audit 记录。

### Alternative Flow
未配置 contract 时只使用基础 visibility/ACL 判断。

### Data Written
Memory、ShareContract、PolicyRecord、Edge、AuditRecord。

### Data Queried
scope-aware query 与 trace。

### Consistency Requirement
关键共享决策使用 strict。

### Failure Expectation
当前 ACL 是基础实现，上层仍需身份认证和租户边界保护。

### Related API
`/v1/share-contracts`, `/v1/policies`, internal memory share/conflict routes。

## UC-005: Operator 恢复服务

### Actor
AI platform operator。

### Goal
服务中断后从 durable WAL 和 checkpoint 恢复 projection。

### Preconditions
`PLASMOD_STORAGE=disk`，数据目录和 WAL 可读；版本/embedding 配置兼容。

### Main Flow
BuildServer 打开 Badger/FileWAL；consistency controller 读取 checkpoint；扫描后续 WAL；重放未完成 projection；健康检查通过。

### Alternative Flow
先调用 replay preview，再由 admin replay apply；embedding family 不兼容时进行受控 reindex。

### Data Written
checkpoint、canonical projection、retrieval index。

### Data Queried
admin storage/config/consistency/replay 状态。

### Consistency Requirement
恢复期间不得把未推进 watermark 的数据报告为可见。

### Failure Expectation
WAL 解码、checkpoint 或 projection 错误应阻止错误启动或返回明确失败。

### Related API
`/v1/admin/replay`, `/v1/admin/consistency-mode`, `/v1/admin/storage`。
