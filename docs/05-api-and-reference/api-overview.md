# API Overview

## HTTP 面

Plasmod 使用 `net/http` 注册三类接口：

- 数据/应用接口：Event ingest、Query、canonical object、Trace；
- 内部 runtime 接口：memory algorithm、task、plan、MAS、transport；
- 管理接口：配置、存储、replay、删除、模式和 metrics。

统一模式把管理和数据路由放在 `127.0.0.1:8080`；拆分模式默认管理 `9091`、数据 `19530`。

## Transport 面

`src/internal/transport/server.go` 提供内部 HTTP RPC 和 WAL stream。gRPC server 默认监听 `19531`，当前
能力范围小于 HTTP API，不应假定每个 HTTP route 都有 gRPC 等价物。

## Content Type

请求和成功响应主要使用 `application/json`。部分错误由 `http.Error` 返回纯文本。客户端应先检查 HTTP
状态，再根据 `Content-Type` 解码，不能对所有失败直接调用 JSON parser。

## 稳定性标签

- **Implemented**：当前启动链路注册且有实现；
- **Experimental**：已实现，但命名或 payload 可能变化；
- **Partial**：能力存在，但契约覆盖不完整；
- **Not Confirmed**：代码中没有足够证据，不应依赖。

`v1` 是路由名称，不构成完整的兼容性承诺；参见 [`api-versioning.md`](api-versioning.md)。
