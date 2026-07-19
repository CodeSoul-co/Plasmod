# Dynamic Event v0.4

Canonical schema 为 `schemas.Event` 和 `schemas.DynamicEvent` 相关嵌套类型。

| Group | Responsibility |
|---|---|
| `identity` | event、tenant、workspace 标识 |
| `actor` | agent、session 和角色 |
| `time` | event/ingest/visible time、logical timestamp |
| `event` | event type、importance 等描述 |
| `object` | 目标对象类型和可选 ID |
| `causality` | parent、causal refs、dependencies |
| `access` | visibility、consistency、ACL/policy references |
| `materialization` | enabled、targets、状态提示 |
| `retrieval` | namespace、index text、embedding、query hints |
| `payload`/`data` | 业务内容和结构化数据 |
| `runtime` | 写入/可见状态 |
| `extensions` | 扩展 labels/fields |

文本抽取顺序优先 `retrieval.index_text`，然后 payload 中 text/content。Namespace 优先 retrieval namespace，
再 workspace、session，最后 default。

旧平铺字段可被输入兼容层吸收，但 canonical JSON 输出隐藏对应 `json:"-"` alias。
