# API Versioning

## 当前版本边界

- HTTP 路径使用 `/v1`；
- Event schema 使用 `plasmod.dynamic_event.v0.4`；
- canonical Go structs 通过 JSON tag 定义 wire shape；
- internal/transport API 与仓库 commit 更紧密绑定。

`/v1` 不意味着所有字段已冻结。新增 optional 字段通常向后兼容；重命名、类型变化、默认一致性变化和删除字段
都需要迁移说明。

## 兼容规则

1. 新客户端输出 canonical v0.4 嵌套字段；
2. 服务端可读取 legacy alias，但不应在新响应中继续扩散；
3. 未知 optional 字段应通过 extensions/payload 承载；
4. internal API 只保证同版本组件互通；
5. 持久化 schema 变化必须同时考虑 replay 旧 WAL；
6. SDK release 必须标明对应服务 commit/tag。

正式升级流程见 [`../11-evolution-and-status/migration-guide.md`](../11-evolution-and-status/migration-guide.md)。
