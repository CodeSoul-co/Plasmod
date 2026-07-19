# 术语表

| 术语 | 工程定义 |
|---|---|
| Event | agent runtime 中一次结构化事实或动作；`schemas.Event` 使用 Dynamic Event v0.4 canonical JSON。 |
| WAL | Event 的接受顺序与 replay 来源；`Append` 返回 LSN，`Scan` 从指定 LSN 读取。 |
| LSN | WAL 内单调递增的 log sequence number，不等同于 wall-clock 时间。 |
| Canonical Object | 可持久化的权威对象表示，如 Memory、AgentState、Artifact。 |
| Memory | 从 Event 物化的知识对象，带类型、scope、版本、生命周期与来源 Event。 |
| AgentState | 某 agent/session/state key 的当前值；Go 类型名仍为 `State`，`AgentState` 是别名。 |
| Artifact | agent 产生的外部或内联产物，如文本、报告、代码或工具结果。 |
| Edge | 两个 canonical object 之间的有向类型关系。 |
| ObjectVersion | 对象版本/快照记录，保存 mutation event 与有效时间。 |
| Materialization | 将已接受 Event 转换为 canonical object、edge 和 version 的过程。 |
| Canonical Projection | 一次 Event 产生的 Memory/State/Artifact、Edges 和 Versions 原子写入集合。 |
| Retrieval Projection | 从 canonical object 派生的 lexical/dense/sparse index，可重建，不是 source of truth。 |
| Evidence | 查询命中的对象加上 edge、version、provenance、proof trace 和过滤说明。 |
| Proof Trace | planner、retrieval、policy、tier、graph 与 derivation 步骤组成的可解释链。 |
| Watermark | runtime 已完成 projection、可用于可见性判断的最高 LSN。 |
| Strict | 写请求等待对应 LSN projection 可见后返回。 |
| Bounded Staleness | 写入按 freshness SLA 排队并在 deadline 前推进，读取可等待 watermark。 |
| Eventual | WAL 接受后允许异步 projection，读取只保证最终推进。 |
| Hot | 高显著度/近期对象的进程内 cache 与快速 index。 |
| Warm | canonical object store 与主要 retrieval segment 所在层。 |
| Cold | 显式归档对象所在的 S3/MinIO 或 in-memory cold store。 |
| RRF | Reciprocal Rank Fusion，用于合并多路检索排名。 |
| Vector-only mode | 关闭 graph/policy/provenance 的条件模式；不代表完整 Plasmod 语义。 |
| Source of Truth | 恢复和冲突判断时的权威事实来源；Event 为因果源，canonical store 为当前对象事实。 |
