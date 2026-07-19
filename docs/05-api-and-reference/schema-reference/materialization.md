# Materialization Schema

Event 的 `materialization` group 描述期望物化状态和目标类型。常见 target：

- `memory`；
- `agent_state`；
- `artifact`；
- `relation`/`edge`；
- `object_version`；
- retrieval projection。

当前 active materializer 尚未把 `enabled`/`targets` 作为通用硬 gate。每次 Event ingest 默认生成 Memory、
Memory ObjectVersion 和 ingest checkpoint State；Artifact 与专用 AgentState 再由 event/object/payload 和
state key 条件触发。设置 target 不保证任意未知类型能被自动创建，设置 `enabled=false` 也不保证跳过上述
默认 canonical projection。

Memory ID 默认 `mem_<event_id>`；ingest checkpoint State ID 为 `state_<session_id>_<event_id>`；专用 keyed
State ID 为 `state_<agent_id>_<state_key>`；Artifact 可优先采用 `object.object_id`。这些规则属于持久化
兼容面，修改时需要迁移与 replay 验证。

Runtime status 区分接受、物化、投影和可见阶段；一致性 controller 的 tracker/checkpoint 保存推进位置。
