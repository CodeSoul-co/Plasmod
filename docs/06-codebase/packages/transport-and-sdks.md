# transport And SDKs

`internal/transport/server.go` 通过小型 `RuntimeAPI` interface 暴露 batch ingest、warm query、segment register
和 WAL stream，避免 transport 直接依赖 runtime concrete type。

Python SDK 是当前较完整应用客户端。Node SDK 仍保留旧包/类命名且功能有限。SDK 变更应配合 HTTP contract
test，不能将 internal transport 当公共 SDK endpoint。
