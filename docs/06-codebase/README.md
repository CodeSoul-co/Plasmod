# Codebase Guide

本目录把用户能力映射到真实启动链路、package、接口、存储 key 和调用路径。

- [`repository-overview.md`](repository-overview.md)
- [`repository-tree.md`](repository-tree.md)
- [`package-index.md`](package-index.md)
- [`architecture-to-code-map.md`](architecture-to-code-map.md)
- [`component-ownership.md`](component-ownership.md)
- [`bootstrap-and-runtime-wiring.md`](bootstrap-and-runtime-wiring.md)
- [`source-of-truth-map.md`](source-of-truth-map.md)
- [`interfaces/README.md`](interfaces/README.md)
- [`data-structures/README.md`](data-structures/README.md)
- [`storage-key-layout.md`](storage-key-layout.md)
- [`call-paths/README.md`](call-paths/README.md)
- [`packages/README.md`](packages/README.md)

## 设计到代码的详细索引

- [System Design Reference](../02-concepts-and-design/system-design/README.md)：30 个 Architecture/Chain/Perspective/Mechanism/Engine 条目；
- [Object and Message Registry](../02-concepts-and-design/system-design/06-cross-reference/object-and-message-registry.md)：字段、typed input/output 和 ID 不变量；
- [Interface Implementation Registry](../02-concepts-and-design/system-design/06-cross-reference/interface-implementation-registry.md)：interface、implementation、constructor、bootstrap selection；
- [API to Engine Matrix](../02-concepts-and-design/system-design/06-cross-reference/api-to-engine-matrix.md)：route 到真实调用链；
- [Execution State and Failure Matrix](../02-concepts-and-design/system-design/06-cross-reference/execution-state-and-failure-matrix.md)：状态、事务、重试与恢复。

阅读代码时先从 `src/cmd/server/main.go` 和 `src/internal/app/bootstrap.go` 进入。不要从
`coordinator/controlplane` 或 `eventbackbone/streamplane` 的文件量推断当前默认进程完整启用了其全部上游
分布式功能。
