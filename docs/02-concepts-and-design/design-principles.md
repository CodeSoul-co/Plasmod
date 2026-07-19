# 设计原则

## 1. Event first

状态改变优先表达为 Event，并通过 WAL 获得 LSN。新增核心写功能应接入 `Runtime.SubmitIngestContext`，除非它被明确标记为管理或兼容路径。

## 2. Canonical object as source of truth

Memory、State、Artifact、Edge 和 Version 的 canonical store 决定对象事实。索引可以丢弃重建，canonical data 不应依赖某个 ANN 文件才能解释。

## 3. Retrieval as projection

lexical、dense、sparse、hot/warm/cold index 是查询加速层。embedding family/dimension 必须与 segment metadata 一致，变更时进行受控 reindex。

## 4. Evidence as query-stage primitive

查询返回的不只是 object IDs；edge、version、provenance、proof trace 和 applied filters 是 response contract 的组成部分。

## 5. Explicit version and lifecycle

对象更新必须有稳定 ID、版本与生命周期；logical delete、archive 和 hard purge 语义分开。

## 6. Policy as infrastructure

PolicyRecord、ShareContract 和 AuditRecord 属于存储与查询基础设施，但不伪装为完整 IAM。

## 7. Pluggable algorithms

Memory algorithm 通过 dispatcher 和 AlgorithmStateStore 扩展，不改写 canonical storage contract。

## 8. Extensible schema and hooks

Dynamic Event v0.4 为 actor、access、materialization、retrieval 和 hooks 留出扩展点；扩展必须保持旧输入兼容和 replay 可解释性。
