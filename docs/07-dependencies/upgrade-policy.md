# Dependency Upgrade Policy

## Go modules

按小批次升级，运行 `go mod tidy` 后检查 diff，并执行 `go test ./src/...`。对 Badger、protobuf、gRPC 等核心
依赖单独验证持久化和 wire compatibility。

## Native stack

1. 固定 upstream commit/version；
2. 记录本地 patch；
3. 在支持平台重建；
4. 验证所有启用 index type；
5. 检查 ABI、symbol 和 dynamic links；
6. 验证旧 segment load 或明确要求 rebuild。

## Storage/external service

Badger 升级需测试旧目录打开、backup/restore、key scan。S3/MinIO 升级需测试 endpoint/TLS/signature 和
existing object keys。

依赖升级不得顺带改变 Agent schema 或 API 默认语义。
