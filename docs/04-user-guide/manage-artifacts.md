# Manage Artifacts

Artifact 表示计划、报告、代码修改、工具产物或其他可独立寻址的 Agent 输出，类型定义在
`src/internal/schemas/canonical.go`。

## 创建

推荐通过 Event 创建：

1. `event.event_type` 使用 artifact/tool result 相关类型；
2. `object.object_type` 设为 `artifact`；
3. `object.object_id` 可提供稳定业务 ID；
4. `materialization.targets` 可声明 `artifact` 和 `object_version` 作为期望元数据；
5. 在 causality 中提供 parents/dependencies；
6. payload 保存内容或外部位置描述。

当前是否创建 Artifact 由 artifact-like object/event、tool event 或 URI/body 等 payload 条件决定，而不是仅由
targets 决定。Artifact materializer 的 `ArtifactIDOrDefault` 会优先使用有效的显式对象 ID；这与默认
Memory ID 规则不同。

## 直接访问

`/v1/artifacts` 提供 canonical CRUD。适合迁移或管理已有 Artifact，但要由调用方补充 Edge、Version 和
Event 记录，否则 trace 可能只看到孤立对象。

## 更新

不要原地覆盖后丢失来源。推荐为更新生成新 Event 和 ObjectVersion，并用 `updates`、`derived_from` 或
`supersedes` 语义的 Edge 连接前后对象。

## 内容存储

Plasmod 可保存结构化 payload 和对象元数据；大文件通常应保存在对象存储中，Artifact 保存 URI、hash、
media type 和 provenance。上传文件生命周期不应只依赖检索索引。
