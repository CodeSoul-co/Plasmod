# Manage Agent State

## 数据模型

Canonical 类型为 `schemas.AgentState`，兼容别名为 `State`；对象类型常量是 `agent_state`，`state` 仅作
旧名称兼容。State 由 Agent、state key、value、version 和时间等字段描述。

## Event 驱动更新

状态变化应写成 `state_update`、`state_change` 或相关 tool result Event，并提供 state key/value。专用 State
materializer 使用：

```text
state_<agent_id>_<state_key>
```

作为稳定 ID，并在进程内跟踪 key 的递增版本。

此外，通用 `materialization.Service` 会为每次 ingest 创建 `ingest_checkpoint` State，ID 为
`state_<session_id>_<event_id>`，值指向本次默认 Memory。`materialization.targets` 当前不是阻止这一默认
checkpoint 的硬开关。

## 直接状态接口

`/v1/states` 提供 canonical 状态访问。直接 POST 不等同于 Event 驱动更新：它不会自动补齐原始 Event、
WAL 因果链和所有派生关系。

## 查询最新状态

查询时至少约束 tenant/workspace/agent 和 state key，对返回版本取最大值。不要仅依赖相似度排序。

## 当前限制

- state version 的部分递增状态保存在 worker 进程内；重启恢复语义应通过持久化 ObjectVersion 和 replay 验证；
- 没有跨进程全局事务锁来保证任意直接 CRUD 更新顺序；
- `state` 与 `agent_state` 名称并存，新增客户端应使用 canonical `agent_state`。
