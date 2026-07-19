# Binary And gRPC Transport

## gRPC Server

gRPC 默认启用并监听 `0.0.0.0:19531`。可通过 `PLASMOD_GRPC_ENABLED=0` 关闭。默认最大消息尺寸为
512 MiB，具体解析位于 `src/internal/app/ports.go`。

当前 gRPC 面主要服务高吞吐或内部数据传输，能力不与所有 HTTP route 一一对应。部署前应从已注册 service
定义确认客户端方法，不要仅根据端口开放判断协议完整。

当前 active Gateway/transport 未注册 SSE endpoint；WAL stream 是内部 HTTP stream，不是浏览器 EventSource
契约。所谓 binary transport 主要指 protobuf/gRPC 和 row-major float vector payload 的组件协议。

## Internal HTTP RPC

`src/internal/transport/server.go` 提供：

- batch ingest；
- unload segment；
- warm query 及其 batch/raw 变体；
- warm segment register；
- WAL stream。

这些接口面向 Plasmod 组件，不是 Agent 应用的首选入口。

Warm batch vector 使用 row-major layout：二维 HTTP vectors 在服务端展平为 `nq * dim`，每行维度必须一致；
internal flat request 还显式携带 `nq`、`dim`、`top_k`。连接复用由 HTTP client/gRPC channel 管理，服务端不为
每个 Agent 建立独立持久 session connection。

## Message Size

大向量 batch 需要同时考虑：

- gRPC max receive/send size；
- HTTP server/client body 和 timeout；
- gateway write concurrency semaphore；
- native segment 内存；
- Badger transaction size。

提高消息上限不会自动提高吞吐，反而可能放大内存峰值。
