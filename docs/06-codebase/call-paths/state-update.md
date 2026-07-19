# State Update

```text
state_update/tool_result Event
  -> normalize actor/session + state key/value
  -> StateMaterializationWorker
  -> state_<agent>_<key>
  -> version increment
  -> AgentState + ObjectVersion
  -> query by scope/key/latest version
```

State key/version 提取失败应阻止伪造空状态。直接 `/v1/states` POST 绕过此 Event 链，应只用于管理迁移。
