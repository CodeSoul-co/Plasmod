# Known Limitations

1. 默认 runtime 是单进程组合；大体量 upstream controlplane 不是默认完整集群。
2. Badger data directory 不支持多个 Plasmod 进程直接共享写入。
3. Admin key 只保护 `/v1/admin/*`；数据/internal route 需要外部认证和网络隔离。
4. `/healthz` 不是完整 readiness。
5. 公共 API 没有统一 Idempotency-Key、cursor pagination 或 ETag/乐观锁。
6. Canonical CRUD 不自动获得完整 Event/WAL/replay 因果链。
7. Cold tier 是显式归档/查询，不是所有写入自动复制。
8. IVF、DiskANN、GPU/TensorRT 依赖构建和平台。
9. gRPC/Node SDK 与 HTTP/Python SDK 不完全等价。
10. State worker 的部分 version tracking 在进程内，恢复需依赖持久化版本和 replay 校验。
11. `v1` 与 Dynamic Event v0.4 仍需要正式 release compatibility policy。
12. 某些 YAML 文件存在但未由 active startup 读取。
