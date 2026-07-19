# 项目总览

## 一句话定位

Plasmod 是面向 agent runtime 的数据库核心：以事件作为写入入口，将 Memory、AgentState、Artifact、Edge 和 ObjectVersion 物化为一等对象，并在查询阶段返回对象、版本、关系、provenance 与 proof trace 组成的结构化证据。

## 要解决的问题

长时间运行的 agent 不只需要“找到相似文本”，还需要回答：某条信息来自哪个事件、当前状态是哪一版、对象之间是什么关系、谁可以看到它、写入何时可见，以及服务重启后如何恢复。仅把文本、向量和 metadata 放进一个扁平集合，无法自然表达这些约束。

Plasmod 的核心路径因此分为：

1. Gateway 接收 Event 或 canonical object 请求。
2. Event 写入 WAL，获得单调 LSN。
3. consistency controller 按 strict、bounded 或 eventual 模式安排 projection。
4. materialization 将 Event 派生为 Memory、AgentState、Artifact、Edge 与 ObjectVersion。
5. canonical store 保存对象事实；retrieval plane 保存可重建的 lexical/vector projection。
6. query planner 执行检索和过滤，evidence assembler 补充关系、版本与证明链。

真实入口分别位于 `src/internal/access/gateway.go`、`src/internal/worker/runtime_consistency.go`、`src/internal/worker/runtime.go`、`src/internal/materialization/`、`src/internal/storage/` 和 `src/internal/evidence/assembler.go`。

## 主要使用者

- Agent framework 开发者：将 runtime event、session、state 和 artifact 接入统一存储。
- Agent 应用开发者：通过 HTTP、gRPC 或 Python SDK 写入和查询。
- 平台运维人员：配置持久化、S3/MinIO、可见性模式、恢复和管理 API。
- 核心开发者：扩展 schema、materializer、query operator、storage 或 retrieval backend。

## 当前核心能力

- Dynamic Event v0.4 输入及旧扁平事件输入兼容。
- file/in-memory WAL、LSN 扫描、恢复 checkpoint 与 replay admin API。
- Memory、State、Artifact、Edge、Version、Policy、ShareContract 的 canonical 存储。
- strict、bounded staleness、eventual visibility 三种 runtime 一致性模式。
- hot/warm/cold 对象路径；Badger 持久化；可选 S3/MinIO cold store。
- lexical + 可选 dense/sparse ANN retrieval；C++ bridge 不可用时降级。
- structured evidence response、1-hop edge expansion、version 与 proof trace。
- unified/split HTTP 监听和独立 gRPC 监听。
- Python SDK；Node SDK 当前只覆盖一致性模式控制。

## 当前状态

核心 Event ingest、query、canonical store、Badger、WAL 和 consistency controller 为 **Implemented**。权限隔离、通用认证、分页、跨对象事务、完整 SDK 覆盖和生产级备份编排为 **Partial**。可选 ANN backend、GPU、TensorRT、MemoryBank/Zep profile 等属于 **Experimental** 或条件能力，不能默认视为所有构建都具备。

## 最短阅读路径

继续阅读 [定位与边界](positioning-and-boundaries.md)、[能力地图](capability-map.md)、[设计总览](../02-concepts-and-design/design-overview.md) 和 [Quickstart](../03-getting-started/quickstart.md)。
