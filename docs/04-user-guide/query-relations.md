# Query Relations

## Edge 模型

`schemas.Edge` 是 canonical relation，至少连接 source 和 destination，并携带 edge type、scope、时间和
可选属性。常用类型包括：

- `caused_by`、`derived_from`；
- `supports`、`contradicts`；
- `summarizes`、`updates`；
- `uses_tool`、`tool_produces`；
- `belongs_to_session`、`owned_by_agent`、`shared_with`。

## 写入

Event 的 causality/parents、relation 类型和 materialization 配置可以生成 Edge。`/v1/edges` 也支持直接
canonical 管理，但直接写 Edge 时调用方必须保证两端对象和 scope 合法。

## 查询

可以：

- 通过 `/v1/edges` 读取已知关系；
- 在 `/v1/query` 中使用 `edge_types` 和 relation constraints；
- 使用 `/v1/traces/{id}` 请求围绕对象的 evidence graph。

## 解释结果

关系命中不等于事实正确。`supports` 和 `contradicts` 是系统记录的 Agent runtime 关系，需要结合来源对象、
ObjectVersion、PolicyRecord 和 ProofStep 解释。Evidence assembler 负责结构化返回，不负责替代领域验证。
