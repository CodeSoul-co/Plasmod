# Memory Lifecycle

Plasmod 把 Memory 生命周期拆成写入、召回、压缩/总结、衰减、共享、冲突处理、分层和删除。

## 创建与分类

Event materializer 默认创建 `mem_<event_id>`。Memory type 可表示 episodic、semantic、procedural、social、
reflective、factual、profile、affective state 或 preference/constraint。

## 算法操作

内部 runtime 路由包括 recall、ingest、compress、summarize、decay、share、conflict resolve 和 stale 标记。
这些 `/v1/internal/memory/*` 接口用于受控集成，当前稳定性为 Experimental，不应直接暴露到不可信网络。

算法配置由 `configs/memory_tiering.yaml` 以及 `configs/algorithm_*.yaml` 读取。算法改变 score、重要性或
生命周期决策，但 canonical Memory/Edge/Event 仍是数据库记录。

## Tier 转换

- Hot：进程内有限缓存；
- Warm：主要 canonical store 和检索层；
- Cold：显式归档到 S3/MinIO 或内存 cold store。

对象不是写入后自动复制到所有层。归档、warm prebuild 和 purge 都是显式操作。

## 删除

逻辑删除、按 source/dataset 删除和物理 purge 语义不同。需要审计或 replay 时，应先明确保留 WAL、Edge、
Version 和冷存档的范围。
