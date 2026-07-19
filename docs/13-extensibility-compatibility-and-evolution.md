# 13. 扩展、兼容性与系统演进

---

给出新增对象、算法、算子、策略和 backend 的模板及兼容演进规则。

---

## 13.1. Add An Agent Framework Adapter

Adapter 的职责是把 framework lifecycle 转换为 Plasmod Event/Query，而不是把 framework 状态塞进自由文本。

### 13.1.1. Mapping

- conversation/task -> Session；
- model/tool callback -> Event；
- durable memory -> Memory target；
- environment mutation -> AgentState；
- plan/report/file -> Artifact；
- dependency/evidence -> Edge；
- framework identity -> tenant/workspace/agent scope。

### 13.1.2. Requirements

稳定 ID、时间/逻辑顺序、重试幂等、precomputed embedding、consistency 选择、错误回传、shutdown flush 和版本兼容。

Adapter 放在独立 SDK/package，不把特定 framework 依赖引入 core composition root。

---

## 13.2. Add An Agent State Type

1. 定义 `state_type` 和 `state_key` 语义；
2. 规定 value codec/schema version；
3. 更新 state Event extraction；
4. 保持 `state_<agent>_<key>` ID 或提供迁移；
5. 定义版本递增和并发更新规则；
6. 增加 latest query operator/filter；
7. 验证 restart/replay 后版本；
8. 补充 purge、trace 和 SDK 示例。

不要为每种状态创建新的顶层数据库类型；只有存储、查询和生命周期显著不同才考虑新 canonical object。

---

## 13.3. Add A Canonical Object

### 13.3.1. Required code changes

- `schemas` struct 和 object type constant；
- ObjectStore/RuntimeStorage contract；
- memory store、Badger codec/prefix、S3 cold representation；
- factory wiring；
- coordinator/handler；
- materializer 和 ObjectVersion；
- query listing/filter/evidence；
- backup/replay/delete/purge；
- SDK 和 docs。

### 13.3.2. Required tests

- CRUD/reopen；
- transaction with Edge/Version；
- deterministic replay；
- scope/policy；
- old data compatibility；
- cold archive/read；
- purge completeness。

新增 prefix 前检查已有 key space，写入后不得在 patch release 中随意修改。

---

## 13.4. Add An Event Type

1. 在 `schemas/constants.go` 添加 canonical string；
2. 更新 Dynamic Event validation/normalize；
3. 定义 payload/object/causality 约束；
4. 在 materializer/worker dispatch 接入；
5. 规定生成对象、Edge、Version 和 deterministic IDs；
6. 更新 query filters 和 trace；
7. 增加 ingest、replay、invalid payload 和 consistency tests；
8. 更新 schema/API/user guide。

Event type 名称一旦进入 WAL 就是兼容面。重命名应通过 alias + migration，而不是直接删除旧常量。

---

## 13.5. Add An Evidence Hook

Evidence hook 可以补充 GraphNode、Edge、ProofStep、provenance 或过滤说明。

要求：

- 输入只使用当前查询允许的对象；
- 输出 deterministic，可限制节点/边数量；
- 遵守 context cancel/timeout；
- 不修改 canonical source；
- 不通过描述文本泄露被拒绝对象；
- failure 语义明确为 fail query、omit hook 或 mark partial；
- 在 production visibility middleware 前返回 typed fields。

新增 hook 要补 query/evidence tests，并在 response contract 中标明稳定性。

---

## 13.6. Add A Materializer

实现流程：

1. 定义支持的 Event/object/target；
2. 解析 typed payload；
3. 计算 deterministic object/edge/version IDs；
4. 构建 canonical projection；
5. 通过 RuntimeStorage transaction 写入；
6. 请求 retrieval projection；
7. 将失败返回 consistency controller；
8. 注册到 app/runtime worker graph；
9. 添加 replay/retry/idempotency tests。

Materializer 不应自行更新 visible checkpoint；只有 runtime 确认所需阶段完成后 tracker 才推进。

---

## 13.7. Add A Memory Algorithm

1. 定义 provider/algorithm ID 和配置 schema；
2. 实现 agent SDK/dispatch contract；
3. 使用 canonical Memory 和独立 algorithm state；
4. 明确 ingest/recall/compress/summarize/decay/conflict 行为；
5. 尊重 scope、policy、TTL 和 lifecycle；
6. 注册 profile 和 health；
7. 定义切换 provider 时 state migration；
8. 增加 deterministic unit/contract tests。

算法不得直接修改 Badger key 或绕过 Event provenance。算法 score 也不能覆盖数据库的访问控制结论。

---

## 13.8. Add A Policy Rule

Policy rule 输入应包含 object、actor/scope、operation、PolicyRecord/ShareContract 和 runtime context；输出包含
allow/deny/quarantine/weight/TTL 等明确 decision。

实现步骤：

1. 定义 rule ID/version；
2. 添加 typed config；
3. 在读/写正确 hook point 执行；
4. 记录 PolicyRecord/decision reason/source/event ID；
5. Evidence 中只暴露允许内容；
6. 测试 deny 优先级、冲突规则和默认失败策略；
7. 将配置加入 effective config（脱敏）。

安全规则异常时默认行为必须显式，不能无意 fail-open。

---

## 13.9. Add A Query Operator

1. 在 QueryRequest 增加 typed optional field 或 `query_ops` descriptor；
2. semantic planner 解析；
3. 对 Hot、Warm、Cold 和 canonical supplement 定义一致语义；
4. 在 policy/filter 之前或之后的位置写清楚；
5. 返回 `applied_filters`/proof 信息；
6. 批查询保持等价；
7. 更新 Python SDK；
8. 测试空值、组合、scope leak 和 unsupported backend。

不能只在 ANN 结果上实现过滤，否则 canonical supplement 或 Cold 可能绕过条件。

---

## 13.10. Add A Retrieval Backend

实现 `retrievalplane`/DataPlane 所需 search/storage contracts，并保留 Go 层业务语义。

必须定义：

- index types 和 build/load lifecycle；
- metric、dimension、embedding family；
- batch 和 concurrency；
- segment persistence；
- timeout/cancel/error mapping；
- delete/reindex/compaction；
- handle/resource ownership。

Backend 返回 object ID + score/candidate metadata；tenant policy、canonical load、fusion 和 Evidence 仍由 Go 完成。

对不支持的 index type 返回明确错误，不静默回退成不同算法。

---

## 13.11. Add A Storage Backend

新 backend 必须实现所需 `RuntimeStorage` 子接口，并说明：

- object/edge/version transaction；
- key/order/list semantics；
- durability 和 fsync；
- concurrent readers/writers；
- backup/restore；
- delete/purge；
- error classes；
- shutdown；
- schema migration。

在 `storage/factory.go` 添加显式 mode 和 config snapshot。不要根据 DSN 内容静默猜 backend。

Contract tests 应同时对 memory、Badger 和新 backend 运行，确保空列表、not found、覆盖和事务行为一致。

---

## 13.12. Extension Overview

### 13.12.1. Stable extension boundaries

- Event payload/extensions；
- schema constants 和 canonical types；
- materializer/worker contracts；
- RuntimeStorage interfaces；
- DataPlane/retrievalplane interfaces；
- semantic query operators；
- policy/evidence hooks；
- HTTP/SDK adapters。

### 13.12.2. Required questions

1. 新事实是否写入 WAL？
2. canonical source 是什么？
3. ID 是否 deterministic/replay-safe？
4. 哪些 scope/policy 适用？
5. projection 失败如何恢复？
6. storage migration 如何处理？
7. delete/purge/backup 是否覆盖？
8. public API 是否需要兼容承诺？

只新增 handler 或 struct 通常不足以形成完整功能。

---

## 13.13. Schema And Payload Extension

优先级：

1. 业务非索引字段放 `payload`；
2. 可选扩展标签/字段放 `extensions`；
3. 只有跨功能稳定语义才提升为 Event/canonical 顶层字段；
4. 新 canonical object 必须有持久化和生命周期。

新增字段应 optional、有明确默认值，并验证旧 WAL JSON 能读取。不要复用旧字段表达不同语义。

若字段参与 scope、policy、ID、排序或持久化 key，则不能只作为自由 payload；需要 typed schema 和全链路测试。

---

## 13.14. API Compatibility

### 13.14.1. Public HTTP

Changes to `/v1/ingest/events`、`/v1/query`、canonical collections 和 Trace 需要兼容评估。新增 optional field
优于改变 existing field type/default。

### 13.14.2. Internal API

`/v1/internal/*` 与 transport routes 只保证同版本组件。仍应避免无迁移地破坏已部署 adapters。

### 13.14.3. SDK

Python SDK release 应标明服务版本；先让服务端接受新字段，再发布使用新字段的 SDK。Node SDK 的旧命名需要
独立迁移方案。

### 13.14.4. Error behavior

HTTP status、plain-text/JSON error body、query status 都属于客户端可观察行为。改变 error mapping 需测试和
release note。

---

## 13.15. Configuration Deprecation

### 13.15.1. Current compatibility

部分环境变量仍接受 `ANDB_*` alias，构建 option 也保留该前缀。新配置使用 `PLASMOD_*`，但移除 alias 前要
给出至少一个明确迁移窗口。

### 13.15.2. YAML status

只有启动代码真实读取的 YAML 才是 active config。未接入 `BuildServer` 的 app/storage/retrieval/graph YAML 应
标为 reference，而不是承诺自动生效。

### 13.15.3. Deprecation process

1. 增加新 key 并保持旧 key fallback；
2. 日志警告旧 key（不打印 secret）；
3. effective config 只显示 canonical key；
4. 更新 Compose/SDK/docs；
5. 发布迁移说明；
6. 在后续 major release 删除 fallback。

---

## 13.16. Migration Guide

### 13.16.1. Before upgrade

1. 记录当前 commit/tag、Go/native dependency 和 effective config；
2. 备份 Badger、WAL、checkpoint 和 Cold store；
3. 阅读 schema/API/storage/config changes；
4. 在副本上运行新 binary；
5. 验证旧数据 query/trace/replay。

### 13.16.2. Upgrade

1. 停止新写；
2. 等待 visible checkpoint；
3. 正常 shutdown；
4. 执行离线 migration（若需要）；
5. 启动新版本；
6. 检查 health/storage/config/provider；
7. 写一个 strict Event；
8. 验证 Memory、State、Artifact、Edge、Trace、Cold；
9. 恢复流量。

### 13.16.3. Rollback

只有旧 binary 能读取新版本写入格式时才能直接 rollback。否则恢复升级前备份，并隔离升级期间新增写入。

---

## 13.17. Schema Evolution

### 13.17.1. Event

Dynamic Event 有独立 `schema_version`。新增字段应 optional；旧 flat aliases 可读取但不作为新输出。重大语义变化
需要新 schema version 和 replay adapter。

### 13.17.2. Canonical objects

Go JSON struct、Badger encoded bytes、S3 JSON 和 SDK 都可能持有 schema。新增字段需定义 zero value；删除/重命名
需双读和 migration。

### 13.17.3. Constants

Event/object/edge/memory type 字符串会进入 WAL 和持久化数据。改名采用 alias + canonical normalization，不能只改常量。

### 13.17.4. Validation

升级测试必须覆盖旧 Event JSON、旧 Badger object、旧 S3 object 和混合版本 replay。

---

## 13.18. Storage Format Evolution

持久化兼容面包括：

- Badger key prefix 和 key composition；
- JSON/binary value codec；
- WAL record framing/schema；
- consistency checkpoint；
- derivation log；
- native segment/index format；
- S3 key prefix/object body。

### 13.18.1. Migration patterns

- additive value fields：旧 reader/new reader compatibility；
- key change：dual-read/dual-write + backfill；
- WAL change：versioned decoder；
- native index incompatibility：canonical/embedding-driven rebuild；
- S3 layout change：copy + manifest + cutover。

任何迁移都要可中断恢复、可观测进度并保留回滚备份。
