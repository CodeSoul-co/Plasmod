# ADR-0001: Event/WAL 作为因果源

- Status: Accepted
- Context: agent state、memory、artifact 和 relation 会并发演进，直接覆盖对象无法恢复因果顺序。
- Decision: 核心业务写入先表达为 Event，并通过 WAL 获得 LSN；projection 生成 canonical objects。
- Consequences: 可以 replay、追踪 accepted/visible 和绑定 mutation event；调用方必须处理 accepted-not-visible。
- Alternatives: 直接 CRUD 作为唯一写路径，被拒绝，因为缺少顺序和 replay。direct CRUD 仅保留为管理/兼容入口。
- Invariant: 成功 projection 前不得推进 visible watermark。
