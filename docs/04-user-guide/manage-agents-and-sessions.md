# Manage Agents And Sessions

## Agent

Canonical `schemas.Agent` 保存 Agent ID、名称、类型、能力、运行状态、租户和工作空间信息。

HTTP 集合入口为 `/v1/agents`。GET 用于读取或列举，POST 用于直接保存 canonical record。直接 POST
适合注册目录数据；若 Agent 创建本身需要成为可回放业务事实，应同时或优先写入 Event。

## Session

`schemas.Session` 把一组 Event、Memory、State 和 Artifact 绑定到会话范围。入口为 `/v1/sessions`。

建议：

- Session ID 在上游 runtime 中生成并保持稳定；
- 每次 Event 都携带同一 tenant/workspace/agent/session scope；
- 不要用 Session ID 代替租户隔离；
- 会话结束可以更新状态，但不要立即物理删除其证据链。

## 查询作用域

`QueryRequest` 可同时使用 `tenant_id`、`workspace_id`、`agent_id` 和 `session_id`。过滤应从最强隔离
边界开始，再缩小到 Agent 和 Session。仅传自然语言 query 不能替代 scope filter。

## 生命周期边界

Agent/Session canonical CRUD 当前没有统一 ETag、乐观锁或通用分页协议。并发管理者应在应用层维护版本，
或通过 Event + ObjectVersion 建立可审计更新链。
