# Source of Truth 模型

| 数据 | Source of Truth | Derived View | 恢复方式 |
|---|---|---|---|
| 接受顺序 | WAL + LSN | bus/subscriber stream | 从 checkpoint 后扫描 WAL |
| Event 内容 | ObjectStore Event + WAL entry | stream/debug view | replay/scan |
| Memory | Canonical ObjectStore | hot cache、lexical/vector/sparse index、cold copy | canonical rebuild/reindex |
| AgentState | Canonical ObjectStore | state selector/query result | Event replay；注意 worker lookup state |
| Artifact | Canonical ObjectStore | retrieval projection/cold copy | Event replay |
| Edge | GraphEdgeStore | evidence subgraph | canonical projection/replay |
| Version | SnapshotVersionStore | latest/historical response | canonical projection/replay |
| Policy/Contract | PolicyStore/ContractStore | policy filter/trace annotation | store backup/replay where applicable |
| Algorithm State | MemoryAlgorithmStateStore | lifecycle/recall response | store backup/algorithm rebuild |
| Retrieval Segment | canonical memory + segment metadata | native/lexical index | reindex |

## 两层权威事实

Event/WAL 是因果和恢复顺序的权威来源；canonical store 是在线对象读写的权威来源。两者通过 LSN、mutation event 和 version 关联。只保留 index 无法恢复完整对象语义；只保留 canonical store 可能恢复对象，但无法证明原始接受顺序。

## 不变量

- watermark 只能推进到成功 projection 的 LSN。
- retrieval hit 不得创造不存在的 canonical object 事实。
- index family/dimension 与当前 embedder 不兼容时必须阻止错误复用或显式 reindex。
- cold copy 是归档层，不自动取代 WAL/Badger backup。
