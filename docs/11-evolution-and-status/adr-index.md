# ADR Index

| ADR | Decision |
|---|---|
| [`0001`](../02-concepts-and-design/adr/0001-event-as-causal-source.md) | Event 作为因果 source |
| [`0002`](../02-concepts-and-design/adr/0002-canonical-before-retrieval.md) | Canonical object 与 retrieval projection 分层 |
| [`0003`](../02-concepts-and-design/adr/0003-go-runtime-cpp-retrieval.md) | Go runtime + C++ retrieval |
| [`0004`](../02-concepts-and-design/adr/0004-tiered-storage.md) | Hot/Warm/Cold 分层 |
| [`0005`](../02-concepts-and-design/adr/0005-structured-evidence.md) | 返回结构化 Evidence |
| [`0006`](../02-concepts-and-design/adr/0006-knowhere-native-ann.md) | Knowhere-style native ANN adapter |
| [`0007`](../02-concepts-and-design/adr/0007-result-layer-rrf.md) | 结果层 RRF fusion |
| [`0008`](../02-concepts-and-design/adr/0008-vector-only-boundary.md) | Vector-only 仅作为物理投影接口 |

新增跨模块、影响 API/持久化/故障语义的设计决定应增加 ADR，并记录 context、decision、alternatives、consequences
和 migration impact。
