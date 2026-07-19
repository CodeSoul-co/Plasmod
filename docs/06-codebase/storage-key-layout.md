# Storage Key Layout

Badger store 使用稳定 prefix 区分逻辑表：

| Prefix | Record |
|---|---|
| `seg\|` | retrieval segment |
| `idx\|` | index metadata |
| `obj\|agent\|` | Agent |
| `obj\|session\|` | Session |
| `obj\|memory\|` | Memory |
| `obj\|state\|` | AgentState |
| `obj\|artifact\|` | Artifact |
| `obj\|event\|` | Event |
| `obj\|user\|` | User |
| `edg\|` | Edge |
| `ver\|` | ObjectVersion |
| `pol\|` | PolicyRecord |
| `ctr\|` | ShareContract |
| `kpeS\|` | source-oriented edge index |
| `kpeD\|` | destination-oriented edge index |

定义位于 `src/internal/storage/badger_stores.go`。部分 algorithm/audit/outbox 数据有各自 namespace，应通过
对应 store API 访问。

修改 prefix 会造成旧数据不可见。迁移必须双读/双写或离线转换，不能直接替换常量后发布。
