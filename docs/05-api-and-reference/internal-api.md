# Internal API

Internal API 连接 Agent SDK、算法 provider 和 runtime 内部组件。它们已在当前 Gateway 注册，但不具备稳定
公共契约。

## Memory Algorithm Bridge

- `POST /v1/internal/memory/recall`
- `POST /v1/internal/memory/ingest`
- `POST /v1/internal/memory/compress`
- `POST /v1/internal/memory/summarize`
- `POST /v1/internal/memory/decay`
- `POST /v1/internal/memory/share`
- `POST /v1/internal/memory/conflict/resolve`
- `POST /v1/internal/memory/stale`
- `POST /v1/internal/memory/conflict/inject`

请求由 `src/internal/access/gateway.go` 解码并分派到 `agent-sdk`/semantic/coordinator 服务。算法 profile 可以
来自 baseline、MemoryBank 或 Zep 配置，但 canonical schema 不随 provider 改变。

## Task And Plan

- task：start、complete、tokens、claim、stage；
- plan：step、repair；
- session：context；
- tool：tool-state；
- agent：handoff；
- MAS：answer-consistency、aggregate。

这些接口假设调用者是受信 runtime。当前 admin auth middleware 不覆盖 `/v1/internal/*`。

## Transport RPC

`src/internal/transport/server.go` 另行注册 ingest batch、unload segment、warm query 和 register warm 等
`/v1/internal/rpc/*` 路由。它们是节点组件协议，payload 与 Gateway API 不同。

## 使用约束

1. 只在私有网络开放；
2. 客户端和服务端按同一 commit 部署；
3. 升级时先对照 handler request struct；
4. 不把 internal response 保存为长期外部契约；
5. 需要业务稳定性时封装在自有 adapter 后面。
