# Route Index

## Management

| Method | Route | Purpose | Status | Auth |
|---|---|---|---|---|
| GET | `/healthz` | 进程健康 | Implemented | 无内置认证 |
| GET | `/v1/system/mode` | APP_MODE/调试状态 | Implemented | 无内置认证 |
| GET | `/v1/admin/topology` | 拓扑摘要 | Implemented | Admin key |
| GET | `/v1/admin/storage` | 存储配置摘要 | Implemented | Admin key |
| GET | `/v1/admin/config/effective` | 有效配置 | Implemented | Admin key |
| POST | `/v1/admin/s3/export` | 导出到 cold store | Implemented | Admin key |
| POST | `/v1/admin/s3/snapshot-export` | 快照导出 | Implemented | Admin key |
| POST | `/v1/admin/s3/cold-purge` | 清理 cold records | Implemented | Admin key |
| POST | `/v1/admin/warm/prebuild` | 预构建 warm segment | Implemented | Admin key |
| POST | `/v1/admin/embeddings/reindex` | 重建 embedding/index | Implemented | Admin key |
| POST | `/v1/admin/dataset/delete` | 数据集逻辑删除 | Implemented | Admin key |
| POST | `/v1/admin/dataset/purge` | 数据集物理清理 | Implemented | Admin key |
| GET/POST | `/v1/admin/dataset/purge/task` | purge task 状态/控制 | Implemented | Admin key |
| POST | `/v1/admin/memory/delete-by-source` | 按来源删除 | Implemented | Admin key |
| POST | `/v1/admin/memory/purge-by-source` | 按来源清理 | Implemented | Admin key |
| POST | `/v1/admin/data/wipe` | 清空数据 | Implemented | Admin key |
| POST | `/v1/admin/rollback` | 管理回退 | Partial | Admin key |
| GET/POST | `/v1/admin/consistency-mode` | 查看/切换一致性 | Implemented | Admin key |
| POST | `/v1/admin/replay` | WAL replay | Implemented | Admin key |
| GET | `/v1/admin/metrics` | 运行指标 | Implemented | Admin key |
| GET/POST | `/v1/admin/governance-mode` | 治理模式 | Implemented | Admin key |
| GET/POST | `/v1/admin/runtime-mode` | runtime 模式 | Implemented | Admin key |
| GET/POST | `/v1/admin/memory/providers/mode` | 算法 provider 模式 | Experimental | Admin key |
| GET | `/v1/admin/memory/providers/health` | provider 健康 | Experimental | Admin key |

## Application Data API

| Method | Route | Purpose | Status |
|---|---|---|---|
| POST | `/v1/ingest/events` | Dynamic Event ingest | Implemented |
| POST | `/v1/ingest/vectors` | 预计算向量 segment ingest | Implemented |
| POST | `/v1/ingest/document` | 长文档分段写入 | Experimental |
| POST | `/v1/query` | 单查询 | Implemented |
| POST | `/v1/query/batch` | Warm Segment 向量批查询 | Implemented |
| GET/POST | `/v1/agents` | Agent canonical records | Implemented |
| GET/POST | `/v1/sessions` | Session canonical records | Implemented |
| GET/POST | `/v1/memory` | Memory canonical records | Implemented |
| GET/POST | `/v1/states` | AgentState canonical records | Implemented |
| GET/POST | `/v1/artifacts` | Artifact canonical records | Implemented |
| GET/POST | `/v1/edges` | Edge canonical records | Implemented |
| GET/POST | `/v1/policies` | Policy records | Implemented |
| GET/POST | `/v1/share-contracts` | Share contracts | Implemented |
| GET | `/v1/traces/{object_id}` | Evidence/provenance trace | Implemented |
| GET | `/v1/agent/list` | 按角色/范围列 Agent | Experimental |

`net/http.ServeMux` 只按路径注册；每个 handler 自己检查 Method。表中方法来自当前 handler 行为，调用不支持的
Method 通常得到 `405`。

## Internal Runtime API

| Routes | Purpose | Status |
|---|---|---|
| `/v1/internal/memory/*` | recall/ingest/compress/summarize/decay/share/conflict/stale | Experimental |
| `/v1/internal/task/*` | start/complete/tokens/claim/stage | Experimental |
| `/v1/internal/plan/*` | step/repair | Experimental |
| `/v1/internal/mas/*` | answer consistency/aggregate | Experimental |
| `/v1/internal/tool-state` | stateful tool query | Experimental |
| `/v1/internal/agent/handoff` | Agent handoff | Experimental |
| `/v1/internal/session/context` | Session context aggregation | Experimental |
| `/v1/internal/warm-segment/register` | 注册原生 warm segment | Experimental |

最后一组路由虽存在于核心 Gateway，但并非稳定公共 API；生产部署应阻止外网直接访问。
