# Architecture Decision Records

| ADR | Decision | Status |
|---|---|---|
| [0001](0001-event-as-causal-source.md) | Event/WAL 作为因果与恢复顺序来源 | Accepted |
| [0002](0002-canonical-before-retrieval.md) | Canonical object 优先于 retrieval projection | Accepted |
| [0003](0003-go-runtime-cpp-retrieval.md) | Go runtime + C++ ANN bridge | Accepted, conditional |
| [0004](0004-tiered-storage.md) | Hot/Warm/Cold 分层 | Accepted |
| [0005](0005-structured-evidence.md) | Query 返回 structured evidence | Accepted |
| [0006](0006-knowhere-native-ann.md) | Knowhere-style native ANN adapter | Accepted, conditional |
| [0007](0007-result-layer-rrf.md) | 结果层 RRF fusion | Accepted |
| [0008](0008-vector-only-boundary.md) | Vector-only 仅作为物理投影接口 | Accepted |

ADR 记录设计原因和不可破坏的不变量，不记录排期或人员决策。
