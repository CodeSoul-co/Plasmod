# 问题定义

## 扁平 memory store 的不足

当 agent memory 只是一组文本、向量和 metadata 时，系统可以完成相似度检索，却难以稳定回答对象版本、因果链、状态覆盖、共享范围和恢复顺序。应用层往往被迫维护第二套 event log、state table 和 provenance graph，导致事实分散。

## 动态状态的问题

tool result、plan update 和 checkpoint 会持续修改 agent state。直接覆盖一个 metadata 字段会丢失 mutation event、版本、可见时刻和恢复依据。并发写入还需要明确哪个 LSN 已经物化、查询是否允许看到旧状态。

## Multi-agent scope 与 provenance

多个 agent 共享 workspace 时，同一个 memory 可能是 private、session、team 或 shared。仅用一个 `scope` 字符串无法表达 visible agents、roles、policy tags、share contract 和派生权限。查询结果还需要说明“为什么返回”和“由什么关系支持”。

## Plasmod 的工程回答

- Event：记录因果输入和接受顺序。
- WAL/LSN：提供 replay 与可见性推进基准。
- Canonical Object：保存当前可查询事实。
- ObjectVersion：保存对象历史边界。
- Edge/Derivation：保存关系和来源。
- Retrieval Projection：为 query 提供速度，但允许从 canonical data 重建。
- Evidence Response：将命中对象、过滤、版本、边和 proof trace 一起返回。

这些能力必须在 runtime、storage、query 与 recovery 中保持同一套不变量，而不是由单个 SDK 或上层 framework 临时拼接。
