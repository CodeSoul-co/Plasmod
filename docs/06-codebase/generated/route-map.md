# Route Map

生成依据：`access.Gateway.RegisterMgmtRoutes`、`RegisterAPIRoutes` 和 `transport.NewServer`。

| Registry | Prefixes |
|---|---|
| Management | `/healthz`, `/v1/system`, `/v1/admin` |
| Application | `/v1/ingest`, `/v1/query`, canonical collections, `/v1/traces` |
| Runtime internal | `/v1/internal/memory`, task, plan, MAS, tool, agent, session |
| Transport internal | `/v1/internal/rpc`, `/v1/wal/stream` |

完整 route/method/stability 表见 [`../../05-api-and-reference/route-index.md`](../../05-api-and-reference/route-index.md)。
