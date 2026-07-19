# Error Model

## HTTP 状态

| Status | 当前含义 |
|---:|---|
| `200`/其他 2xx | handler 完成；仍需检查 query status/task status |
| `400` | JSON、字段或普通运行时校验错误 |
| `401` | admin key 缺失或错误 |
| `405` | Method 不支持 |
| `408` | 请求 context canceled |
| `503` | backpressure、paused、accepted-not-visible、projection failure 或 runtime unavailable |
| `504` | consistency/query 等待 deadline exceeded |

状态映射位于 `src/internal/access/gateway.go`。不同 handler 有些使用 JSON，有些使用纯文本错误。

## 重试分类

- 400/405：不重试，修正调用；
- 401：修正凭证；
- 503 backpressure：指数退避；
- 503 accepted-not-visible：先用 event/object ID 查询状态，避免盲目重复写；
- 503 projection failure：检查 embedding/native backend，再按恢复策略 replay；
- 504：结果可能稍后可见，先读后重试；
- 网络断开：按未知提交结果处理。

## Query 的逻辑状态

HTTP 2xx 下 `query_status` 仍可能是 `no_retrieval_hits` 或
`no_retrieval_hits_supplemented`。后者表示检索没有 seed，但 canonical listing 补充了对象。
