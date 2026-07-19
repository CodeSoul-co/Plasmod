# evidence And semantic

`semantic.QueryPlanner` 把 QueryRequest 转为检索/过滤操作。DataPlane 返回候选后，`evidence` 读取 canonical
object、Edge、Version、Policy 和 derivation，构建 GraphNode、ProofStep 和 provenance。

Evidence 是结构化查询输出，不是一个独立 source of truth。组装器必须尊重 scope 和 policy，且不能通过缺失
Edge 推断不存在的来源。
