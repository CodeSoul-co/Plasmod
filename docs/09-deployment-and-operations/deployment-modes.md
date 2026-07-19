# Deployment Modes

## Local unified

一个 HTTP listener (`127.0.0.1:8080`) 注册 management 和 data routes。适合本地开发，不提供网络隔离。

## Split HTTP

Management `9091`、data API `19530`、gRPC `19531`。适合通过不同 network policy 暴露控制面和数据面。

## Docker Compose

Plasmod + MinIO 的单机容器拓扑，提供可重复依赖环境。不是高可用集群。

## Native process

直接运行 binary，外部管理 Badger data dir、S3、TLS gateway、service manager 和日志。

## Current boundary

仓库包含上游分布式 control/stream 代码，但默认 `BuildServer` 仍是一个主动 runtime 进程。部署多副本共享同一
Badger 目录不是受支持的 HA 方案。
