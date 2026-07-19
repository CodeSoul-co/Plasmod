# Event 到 Object 模型

## 输入规范化

`schemas.Event.UnmarshalJSON` 接受 Dynamic Event v0.4 和已实现的 legacy flat aliases。`NormalizeDynamicEventV04()` 将 identity、actor、time、event、object、causality、access、materialization、retrieval、payload、runtime 和 extensions 归一化。canonical JSON 输出以 v0.4 nested fields 为准。

## 默认路由

| Event 特征 | 主要 canonical 输出 |
|---|---|
| tool call / tool result / artifact-like | Artifact + base edges + version |
| state update / state change / checkpoint | AgentState；checkpoint 可生成 State versions |
| 其他 materializable event | Memory + base/causal edges + version |

Event 可以通过 `object.object_type/object_id` 影响 Artifact 等专用派生路径。`materialization.enabled` 和
`materialization.targets` 当前会被规范化、记录并用于查询过滤，但 active `materialization.Service` 尚未把它们
作为通用硬开关：每次 Event ingest 仍会创建默认 Memory、ObjectVersion 和 ingest checkpoint State，专用
State/Artifact worker 再按 state key、event/object type 和 payload 判断是否增加对象。是否进入向量索引由
retrieval fields 决定。

## Memory materialization

`materialization.Service.MaterializeEvent` 解析 text、scope、memory type、confidence、importance、source event 和 lifecycle，生成 Memory 与推导 edges。Runtime 将 retrieval record 写入 data plane，并将 canonical projection 写入 storage。

## State materialization

`InMemoryStateMaterializationWorker.Apply` 从 Event 读取 state key/value，用 agent + session + key 定位现有 state，并递增 `Version`。state ID 由 agent + state key 决定；当前 worker 的 lookup map 在进程内维护，因此恢复和 direct CRUD 混用需要谨慎。

## Artifact materialization

tool/artifact event 生成 Artifact，URI、MIME、name、body 从 Event object/payload 获取。内联 body 使用 `content_ref=inline` 并保存在 metadata。

## Edge 与 derivation

默认 memory edges 包括 caused_by event、belongs_to_session、owned_by_agent 和 causal refs 的 derived_from。Artifact 也生成 producer/base edges。DerivationLog 保存 event -> object 的 operation，供 trace 使用。
