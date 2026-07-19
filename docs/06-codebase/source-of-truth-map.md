# Source Of Truth Map

| Question | Source of truth |
|---|---|
| Event 顺序和可回放事实 | FileWAL/InMemoryWAL records and LSN |
| 当前 canonical object | RuntimeStorage ObjectStore |
| 对象关系 | GraphEdgeStore |
| 历史版本 | SnapshotVersionStore |
| 治理决策 | PolicyStore/PolicyRecord |
| 共享协议 | ShareContractStore |
| 查询物理候选 | retrieval segment/index，属于派生层 |
| consistency 进度 | tracker + checkpoint |
| cold archive | S3/MinIO keys，显式归档后存在 |
| 实际进程配置 | env 解析结果 + effective config endpoint |

检索索引不是 canonical source of truth。索引可通过 canonical/WAL 重建，但直接 CRUD 未写 WAL 的历史不能凭空恢复。
